/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/robfig/cron/v3"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	corev1 "k8s.io/api/core/v1"
)

const trace = slog.Level(-8)

// ModuleReconciler reconciles a Module object
type ModuleReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	Recorder               record.EventRecorder
	Clock                  sysutil.ClockInterface
	Repos                  git.Repositories
	Queue                  chan<- *tfaplv1beta1.Request
	Log                    *slog.Logger
	MinIntervalBetweenRuns time.Duration
	RunStatus              *sync.Map
}

//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=secrets,resourceNames=terraform-applier-delegate-token,verbs=get
//+kubebuilder:rbac:groups="authorization.k8s.io",resources=subjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=terraform-applier.uw.systems,resources=modules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=terraform-applier.uw.systems,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=terraform-applier.uw.systems,resources=modules/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Module object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *ModuleReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	log := r.Log.With("module", req.NamespacedName)

	log.Log(ctx, trace, "reconciling...")

	module, err := sysutil.GetModule(ctx, r.Client, req.NamespacedName)
	if err != nil {
		log.Error("unable to fetch terraform module", "err", err)
		// we'll ignore not-found errors, since they can't be fixed by an immediate requeue
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Do not requeue if module is being deleted
	if !module.ObjectMeta.DeletionTimestamp.IsZero() {
		// TODO: what if module is in running state?
		log.Info("module is deleting..")
		return ctrl.Result{}, nil
	}

	// verify repoURL exists
	// this step is required as for migration we have kept repoURL as optional
	if module.Spec.RepoURL == "" {
		msg := fmt.Sprintf("repoURL is required, please add repoURL instead of repoName:%s", module.Spec.RepoName)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonSpecsParsingFailure, msg)
		// we don't really care about requeuing until we get an update that
		// fixes the repoURL, so don't return an error
		return ctrl.Result{}, nil
	}

	// pollIntervalDuration is used as minimum duration for re-queue
	pollIntervalDuration := time.Duration(module.Spec.PollInterval) * time.Second

	// check if module is actually running on this controller...
	if _, ok := r.RunStatus.Load(req.NamespacedName.String()); ok {
		// it is running so use next poll internal as minimum queue duration
		return ctrl.Result{RequeueAfter: pollIntervalDuration}, nil
	}

	// at this stage module is not deleting and its not currently running
	//

	// check module's run status, it is possible that process got killed before
	// runner could update module status or module status update API could have
	// failed
	if module.Status.CurrentState == string(tfaplv1beta1.StatusRunning) {
		// module is not actually running so change status and continue
		msg := "wrong status found, module is not actually running"
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonUnknown, msg)
	}

	// case 1:
	// check for initial run
	//
	if module.Status.RunCommitHash == "" {
		log.Debug("starting initial run")
		r.Queue <- module.NewRunRequest(tfaplv1beta1.PollingRun)
		// use next poll internal as minimum queue duration as status change will not trigger Reconcile
		return ctrl.Result{RequeueAfter: pollIntervalDuration}, nil
	}

	// case 2:
	// check for new git hash changes on modules path
	//
	hash, err := r.Repos.Hash(ctx, module.Spec.RepoURL, module.Spec.RepoRef, module.Spec.Path)
	if err != nil {
		msg := fmt.Sprintf("unable to get current hash of the repo err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonGitFailure, msg)
		// since issue is not related to module specs, requeue again in case its fixed
		return ctrl.Result{RequeueAfter: pollIntervalDuration}, nil
	}

	if hash != module.Status.RunCommitHash {
		log.Debug("revision is changed on module path triggering run", "lastRun", module.Status.RunCommitHash, "current", hash)
		r.Queue <- module.NewRunRequest(tfaplv1beta1.PollingRun)
		// use next poll internal as minimum queue duration as status change will not trigger Reconcile
		return ctrl.Result{RequeueAfter: pollIntervalDuration}, nil
	}

	// case 3:
	// check if schedule run required
	//

	// If No schedule is provided, just requeue for next git check
	if module.Spec.Schedule == "" {
		return ctrl.Result{RequeueAfter: pollIntervalDuration}, nil
	}

	// figure out the next times that we need to run or last missed runs time if any.
	numOfMissedRuns, nextRun, err := NextSchedule(module, r.Clock.Now(), r.MinIntervalBetweenRuns)
	if err != nil {
		msg := fmt.Sprintf("unable to figure out CronJob schedule: err:%s", err)
		log.Error(msg)
		r.setFailedStatus(req, module, tfaplv1beta1.ReasonSpecsParsingFailure, msg)
		// we don't really care about requeuing until we get an update that
		// fixes the schedule, so don't return an error
		return ctrl.Result{}, nil
	}

	if numOfMissedRuns > 0 {
		log.Debug("starting scheduled run", "missed-runs", numOfMissedRuns)
		r.Queue <- module.NewRunRequest(tfaplv1beta1.ScheduledRun)
		// use next poll internal as minimum queue duration as status change will not trigger Reconcile
		return ctrl.Result{RequeueAfter: pollIntervalDuration}, nil
	}

	// default:
	// No action required so requeue module
	//

	// Calculate shortest duration to next run
	requeueAfter := nextRun.Sub(r.Clock.Now())
	if pollIntervalDuration < requeueAfter {
		requeueAfter = pollIntervalDuration
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleReconciler) SetupWithManager(mgr ctrl.Manager, filter *Filter) error {
	// set up a real clock
	if r.Clock == nil {
		r.Clock = &sysutil.Clock{}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&tfaplv1beta1.Module{}).
		WithEventFilter(filter).
		Complete(r)
}

// https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html
// NextSchedule returns the number of missed runs if any, time of the next schedule and error
func NextSchedule(module *tfaplv1beta1.Module, now time.Time, minIntervalBetweenRuns time.Duration) (int, time.Time, error) {
	sched, err := cron.ParseStandard(module.Spec.Schedule)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("unparseable schedule %q: %v", module.Spec.Schedule, err)
	}

	earliestTime := module.ObjectMeta.CreationTimestamp.Time
	if module.Status.RunStartedAt != nil {
		earliestTime = module.Status.RunStartedAt.Time
	}

	if earliestTime.After(now) {
		return 0, sched.Next(now), nil
	}

	t1 := sched.Next(earliestTime)
	t2 := sched.Next(t1)

	// https://github.com/kubernetes/kubernetes/blob/master/pkg/controller/cronjob/utils.go#L102-L111
	// It is possible for cron.ParseStandard("59 23 31 2 *") to return an invalid schedule
	// minute - 59, hour - 23, dom - 31, month - 2, and dow is optional, clearly 31 is invalid
	// In this case the timeBetweenTwoSchedules will be 0, and we error out the invalid schedule
	timeBetweenTwoSchedules := int64(t2.Sub(t1).Round(time.Second).Seconds())
	if timeBetweenTwoSchedules < 1 {
		return 0, time.Time{}, fmt.Errorf("time interval between two schedules is less than 1 second, please check schedule for logical error")
	}

	// timeBetweenTwoSchedules should be more then minimum allowed interval set on controllers
	allowedInterval := int64(minIntervalBetweenRuns.Round(time.Second).Seconds())
	if timeBetweenTwoSchedules < allowedInterval {
		return 0, time.Time{}, fmt.Errorf(
			"time interval between two schedules is less than minimum allowed interval set on controller: allowed:%d actual:%d",
			allowedInterval, timeBetweenTwoSchedules,
		)
	}

	numOfMissedRuns := 0
	for t := sched.Next(earliestTime); !t.After(now); t = sched.Next(t) {
		// An object might miss several starts. For example, if
		// controller gets wedged on Friday at 5:01pm when everyone has
		// gone home, and someone comes in on Tuesday AM and discovers
		// the problem and restarts the controller, then all the hourly
		// jobs, more than 80 of them for one hourly scheduledJob, should
		// all start running with no further intervention (if the scheduledJob
		// allows concurrency and late starts).
		//
		// However, if there is a bug somewhere, or incorrect clock
		// on controller's server or apiservers (for setting creationTimestamp)
		// then there could be so many missed start times (it could be off
		// by decades or more), that it would eat up all the CPU and memory
		// of this controller. In that case, we want to not try to list
		// all the missed start times.
		numOfMissedRuns++
	}

	return numOfMissedRuns, sched.Next(now), nil
}

func (r *ModuleReconciler) setFailedStatus(req ctrl.Request, module *tfaplv1beta1.Module, reason, msg string) {

	module.Status.CurrentState = string(tfaplv1beta1.StatusErrored)
	module.Status.StateMessage = tfaplv1beta1.NormaliseStateMsg(msg)
	module.Status.StateReason = reason
	module.Status.RunStartedAt = nil
	module.Status.RunDuration = nil
	module.Status.ObservedGeneration = module.Generation

	r.Recorder.Event(module, corev1.EventTypeWarning, reason, msg)

	if err := sysutil.PatchModuleStatus(context.Background(), r.Client, req.NamespacedName, module.Status); err != nil {
		r.Log.With("module", req).Error("unable to set failed status", "err", err)
	}
}
