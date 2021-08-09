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
	Applier            ApplierInterface
	Clock              sysutil.ClockInterface
	DiffURLFormat      string
	Errors             chan<- error
	GitUtil            git.UtilInterface
	Metrics            metrics.PrometheusInterface
	ModulesPath        string
	ModulesPathFilters []string
	RunResults         chan<- Result
	RunQueue           <-chan bool

	applying bool
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

func (r *Runner) Applying() bool {
	return r.applying
}

// Run performs a full apply run, and returns a Result with data about the completed run (or nil if the run failed to complete).
func (r *Runner) run() (*Result, error) {
	log.Info("Started apply run")

	r.applying = true
	defer func() {
		r.applying = false
	}()

	dirs, err := sysutil.ListDirs(r.ModulesPath)
	if err != nil {
		return nil, err
	}

	dirs = r.pruneDirs(dirs)

	isRepo, err := r.GitUtil.IsRepo()
	if err != nil {
		return nil, err
	}

	var hash, commitLog string
	if isRepo {
		hash, err = r.GitUtil.HeadHashForPaths(r.ModulesPathFilters...)
		if err != nil {
			return nil, err
		}
		commitLog, err = r.GitUtil.HeadCommitLogForPaths(r.ModulesPathFilters...)
		if err != nil {
			return nil, err
		}
	}

	log.Info("applying modules in %s", r.ModulesPath)

	successes, failures := r.Applier.Apply(r.ModulesPath, dirs)

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
	if len(r.ModulesPathFilters) == 0 {
		return dirs
	}

	var prunedDirs []string
	for _, dir := range dirs {
		for _, modulesPathFilter := range r.ModulesPathFilters {
			matched, err := filepath.Match(path.Join(r.ModulesPath, modulesPathFilter), dir)
			if err != nil {
				log.Error("%v", err)
			} else if matched {
				prunedDirs = append(prunedDirs, dir)
			}
		}
	}

	return prunedDirs
}
