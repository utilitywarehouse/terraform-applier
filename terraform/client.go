package terraform

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/utilitywarehouse/terraform-applier/log"
	"github.com/utilitywarehouse/terraform-applier/metrics"
)

// Output is the output of the terraform command
type Output struct {
	Command string
	Output  string
}

// ClientInterface allows for mocking out the functionality of Client when testing the full process of an apply run.
type ClientInterface interface {
	Init(string, []string) (Output, error)
	Plan(string) (Output, string, error)
	Apply(string, string) (Output, error)
	Exec(...string) (*Output, error)
	SetWorkingDir(string)
}

// Client for terraform
type Client struct {
	ExecPath string
	Metrics  metrics.PrometheusInterface

	workDir string
}

// SetWorkingDir changes the working directory
func (c *Client) SetWorkingDir(dir string) {
	c.workDir = dir
}

// Init runs terraform init with some predefined arguments, plus some user defined arguments
func (c *Client) Init(module string, args []string) (Output, error) {
	args = append([]string{"init"}, args...)
	args = append(args, "-reconfigure", "-no-color", "-upgrade=true")

	out, err := c.Exec(args...)
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateTerraformExitCodeCount(module, "init", e.ExitCode())
		}
		return *out, err
	}
	c.Metrics.UpdateTerraformExitCodeCount(module, "init", 0)
	return *out, nil
}

// Plan runs terraform plan with some predefined arguments that are condusive to automation. It writes the plan to a file in the
// working directory, provided there are changes to be made.
func (c *Client) Plan(module string) (Output, string, error) {
	planFile := filepath.Join(c.workDir, "plan.out")

	out, err := c.Exec("plan", "-input=false", "-no-color", "-detailed-exitcode", "-out="+planFile)
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateTerraformExitCodeCount(module, "plan", e.ExitCode())
			if e.ExitCode() == 2 {
				return *out, planFile, nil
			}
		}
		return *out, "", err
	}
	c.Metrics.UpdateTerraformExitCodeCount(module, "plan", 0)
	return *out, "", nil
}

// Apply applies a plan file
func (c *Client) Apply(module, planFile string) (Output, error) {
	out, err := c.Exec("apply", "-no-color", "-auto-approve", planFile)
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			c.Metrics.UpdateTerraformExitCodeCount(module, "apply", e.ExitCode())
		}
		return *out, err
	}
	c.Metrics.UpdateTerraformExitCodeCount(module, "apply", 0)
	return *out, nil
}

// Version returns version info for terraform
func (c *Client) Version() (string, error) {
	out, err := c.Exec("version", "-json")
	if err != nil {
		return "", err
	}

	return out.Output, nil
}

// Exec runs terraform
func (c *Client) Exec(args ...string) (*Output, error) {
	var err error

	args = append([]string{c.ExecPath}, args...)

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = c.workDir

	cmdStr := strings.Join(args, " ")

	log.Info("+ %s", cmdStr)
	out, err := cmd.CombinedOutput()

	return &Output{
		Command: cmdStr,
		Output:  string(out),
	}, err
}
