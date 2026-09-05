package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/policy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// TestModuleController_PolicyRunner exercises the runner's OPA policy gates
// end-to-end against the real conftest binary and the policy fixtures under
// src/policies. Each scenario drives a real terraform plan (and where allowed
// an apply) through the runner while a real policy.Engine evaluates the
// rendered plan.
//
// Policy fixture outcomes (see src/policies):
//   - hello-allowed        : every resource is allowlisted and free of
//     credential markers -> passes both tiers.
//   - hello                : contains echo_AWS_KEY / verify_gcp_token ->
//     hard_deny.
//   - hello-with-providers : contains exec_provider (not allowlisted) ->
//     soft_deny; slow_provider is approved.
func TestModuleController_PolicyRunner(t *testing.T) {
	const (
		moduleNamespace = "default"
		commitHash      = "a1b2c3d4"
		commitMsg       = "test commit"
	)

	// Wire the shared runner to a real conftest-backed engine evaluating the
	// integration-test policy bundle, and restore it afterwards so later tests
	// run with policies disabled.
	engine, err := policy.New(policy.Config{
		Namespace: "main",
		HardDeny:  []string{"src/policies/hard"},
		SoftDeny:  []string{"src/policies/soft"},
		Data:      []string{"src/policies/data"},
	})
	if err != nil {
		t.Fatalf("failed to construct policy engine: %v", err)
	}
	testRunner.PolicyEngine = engine
	defer func() { testRunner.PolicyEngine = nil }()

	sbKeyringData, err := os.ReadFile(".tests_strongbox_keyring")
	if err != nil {
		t.Fatal(err)
	}
	sbIdentityData, err := os.ReadFile(".tests_strongbox_identity")
	if err != nil {
		t.Fatal(err)
	}

	// Wrapper to simplify per-test setup. strongbox is only needed by the
	// `hello` module (it decrypts an age-sec secret).
	setup := func(t *testing.T, path string) *gomock.Controller {
		ctrl := setupTest(t)

		fakeClock.SetTime(time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC))
		testReconciler.Runner = &testRunner

		// remove any label selector
		testFilter.SetLabelSelector("", "")

		// all jobs will be triggered automatically as they do not have initial commit hash
		testRepos.EXPECT().Hash(gomock.Any(), "https://host.xy/dummy/repo.git", "HEAD", path).
			Return(commitHash, nil).AnyTimes()
		testRepos.EXPECT().Subject(gomock.Any(), "https://host.xy/dummy/repo.git", commitHash).
			Return(commitMsg, nil).AnyTimes()

		var dst string
		testRepos.EXPECT().Clone(gomock.Any(), "https://host.xy/dummy/repo.git", gomock.AssignableToTypeOf(dst), "HEAD", nil, true).
			DoAndReturn(func(ctx context.Context, remote, dst, branch string, pathspecs []string, rmGitDir bool) (string, error) {
				return "commit124", os.CopyFS(dst, os.DirFS(filepath.Join("src", "modules")))
			}).AnyTimes()

		testMetrics.EXPECT().UpdateModuleRunDuration(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		testMetrics.EXPECT().UpdateModuleSuccess(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		testMetrics.EXPECT().SetRunPending(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		testCreds.EXPECT().Creds(gomock.Any()).Return("", "token", nil).AnyTimes()
		os.Remove(testStateFilePath)

		return ctrl
	}

	// buildModule returns a Module targeting the given module path. strongbox
	// envs are only attached when the target module needs them (the `hello`
	// module decrypts an age-sec secret).
	buildModule := func(moduleName, path string, strongbox bool) *tfaplv1beta1.Module {
		mod := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule:  "50 * * * *",
				RepoURL:   "https://host.xy/dummy/repo.git",
				Path:      path,
				AutoApply: new(true),
			},
		}
		if strongbox {
			mod.Spec.Env = []tfaplv1beta1.EnvVar{
				{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
				{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
			}
		}
		return mod
	}

	// createModule creates the module and registers deletion cleanup.
	createModule := func(t *testing.T, module *tfaplv1beta1.Module) {
		t.Helper()
		if err := k8sClient.Create(context.Background(), module); err != nil {
			t.Fatalf("Failed to create module: %v", err)
		}
		t.Cleanup(func() {
			if err := k8sClient.Delete(context.Background(), module); err != nil {
				t.Errorf("Failed to delete module: %v", err)
			}
		})
	}

	// waitForState polls until the module reaches wantState (or a retry budget).
	waitForState := func(t *testing.T, name, wantState string) *tfaplv1beta1.Module {
		t.Helper()
		fetchedModule := &tfaplv1beta1.Module{}
		for range 100 {
			k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: moduleNamespace}, fetchedModule)
			if fetchedModule.Status.CurrentState == wantState {
				return fetchedModule
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("Expected state %q, got %q (reason %q)", wantState, fetchedModule.Status.CurrentState, fetchedModule.Status.StateReason)
		return nil
	}

	t.Run("allowed plan applies and reports an allowed policy result", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t, "hello-allowed")
		defer ctrl.Finish()

		var lastRun, lastApplyRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				return nil
			})
		testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastApplyRun = run
				close(redisDoneCh)
				return nil
			})

		module := buildModule("hello-allowed", "hello-allowed", false)
		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.P1", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.P1").Return(fakeClient, nil)

		createModule(t, module)

		select {
		case <-redisDoneCh:
		case <-time.After(120 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetched := waitForState(t, "hello-allowed", string(tfaplv1beta1.StatusOk))
		if fetched.Status.StateReason != tfaplv1beta1.ReasonApplied {
			t.Errorf("Expected reason %q, got %q", tfaplv1beta1.ReasonApplied, fetched.Status.StateReason)
		}
		if lastRun.PolicyResult == nil {
			t.Fatal("Expected non-nil PolicyResult")
		}
		if !lastRun.PolicyResult.Allowed {
			t.Errorf("Expected Allowed=true, got %+v", lastRun.PolicyResult)
		}
		if len(lastRun.PolicyResult.HardDenies) != 0 || len(lastRun.PolicyResult.SoftDenies) != 0 {
			t.Errorf("Expected no denials, got %+v", lastRun.PolicyResult)
		}
		if !strings.Contains(lastApplyRun.Output, "Apply complete!") {
			t.Error("Expected apply output")
		}
	})

	t.Run("hard deny blocks apply and sets Policy_Violation", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t, "hello")
		defer ctrl.Finish()

		var lastRun, lastApplyRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				return nil
			})
		testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastApplyRun = run
				close(redisDoneCh)
				return nil
			})

		module := buildModule("hello-policy-hard", "hello", true)
		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.P2", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.P2").Return(fakeClient, nil)

		createModule(t, module)

		select {
		case <-redisDoneCh:
		case <-time.After(120 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetched := waitForState(t, "hello-policy-hard", string(tfaplv1beta1.StatusPolicyViolation))
		if fetched.Status.StateReason != tfaplv1beta1.ReasonHardDenyViolation {
			t.Errorf("Expected reason %q, got %q", tfaplv1beta1.ReasonHardDenyViolation, fetched.Status.StateReason)
		}
		if fetched.Status.LastAppliedCommitHash != "" {
			t.Errorf("Expected no applied commit, got %q", fetched.Status.LastAppliedCommitHash)
		}
		if lastRun.PolicyResult == nil {
			t.Fatal("Expected non-nil PolicyResult")
		}
		if lastRun.PolicyResult.Allowed {
			t.Error("Expected Allowed=false")
		}
		if len(lastRun.PolicyResult.HardDenies) == 0 {
			t.Errorf("Expected hard denials, got %+v", lastRun.PolicyResult)
		}
		if len(lastRun.PolicyResult.SoftDenies) != 0 {
			t.Errorf("Expected no soft denials, got %+v", lastRun.PolicyResult)
		}
		if strings.Contains(lastApplyRun.Output, "Apply complete!") {
			t.Error("Hard deny must not apply")
		}
	})

	t.Run("hard deny cannot be bypassed by a forced apply with a valid policy override", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t, "hello")
		defer ctrl.Finish()

		var lastRun, lastApplyRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				return nil
			})
		testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastApplyRun = run
				close(redisDoneCh)
				return nil
			})

		module := buildModule("hello-policy-hard-forced", "hello", true)
		// Request a ForcedApply carrying a policy override pinned to the
		// running commit. The override is valid for the soft_deny gate, yet
		// hard_deny is unconditional and must still block the apply.
		req := tfaplv1beta1.Request{
			RequestedAt:    &metav1.Time{Time: time.Now()},
			Type:           tfaplv1beta1.ForcedApply,
			PolicyOverride: true,
			OverriddenHash: commitHash,
			OverriddenBy:   "integration-test",
			OverrideReason: "integration test invalid override attempt on hard deny",
		}
		module.ObjectMeta.Annotations = map[string]string{
			tfaplv1beta1.RunRequestAnnotationKey: mustMarshalRequest(t, &req),
		}

		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.P7", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.P7").Return(fakeClient, nil)

		createModule(t, module)

		select {
		case <-redisDoneCh:
		case <-time.After(120 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetched := waitForState(t, "hello-policy-hard-forced", string(tfaplv1beta1.StatusPolicyViolation))
		if fetched.Status.StateReason != tfaplv1beta1.ReasonHardDenyViolation {
			t.Errorf("Expected reason %q, got %q", tfaplv1beta1.ReasonHardDenyViolation, fetched.Status.StateReason)
		}
		if fetched.Status.LastAppliedCommitHash != "" {
			t.Errorf("Expected no applied commit, got %q", fetched.Status.LastAppliedCommitHash)
		}
		if lastRun.PolicyResult == nil {
			t.Fatal("Expected non-nil PolicyResult")
		}
		if lastRun.PolicyResult.Allowed {
			t.Error("Expected Allowed=false")
		}
		if len(lastRun.PolicyResult.HardDenies) == 0 {
			t.Errorf("Expected hard denials, got %+v", lastRun.PolicyResult)
		}
		if len(lastRun.PolicyResult.SoftDenies) != 0 {
			t.Errorf("Expected no soft denials, got %+v", lastRun.PolicyResult)
		}
		if lastRun.PolicyResult.Overridden {
			t.Error("Expected Overridden=false; hard deny must not be bypassable by an override")
		}
		if strings.Contains(lastApplyRun.Output, "Apply complete!") {
			t.Error("Hard deny must not apply, even under a forced apply with override")
		}
	})

	t.Run("soft deny without override requires override and does not apply", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t, "hello-with-providers")
		defer ctrl.Finish()

		var lastRun, lastApplyRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				return nil
			})
		testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastApplyRun = run
				close(redisDoneCh)
				return nil
			})

		module := buildModule("hello-policy-soft", "hello-with-providers", false)
		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.P3", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.P3").Return(fakeClient, nil)

		createModule(t, module)

		select {
		case <-redisDoneCh:
		case <-time.After(120 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetched := waitForState(t, "hello-policy-soft", string(tfaplv1beta1.StatusOverrideRequired))
		if fetched.Status.StateReason != tfaplv1beta1.ReasonSoftDenyViolation {
			t.Errorf("Expected reason %q, got %q", tfaplv1beta1.ReasonSoftDenyViolation, fetched.Status.StateReason)
		}
		if fetched.Status.LastAppliedCommitHash != "" {
			t.Errorf("Expected no applied commit, got %q", fetched.Status.LastAppliedCommitHash)
		}
		if lastRun.PolicyResult == nil {
			t.Fatal("Expected non-nil PolicyResult")
		}
		if lastRun.PolicyResult.Allowed {
			t.Error("Expected Allowed=false")
		}
		if len(lastRun.PolicyResult.SoftDenies) == 0 {
			t.Errorf("Expected soft denials, got %+v", lastRun.PolicyResult)
		}
		if len(lastRun.PolicyResult.HardDenies) != 0 {
			t.Errorf("Expected no hard denials, got %+v", lastRun.PolicyResult)
		}
		if strings.Contains(lastApplyRun.Output, "Apply complete!") {
			t.Error("Soft deny without override must not apply")
		}
	})

	t.Run("soft deny is bypassed by a valid override and applies", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t, "hello-with-providers")
		defer ctrl.Finish()

		var lastRun, lastApplyRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				return nil
			})
		testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastApplyRun = run
				close(redisDoneCh)
				return nil
			})

		module := buildModule("hello-policy-override", "hello-with-providers", false)
		// Request a ForcedApply with a policy override pinned to the running
		// commit so the runner honors it.
		req := tfaplv1beta1.Request{
			RequestedAt:    &metav1.Time{Time: time.Now()},
			Type:           tfaplv1beta1.ForcedApply,
			PolicyOverride: true,
			OverriddenHash: commitHash,
			OverriddenBy:   "integration-test",
			OverrideReason: "integration test valid override",
		}
		module.ObjectMeta.Annotations = map[string]string{
			tfaplv1beta1.RunRequestAnnotationKey: mustMarshalRequest(t, &req),
		}

		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.P4", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.P4").Return(fakeClient, nil)

		createModule(t, module)

		select {
		case <-redisDoneCh:
		case <-time.After(120 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetched := waitForState(t, "hello-policy-override", string(tfaplv1beta1.StatusOk))
		if fetched.Status.StateReason != tfaplv1beta1.ReasonPolicyOverridden {
			t.Errorf("Expected reason %q, got %q", tfaplv1beta1.ReasonPolicyOverridden, fetched.Status.StateReason)
		}
		if fetched.Status.LastAppliedCommitHash != commitHash {
			t.Errorf("Expected applied commit %q, got %q", commitHash, fetched.Status.LastAppliedCommitHash)
		}
		if lastRun.PolicyResult == nil {
			t.Fatal("Expected non-nil PolicyResult")
		}
		if lastRun.PolicyResult.Allowed {
			t.Error("Expected Allowed=false")
		}
		if len(lastRun.PolicyResult.SoftDenies) == 0 {
			t.Errorf("Expected soft denials, got %+v", lastRun.PolicyResult)
		}
		if !lastRun.PolicyResult.Overridden {
			t.Error("Expected Overridden=true")
		}
		if !strings.Contains(lastApplyRun.Output, "Apply complete!") {
			t.Error("Expected apply output")
		}
	})

	t.Run("stale override is ignored and apply is blocked", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t, "hello-with-providers")
		defer ctrl.Finish()

		var lastRun, lastApplyRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				return nil
			})
		testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastApplyRun = run
				close(redisDoneCh)
				return nil
			})

		module := buildModule("hello-policy-stale", "hello-with-providers", false)
		// Override pinned to a different commit than the one being run, so it
		// must not be honored.
		req := tfaplv1beta1.Request{
			RequestedAt:    &metav1.Time{Time: time.Now()},
			Type:           tfaplv1beta1.ForcedApply,
			PolicyOverride: true,
			OverriddenHash: "deadbeef",
			OverriddenBy:   "integration-test",
			OverrideReason: "integration test stale override",
		}
		module.ObjectMeta.Annotations = map[string]string{
			tfaplv1beta1.RunRequestAnnotationKey: mustMarshalRequest(t, &req),
		}

		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.P5", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.P5").Return(fakeClient, nil)

		createModule(t, module)

		select {
		case <-redisDoneCh:
		case <-time.After(120 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetched := waitForState(t, "hello-policy-stale", string(tfaplv1beta1.StatusOverrideRequired))
		if fetched.Status.StateReason != tfaplv1beta1.ReasonSoftDenyViolation {
			t.Errorf("Expected reason %q, got %q", tfaplv1beta1.ReasonSoftDenyViolation, fetched.Status.StateReason)
		}
		if fetched.Status.LastAppliedCommitHash != "" {
			t.Errorf("Expected no applied commit, got %q", fetched.Status.LastAppliedCommitHash)
		}
		if lastRun.PolicyResult == nil {
			t.Fatal("Expected non-nil PolicyResult")
		}
		if len(lastRun.PolicyResult.SoftDenies) == 0 {
			t.Errorf("Expected soft denials, got %+v", lastRun.PolicyResult)
		}
		if lastRun.PolicyResult.Overridden {
			t.Error("Expected Overridden=false for stale override")
		}
		if strings.Contains(lastApplyRun.Output, "Apply complete!") {
			t.Error("Stale override must not apply")
		}
	})

	t.Run("soft deny on a plan-only run is advisory and does not block", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t, "hello-with-providers")
		defer ctrl.Finish()

		var lastRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				close(redisDoneCh) // plan-only: no apply key written
				return nil
			})

		module := buildModule("hello-policy-planonly", "hello-with-providers", false)
		module.Spec.AutoApply = new(false) // plan-only

		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.P6", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.P6").Return(fakeClient, nil)

		createModule(t, module)

		select {
		case <-redisDoneCh:
		case <-time.After(120 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetched := waitForState(t, "hello-policy-planonly", string(tfaplv1beta1.StatusDriftDetected))
		if fetched.Status.LastAppliedCommitHash != "" {
			t.Errorf("Expected no applied commit for plan-only, got %q", fetched.Status.LastAppliedCommitHash)
		}
		if lastRun.PolicyResult == nil {
			t.Fatal("Expected non-nil PolicyResult")
		}
		if lastRun.PolicyResult.Allowed {
			t.Error("Expected Allowed=false (soft deny recorded)")
		}
		if len(lastRun.PolicyResult.SoftDenies) == 0 {
			t.Errorf("Expected soft denials, got %+v", lastRun.PolicyResult)
		}
		// Plan-only runs record the denial but finish without failing.
		if lastRun.Status != tfaplv1beta1.StatusOk {
			t.Errorf("Expected plan-only run status OK, got %q", lastRun.Status)
		}
	})
}

// mustMarshalRequest serializes a run Request to the JSON stored in the module
// annotation, mirroring sysutil.EnsureRequest.
func mustMarshalRequest(t *testing.T, req *tfaplv1beta1.Request) string {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	return string(b)
}
