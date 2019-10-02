package run

import (
	"fmt"
	"strings"
	"time"
)

// Result stores the data from a single run of the apply loop.
// The functions associated with Result convert raw data into the desired formats for insertion into the status page template.
type Result struct {
	Successes     []ApplyAttempt
	Failures      []ApplyAttempt
	CommitHash    string
	FullCommit    string
	DiffURLFormat string
}

// Duration returns the total duration for all the modules summed in seconds
func (r *Result) Duration() float64 {
	return r.Finish().Sub(r.Start()).Seconds()
}

// Finish returns the finish time for the last apply attempt
func (r *Result) Finish() time.Time {
	var finish time.Time

	attempts := append(r.Successes, r.Failures...)

	for _, attempt := range attempts {
		if finish.IsZero() || attempt.Finish.After(finish) {
			finish = attempt.Finish
		}
	}

	return finish
}

// FormattedStart returns the Start time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (r *Result) FormattedStart() string {
	return r.Start().Truncate(time.Second).String()
}

// FormattedFinish returns the Finish time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (r *Result) FormattedFinish() string {
	return r.Finish().Truncate(time.Second).String()
}

// FormattedDuration returns the total duration as a string, truncated to 3 decimal places.
func (r *Result) FormattedDuration() string {
	return fmt.Sprintf("%.3f sec", r.Duration())
}

// Start returns the start time for the first apply attempt
func (r *Result) Start() time.Time {
	var start time.Time

	attempts := append(r.Successes, r.Failures...)

	for _, attempt := range attempts {
		if start.IsZero() || attempt.Start.Before(start) {
			start = attempt.Start
		}
	}

	return start
}

// TotalModules returns the total count of apply attempts, both successes and failures.
func (r *Result) TotalModules() int {
	return len(r.Successes) + len(r.Failures)
}

// LastCommitLink returns a URL for the most recent commit if the envar $DIFF_URL_FORMAT is specified, otherwise it returns empty string.
func (r *Result) LastCommitLink() string {
	if r.CommitHash == "" || r.DiffURLFormat == "" || !strings.Contains(r.DiffURLFormat, "%s") {
		return ""
	}
	return fmt.Sprintf(r.DiffURLFormat, r.CommitHash)
}
