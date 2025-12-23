package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/vault"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestModuleController_WithRunner(t *testing.T) {
	const (
		moduleNamespace = "default"
		commitHash      = "a1b2c3d4"
		commitMsg       = "test commit"
	)

	var (
		boolTrue = true
	)

	sbKeyringData, err := os.ReadFile(".tests_strongbox_keyring")
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	sbIdentityData, err := os.ReadFile(".tests_strongbox_identity")
	if err != nil {
		fmt.Println(err)
		panic(err)
	}

	// Wrapper to simplify per-test setup
	setup := func(t *testing.T) *gomock.Controller {
		ctrl := setupTest(t)

		fakeClock.T = time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)
		testReconciler.Runner = &testRunner

		// remove any label selector
		testFilter.LabelSelectorKey = ""
		testFilter.LabelSelectorValue = ""

		// all jobs will be triggered automatically as they do not have initial commit hash
		testRepos.EXPECT().Hash(gomock.Any(), "https://host.xy/dummy/repo.git", "HEAD", "hello").
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

	t.Run("Should send module to job queue on initial runner should do plan & apply", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t)
		defer ctrl.Finish()

		const (
			moduleName = "hello"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello"
		)
		var lastRun, lastApplyRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				return nil
			})
		testRedis.EXPECT().SetDefaultApply(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastApplyRun = run
				// Signal that Redis update (and thus runner) is done
				close(redisDoneCh)
				return nil
			})

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "50 * * * *",
				RepoURL:  repoURL,
				Path:     path,
				Env: []tfaplv1beta1.EnvVar{
					{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
					{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
				},
			},
		}
		if err := k8sClient.Create(ctx, module); err != nil {
			t.Fatalf("Failed to create module: %v", err)
		}
		// delete module to stopping requeue
		defer func() {
			if err := k8sClient.Delete(ctx, module); err != nil {
				t.Errorf("Failed to delete module: %v", err)
			}
		}()

		// Setup FakeDelegation
		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.X1", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X1").Return(fakeClient, nil)

		// Wait for completion via channel
		select {
		case <-redisDoneCh:
		case <-time.After(30 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		// Check final state
		fetchedModule := &tfaplv1beta1.Module{}
		// Small retry loop for final status consistency
		for i := 0; i < 20; i++ {
			k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}, fetchedModule)
			if fetchedModule.Status.CurrentState == string(tfaplv1beta1.StatusOk) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if fetchedModule.Status.CurrentState != string(tfaplv1beta1.StatusOk) {
			t.Errorf("Expected state 'StatusOk', got %s", fetchedModule.Status.CurrentState)
		}
		if !fetchedModule.Status.LastDefaultRunStartedAt.UTC().Equal(fakeClock.T.UTC()) {
			t.Errorf("Expected Start Time %v, got %v", fakeClock.T.UTC(), fetchedModule.Status.LastDefaultRunStartedAt.UTC())
		}

		if !strings.Contains(lastRun.Output, "Plan:") {
			t.Error("Expected Plan output")
		}
		if !strings.Contains(lastApplyRun.Output, "Apply complete!") {
			t.Error("Expected Apply output")
		}
		if !strings.Contains(lastApplyRun.Output, "TOP_SECRET_VALUE") {
			t.Error("Expected secret value in output")
		}
		if !strings.Contains(lastApplyRun.Output, "TOP_SECRET_VALUE_ENC_USING_AGE") {
			t.Error("Expected secret value in output")
		}
	})

	t.Run("Should send module to job queue on initial run and runner should only do plan", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t)
		defer ctrl.Finish()

		const (
			moduleName = "hello-plan-only"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello"
		)

		var lastRun *tfaplv1beta1.Run
		testRedis.EXPECT().SetDefaultLastRun(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, run *tfaplv1beta1.Run) error {
				lastRun = run
				close(redisDoneCh) // Only Plan runs
				return nil
			})

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "50 * * * *",
				RepoURL:  repoURL,
				Path:     path,
				PlanOnly: &boolTrue,
				Env: []tfaplv1beta1.EnvVar{
					{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
					{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
				},
			},
		}
		if err := k8sClient.Create(ctx, module); err != nil {
			t.Fatalf("Failed to create module: %v", err)
		}
		// delete module to stopping requeue
		defer func() {
			if err := k8sClient.Delete(ctx, module); err != nil {
				t.Errorf("Failed to delete module: %v", err)
			}
		}()

		// Setup FakeDelegation
		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.X2", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X2").Return(fakeClient, nil)

		select {
		case <-redisDoneCh:
		case <-time.After(30 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetchedModule := &tfaplv1beta1.Module{}
		for i := 0; i < 20; i++ {
			k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}, fetchedModule)
			if fetchedModule.Status.CurrentState == string(tfaplv1beta1.StatusDriftDetected) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if fetchedModule.Status.CurrentState != string(tfaplv1beta1.StatusDriftDetected) {
			t.Errorf("Expected state 'StatusDriftDetected', got %s", fetchedModule.Status.CurrentState)
		}
		if !strings.Contains(lastRun.Output, "Plan:") {
			t.Error("Expected Plan output")
		}
		if fetchedModule.Status.LastAppliedCommitHash != "" {
			t.Error("Expected no LastAppliedCommitHash for PlanOnly")
		}
	})

	t.Run("Should send module to job queue on initial run and runner should read configmaps and secrets before apply and setup local backend", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t)
		defer ctrl.Finish()

		const (
			moduleName = "hello-with-var-env"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello"
		)

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

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "50 * * * *",
				RepoURL:  repoURL,
				Path:     path,
				Backend: []tfaplv1beta1.EnvVar{
					{Name: "path", Value: testStateFilePath},
				},
				Env: []tfaplv1beta1.EnvVar{
					{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
					{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
					{Name: "TF_ENV_1", Value: "ENV-VALUE1"},
					{Name: "TF_ENV_2", ValueFrom: &tfaplv1beta1.EnvVarSource{
						ConfigMapKeyRef: &tfaplv1beta1.ConfigMapKeySelector{
							Name: "test-configmap", Key: "TF_ENV_2"},
					}},
					{Name: "TF_ENV_3", ValueFrom: &tfaplv1beta1.EnvVarSource{
						SecretKeyRef: &tfaplv1beta1.SecretKeySelector{
							Name: "test-secret", Key: "TF_ENV_3"},
					}},
				},
				Var: []tfaplv1beta1.EnvVar{
					{Name: "variable1", Value: "VAR-VALUE1"},
					{Name: "variable2", ValueFrom: &tfaplv1beta1.EnvVarSource{
						ConfigMapKeyRef: &tfaplv1beta1.ConfigMapKeySelector{
							Name: "test-configmap", Key: "variable2"},
					}},
					{Name: "variable3", ValueFrom: &tfaplv1beta1.EnvVarSource{
						SecretKeyRef: &tfaplv1beta1.SecretKeySelector{
							Name: "test-secret", Key: "variable3"},
					}},
				},
			},
		}
		if err := k8sClient.Create(ctx, module); err != nil {
			t.Fatalf("Failed to create module: %v", err)
		}
		// delete module to stopping requeue
		defer func() {
			if err := k8sClient.Delete(ctx, module); err != nil {
				t.Errorf("Failed to delete module: %v", err)
			}
		}()

		// Setup FakeDelegation with Secrets and ConfigMaps
		fakeClient := fake.NewSimpleClientset(
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-configmap", Namespace: moduleNamespace},
				Data: map[string]string{
					"variable2": "VAR-VALUE2",
					"TF_ENV_2":  "ENV-VALUE2",
				},
			},
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: moduleNamespace},
				Data: map[string][]byte{
					"variable3": []byte("VAR-VALUE3"),
					"TF_ENV_3":  []byte("ENV-VALUE3"),
				},
			},
		)
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.X3", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X3").Return(fakeClient, nil)

		select {
		case <-redisDoneCh:
		case <-time.After(30 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetchedModule := &tfaplv1beta1.Module{}
		for i := 0; i < 20; i++ {
			k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}, fetchedModule)
			if fetchedModule.Status.CurrentState == string(tfaplv1beta1.StatusOk) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if fetchedModule.Status.CurrentState != string(tfaplv1beta1.StatusOk) {
			t.Errorf("Expected state 'StatusOk', got %s", fetchedModule.Status.CurrentState)
		}
		if !strings.Contains(lastRun.Output, "Plan:") {
			t.Error("Expected Plan output")
		}

		// Verify Env and Vars in output
		outputs := []string{
			"TOP_SECRET_VALUE",
			"TOP_SECRET_VALUE_ENC_USING_AGE",
			"ENV-VALUE1",
			"ENV-VALUE2",
			"ENV-VALUE3",
			"VAR-VALUE1",
			"VAR-VALUE2",
			"VAR-VALUE3",
		}
		for _, s := range outputs {
			if !strings.Contains(lastApplyRun.Output, s) {
				t.Errorf("Expected output to contain '%s'", s)
			}
		}

		// Verify state file creation
		if _, err := os.Stat(testStateFilePath); os.IsNotExist(err) {
			t.Error("Expected state file to exist")
		}
	})

	t.Run("Should send module to job queue on initial run and runner should generate vault creds", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t)
		defer ctrl.Finish()

		const (
			moduleName = "hello-with-vault-creds"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello"
		)

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

		ctx := context.Background()
		vaultReq := tfaplv1beta1.VaultRequests{
			AWS: &tfaplv1beta1.VaultAWSRequest{VaultRole: "aws-vault-role"},
			GCP: &tfaplv1beta1.VaultGCPRequest{StaticAccount: "gcp-vault-static-account"},
		}
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule:      "50 * * * *",
				RepoURL:       repoURL,
				Path:          path,
				VaultRequests: &vaultReq,
				Env: []tfaplv1beta1.EnvVar{
					{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
					{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
				},
			},
		}
		if err := k8sClient.Create(ctx, module); err != nil {
			t.Fatalf("Failed to create module: %v", err)
		}
		// delete module to stopping requeue
		defer func() {
			if err := k8sClient.Delete(ctx, module); err != nil {
				t.Errorf("Failed to delete module: %v", err)
			}
		}()

		// Setup FakeDelegation and Vault Mocks
		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.X4", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X4").Return(fakeClient, nil)

		testVaultAWSConf.EXPECT().GenerateAWSCreds(gomock.Any(), "token.X4", gomock.Any()).
			Return(&vault.AWSCredentials{AccessKeyID: "AWS_KEY_ABCD1234", SecretAccessKey: "secret", Token: "token"}, nil)
		testVaultAWSConf.EXPECT().GenerateGCPToken(gomock.Any(), "token.X4", gomock.Any()).
			Return("ya29.c.c0ASRK0GZ2fzoXHQakYwhwQhSJZ3gFQT5V0Ro_E94zL3fo", nil)

		select {
		case <-redisDoneCh:
		case <-time.After(30 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetchedModule := &tfaplv1beta1.Module{}
		for i := 0; i < 20; i++ {
			k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}, fetchedModule)
			if fetchedModule.Status.CurrentState == string(tfaplv1beta1.StatusOk) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if fetchedModule.Status.CurrentState != string(tfaplv1beta1.StatusOk) {
			t.Errorf("Expected state 'StatusOk', got %s", fetchedModule.Status.CurrentState)
		}

		// Verify AWS creds in output
		if !strings.Contains(lastRun.Output, "AWS_KEY_ABCD1234") {
			t.Error("Expected AWS Access Key in plan output")
		}
		if !strings.Contains(lastApplyRun.Output, "AWS_KEY_ABCD1234") {
			t.Error("Expected AWS Access Key in apply output")
		}
	})

	t.Run("Should send module to job queue on initial run and runner should generate vault creds using run-as-serviceAccount", func(t *testing.T) {
		redisDoneCh := make(chan struct{})
		ctrl := setup(t)
		defer ctrl.Finish()

		const (
			moduleName = "hello-with-run-as-sa-vault-creds"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello"
			runAsSA    = "tf-applier-run-as-sa"
		)

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

		ctx := context.Background()
		vaultReq := tfaplv1beta1.VaultRequests{
			AWS: &tfaplv1beta1.VaultAWSRequest{VaultRole: "aws-vault-role"},
			GCP: &tfaplv1beta1.VaultGCPRequest{StaticAccount: "gcp-vault-static-account"},
		}
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule:            "50 * * * *",
				RepoURL:             repoURL,
				Path:                path,
				VaultRequests:       &vaultReq,
				RunAsServiceAccount: runAsSA,
				Env: []tfaplv1beta1.EnvVar{
					{Name: "TF_APPLIER_STRONGBOX_KEYRING", Value: string(sbKeyringData)},
					{Name: "TF_APPLIER_STRONGBOX_IDENTITY", Value: string(sbIdentityData)},
					{Name: "TF_ENV_3", ValueFrom: &tfaplv1beta1.EnvVarSource{
						SecretKeyRef: &tfaplv1beta1.SecretKeySelector{
							Name: "test-secret", Key: "TF_ENV_3"},
					}},
				},
			},
		}
		if err := k8sClient.Create(ctx, module); err != nil {
			t.Fatalf("Failed to create module: %v", err)
		}
		// delete module to stopping requeue
		defer func() {
			if err := k8sClient.Delete(ctx, module); err != nil {
				t.Errorf("Failed to delete module: %v", err)
			}
		}()

		// Setup FakeDelegation
		fakeClient := fake.NewSimpleClientset()
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, "terraform-applier-delegate").Return("token.X4", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X4").Return(fakeClient, nil)

		// Setup FakeDelegation for run-as-sa
		runAsfakeClient := fake.NewSimpleClientset(
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: moduleNamespace},
				Data: map[string][]byte{
					"TF_ENV_3": []byte("ENV-VALUE3"),
				},
			},
		)
		// runner should use delegated token to fetch run-as-sa token
		testDelegate.EXPECT().DelegateToken(gomock.Any(), gomock.Any(), moduleNamespace, runAsSA).Return("token.X4_run_as", nil)
		testDelegate.EXPECT().SetupDelegation(gomock.Any(), "token.X4_run_as").Return(runAsfakeClient, nil)

		testVaultAWSConf.EXPECT().GenerateAWSCreds(gomock.Any(), "token.X4_run_as", gomock.Any()).
			Return(&vault.AWSCredentials{AccessKeyID: "AWS_KEY_ABCD1234", SecretAccessKey: "secret", Token: "token"}, nil)
		testVaultAWSConf.EXPECT().GenerateGCPToken(gomock.Any(), "token.X4_run_as", gomock.Any()).
			Return("ya29.c.c0ASRK0GZ2fzoXHQakYwhwQhSJZ3gFQT5V0Ro_E94zL3fo", nil)

		select {
		case <-redisDoneCh:
		case <-time.After(30 * time.Second):
			t.Fatal("Timeout waiting for runner to complete")
		}

		fetchedModule := &tfaplv1beta1.Module{}
		for i := 0; i < 20; i++ {
			k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}, fetchedModule)
			if fetchedModule.Status.CurrentState == string(tfaplv1beta1.StatusOk) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if fetchedModule.Status.CurrentState != string(tfaplv1beta1.StatusOk) {
			t.Errorf("Expected state 'StatusOk', got %s", fetchedModule.Status.CurrentState)
		}

		// Verify output includes AWS Key and Env Value retrieved via run-as-sa client
		if !strings.Contains(lastRun.Output, "AWS_KEY_ABCD1234") {
			t.Error("Expected AWS Access Key in plan output")
		}
		if !strings.Contains(lastApplyRun.Output, "AWS_KEY_ABCD1234") {
			t.Error("Expected AWS Access Key in apply output")
		}
		if !strings.Contains(lastApplyRun.Output, "ENV-VALUE3") {
			t.Error("Expected Env Value from Secret in apply output")
		}
	})
}
