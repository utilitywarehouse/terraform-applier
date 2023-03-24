package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

//go:generate go run github.com/golang/mock/mockgen -package runner -destination tfexec_mock.go github.com/utilitywarehouse/terraform-applier/runner TFExecuter

type TFExecuter interface {
	init(ctx context.Context) (string, error)
	plan(ctx context.Context) (bool, string, error)
	apply(ctx context.Context) (string, error)
	cleanUp()
}

// tfRunner inits, plans and applies terraform modules
type tfRunner struct {
	wsNamespacedName string
	workingDir       string
	planFileName     string

	metrics metrics.PrometheusInterface
	tf      *tfexec.Terraform
}

func (r *Runner) NewTFRunner(
	ctx context.Context,
	module *tfaplv1beta1.Module,
	envs map[string]string,
	vars map[string]string,
) (TFExecuter, error) {
	// Copy repo path to a temporary directory
	tmpWSDir, err := os.MkdirTemp("", module.Namespace+"-"+module.Name+"-*")
	if err != nil {
		return nil, fmt.Errorf("unable to create tmp dir %w", err)
	}

	err = sysutil.CopyDir(filepath.Join(r.RepoPath, module.Spec.Path), tmpWSDir)
	if err != nil {
		return nil, fmt.Errorf("unable copy module's tf files to tmp dir err:%w", err)
	}

	tfr := &tfRunner{
		wsNamespacedName: module.Namespace + "/" + module.Name,
		metrics:          r.Metrics,
		workingDir:       tmpWSDir,
		planFileName:     "plan.out",
	}

	tf, err := tfexec.NewTerraform(tmpWSDir, r.TerraformExecPath)
	if err != nil {
		return nil, err
	}

	tf.SetEnv(envs)

	// Setup *.auto.tfvars.json file to auto load TF variables during plan and apply
	jsonBytes, err := json.Marshal(vars)
	if err != nil {
		return nil, fmt.Errorf("unable to json encode variables err:%w", err)
	}

	tfvarFile := filepath.Join(tmpWSDir, "terraform-applier-generated.auto.tfvars.json")
	if err := os.WriteFile(tfvarFile, jsonBytes, 0644); err != nil {
		return nil, fmt.Errorf("unable to write the data to file %s err:%s", tfvarFile, err)
	}

	tfr.tf = tf
	return tfr, nil
}

func (te *tfRunner) cleanUp() {
	os.RemoveAll(te.workingDir)
}

func (te *tfRunner) init(ctx context.Context) (string, error) {
	var out bytes.Buffer
	te.tf.SetStdout(&out)
	te.tf.SetStderr(&out)

	if err := te.tf.Init(ctx, tfexec.Upgrade(true)); err != nil {
		if uerr := errors.Unwrap(err); uerr != nil {
			if e, ok := uerr.(*exec.ExitError); ok {
				te.metrics.UpdateTerraformExitCodeCount(te.wsNamespacedName, "init", e.ExitCode())
			}
		}
		return out.String(), err
	}
	te.metrics.UpdateTerraformExitCodeCount(te.wsNamespacedName, "init", 0)

	return out.String(), nil
}

func (te *tfRunner) plan(ctx context.Context) (bool, string, error) {
	var out bytes.Buffer
	te.tf.SetStdout(&out)
	te.tf.SetStderr(&out)

	planOut := filepath.Join(te.workingDir, te.planFileName)

	changes, err := te.tf.Plan(ctx, tfexec.Out(planOut))
	if err != nil {
		if uerr := errors.Unwrap(err); uerr != nil {
			if e, ok := uerr.(*exec.ExitError); ok {
				te.metrics.UpdateTerraformExitCodeCount(te.wsNamespacedName, "plan", e.ExitCode())
			}
		}
		return changes, out.String(), err
	}
	if changes {
		te.metrics.UpdateTerraformExitCodeCount(te.wsNamespacedName, "plan", 2)
	} else {
		te.metrics.UpdateTerraformExitCodeCount(te.wsNamespacedName, "plan", 0)
	}

	return changes, out.String(), nil
}

func (te *tfRunner) apply(ctx context.Context) (string, error) {
	var out bytes.Buffer
	te.tf.SetStdout(&out)
	te.tf.SetStderr(&out)

	planOut := filepath.Join(te.workingDir, te.planFileName)

	_, err := os.Stat(planOut)
	if err != nil {
		return "", fmt.Errorf("plan output file is required for apply run expected_loc:%s err:%w", planOut, err)
	}

	if err := te.tf.Apply(ctx, tfexec.DirOrPlan(planOut)); err != nil {
		if uerr := errors.Unwrap(err); uerr != nil {
			if e, ok := uerr.(*exec.ExitError); ok {
				te.metrics.UpdateTerraformExitCodeCount(te.wsNamespacedName, "apply", e.ExitCode())
			}
		}
		return out.String(), err
	}

	te.metrics.UpdateTerraformExitCodeCount(te.wsNamespacedName, "apply", 0)

	return out.String(), nil
}
