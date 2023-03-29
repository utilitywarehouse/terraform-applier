package runner

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	rePlanStatus  = regexp.MustCompile(`.*(Plan: .* destroy)`)
	reApplyStatus = regexp.MustCompile(`.*(Apply complete! .* destroyed)`)
)

type Runner struct {
	Clock                  sysutil.ClockInterface
	ClusterClt             client.Client
	Recorder               record.EventRecorder
	KubeClt                kubernetes.Interface
	GitUtil                git.UtilInterface
	RepoPath               string
	Queue                  <-chan ctrl.Request
	Log                    hclog.Logger
	Delegate               DelegateInterface
	Metrics                metrics.PrometheusInterface
	TerraformExecPath      string
	TerminationGracePeriod time.Duration
}

// Start runs a continuous loop that starts a new run when a request comes into the queue channel.
func (r *Runner) Start(ctx context.Context, done chan bool) {
	wg := &sync.WaitGroup{}

	if r.Delegate == nil {
		r.Delegate = &Delegate{}
	}

	cancelChan := make(chan struct{})

	for {
		select {
		case <-ctx.Done():
			// notify workers
			close(cancelChan)
			// wait for all run to finish
			wg.Wait()
			done <- true
			return

		case req := <-r.Queue:
			wg.Add(1)
			go func(req ctrl.Request) {
				defer wg.Done()
				defer r.Metrics.DecRunningModuleCount(req.Namespace)

				r.Metrics.IncRunningModuleCount(req.Namespace)
				start := time.Now()

				success := r.process(req, cancelChan)

				r.Metrics.UpdateModuleSuccess(req.Name, req.Namespace, success)
				r.Metrics.UpdateModuleRunDuration(req.Name, req.Namespace, time.Since(start).Seconds(), success)
			}(req)
		}
	}
}

// process will prepare and run module it returns bool indicating failed run
func (r *Runner) process(req ctrl.Request, cancelChan <-chan struct{}) bool {
	log := r.Log.With("module", req)

	log.Debug("starting run....")

	// create new context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// get Object
	module := new(tfaplv1beta1.Module)
	if err := r.ClusterClt.Get(ctx, req.NamespacedName, module); err != nil {
		log.Error("unable to fetch terraform module", "err", err)
		return false
	}

	// setup go routine for graceful shutdown of current run
	go func() {
		moduleRunTimeout := time.NewTicker(time.Duration(module.Spec.RunTimeout) * time.Second)
		gracePeriod := r.TerminationGracePeriod
		for {
			select {
			case <-moduleRunTimeout.C:
				log.Error("module run timed out stopping run", "RunTimeout", module.Spec.RunTimeout)
				cancel()
				return
			case _, ok := <-cancelChan:
				if ok {
					continue
				}
				// if channel is Closed start timeout and then cancel run Context
				log.Info("shutdown signal received waiting for termination grace period", "GracePeriod", gracePeriod.Seconds())
				time.Sleep(gracePeriod)
				log.Info("module termination grace period timed out stopping run")
				cancel()
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	commitHash, commitLog, err := r.GitUtil.HeadCommitHashAndLog(module.Spec.Path)
	if err != nil {
		log.Error("unable to get commit hash and log", "err", err)
		return false
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		msg := "terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonControllerShutdown, msg, r.Clock.Now())
		return false
	}

	// Update Status
	if err = r.SetRunStartedStatus(req.NamespacedName, module, "preparing for TF run", commitHash, commitLog, r.Clock.Now()); err != nil {
		log.Error("unable to set run starting status", "err", err)
		return false
	}

	// Setup Delegation and get vars and envs
	delegatedClient, err := r.Delegate.SetupDelegation(ctx, r.KubeClt, module)
	if err != nil {
		log.Error("unable to create kube client", "err", err)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonDelegationFailed, err.Error(), r.Clock.Now())
		return false
	}

	envs, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Env)
	if err != nil {
		log.Error("unable to get envs", "err", err)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, err.Error(), r.Clock.Now())
		return false
	}

	vars, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Var)
	if err != nil {
		log.Error("unable to get vars", "err", err)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, err.Error(), r.Clock.Now())
		return false
	}

	te, err := r.NewTFRunner(ctx, module, envs, vars)
	if err != nil {
		log.Error("unable to create terraform executer", "err", err)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, err.Error(), r.Clock.Now())
		return false
	}
	defer te.cleanUp()

	// Process RUN
	return r.runTF(ctx, req, module, te, commitHash, cancelChan)
}

