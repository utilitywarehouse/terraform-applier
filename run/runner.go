package run

import (
	"path"
	"path/filepath"

	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/log"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

// Runner manages the full process of an apply run, including getting the appropriate files, running apply commands on them, and handling the results.
type Runner struct {
	Applier         ApplierInterface
	Clock           sysutil.ClockInterface
	DiffURLFormat   string
	Errors          chan<- error
	GitUtil         git.UtilInterface
	Metrics         metrics.PrometheusInterface
	RepoPath        string
	RepoPathFilters []string
	RunResults      chan<- Result
	RunQueue        <-chan bool
}

// Start runs a continuous loop that starts a new run when a request comes into the queue channel.
func (r *Runner) Start() {
	for range r.RunQueue {
		newRun, err := r.run()
		if err != nil {
			r.Errors <- err
			return
		}
		r.RunResults <- *newRun
	}
}

// Run performs a full apply run, and returns a Result with data about the completed run (or nil if the run failed to complete).
func (r *Runner) run() (*Result, error) {

	log.Info("Started apply run")

	dirs, err := sysutil.ListDirs(r.RepoPath)
	if err != nil {
		return nil, err
	}

	dirs = r.pruneDirs(dirs)

	hash, err := r.GitUtil.HeadHashForPaths(r.RepoPathFilters...)
	if err != nil {
		return nil, err
	}
	commitLog, err := r.GitUtil.HeadCommitLogForPaths(r.RepoPathFilters...)
	if err != nil {
		return nil, err
	}

	log.Info("applying modules in %s", r.RepoPath)

	successes, failures := r.Applier.Apply(r.RepoPath, dirs)

	log.Info("Finished apply run")

	// Update metrics
	for _, success := range successes {
		r.Metrics.UpdateModuleSuccess(success.Module, true)
		r.Metrics.UpdateModuleApplyDuration(success.Module, success.Duration(), true)
	}

	for _, failure := range failures {
		r.Metrics.UpdateModuleSuccess(failure.Module, false)
		r.Metrics.UpdateModuleApplyDuration(failure.Module, failure.Duration(), false)
	}

	newRun := &Result{
		Successes:     successes,
		Failures:      failures,
		CommitHash:    hash,
		FullCommit:    commitLog,
		DiffURLFormat: r.DiffURLFormat,
	}

	return newRun, nil
}

func (r *Runner) pruneDirs(dirs []string) []string {
	if len(r.RepoPathFilters) == 0 {
		return dirs
	}

	var prunedDirs []string
	for _, dir := range dirs {
		for _, repoPathFilter := range r.RepoPathFilters {
			matched, err := filepath.Match(path.Join(r.RepoPath, repoPathFilter), dir)
			if err != nil {
				log.Error("%v", err)
			} else if matched {
				prunedDirs = append(prunedDirs, dir)
			}
		}
	}

	return prunedDirs
}
