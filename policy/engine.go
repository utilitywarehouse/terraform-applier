// Package policy evaluates OPA/Rego enforcement policies against terraform
// plan input by invoking the conftest CLI binary (not the OPA SDK) as a
// subprocess, guaranteeing behavioral parity with local `conftest test` runs at
// the cost of one subprocess per evaluation
//
// Policies are split into two independent tiers, each a separate conftest
// invocation:
//
//   - hard_deny: unconditional failure. Any violation blocks the run, never
//     bypassable.
//   - soft_deny: conditional failure. Violations require an admin override.
//
// The data config lists directories passed to conftest via --data.
package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

// conftestResult mirrors the JSON array conftest emits with --output json: one
// entry per evaluated file.
type conftestResult struct {
	Filename  string                    `json:"filename"`
	Namespace string                    `json:"namespace"`
	Successes int                       `json:"successes"`
	Failures  []v1beta1.PolicyViolation `json:"failures"`
	Warnings  []v1beta1.PolicyViolation `json:"warnings"`
}

// Engine evaluates inputs against the configured policy tiers by shelling out
// to the conftest binary. It is safe for concurrent use: every Eval runs its
// own isolated subprocess.
type Engine struct {
	bin  string
	ns   string
	hard []string // hard_deny policy dirs
	soft []string // soft_deny policy dirs
	data []string // --data dirs
}

// New validates cfg and returns a ready-to-use Engine, failing closed on any
// invalid configured path, a missing conftest binary, or a bundle that fails
// the startup smoke check.
func New(cfg Config) (*Engine, error) {
	ns := cfg.Namespace
	if ns == "" {
		ns = "main"
	}

	// CONFTEST_BIN overrides the binary path so tests can exercise the
	// missing-binary failure deterministically; otherwise resolve conftest on
	// PATH.
	bin := os.Getenv("CONFTEST_BIN")
	if bin == "" {
		var err error
		bin, err = exec.LookPath("conftest")
		if err != nil {
			return nil, fmt.Errorf("policy: conftest binary not found on PATH: %w", err)
		}
	}
	if bin == "" {
		return nil, fmt.Errorf("policy: conftest binary path is empty")
	}

	allPolicyPaths := append(cfg.HardDeny, cfg.SoftDeny...)

	// Validate every configured path up front
	if err := validatePaths(append(allPolicyPaths, cfg.Data...)); err != nil {
		return nil, err
	}

	if dirs := allPolicyPaths; len(dirs) > 0 {
		if err := smokeCheck(bin, dirs, cfg.Data, ns); err != nil {
			return nil, err
		}
	}

	return &Engine{
		bin:  bin,
		ns:   ns,
		hard: cfg.HardDeny,
		soft: cfg.SoftDeny,
		data: cfg.Data,
	}, nil
}

// validatePaths fails closed when any configured path is missing or is not a
// directory, surfacing a precise per-path error before conftest runs.
func validatePaths(dirs []string) error {
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("policy path %q: %w", dir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("policy path %q is not a directory", dir)
		}
	}
	return nil
}

// smokeCheck verifies the enabled policy bundles compile by running conftest
// once against an empty input document `{}`. Because conftest exits 1 both for
// a compiled bundle with violations and for a broken bundle, the exit code
// alone cannot distinguish them; both 0 and 1 count as "compiles". Any other
// exit code, an exec error, or unparseable output fails closed at startup.
func smokeCheck(bin string, dirs, data []string, ns string) error {
	_, code, err := runConftest(context.Background(), bin, dirs, data, ns, []byte("{}"))
	if err != nil {
		return fmt.Errorf("policy: policy bundles failed to compile (conftest exit %d): %w",
			code, err)
	}
	if code != 0 && code != 1 {
		return fmt.Errorf("policy: policy bundles failed to compile (conftest exit %d)", code)
	}
	return nil
}

// Eval evaluates input against both tiers and returns the combined verdict as
// the api/v1beta1 result type.
func (e *Engine) Eval(ctx context.Context, input interface{}) (*v1beta1.PolicyEvalResult, error) {
	planJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("policy: marshaling input: %w", err)
	}

	var hard, soft, hardWarn, softWarn []v1beta1.PolicyViolation
	if len(e.hard) > 0 {
		violations, warnings, err := e.evalTier(ctx, e.bin, e.hard, e.data, e.ns, planJSON)
		if err != nil {
			return nil, fmt.Errorf("policy: hard_deny evaluation failed: %w", err)
		}
		hard = violations
		hardWarn = warnings
	}
	if len(e.soft) > 0 {
		violations, warnings, err := e.evalTier(ctx, e.bin, e.soft, e.data, e.ns, planJSON)
		if err != nil {
			return nil, fmt.Errorf("policy: soft_deny evaluation failed: %w", err)
		}
		soft = violations
		softWarn = warnings
	}

	return &v1beta1.PolicyEvalResult{
		Allowed:    len(hard) == 0 && len(soft) == 0,
		HardDenies: hard,
		SoftDenies: soft,
		Warnings:   append(hardWarn, softWarn...),
	}, nil
}

// evalTier runs conftest once for a single tier and maps its failures and
// warnings to PolicyViolations (the v1beta1 type mirrors conftest's JSON
// shape: msg plus metadata). Warnings are collected and surfaced but never
// enforced: they are advisory only. Conftest exits 0 for no violations and 1
// for violations present; any other exit code or unparseable output is
// rejected so the caller fails the run closed.
func (e *Engine) evalTier(ctx context.Context, bin string, dirs, data []string, ns string, planJSON []byte) (failures, warnings []v1beta1.PolicyViolation, err error) {
	results, code, err := runConftest(ctx, bin, dirs, data, ns, planJSON)
	if err != nil {
		// runConftest failed: the binary could not run, or the output was not
		// parseable conftest JSON. Both fail the run closed.
		return nil, nil, err
	}
	// reject unexpected exit codes
	if code != 0 && code != 1 {
		return nil, nil, fmt.Errorf("conftest exited %d", code)
	}

	for _, r := range results {
		failures = append(failures, r.Failures...)
		warnings = append(warnings, r.Warnings...)
	}
	return failures, warnings, nil
}

// runConftest runs conftest once with the given policy/data dirs and
// namespace, piping inputJSON on stdin. It returns  (results, exit code, error),
// with error only for exec-failure or unparseable stdout (callers interpret the exit code).
func runConftest(ctx context.Context, bin string, dirs, data []string, ns string, inputJSON []byte) ([]conftestResult, int, error) {
	args := append([]string{"test"}, repeatArgs("--policy", dirs)...)
	args = append(args, repeatArgs("--data", data)...)
	args = append(args, "--namespace", ns, "--output", "json", "-")

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			// Binary failed to execute entirely (e.g., executable not found)
			return nil, -1, fmt.Errorf("failed to execute conftest: %w", err)
		}
	}

	var results []conftestResult
	if perr := json.Unmarshal(stdout.Bytes(), &results); perr != nil {
		stderrMsg := strings.TrimSpace(stderr.String())
		if stderrMsg == "" {
			// Fall back to stdout snippet if stderr was empty
			stderrMsg = strings.TrimSpace(stdout.String())
		}
		return nil, code, fmt.Errorf("parsing conftest output: %v (diagnostics: %s)", perr, stderrMsg)
	}

	return results, code, nil
}

func repeatArgs(flag string, dirs []string) []string {
	args := make([]string, 0, len(dirs))
	for _, d := range dirs {
		args = append(args, flag, d)
	}
	return args
}
