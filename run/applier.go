package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/utilitywarehouse/terraform-applier/log"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

// ApplyAttempt stores the data from an attempt at applying a single module
type ApplyAttempt struct {
	DryRun bool
	Finish time.Time
	Output string
	Module string
	Start  time.Time
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
	Clock             sysutil.ClockInterface
	DryRun            bool
	Errors            chan<- error
	Metrics           metrics.PrometheusInterface
	TerraformExecPath string
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
			DryRun: a.DryRun,
			Finish: time.Time{},
			Output: "",
			Module: module,
			Start:  a.Clock.Now(),
		}

		tmpModulePath := filepath.Join(tmpDir, filepath.Base(module))

		out, err := a.applyModule(context.Background(), tmpModulePath)
		appliedModule.Finish = a.Clock.Now()
		appliedModule.Output = out
		if err != nil {
			log.Warn("error applying %s: %s", module, err)
			failures = append(failures, appliedModule)
		} else {
			successes = append(successes, appliedModule)
		}
	}

	return successes, failures
}

// tfLogMessageRe matches `[LEVEL] running Terraform command:` at the start of a
// line ($1) and the actual terraform command ($2)
var tfLogMessageRe = regexp.MustCompile(`(^\[[A-Z]+\] running Terraform command: )(.+)`)

// tfLogger implements tfexec.printfer. It logs messages from tfexec and also
// writes them to the io.Writer w
type tfLogger struct {
	w io.Writer
}

// Printf logs the message and also writes it to the given io.Writer
func (l *tfLogger) Printf(format string, v ...interface{}) {
	// Extract the terraform command from the log line
	msg := tfLogMessageRe.ReplaceAllString(fmt.Sprintf(format, v...), `$2`)
	log.Info("+ %s", msg)
	// Write the command to the output with a faux shell prompt and a new
	// line at the end. This aids readability.
	fmt.Fprint(l.w, "$ "+msg+"\n")
}

func (a *Applier) applyModule(ctx context.Context, modulePath string) (string, error) {
	var out bytes.Buffer

	tf, err := tfexec.NewTerraform(modulePath, a.TerraformExecPath)
	if err != nil {
		return "", err
	}
	tf.SetLogger(&tfLogger{w: &out})
	tf.SetStdout(&out)
	tf.SetStderr(&out)

	// Sometimes the error text would be useful in the command output that's
	// displayed in the UI. For this reason, we append the error to the
	// output before we return it.
	errReturn := func(out bytes.Buffer, err error) (string, error) {
		if err != nil {
			return fmt.Sprintf("%s\n%s", out.String(), err.Error()), err
		}

		return out.String(), nil
	}

	if err := tf.Init(ctx, tfexec.Upgrade(true)); err != nil {
		return errReturn(out, err)
	}
	fmt.Fprint(&out, "\n")

	planOut := filepath.Join(modulePath, "plan.out")

	changes, err := tf.Plan(ctx, tfexec.Out(planOut))
	if err != nil {
		return errReturn(out, err)
	}
	fmt.Fprint(&out, "\n")

	if changes && !a.DryRun {
		if err := tf.Apply(ctx, tfexec.DirOrPlan(planOut)); err != nil {
			return errReturn(out, err)
		}
		fmt.Fprint(&out, "\n")
	}

	return out.String(), nil
}
