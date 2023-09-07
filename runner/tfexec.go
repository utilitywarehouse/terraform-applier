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
)

const strongBoxEnv = "TF_APPLIER_STRONGBOX_KEYRING"

//go:generate go run github.com/golang/mock/mockgen -package runner -destination tfexec_mock.go github.com/utilitywarehouse/terraform-applier/runner TFExecuter

type TFExecuter interface {
	init(ctx context.Context, backendConf map[string]string) (string, error)
	plan(ctx context.Context) (bool, string, error)
	apply(ctx context.Context) (string, error)
	cleanUp()
}

// tfRunner inits, plans and applies terraform modules
type tfRunner struct {
	moduleName      string
	moduleNamespace string
	workingDir      string
	planFileName    string

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

	err = r.GitSyncPool.CopyPath(ctx, module.Spec.RepoName, module.Spec.Path, tmpWSDir)
	if err != nil {
		return nil, fmt.Errorf("unable copy module's tf files to tmp dir err:%w", err)
	}

	tfr := &tfRunner{
		moduleName:      module.Name,
		moduleNamespace: module.Namespace,
		metrics:         r.Metrics,
		workingDir:      tmpWSDir,
		planFileName:    "plan.out",
	}

	tf, err := tfexec.NewTerraform(tmpWSDir, r.TerraformExecPath)
	if err != nil {
		return nil, err
	}

	runEnv := make(map[string]string)
	var strongboxKeyringData string

	// first add Global ENV and then module ENVs
	// this way user can override Global ENV if needed
	for key := range r.GlobalENV {
		runEnv[key] = r.GlobalENV[key]
	}
	for key := range envs {
		// get SB keyring data if corresponding ENV is set
		if key == strongBoxEnv {
			strongboxKeyringData = envs[key]
			continue
		}
		runEnv[key] = envs[key]
	}

	// Set HOME to cwd, this means that SSH should not pick up any
	// HOME is also used to setup git config in current dir
	runEnv["HOME"] = tmpWSDir
	//setup SB home for terraform remote module
	runEnv["STRONGBOX_HOME"] = tmpWSDir

	if strongboxKeyringData != "" {
		err := ensureDecryption(ctx, tmpWSDir, strongboxKeyringData)
		if err != nil {
			return nil, fmt.Errorf("unable to setup strongbox err:%w", err)
		}
	}

	tf.SetEnv(runEnv)

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

func (te *tfRunner) init(ctx context.Context, backendConf map[string]string) (string, error) {
	var out bytes.Buffer
	te.tf.SetStdout(&out)
	te.tf.SetStderr(&out)

	opts := []tfexec.InitOption{
		tfexec.Upgrade(true),
	}

	for k, v := range backendConf {
		attrStr := fmt.Sprintf("%s=%s", k, v)
		opts = append(opts, tfexec.BackendConfig(attrStr))
	}

	if err := te.tf.Init(ctx, opts...); err != nil {
		if uerr := errors.Unwrap(err); uerr != nil {
			if e, ok := uerr.(*exec.ExitError); ok {
				te.metrics.UpdateTerraformExitCodeCount(te.moduleName, te.moduleNamespace, "init", e.ExitCode())
			}
		}
		return out.String(), err
	}
	te.metrics.UpdateTerraformExitCodeCount(te.moduleName, te.moduleNamespace, "init", 0)

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
				te.metrics.UpdateTerraformExitCodeCount(te.moduleName, te.moduleNamespace, "plan", e.ExitCode())
			}
		}
		return changes, out.String(), err
	}
	if changes {
		te.metrics.UpdateTerraformExitCodeCount(te.moduleName, te.moduleNamespace, "plan", 2)
	} else {
		te.metrics.UpdateTerraformExitCodeCount(te.moduleName, te.moduleNamespace, "plan", 0)
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
				te.metrics.UpdateTerraformExitCodeCount(te.moduleName, te.moduleNamespace, "apply", e.ExitCode())
			}
		}
		return out.String(), err
	}

	te.metrics.UpdateTerraformExitCodeCount(te.moduleName, te.moduleNamespace, "apply", 0)

	return out.String(), nil
}
