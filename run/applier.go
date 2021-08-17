package run

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/utilitywarehouse/terraform-applier/log"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"github.com/utilitywarehouse/terraform-applier/terraform"
)

// ApplyAttempt stores the data from an attempt at applying a single module
type ApplyAttempt struct {
	DryRun       bool
	ErrorMessage string
	Finish       time.Time
	Output       []terraform.Output
	Module       string
	Start        time.Time
}

// FormattedStart returns the Start time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (a *ApplyAttempt) FormattedStart() string {
	return a.Start.Truncate(time.Second).String()
}

// FormattedFinish returns the Finish time in the format "YYYY-MM-DD hh:mm:ss -0000 GMT"
func (a *ApplyAttempt) FormattedFinish() string {
	return a.Finish.Truncate(time.Second).String()
}

// FormattedDuration returns the duration of the module run in seconds, truncated to 3 decimal places.
func (a *ApplyAttempt) FormattedDuration() string {
	return fmt.Sprintf("%.3fs", a.Duration())
}

// Duration returns the duration of the module run in seconds
func (a *ApplyAttempt) Duration() float64 {
	return a.Finish.Sub(a.Start).Seconds()
}

// ApplierInterface allows for mocking out the functionality of Applier when testing the full process of an apply run.
type ApplierInterface interface {
	Apply(string, []string) ([]ApplyAttempt, []ApplyAttempt)
}

// Applier inits, plans and applies terraform modules
type Applier struct {
	Clock           sysutil.ClockInterface
	DryRun          bool
	Errors          chan<- error
	InitArgs        string
	Metrics         metrics.PrometheusInterface
	TerraformClient terraform.ClientInterface
}

// Apply runs terraform for each of the modules and reports the successes and failures
func (a *Applier) Apply(modulesPath string, modules []string) ([]ApplyAttempt, []ApplyAttempt) {
	successes := []ApplyAttempt{}
	failures := []ApplyAttempt{}

	// Copy repo path to a temporary directory
	tmpDir, err := ioutil.TempDir("", "terraform")
	defer os.RemoveAll(tmpDir)
	if err != nil {
		return successes, failures
	}
	err = sysutil.CopyDir(modulesPath, tmpDir)
	if err != nil {
		return successes, failures
	}

	for _, module := range modules {
		log.Info("-> %s", module)

		appliedModule := ApplyAttempt{
			DryRun:       a.DryRun,
			ErrorMessage: "",
			Finish:       time.Time{},
			Output:       []terraform.Output{},
			Module:       module,
			Start:        a.Clock.Now(),
		}

		// Run terraform from the module's counterpart in the temporary directory
		a.TerraformClient.SetWorkingDir(filepath.Join(tmpDir, filepath.Base(module)))

		// Bool to track whether this was a successful run or not
		success := true

		var initArgs []string
		if a.InitArgs != "" {
			formattedInitArgs := fmt.Sprintf(a.InitArgs, filepath.Base(module))
			initArgs = strings.Split(formattedInitArgs, ",")
		}

		// Init
		initOut, err := a.TerraformClient.Init(module, initArgs)
		appliedModule.Output = append(appliedModule.Output, initOut)
		if err != nil {
			appliedModule.ErrorMessage = err.Error()
			success = false
		} else {
			// Plan
			planOut, planFile, err := a.TerraformClient.Plan(module)
			appliedModule.Output = append(appliedModule.Output, planOut)
			if err != nil {
				appliedModule.ErrorMessage = err.Error()
				success = false
			} else if len(planFile) > 0 && !a.DryRun {
				// Apply (if there are changes to apply and dry run isn't set)
				applyOut, err := a.TerraformClient.Apply(module, planFile)
				appliedModule.Output = append(appliedModule.Output, applyOut)
				if err != nil {
					appliedModule.ErrorMessage = err.Error()
					success = false
				}
			}
		}

		appliedModule.Finish = a.Clock.Now()

		if success {
			successes = append(successes, appliedModule)
		} else {
			log.Warn("%v\n%v", appliedModule.ErrorMessage, appliedModule.Output)
			failures = append(failures, appliedModule)
		}
	}

	return successes, failures
}
