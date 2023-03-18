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
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hashicorp/go-hclog"
	"github.com/robfig/cron/v3"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

// ModuleReconciler reconciles a Module object
type ModuleReconciler struct {
	client.Client
	Scheme                 *runtime.Scheme
	Clock                  sysutil.ClockInterface
	GitUtil                git.UtilInterface
	Queue                  chan<- ctrl.Request
	Log                    hclog.Logger
	MinIntervalBetweenRuns time.Duration
}

//+kubebuilder:rbac:groups=terraform-applier.uw.systems,resources=modules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=terraform-applier.uw.systems,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=terraform-applier.uw.systems,resources=modules/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,resourceNames=terraform-applier-delegate-token,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Module object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *ModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.With("module", req.NamespacedName)

	var module tfaplv1beta1.Module
	if err := r.Get(ctx, req.NamespacedName, &module); err != nil {
		log.Error("unable to fetch terraform module", "err", err)
		// we'll ignore not-found errors, since they can't be fixed by an immediate requeue
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Do not requeue if suspended
	if module.Spec.Suspend != nil && *module.Spec.Suspend {
		log.Debug("module suspended, skipping")
		return ctrl.Result{}, nil
	}

	// Do not requeue if module is being deleted
	if !module.ObjectMeta.DeletionTimestamp.IsZero() {
		// TODO: what if module is in running state?
		log.Info("module is deleting..")
		return ctrl.Result{}, nil
	}

	// Do not requeue if module is running as it will be added back once
	// status is updated
	if module.Status.CurrentState == string(tfaplv1beta1.StatusRunning) {
		return ctrl.Result{}, nil
	}

	// check for new commit on modules path
	commitHash, _, err := r.GitUtil.GetHeadCommitHashAndLogForPath(module.Spec.Path)
	if err != nil {
		log.Error("unable to get commit hash", "err", err)
		// TODO: should we requeue here?
		return ctrl.Result{}, nil
	}

	if module.Status.RunCommitHash != commitHash {
		log.Info("new commit is available starting run", "RunCommitHash", module.Status.RunCommitHash, "currentHash", commitHash)
		r.Queue <- req
		// no need to add to queue as we will see this object again once status is updated
		return ctrl.Result{}, nil
	}

	pollIntervalDuration := time.Duration(module.Spec.PollInterval) * time.Second

	// If No schedule is provided, just requeue for next git check
	if module.Spec.Schedule == "" {
		return ctrl.Result{RequeueAfter: pollIntervalDuration}, nil
	}

	// figure out the next times that we need to run or last missed runs time if any.
	numOfMissedRuns, nextRun, err := getNextSchedule(&module, r.Clock.Now(), r.MinIntervalBetweenRuns)
	if err != nil {
		log.Error("unable to figure out CronJob schedule", "err", err)
		// we don't really care about requeuing until we get an update that
		// fixes the schedule, so don't return an error
		return ctrl.Result{}, nil
	}

	if numOfMissedRuns > 0 {
		log.Info("starting scheduled run", "missed-runs", numOfMissedRuns)
		r.Queue <- req

		// no need to add to queue as we will see this object again once status is updated
		return ctrl.Result{}, nil
	}

	// Calculate shortest duration to next run
	requeueAfter := nextRun.Sub(r.Clock.Now())
	if pollIntervalDuration < requeueAfter {
		requeueAfter = pollIntervalDuration
	}

	// Requeue if there are no missed runs or new commits
	// ie still waiting on next schedule
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// set up a real clock
	if r.Clock == nil {
		r.Clock = &sysutil.Clock{}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&tfaplv1beta1.Module{}).
		Complete(r)
}

// https://book.kubebuilder.io/cronjob-tutorial/controller-implementation.html
// getNextSchedule returns the number of missed runs if any, time of the next schedule and error
func getNextSchedule(module *tfaplv1beta1.Module, now time.Time, minIntervalBetweenRuns time.Duration) (int, time.Time, error) {
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
		if numOfMissedRuns > 100 {
			return 0, time.Time{}, fmt.Errorf("too many missed start times (%d) check clock skew", numOfMissedRuns)
		}
	}

	return numOfMissedRuns, sched.Next(now), nil
}
