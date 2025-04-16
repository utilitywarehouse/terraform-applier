package runner

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"time"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/vault"
	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	rePlanStatus  = regexp.MustCompile(`.*((Plan:|No changes.) .*)`)
	reApplyStatus = regexp.MustCompile(`.*(Apply complete! .* destroyed)`)

	defaultDirMode fs.FileMode = os.FileMode(0700) // 'rwx------'
)

//go:generate go run github.com/golang/mock/mockgen -package runner -destination runnner_mock.go github.com/utilitywarehouse/terraform-applier/runner RunnerInterface

type RunnerInterface interface {
	Start(run *tfaplv1beta1.Run, cancelChan chan struct{}) bool
}

type Runner struct {
	Clock                  sysutil.ClockInterface
	ClusterClt             client.Client
	Recorder               record.EventRecorder
	KubeClt                kubernetes.Interface
	Repos                  git.Repositories
	Redis                  sysutil.RedisInterface
	Log                    *slog.Logger
	Delegate               DelegateInterface
	Metrics                metrics.PrometheusInterface
	TerraformExecPath      string
	TerminationGracePeriod time.Duration
	Vault                  vault.ProviderInterface
	RunStatus              *sysutil.RunStatus
	GlobalENV              map[string]string
	pluginCacheEnabled     bool
	pluginCache            *pluginCache
	DataRootPath           string
}

func (r *Runner) Init(enablePluginCache bool, maxRunners int) error {
	var err error

	if enablePluginCache {
		r.pluginCacheEnabled = true
		r.pluginCache, err = newPluginCache(
			r.Log.With("logger", "pcp"),
			r.DataRootPath)
		if err != nil {
			r.Log.Error("unable to init plugin cache pool, plugin caching is disabled", "err", err)
			r.pluginCacheEnabled = false
		}
	}

	return nil
}

// Start will start given run and return true if run is successful
// This function is concurrency safe
func (r *Runner) Start(run *tfaplv1beta1.Run, cancelChan chan struct{}) bool {
	if err := run.Request.Validate(); err != nil {
		r.Log.Error("run triggered with invalid request", "req", run, "err", err)
		return false
	}
	envs := make(map[string]string)
	maps.Copy(envs, r.GlobalENV)

	r.Log.Info("starting run", "module", run.Module, "type", run.Request.Type)

	start := time.Now()

	if r.pluginCacheEnabled {
		// request plugin cache folder from the pool and clean up once done
		pluginCacheDir := r.pluginCache.new()
		if pluginCacheDir != "" {
			envs["TF_PLUGIN_CACHE_DIR"] = pluginCacheDir
			defer r.pluginCache.done(pluginCacheDir)
			r.Log.Info("plugin cache folder is ready", "module", run.Module)
		}
	}

	success := r.process(run, cancelChan, envs)

	dur := time.Since(start).Seconds()

	r.Metrics.UpdateModuleSuccess(run.Module.Name, run.Module.Namespace, run.Request.Type, success)
	r.Metrics.UpdateModuleRunDuration(run.Module.Name, run.Module.Namespace, run.Request.Type, dur, success)

	if success {
		r.Log.Info("run completed successfully", "module", run.Module, "duration", dur)
	} else {
		r.Log.Error("run completed with error", "module", run.Module, "duration", dur)
	}

	return success
}

