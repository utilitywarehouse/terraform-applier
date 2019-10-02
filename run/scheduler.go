package run

import (
	"time"

	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/log"
)

// Scheduler handles queueing apply runs at a given time interval and upon every new Git commit.
type Scheduler struct {
	Errors          chan<- error
	FullRunInterval time.Duration
	GitUtil         git.UtilInterface
	PollInterval    time.Duration
	RepoPathFilters []string
	RunQueue        chan<- bool
}

// Start runs a continuous loop with two tickers for queueing runs.
// One ticker queues a new run every X seconds, where X is the value from $FULL_RUN_INTERVAL_SECONDS.
// The other ticker queues a new run upon every new Git commit, checking the repo every Y seconds where Y is the value from $POLL_INTERVAL_SECONDS.
func (s *Scheduler) Start() {
	if s.FullRunInterval != 0 {
		fullRunTicker := time.NewTicker(s.FullRunInterval)
		defer fullRunTicker.Stop()
		fullRunTickerChan := fullRunTicker.C
		go func() {
			for {
				select {
				case <-fullRunTickerChan:
					log.Info("Full run interval reached, queueing run, interval=%v", s.FullRunInterval)
					s.enqueue(s.RunQueue)
				}
			}
		}()
	}

	pollTicker := time.NewTicker(s.PollInterval)
	defer pollTicker.Stop()
	pollTickerChan := pollTicker.C
	lastCommitHash := ""
	for {
		select {
		case <-pollTickerChan:
			newCommitHash, err := s.GitUtil.HeadHashForPaths(s.RepoPathFilters...)
			if err != nil {
				s.Errors <- err
				return
			}
			if newCommitHash != lastCommitHash {
				log.Info("Queueing run, newest-commit=%s, last-commit=%s", newCommitHash, lastCommitHash)
				s.enqueue(s.RunQueue)
				lastCommitHash = newCommitHash
			} else {
				log.Debug("Run not queued, no new commit, newest-commit=%s, last-commit=%s", newCommitHash, lastCommitHash)
			}
		}
	}
}

// enqueue attempts to add a run to the queue, logging the result of the request.
func (s *Scheduler) enqueue(runQueue chan<- bool) {
	select {
	case runQueue <- true:
		log.Info("Run queued")
	default:
		log.Info("Run queue is already full")
	}
}
