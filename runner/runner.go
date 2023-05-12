package runner

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/vault"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	rePlanStatus  = regexp.MustCompile(`.*((Plan:|No changes.) .*)`)
	reApplyStatus = regexp.MustCompile(`.*(Apply complete! .* destroyed)`)
)

type Request struct {
	types.NamespacedName
	Type string
}

type Runner struct {
	Clock                  sysutil.ClockInterface
	ClusterClt             client.Client
	Recorder               record.EventRecorder
	KubeClt                kubernetes.Interface
	GitSyncPool            git.SyncInterface
	Queue                  <-chan Request
	Log                    hclog.Logger
	Delegate               DelegateInterface
	Metrics                metrics.PrometheusInterface
	TerraformExecPath      string
	TerminationGracePeriod time.Duration
	AWSSecretsEngineConfig vault.AWSSecretsEngineInterface
	GlobalENV              map[string]string
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
			go func(req Request) {
				defer wg.Done()
				defer r.Metrics.DecRunningModuleCount(req.Namespace)

				r.Metrics.IncRunningModuleCount(req.Namespace)
				start := time.Now()

				r.Log.Info("starting run", "module", req.NamespacedName, "type", req.Type)

				success := r.process(req, cancelChan)

				r.Log.Info("run finished", "module", req.NamespacedName, "success", success)

				r.Metrics.UpdateModuleSuccess(req.Name, req.Namespace, success)
				r.Metrics.UpdateModuleRunDuration(req.Name, req.Namespace, time.Since(start).Seconds(), success)
			}(req)
		}
	}
}

