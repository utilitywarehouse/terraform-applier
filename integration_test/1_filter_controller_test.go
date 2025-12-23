package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestModuleController_Filter(t *testing.T) {
	const (
		moduleNamespace         = "default"
		commitHash              = "a1b2c3d4"
		commitMsg               = "test commit"
		labelSelectorKey        = "terraform-applier.uw.systems/test"
		labelSelectorKeyInvalid = "terraform-applier.uw.systems/test-invalid"
	)

	setup := func(t *testing.T, runCh chan *tfaplv1beta1.Run) *gomock.Controller {
		ctrl := setupTest(t)

		// reset Time
		fakeClock.T = time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)
		testReconciler.Runner = testMockRunner1

		// add label selector
		testFilter.LabelSelectorKey = labelSelectorKey
		testFilter.LabelSelectorValue = "true"

		// Trigger Job run as soon as module is created
		testRepos.EXPECT().Hash(gomock.Any(), "https://host.xy/dummy/repo.git", "HEAD", "hello-filter-test").
			Return(commitHash, nil).AnyTimes()
		testRepos.EXPECT().Subject(gomock.Any(), "https://host.xy/dummy/repo.git", commitHash).
			Return(commitMsg, nil).AnyTimes()

		testMetrics.EXPECT().SetRunPending(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		// Configure Mock to send to channel
		testMockRunner1.EXPECT().Start(gomock.Any(), gomock.Any()).
			DoAndReturn(func(run *tfaplv1beta1.Run, _ chan struct{}) bool {
				runCh <- run
				return true
			}).AnyTimes()

		return ctrl
	}

	t.Run("Should send module with valid selector label selector to job queue", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "filter-test-module1"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello-filter-test"
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: moduleNamespace,
				Labels:    map[string]string{labelSelectorKey: "true"},
			},
			Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
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

		moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

		// Wait for run on channel
		select {
		case run := <-runCh:
			if run.Module != moduleLookupKey {
				t.Errorf("Expected run for %v, got %v", moduleLookupKey, run.Module)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout waiting for run of module %s", moduleName)
		}
	})

	t.Run("Should not send module with valid selector label key but invalid value to job queue", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "filter-test-module2"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello-filter-test"
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: moduleNamespace,
				Labels:    map[string]string{labelSelectorKey: "false"},
			},
			Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
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

		// Verify no run is triggered
		select {
		case <-runCh:
			t.Fatal("Run should NOT have been triggered")
		case <-time.After(2 * time.Second):
			// Success
		}
	})

	t.Run("Should not send module with missing selector label selector to job queue", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "filter-test-module3"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello-filter-test"
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: moduleNamespace,
				Labels:    map[string]string{labelSelectorKeyInvalid: "true"},
			},
			Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
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

		select {
		case <-runCh:
			t.Fatal("Run should NOT have been triggered")
		case <-time.After(2 * time.Second):
			// Success
		}
	})

	t.Run("Should not send module with no labels to job queue", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "filter-test-module4"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "hello-filter-test"
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: moduleNamespace,
			},
			Spec: tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
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

		select {
		case <-runCh:
			t.Fatal("Run should NOT have been triggered")
		case <-time.After(2 * time.Second):
			// Success
		}
	})
}
