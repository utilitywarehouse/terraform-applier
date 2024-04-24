package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/metrics"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

const strongBoxEnv = "TF_APPLIER_STRONGBOX_KEYRING"

//go:generate go run github.com/golang/mock/mockgen -package runner -destination tfexec_mock.go github.com/utilitywarehouse/terraform-applier/runner TFExecuter

type TFExecuter interface {
	init(ctx context.Context, backendConf map[string]string) (string, error)
	plan(ctx context.Context) (bool, string, error)
	showPlanFileRaw(ctx context.Context) (string, error)
	apply(ctx context.Context) (string, error)
	cleanUp()
}

// tfRunner inits, plans and applies terraform modules
type tfRunner struct {
	moduleName      string
	moduleNamespace string
	rootDir         string
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
) (te TFExecuter, err error) {

	// create module temp root to copy repo path to a temporary directory
	tmpRoot := path.Join(
		r.DataRootPath,
		runTmpRoot,
		fmt.Sprintf("%s-%s-%d", module.Namespace, module.Name, rand.Uint32()),
	)
	err = os.MkdirAll(tmpRoot, 0700)
	if err != nil {
		return nil, fmt.Errorf("unable to create tmp dir %w", err)
	}

	defer func() {
		// calling function can only run cleanUp() if TFExecuter is successfully created
		// hence cleanup temp repo dir if errored
		if err != nil {
			sysutil.RemoveAll(tmpRoot)
		}
	}()

	// clone repo to new temp dir so that file doesn't change during run.
	// checkout whole repo as module might contain relative path to modules/files
	// which are outside of its path
	_, err = r.Repos.Clone(ctx, module.Spec.RepoURL, tmpRoot, module.Spec.RepoRef, "", true)
	if err != nil {
		return nil, fmt.Errorf("unable copy module's tf files to tmp dir err:%w", err)
	}

	tfr := &tfRunner{
		moduleName:      module.Name,
		moduleNamespace: module.Namespace,
		metrics:         r.Metrics,
		rootDir:         tmpRoot,
		workingDir:      filepath.Join(tmpRoot, module.Spec.Path),
		planFileName:    "plan.out",
	}

	tf, err := tfexec.NewTerraform(tfr.workingDir, r.TerraformExecPath)
	if err != nil {
		return nil, err
	}

	runEnv := make(map[string]string)
	var strongboxKeyringData string

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
	runEnv["HOME"] = tfr.workingDir
	//setup SB home for terraform remote module
	runEnv["STRONGBOX_HOME"] = tfr.workingDir

	if strongboxKeyringData != "" {
		// TODO: should we be decrypting whole repo with given key?
		err := ensureDecryption(ctx, tfr.workingDir, strongboxKeyringData)
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

	tfvarFile := filepath.Join(tfr.workingDir, "terraform-applier-generated.auto.tfvars.json")
	if err := os.WriteFile(tfvarFile, jsonBytes, 0644); err != nil {
		return nil, fmt.Errorf("unable to write the data to file %s err:%s", tfvarFile, err)
	}

	tfr.tf = tf
	return tfr, nil
}

func (te *tfRunner) cleanUp() {
	sysutil.RemoveAll(te.rootDir)
}

func (te *tfRunner) init(ctx context.Context, backendConf map[string]string) (string, error) {
	var out bytes.Buffer
	te.tf.SetStdout(&out)
	te.tf.SetStderr(&out)

	opts := []tfexec.InitOption{
		// unset upgrade so that tf-applier doesn't override providers version in lock file.
		// tf-applier should always select provider version from the lock file
		// if lock file is not there TF will download newest available version
		// that matches the given version constraint of the provider
		tfexec.Upgrade(false),
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

// showPlanFileRaw reads a given plan file and outputs the plan in a
// human-friendly, opaque format.
func (te *tfRunner) showPlanFileRaw(ctx context.Context) (string, error) {
	planOut := filepath.Join(te.workingDir, te.planFileName)
	return te.tf.ShowPlanFileRaw(ctx, planOut)
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