// process will prepare and run module it returns bool indicating failed run
func (r *Runner) process(req Request, cancelChan <-chan struct{}) bool {
	log := r.Log.With("module", req.NamespacedName)

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

	commitHash, err := r.GitSyncPool.HashForPath(ctx, module.Spec.RepoName, module.Spec.Path)
	if err != nil {
		log.Error("unable to get commit hash", "err", err)
		return false
	}

	commitLog, err := r.GitSyncPool.LogMsgForPath(ctx, module.Spec.RepoName, module.Spec.Path)
	if err != nil {
		log.Error("unable to get commit log subject", "err", err)
		return false
	}

	repo, err := r.GitSyncPool.RepositoryConfig(module.Spec.RepoName)
	if err != nil {
		log.Error("unable to get repo's remote url", "err", err)
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		msg := "terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonControllerShutdown, msg, r.Clock.Now())
		return false
	}

	// Update Status
	if err = r.SetRunStartedStatus(req, module, "preparing for TF run", commitHash, commitLog, repo.Remote, r.Clock.Now()); err != nil {
		log.Error("unable to set run starting status", "err", err)
		return false
	}

	// Setup Delegation and get vars and envs
	jwt, err := r.Delegate.DelegateToken(ctx, r.KubeClt, module)
	if err != nil {
		msg := fmt.Sprintf("unable to get service account token: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonDelegationFailed, msg, r.Clock.Now())
		return false
	}

	delegatedClient, err := r.Delegate.SetupDelegation(ctx, jwt)
	if err != nil {
		msg := fmt.Sprintf("unable to create kube client: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonDelegationFailed, msg, r.Clock.Now())
		return false
	}

	backendConf, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Backend)
	if err != nil {
		msg := fmt.Sprintf("unable to get backend config: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, msg, r.Clock.Now())
		return false
	}

	envs, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Env)
	if err != nil {
		msg := fmt.Sprintf("unable to get envs: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, msg, r.Clock.Now())
		return false
	}

	vars, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Var)
	if err != nil {
		msg := fmt.Sprintf("unable to get vars: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, msg, r.Clock.Now())
		return false
	}

	if module.Spec.VaultRequests != nil {
		if module.Spec.VaultRequests.AWS != nil {
			err = r.generateVaultAWSCreds(ctx, module, jwt, envs)
			if err != nil {
				msg := fmt.Sprintf("unable to generate vault aws secrets: err:%s", err)
				log.Error(msg)
				r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, msg, r.Clock.Now())
				return false
			}
		}
	}

	te, err := r.NewTFRunner(ctx, module, envs, vars)
	if err != nil {
		msg := fmt.Sprintf("unable to create terraform executer: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonRunPreparationFailed, msg, r.Clock.Now())
		return false
	}
	defer te.cleanUp()

	// Process RUN
	return r.runTF(ctx, req, module, te, backendConf, commitHash, cancelChan)
}

// runTF executes terraform commands and updates module status when required.
// it returns bool indicating success or failure
func (r *Runner) runTF(
	ctx context.Context,
	req Request,
	module *tfaplv1beta1.Module,
	te TFExecuter,
	backendConf map[string]string,
	commitHash string,
	cancelChan <-chan struct{},
) bool {
	log := r.Log.With("module", req.NamespacedName)

	if err := r.SetProgressingStatus(req.NamespacedName, module, "Initialising"); err != nil {
		log.Error("unable to set init status", "err", err)
		return false
	}

	initOut, err := te.init(ctx, backendConf)
	if err != nil {
		msg := fmt.Sprintf("unable to init module: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonInitialiseFailed, msg, r.Clock.Now())
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
		msg := "unable to plan module: terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonControllerShutdown, msg, r.Clock.Now())
		return false
	}

	diffDetected, planOut, err := te.plan(ctx)
	module.Status.RunOutput = planOut
	if err != nil {
		msg := fmt.Sprintf("unable to plan module: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonPlanFailed, msg, r.Clock.Now())
		return false
	}

	// extract last line of output
	// Plan: X to add, 0 to change, 0 to destroy.
	// OR
	// No changes. Your infrastructure matches the configuration.
	planStatus := rePlanStatus.FindString(planOut)

	log.Info("planed", "status", planStatus)

	// return if no drift detected
	if !diffDetected {
		if err = r.SetRunFinishedStatus(req.NamespacedName, module, tfaplv1beta1.ReasonPlanedNoDriftDetected, planStatus, r.Clock.Now()); err != nil {
			log.Error("unable to set no drift status", "err", err)
			return false
		}
		return true
	}

	// return if plan only mode
	if module.Spec.PlanOnly != nil && *module.Spec.PlanOnly {
		if err = r.SetRunFinishedStatus(req.NamespacedName, module, tfaplv1beta1.ReasonPlanedDriftDetected, "PlanOnly/"+planStatus, r.Clock.Now()); err != nil {
			log.Error("unable to set drift status", "err", err)
			return false
		}
		return true
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		msg := "unable to apply module: terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonControllerShutdown, msg, r.Clock.Now())
		return false
	}

	// Start Applying
	if err = r.SetProgressingStatus(req.NamespacedName, module, "Applying/"+planStatus); err != nil {
		log.Error("unable to set applying status", "err", err)
		return false
	}

	applyOut, err := te.apply(ctx)
	module.Status.RunOutput = planOut + applyOut
	if err != nil {
		msg := fmt.Sprintf("unable to apply module: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonApplyFailed, msg, r.Clock.Now())
		return false
	}

	module.Status.LastApplyInfo = tfaplv1beta1.OutputStats{Timestamp: &metav1.Time{Time: r.Clock.Now()}, CommitHash: commitHash, Output: planOut + applyOut}

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

func (r *Runner) SetRunStartedStatus(req Request, m *tfaplv1beta1.Module, msg, commitHash, commitMsg, remoteURL string, now time.Time) error {

	m.Status.CurrentState = string(tfaplv1beta1.StatusRunning)
	m.Status.Type = req.Type
	m.Status.RunStartedAt = &metav1.Time{Time: now}
	m.Status.RunDuration = nil
	m.Status.ObservedGeneration = m.Generation
	m.Status.RunCommitHash = commitHash
	m.Status.RunCommitMsg = commitMsg
	m.Status.RemoteURL = remoteURL
	m.Status.StateMessage = msg

	r.Recorder.Eventf(m, corev1.EventTypeNormal, tfaplv1beta1.ReasonRunTriggered, "%s: type:%s, commit:%s", msg, req.Type, commitHash)

	return r.patchStatus(context.Background(), req.NamespacedName, m.Status)
}

func (r *Runner) SetRunFinishedStatus(objectKey types.NamespacedName, m *tfaplv1beta1.Module, reason, msg string, now time.Time) error {
	m.Status.CurrentState = string(tfaplv1beta1.StatusReady)
	m.Status.RunDuration = &metav1.Duration{Duration: now.Sub(m.Status.RunStartedAt.Time).Round(time.Second)}
	m.Status.StateMessage = msg

	r.Recorder.Event(m, corev1.EventTypeNormal, reason, msg)

	return r.patchStatus(context.Background(), objectKey, m.Status)
}

func (r *Runner) setFailedStatus(req Request, module *tfaplv1beta1.Module, reason, msg string, now time.Time) {

	module.Status.CurrentState = string(tfaplv1beta1.StatusErrored)
	module.Status.RunDuration = &metav1.Duration{Duration: now.Sub(module.Status.RunStartedAt.Time).Round(time.Second)}
	module.Status.StateMessage = msg

	r.Recorder.Event(module, corev1.EventTypeWarning, reason, msg)

	if err := r.patchStatus(context.Background(), req.NamespacedName, module.Status); err != nil {
		r.Log.With("module", req.NamespacedName).Error("unable to set failed status", "err", err)
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
