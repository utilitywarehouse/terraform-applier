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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
				r.process(req, cancelChan)
			}(req)
		}
	}
}

func (r *Runner) process(req ctrl.Request, cancelChan <-chan struct{}) {
	log := r.Log.With("module", req)

	log.Debug("starting run....")

	// create new context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// get Object
	module := new(tfaplv1beta1.Module)
	if err := r.ClusterClt.Get(ctx, req.NamespacedName, module); err != nil {
		log.Error("unable to fetch terraform module", "err", err)
		return
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

	commitHash, commitLog, err := r.GitUtil.GetHeadCommitHashAndLogForPath(module.Spec.Path)
	if err != nil {
		log.Error("unable to get commit hash and log", "err", err)
		return
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		msg := "terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.updateFailedStatus(req, module, msg)
		return
	}

	// Update Status
	tfaplv1beta1.SetModuleStatusRunStarted(module, "preparing for TF run", commitHash, commitLog, r.Clock.Now())
	if err = r.patchStatus(ctx, req.NamespacedName, module.Status); err != nil {
		log.Error("unable to set run starting status", "err", err)
		return
	}

	// Setup Delegation and get vars and envs
	delegatedClient, err := r.Delegate.SetupDelegation(ctx, r.KubeClt, module)
	if err != nil {
		log.Error("unable to create kube client", "err", err)
		r.updateFailedStatus(req, module, err.Error())
		return
	}

	envs, err := getEnvVars(ctx, delegatedClient, module, module.Spec.Env)
	if err != nil {
		log.Error("unable to get envs", "err", err)
		r.updateFailedStatus(req, module, err.Error())
		return
	}

	vars, err := getEnvVars(ctx, delegatedClient, module, module.Spec.Var)
	if err != nil {
		log.Error("unable to get vars", "err", err)
		r.updateFailedStatus(req, module, err.Error())
		return
	}

	te, err := r.NewTFRunner(ctx, module, envs, vars)
	if err != nil {
		log.Error("unable to create terraform executer", "err", err)
		r.updateFailedStatus(req, module, err.Error())
		return
	}
	defer te.cleanUp()

	// Process RUN
	r.runTF(ctx, req, module, te, commitHash, cancelChan)

	log.Debug("terraform run completed")
}

func (r *Runner) runTF(
	ctx context.Context,
	req ctrl.Request,
	module *tfaplv1beta1.Module,
	te TFExecuter,
	commitHash string,
	cancelChan <-chan struct{},
) {
	log := r.Log.With("module", req)

	tfaplv1beta1.SetModuleStatusProgressing(module, "Initialising")
	if err := r.patchStatus(ctx, req.NamespacedName, module.Status); err != nil {
		log.Error("unable to set init status", "err", err)
		return
	}

	initOut, err := te.init(ctx)
	if err != nil {
		log.Error("unable to init module", "err", err)
		r.updateFailedStatus(req, module, err.Error())
		return
	}

	log.Info("initialised", "output", initOut)

	// Start Planing
	tfaplv1beta1.SetModuleStatusProgressing(module, "Planning")
	if err = r.patchStatus(ctx, req.NamespacedName, module.Status); err != nil {
		log.Error("unable to set planning status", "err", err)
		return
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		log.Error("terraform run interrupted as runner is shutting down")
		r.updateFailedStatus(req, module, "terraform run interrupted as runner is shutting down")
		return
	}

	diffDetected, planOut, err := te.plan(ctx)
	if err != nil {
		log.Error("unable to plan module", "err", err)
		module.Status.LastDriftInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: planOut + "\n" + err.Error()}
		r.updateFailedStatus(req, module, err.Error())
		return
	}

	// extract last line of output
	// Plan: X to add, 0 to change, 0 to destroy.
	planStatus := rePlanStatus.FindString(planOut)
	log.Info("planed", "status", planStatus)

	if !diffDetected {
		tfaplv1beta1.SetModuleStatusRunFinished(module, planStatus, r.Clock.Now())
		if err = r.patchStatus(ctx, req.NamespacedName, module.Status); err != nil {
			log.Error("unable to set no drift status", "err", err)
			return
		}
		return
	}

	module.Status.LastDriftInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: planOut}

	if module.Spec.PlanOnly != nil && *module.Spec.PlanOnly {
		tfaplv1beta1.SetModuleStatusRunFinished(module, "PlanOnly/"+planStatus, r.Clock.Now())
		if err = r.patchStatus(ctx, req.NamespacedName, module.Status); err != nil {
			log.Error("unable to set drift status", "err", err)
			return
		}
		return
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		log.Error("terraform run interrupted as runner is shutting down")
		r.updateFailedStatus(req, module, "terraform run interrupted as runner is shutting down")
		return
	}

	// Start Applying
	tfaplv1beta1.SetModuleStatusProgressing(module, "Applying/"+planStatus)
	if err = r.patchStatus(ctx, req.NamespacedName, module.Status); err != nil {
		log.Error("unable to set applying status", "err", err)
		return
	}

	applyOut, err := te.apply(ctx)
	if err != nil {
		log.Error("unable to plan module", "err", err)
		module.Status.LastApplyInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: applyOut + "\n" + err.Error()}
		r.updateFailedStatus(req, module, err.Error())
		return
	}

	module.Status.LastApplyInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: applyOut}

	// extract last line of output
	// Apply complete! Resources: 1 added, 0 changed, 0 destroyed.
	applyStatus := reApplyStatus.FindString(applyOut)
	log.Info("applied", "status", applyStatus)

	tfaplv1beta1.SetModuleStatusRunFinished(module, applyStatus, r.Clock.Now())
	if err = r.patchStatus(ctx, req.NamespacedName, module.Status); err != nil {
		log.Error("unable to set finished status", "err", err)
		return
	}
}

func (r *Runner) updateFailedStatus(req ctrl.Request, module *tfaplv1beta1.Module, msg string) {
	tfaplv1beta1.SetModuleStatusFailed(module, msg, r.Clock.Now())
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
