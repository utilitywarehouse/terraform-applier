package runner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newRetryTestRunner(ctrl *gomock.Controller, runRetries int) (*Runner, *MockTFExecuter, *tfaplv1beta1.Run, *tfaplv1beta1.Module) {
	te := NewMockTFExecuter(ctrl)

	run := &tfaplv1beta1.Run{
		Module:    types.NamespacedName{Namespace: "sys-vault", Name: "backends"},
		Request:   &tfaplv1beta1.Request{Type: tfaplv1beta1.PRPlan},
		Mode:      tfaplv1beta1.ModePlanOnly,
		StartedAt: &metav1.Time{Time: time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)},
	}
	module := &tfaplv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: "sys-vault", Name: "backends"},
	}

	r := &Runner{
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:         &sysutil.FakeClock{T: time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)},
		Recorder:      record.NewFakeRecorder(10),
		RunRetries:    runRetries,
		RunRetryDelay: time.Millisecond,
	}
	return r, te, run, module
}

func TestRunTFRetriesInitOnTransientError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	r, te, run, module := newRetryTestRunner(ctrl, 3)

	// first attempt fails at init (transient), second attempt succeeds
	te.EXPECT().init(gomock.Any(), gomock.Any()).Return("", errors.New("secrets is forbidden")).Times(1)
	te.EXPECT().init(gomock.Any(), gomock.Any()).Return("Initialised successfully", nil).Times(1)
	te.EXPECT().plan(gomock.Any()).Return(false, "No changes. Your infrastructure matches the configuration.", nil).Times(1)
	te.EXPECT().showPlanFileRaw(gomock.Any()).Return("plan output", nil).Times(1)

	if ok := r.runTF(context.Background(), run, module, te, nil, "commit", make(chan struct{})); !ok {
		t.Fatal("expected run to succeed after retry, got failure")
	}
	if run.Status != tfaplv1beta1.StatusOk {
		t.Errorf("expected run status %q, got %q", tfaplv1beta1.StatusOk, run.Status)
	}
}

func TestRunTFFailsAfterRetriesExhausted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	r, te, run, module := newRetryTestRunner(ctrl, 3)

	// init keeps failing, all attempts are consumed
	te.EXPECT().init(gomock.Any(), gomock.Any()).Return("", errors.New("secrets is forbidden")).Times(3)

	if ok := r.runTF(context.Background(), run, module, te, nil, "commit", make(chan struct{})); ok {
		t.Fatal("expected run to fail after retries exhausted, got success")
	}
	if run.Status != tfaplv1beta1.StatusErrored {
		t.Errorf("expected run status %q, got %q", tfaplv1beta1.StatusErrored, run.Status)
	}
	// the underlying error must be surfaced in the failed run output
	if !strings.Contains(run.Output, "secrets is forbidden") {
		t.Errorf("expected underlying error in run output, got %q", run.Output)
	}
}

func TestRunTFDoesNotRetryAfterShutdown(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	r, te, run, module := newRetryTestRunner(ctrl, 5)

	// only one attempt is made: the loop must not retry after cancellation
	te.EXPECT().init(gomock.Any(), gomock.Any()).Return("", errors.New("secrets is forbidden")).Times(1)

	cancelChan := make(chan struct{})
	close(cancelChan)

	if ok := r.runTF(context.Background(), run, module, te, nil, "commit", cancelChan); ok {
		t.Fatal("expected run to fail after shutdown, got success")
	}
	if run.Status != tfaplv1beta1.StatusErrored {
		t.Errorf("expected run status %q, got %q", tfaplv1beta1.StatusErrored, run.Status)
	}
}

func TestRunTFRetriesApplyOnTransientError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sch := runtime.NewScheme()
	if err := tfaplv1beta1.AddToScheme(sch); err != nil {
		t.Fatalf("unable to add scheme: %v", err)
	}
	module := &tfaplv1beta1.Module{
		TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
		ObjectMeta: metav1.ObjectMeta{Namespace: "sys-vault", Name: "backends"},
		Spec:       tfaplv1beta1.ModuleSpec{AutoApply: new(true)},
	}
	clt := fake.NewClientBuilder().WithScheme(sch).WithObjects(module).WithStatusSubresource(module).Build()

	te := NewMockTFExecuter(ctrl)
	te.EXPECT().init(gomock.Any(), gomock.Any()).Return("Initialised successfully", nil).Times(2)
	te.EXPECT().plan(gomock.Any()).Return(false, "No changes. Your infrastructure matches the configuration.", nil).Times(2)
	te.EXPECT().showPlanFileRaw(gomock.Any()).Return("plan output", nil).Times(2)
	// first apply fails transiently, second succeeds
	te.EXPECT().apply(gomock.Any()).Return("", errors.New("secrets is forbidden")).Times(1)
	te.EXPECT().apply(gomock.Any()).Return("Apply complete! Resources: 0 added, 0 changed, 0 destroyed", nil).Times(1)

	run := &tfaplv1beta1.Run{
		Module:    types.NamespacedName{Namespace: "sys-vault", Name: "backends"},
		Request:   &tfaplv1beta1.Request{Type: tfaplv1beta1.ForcedApply},
		Mode:      tfaplv1beta1.ModeApply,
		StartedAt: &metav1.Time{Time: time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)},
	}
	r := &Runner{
		ClusterClt:    clt,
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:         &sysutil.FakeClock{T: time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)},
		Recorder:      record.NewFakeRecorder(10),
		RunRetries:    3,
		RunRetryDelay: time.Millisecond,
	}

	if ok := r.runTF(context.Background(), run, module, te, nil, "commit", make(chan struct{})); !ok {
		t.Fatal("expected run to succeed after apply retry, got failure")
	}
	if run.Status != tfaplv1beta1.StatusOk {
		t.Errorf("expected run status %q, got %q", tfaplv1beta1.StatusOk, run.Status)
	}
}

func TestWaitForRunRetryAbortsOnCancellation(t *testing.T) {
	r := &Runner{RunRetryDelay: time.Hour}

	cancelChan := make(chan struct{})
	done := make(chan bool)
	go func() {
		done <- r.waitForRunRetry(context.Background(), cancelChan, 1)
	}()
	close(cancelChan)

	select {
	case ok := <-done:
		if ok {
			t.Error("expected waitForRunRetry to return false on cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("waitForRunRetry did not abort on cancellation")
	}
}
