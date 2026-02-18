package integration_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestModuleController_NoRunner(t *testing.T) {
	const (
		moduleNamespace = "default"
	)

	setup := func(t *testing.T, runCh chan *tfaplv1beta1.Run) *gomock.Controller {
		ctrl := setupTest(t)

		// reset Time
		fakeClock.T = time.Date(2022, 02, 01, 01, 00, 00, 0000, time.UTC)
		testReconciler.Runner = testMockRunner2

		// remove any label selector
		testFilter.LabelSelectorKey = ""
		testFilter.LabelSelectorValue = ""

		testMetrics.EXPECT().SetRunPending(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

		// Mock Start to send to channel
		testMockRunner2.EXPECT().Start(gomock.Any(), gomock.Any()).
			DoAndReturn(func(run *tfaplv1beta1.Run, _ chan struct{}) bool {
				runCh <- run
				return true
			}).AnyTimes()

		return ctrl
	}

	t.Run("Should send module to job queue on schedule", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "test-module"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "dev/" + moduleName
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "terraform-applier.uw.systems/v1beta1",
				Kind:       "Module",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: moduleNamespace,
			},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "1 * * * *",
				RepoURL:  repoURL,
				Path:     path,
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

		moduleLookupKey := types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}

		// Absorb initial run
		select {
		case run := <-runCh:
			if run.Module != moduleLookupKey {
				t.Errorf("Expected run for %v, got %v", moduleLookupKey, run.Module)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for initial run")
		}

		// Setup status for next run
		module.Status.LastDefaultRunCommitHash = "CommitAbc123"
		module.Status.LastDefaultRunStartedAt = &metav1.Time{Time: time.Date(2022, 02, 01, 01, 00, 30, 0000, time.UTC)}
		if err := k8sClient.Status().Update(ctx, module); err != nil {
			t.Fatalf("Failed to update module status: %v", err)
		}

		testRepos.EXPECT().Hash(gomock.Any(), repoURL, "HEAD", path).Return("CommitAbc123", nil).Times(2)

		// Advance time but NOT enough for schedule (40s vs 60s)
		fakeClock.T = time.Date(2022, 02, 01, 01, 00, 40, 0000, time.UTC)
		triggerReconcile(ctx, k8sClient, moduleLookupKey) // Force check

		select {
		case <-runCh:
			t.Fatal("Run should NOT have triggered before schedule")
		case <-time.After(1 * time.Second):
			// Success
		}

		// Advance time PAST schedule
		fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)
		triggerReconcile(ctx, k8sClient, moduleLookupKey) // Force check

		select {
		case run := <-runCh:
			if run.Module != moduleLookupKey {
				t.Errorf("Expected run for %v, got %v", moduleLookupKey, run.Module)
			}
			if run.Request.Type != "ScheduledRun" {
				t.Errorf("Expected scheduled run for %s, got %v", moduleName, run.Request.Type)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for scheduled run")
		}
	})

	t.Run("Should send module to job queue on commit change", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "test-module2"
			repoURL    = "https://host.xy/dummy/repo2.git"
			path       = "dev/" + moduleName
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec:       tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
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

		// Absorb initial run
		select {
		case <-runCh:
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for initial run")
		}

		// add last run commit info
		module.Status.LastDefaultRunCommitHash = "CommitAbc123"
		if err := k8sClient.Status().Update(ctx, module); err != nil {
			t.Fatalf("Failed to update module status: %v", err)
		}

		// Commit changed
		testRepos.EXPECT().Hash(gomock.Any(), repoURL, "HEAD", path).Return("CommitAbc456", nil)

		triggerReconcile(ctx, k8sClient, moduleLookupKey)

		select {
		case run := <-runCh:
			if run.Module != moduleLookupKey {
				t.Errorf("Expected run for %v, got %v", moduleLookupKey, run.Module)
			}
			if run.Request.Type != "PollingRun" {
				t.Errorf("Expected PollingRun run for %s, got %v", moduleName, run.Request.Type)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for run after commit change")
		}
	})

	t.Run("Should not trigger run for module with invalid schedule", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "test-module3"
			repoURL    = "https://host.xy/dummy/repo.git"
			path       = "dev/" + moduleName
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec:       tfaplv1beta1.ModuleSpec{Schedule: "1 * * *", RepoURL: repoURL, Path: path},
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

		// Absorb initial run
		select {
		case <-runCh:
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for initial run")
		}

		module.Status.LastDefaultRunCommitHash = "CommitAbc123"
		if err := k8sClient.Status().Update(ctx, module); err != nil {
			t.Fatalf("Failed to update status: %v", err)
		}

		testRepos.EXPECT().Hash(gomock.Any(), repoURL, "HEAD", path).Return("CommitAbc123", nil)

		// Advance time
		fakeClock.T = time.Date(2022, 02, 01, 01, 01, 00, 0000, time.UTC)
		triggerReconcile(ctx, k8sClient, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace})

		// Check for Error state
		fetchedModule := &tfaplv1beta1.Module{}
		// Small retry loop for status update
		for range 20 {
			k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}, fetchedModule)
			if fetchedModule.Status.CurrentState == "Errored" {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if fetchedModule.Status.CurrentState != "Errored" {
			t.Errorf("Expected module state 'Errored', got %s", fetchedModule.Status.CurrentState)
		}
	})

	t.Run("Should not trigger run for module with git error", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "test-module4"
			repoURL    = "https://host.xy/dummy/repo2.git"
			path       = "dev/" + moduleName
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: moduleNamespace},
			Spec:       tfaplv1beta1.ModuleSpec{Schedule: "1 * * * *", RepoURL: repoURL, Path: path},
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
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for initial run")
		}

		module.Status.LastDefaultRunCommitHash = "CommitAbc123"
		module.Status.CurrentState = "OK"
		if err := k8sClient.Status().Update(ctx, module); err != nil {
			t.Fatalf("Failed to update status: %v", err)
		}

		testRepos.EXPECT().Hash(gomock.Any(), repoURL, "HEAD", path).Return("", fmt.Errorf("some git error"))

		fakeClock.T = time.Date(2022, 02, 01, 01, 02, 00, 0000, time.UTC)
		triggerReconcile(ctx, k8sClient, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace})

		// Check state remains OK
		time.Sleep(1 * time.Second) // Wait for potential update
		fetchedModule := &tfaplv1beta1.Module{}
		k8sClient.Get(ctx, types.NamespacedName{Name: moduleName, Namespace: moduleNamespace}, fetchedModule)

		if fetchedModule.Status.CurrentState != "OK" {
			t.Errorf("Expected module state to remain 'OK', got %s", fetchedModule.Status.CurrentState)
		}
	})

	t.Run("Should send module to job queue on pending run request", func(t *testing.T) {
		runCh := make(chan *tfaplv1beta1.Run, 1)
		ctrl := setup(t, runCh)
		defer ctrl.Finish()

		const (
			moduleName = "test-module5"
			repoURL    = "https://host.xy/dummy/repo2.git"
			path       = "dev/" + moduleName
		)

		ctx := context.Background()
		module := &tfaplv1beta1.Module{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: moduleNamespace,
				Annotations: map[string]string{
					tfaplv1beta1.RunRequestAnnotationKey: `{"id":"ueLMEbQj","reqAt":"2024-04-11T14:55:04Z","type":"ForcedPlan"}`,
				},
			},
			Spec: tfaplv1beta1.ModuleSpec{RepoURL: repoURL, Path: path},
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
		case run := <-runCh:
			if run.Module.Name != moduleName {
				t.Errorf("Expected run for %s, got %v", moduleName, run.Module)
			}
			if run.Request.Type != "ForcedPlan" {
				t.Errorf("Expected ForcedPlan run for %s, got %v", moduleName, run.Request.Type)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for forced run")
		}
	})

}