// process will prepare and run module it returns bool indicating failed run
func (r *Runner) process(run *tfaplv1beta1.Run, cancelChan <-chan struct{}, envs map[string]string) bool {
	log := r.Log.With("module", run.Module)

	// make sure module is not already running
	_, ok := r.RunStatus.Load(run.Module.String())
	if ok {
		log.Error("skipping run request as another run is in progress on this module")
		return false
	}
	// set running status
	r.RunStatus.Store(run.Module.String(), true)
	defer r.RunStatus.Delete(run.Module.String())

	run.Status = tfaplv1beta1.StatusRunning

	// remove pending run request regardless of run outcome
	defer func() {
		// there are no annotations for schedule and polling runs
		if run.Request.Type == tfaplv1beta1.ScheduledRun ||
			run.Request.Type == tfaplv1beta1.PollingRun ||
			run.Request.Type == tfaplv1beta1.PRPlan {
			return
		}
		if err := sysutil.RemoveRequest(context.Background(), r.ClusterClt, run.Module, run.Request); err != nil {
			log.Error("unable to remove run request", "err", err)
		}
	}()

	defer func() {
		if err := r.updateRedis(context.Background(), run); err != nil {
			log.Error("unable to store run details", "err", err)
		}
	}()

	// create new context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// get Object
	module, err := sysutil.GetModule(ctx, r.ClusterClt, run.Module)
	if err != nil {
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

	commitHash, err := r.Repos.Hash(ctx, module.Spec.RepoURL, run.RepoRef, module.Spec.Path)
	if err != nil {
		log.Error("unable to get commit hash", "err", err)
		return false
	}

	commitLog, err := r.Repos.Subject(ctx, module.Spec.RepoURL, commitHash)
	if err != nil {
		log.Error("unable to get commit log subject", "err", err)
		return false
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		msg := "terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonControllerShutdown, msg)
		return false
	}

	// Update Status
	if err = r.SetRunStartedStatus(run, module, "preparing for TF run", commitHash, commitLog, module.Spec.RepoURL, r.Clock.Now()); err != nil {
		log.Error("unable to set run starting status", "err", err)
		return false
	}

	// Setup Delegation and get vars and envs
	jwt, err := r.Delegate.DelegateToken(ctx, r.KubeClt, module)
	if err != nil {
		msg := fmt.Sprintf("unable to get service account token: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonDelegationFailed, msg)
		return false
	}

	delegatedClient, err := r.Delegate.SetupDelegation(ctx, jwt)
	if err != nil {
		msg := fmt.Sprintf("unable to create kube client: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonDelegationFailed, msg)
		return false
	}

	backendConf, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Backend)
	if err != nil {
		msg := fmt.Sprintf("unable to get backend config: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonRunPreparationFailed, msg)
		return false
	}

	moduleEnvs, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Env)
	if err != nil {
		msg := fmt.Sprintf("unable to get envs: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonRunPreparationFailed, msg)
		return false
	}

	// copy module Env to given env so that user can override Global ENV if needed
	maps.Copy(envs, moduleEnvs)

	vars, err := fetchEnvVars(ctx, delegatedClient, module, module.Spec.Var)
	if err != nil {
		msg := fmt.Sprintf("unable to get vars: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonRunPreparationFailed, msg)
		return false
	}

	if module.Spec.VaultRequests != nil {
		if module.Spec.VaultRequests.AWS != nil {
			err = r.generateVaultAWSCreds(ctx, module, jwt, envs)
			if err != nil {
				msg := fmt.Sprintf("unable to generate vault aws secrets: err:%s", err)
				log.Error(msg)
				r.setFailedStatus(run, module, tfaplv1beta1.ReasonRunPreparationFailed, msg)
				return false
			}
		}

		if module.Spec.VaultRequests.GCP != nil {
			err = r.generateVaultGCPToken(ctx, module, jwt, envs)
			if err != nil {
				msg := fmt.Sprintf("unable to generate vault gcp access token: err:%s", err)
				log.Error(msg)
				r.setFailedStatus(run, module, tfaplv1beta1.ReasonRunPreparationFailed, msg)
				return false
			}
		}
	}

	// run should happen on the head of the reference instead of commit to capture
	// non-module path related changes
	te, err := r.NewTFRunner(ctx, module, run.RepoRef, envs, vars)
	if err != nil {
		msg := fmt.Sprintf("unable to create terraform executer: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonRunPreparationFailed, msg)
		return false
	}
	defer te.cleanUp()

	// Process RUN
	return r.runTF(ctx, run, module, te, backendConf, commitHash, cancelChan)
}

// runTF executes terraform commands and updates module status when required.
// it returns bool indicating success or failure
func (r *Runner) runTF(
	ctx context.Context,
	run *tfaplv1beta1.Run,
	module *tfaplv1beta1.Module,
	te TFExecuter,
	backendConf map[string]string,
	commitHash string,
	cancelChan <-chan struct{},
) bool {
	log := r.Log.With("module", run.Module, "ref", run.RepoRef)
	var err error

	run.InitOutput, err = te.init(ctx, backendConf)
	if err != nil {
		// tf err contains new lines not suitable logging
		log.Error("unable to init module", "err", fmt.Sprintf("%q", err))
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonInitialiseFailed, "unable to init module")
		return false
	}
	log.Info("Initialised successfully")
	r.Recorder.Event(module, corev1.EventTypeNormal, tfaplv1beta1.ReasonInitialised, "Initialised successfully")

	// run `terraform force-unlock <lock-id>`
	if run.Request.LockID != "" {
		_, err := te.forceUnlock(ctx, run.Request.LockID)
		if err != nil {
			log.Error("unable to force unlock state", "err", fmt.Sprintf("%q", err))
		} else {
			log.Info("force unlock successful")
		}
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		msg := "unable to plan module: terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonControllerShutdown, msg)
		return false
	}

	diffDetected, planOut, err := te.plan(ctx)
	if err != nil {
		run.Output = planOut
		// tf err contains new lines not suitable logging
		log.Error("unable to plan module", "err", fmt.Sprintf("%q", err))
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonPlanFailed, "unable to plan module")
		return false
	}

	run.DiffDetected = diffDetected

	// extract last line of output
	// Plan: X to add, 0 to change, 0 to destroy.
	// OR
	// No changes. Your infrastructure matches the configuration.
	planStatus := rePlanStatus.FindString(planOut)

	log.Info("planned", "status", planStatus)
	run.Summary = planStatus

	// get saved plan to update status
	run.Output, err = te.showPlanFileRaw(ctx)
	if err != nil {
		// tf err contains new lines not suitable logging
		log.Error("unable to get saved plan", "err", fmt.Sprintf("%q", err))
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonPlanFailed, "unable to get saved plan")
		return false
	}

	// return if plan only mode
	if run.PlanOnly {
		reason := tfaplv1beta1.ReasonNoDriftDetected
		if diffDetected {
			reason = tfaplv1beta1.ReasonPlanOnlyDriftDetected
		}
		if err = r.SetRunFinishedStatus(run, module, reason, planStatus, r.Clock.Now()); err != nil {
			log.Error("unable to set drift status", "err", err)
			return false
		}
		return true
	}

	// if termination signal received its safe to return here
	if isChannelClosed(cancelChan) {
		msg := "unable to apply module: terraform run interrupted as runner is shutting down"
		log.Error(msg)
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonControllerShutdown, msg)
		return false
	}

	applyOut, err := te.apply(ctx)
	run.Output += applyOut
	if err != nil {
		// tf err contains new lines not suitable logging
		log.Error("unable to apply module", "err", fmt.Sprintf("%q", err))
		r.setFailedStatus(run, module, tfaplv1beta1.ReasonApplyFailed, "unable to apply module")
		return false
	}
	module.Status.LastAppliedAt = &metav1.Time{Time: r.Clock.Now()}
	module.Status.LastAppliedCommitHash = commitHash

	// extract last line of output
	// Apply complete! Resources: 1 added, 0 changed, 0 destroyed.
	applyStatus := reApplyStatus.FindString(applyOut)

	log.Info("applied", "status", applyStatus)
	run.Summary = applyStatus

	if err = r.SetRunFinishedStatus(run, module, tfaplv1beta1.ReasonApplied, applyStatus, r.Clock.Now()); err != nil {
		log.Error("unable to set finished status", "err", err)
		return false
	}

	return true
}

func (r *Runner) SetRunStartedStatus(run *tfaplv1beta1.Run, m *tfaplv1beta1.Module, msg, commitHash, commitMsg, remoteURL string, now time.Time) error {
	run.StartedAt = &metav1.Time{Time: now}
	run.CommitHash = commitHash
	run.CommitMsg = commitMsg

	r.Recorder.Eventf(m, corev1.EventTypeNormal, tfaplv1beta1.ReasonRunTriggered, "%s: type:%s, commit:%s", msg, run.Request.Type, commitHash)

	if run.Request.SkipStatusUpdate() {
		return nil
	}

	m.Status.CurrentState = string(tfaplv1beta1.StatusRunning)
	m.Status.LastRunType = run.Request.Type
	m.Status.LastDefaultRunStartedAt = &metav1.Time{Time: now}
	m.Status.ObservedGeneration = m.Generation
	m.Status.LastDefaultRunCommitHash = commitHash
	m.Status.StateReason = tfaplv1beta1.ReasonRunTriggered

	return sysutil.PatchModuleStatus(context.Background(), r.ClusterClt, run.Module, m.Status)
}

func (r *Runner) SetRunFinishedStatus(run *tfaplv1beta1.Run, m *tfaplv1beta1.Module, reason, msg string, now time.Time) error {
	run.Status = tfaplv1beta1.StatusOk
	run.Duration = time.Since(run.StartedAt.Time)

	r.Recorder.Event(m, corev1.EventTypeNormal, reason, msg)

	if run.Request.SkipStatusUpdate() {
		return nil
	}

	m.Status.StateReason = reason
	m.Status.CurrentState = string(tfaplv1beta1.StatusOk)
	if reason == tfaplv1beta1.ReasonPlanOnlyDriftDetected {
		m.Status.CurrentState = string(tfaplv1beta1.StatusDriftDetected)
	}

	return sysutil.PatchModuleStatus(context.Background(), r.ClusterClt, m.NamespacedName(), m.Status)
}

func (r *Runner) setFailedStatus(run *tfaplv1beta1.Run, module *tfaplv1beta1.Module, reason, msg string) {
	run.Status = tfaplv1beta1.StatusErrored
	run.Duration = time.Since(run.StartedAt.Time)
	run.Summary = msg
	// msg in this case is an error message
	run.Output = msg + "\n" + run.Output

	r.Recorder.Event(module, corev1.EventTypeWarning, reason, msg)

	if run.Request.SkipStatusUpdate() {
		return
	}

	module.Status.CurrentState = string(tfaplv1beta1.StatusErrored)
	module.Status.StateReason = reason

	if err := sysutil.PatchModuleStatus(context.Background(), r.ClusterClt, run.Module, module.Status); err != nil {
		r.Log.With("module", run).Error("unable to set failed status", "err", err)
	}
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

// updateRedis will add given run to Redis
func (r *Runner) updateRedis(ctx context.Context, run *tfaplv1beta1.Run) error {
	// if its PR run only update relevant PR key
	if run.Request.Type == tfaplv1beta1.PRPlan {
		return r.Redis.SetPRRun(ctx, run)
	}

	// set default last run
	if err := r.Redis.SetDefaultLastRun(ctx, run); err != nil {
		return err
	}

	if run.DiffDetected && !run.PlanOnly {
		// set default last applied run
		if err := r.Redis.SetDefaultApply(ctx, run); err != nil {
			return err
		}
	}

	return nil
}