// runTF executes terraform commands and updates module status when required.
// it returns bool indicating success or failure
func (r *Runner) runTF(
	ctx context.Context,
	req ctrl.Request,
	module *tfaplv1beta1.Module,
	te TFExecuter,
	commitHash string,
	cancelChan <-chan struct{},
) bool {
	log := r.Log.With("module", req)

	if err := r.SetProgressingStatus(req.NamespacedName, module, "Initialising"); err != nil {
		log.Error("unable to set init status", "err", err)
		return false
	}

	initOut, err := te.init(ctx)
	if err != nil {
		log.Error("unable to init module", "err", err)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonInitialiseFailed, err.Error(), r.Clock.Now())
		return false
	}

	log.Info("initialised", "output", initOut)
	r.Recorder.Event(module, corev1.EventTypeNormal, tfaplv1beta1.ReasonInitialised, "Initialised successfully")

	// Start Planing
	if err = r.SetProgressingStatus(req.NamespacedName, module, "Planning"); err != nil {
		log.Error("unable to set planning status", "err", err)
		return false
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		log.Error("terraform run interrupted as runner is shutting down")
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonControllerShutdown, "terraform run interrupted as runner is shutting down", r.Clock.Now())
		return false
	}

	diffDetected, planOut, err := te.plan(ctx)
	if err != nil {
		log.Error("unable to plan module", "err", err)
		module.Status.LastDriftInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: planOut + "\n" + err.Error()}
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonPlanFailed, err.Error(), r.Clock.Now())
		return false
	}

	// extract last line of output
	// Plan: X to add, 0 to change, 0 to destroy.
	planStatus := rePlanStatus.FindString(planOut)

	log.Info("planed", "status", planStatus)

	if !diffDetected {
		if err = r.SetRunFinishedStatus(req.NamespacedName, module, tfaplv1beta1.ReasonPlanedNoDriftDetected, planStatus, r.Clock.Now()); err != nil {
			log.Error("unable to set no drift status", "err", err)
			return false
		}
		return true
	}

	module.Status.LastDriftInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: planOut}

	if module.Spec.PlanOnly != nil && *module.Spec.PlanOnly {
		if err = r.SetRunFinishedStatus(req.NamespacedName, module, tfaplv1beta1.ReasonPlanedDriftDetected, "PlanOnly/"+planStatus, r.Clock.Now()); err != nil {
			log.Error("unable to set drift status", "err", err)
			return false
		}
		return true
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		log.Error("terraform run interrupted as runner is shutting down")
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonControllerShutdown, "terraform run interrupted as runner is shutting down", r.Clock.Now())
		return false
	}

	// Start Applying
	if err = r.SetProgressingStatus(req.NamespacedName, module, "Applying/"+planStatus); err != nil {
		log.Error("unable to set applying status", "err", err)
		return false
	}

	applyOut, err := te.apply(ctx)
	if err != nil {
		log.Error("unable to plan module", "err", err)
		module.Status.LastApplyInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: applyOut + "\n" + err.Error()}
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonApplyFailed, err.Error(), r.Clock.Now())
		return false
	}

	module.Status.LastApplyInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: applyOut}

	// extract last line of output
	// Apply complete! Resources: 1 added, 0 changed, 0 destroyed.
	applyStatus := reApplyStatus.FindString(applyOut)

	log.Info("applied", "status", applyStatus)

	if err = r.SetRunFinishedStatus(req.NamespacedName, module, tfaplv1beta1.ReasonApplied, applyStatus, r.Clock.Now()); err != nil {
		log.Error("unable to set finished status", "err", err)
		return false
	}

	return true
}

func (r *Runner) SetProgressingStatus(objectKey types.NamespacedName, m *tfaplv1beta1.Module, msg string) error {
	m.Status.CurrentState = string(tfaplv1beta1.StatusRunning)
	m.Status.StateMessage = msg
	return r.patchStatus(context.Background(), objectKey, m.Status)
}

func (r *Runner) SetRunStartedStatus(objectKey types.NamespacedName, m *tfaplv1beta1.Module, msg, commitHash, commitMsg string, now time.Time) error {

	m.Status.CurrentState = string(tfaplv1beta1.StatusRunning)
	m.Status.RunStartedAt = &metav1.Time{Time: now}
	m.Status.RunFinishedAt = nil
	m.Status.ObservedGeneration = m.Generation
	m.Status.RunCommitHash = commitHash
	m.Status.RunCommitMsg = commitMsg
	m.Status.StateMessage = msg

	return r.patchStatus(context.Background(), objectKey, m.Status)
}

func (r *Runner) SetRunFinishedStatus(objectKey types.NamespacedName, m *tfaplv1beta1.Module, reason, msg string, now time.Time) error {
	m.Status.CurrentState = string(tfaplv1beta1.StatusReady)
	m.Status.RunFinishedAt = &metav1.Time{Time: now}
	m.Status.StateMessage = msg

	r.Recorder.Event(m, corev1.EventTypeNormal, reason, msg)

	return r.patchStatus(context.Background(), objectKey, m.Status)
}

func (r *Runner) setFailedStatus(req ctrl.Request, module *tfaplv1beta1.Module, reason, msg string, now time.Time) {

	module.Status.CurrentState = string(tfaplv1beta1.StatusErrored)
	module.Status.RunFinishedAt = &metav1.Time{Time: now}
	module.Status.StateMessage = msg

	r.Recorder.Event(module, corev1.EventTypeWarning, reason, msg)

	if err := r.patchStatus(context.Background(), req.NamespacedName, module.Status); err != nil {
		r.Log.With("module", req).Error("unable to set failed status", "err", err)
	}
}

func (r *Runner) patchStatus(ctx context.Context, objectKey types.NamespacedName, newStatus tfaplv1beta1.ModuleStatus) error {
	module := new(tfaplv1beta1.Module)
	if err := r.ClusterClt.Get(ctx, objectKey, module); err != nil {
		return err
	}

	patch := client.MergeFrom(module.DeepCopy())
	module.Status = newStatus

	return r.ClusterClt.Status().Patch(ctx, module, patch, client.FieldOwner("terraform-applier"))
}

func isChannelClosed(cancelChan <-chan struct{}) bool {
	select {
	case _, ok := <-cancelChan:
		if !ok {
			return true
		}
	default:
		return false
	}
	return false
}
