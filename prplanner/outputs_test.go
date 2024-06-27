package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func mustParseTime(str string) *time.Time {
	at, err := time.Parse(time.RFC3339, str)
	if err != nil {
		panic(err)
	}
	return &at
}

func mustParseMetaTime(str string) *metav1.Time {
	at, err := time.Parse(time.RFC3339, str)
	if err != nil {
		panic(err)
	}
	return &metav1.Time{Time: at}
}

func Test_checkPRCommentForOutputRequests(t *testing.T) {
	ctx := context.Background()
	goMockCtrl := gomock.NewController(t)
	testRedis := sysutil.NewMockRedisInterface(goMockCtrl)

	planner := &Planner{
		Log:         slog.Default(),
		RedisClient: testRedis,
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	mockRuns := []*v1beta1.Run{
		{Request: &v1beta1.Request{RequestedAt: mustParseMetaTime("2023-04-02T15:01:05Z")}, CommitHash: "hash1", Output: "terraform plan output"},
		{Request: &v1beta1.Request{RequestedAt: mustParseMetaTime("2023-04-02T15:02:05Z")}},
		{Request: &v1beta1.Request{RequestedAt: mustParseMetaTime("2023-04-02T15:04:05Z")}, CommitHash: "hash1", Module: types.NamespacedName{Namespace: "foo", Name: "bar"}, Summary: "plan summary", Output: "terraform plan output"},
	}

	testRedis.EXPECT().Runs(gomock.Any(), types.NamespacedName{Namespace: "foo", Name: "two"}).
		Return(mockRuns, nil).AnyTimes()

	t.Run("terraform plan output comment", func(t *testing.T) {
		comment := prComment{
			Body: runOutputMsg("foo/two", "foo/two", &v1beta1.Run{CommitHash: "hash1", Summary: "Plan: x to add, x to change, x to destroy.", Output: "terraform plan output"}),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty request time", func(t *testing.T) {
		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedMsgTml, "foo/two", ""),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("run not found in redis", func(t *testing.T) {
		comment := prComment{
			Body: requestAcknowledgedMsg("foo/two", "foo/two", mustParseMetaTime("2023-04-02T15:03:05Z")),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty run output in redis", func(t *testing.T) {
		comment := prComment{
			Body: requestAcknowledgedMsg("foo/two", "foo/two", mustParseMetaTime("2023-04-02T15:02:05Z")),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("plan output ready in redis", func(t *testing.T) {
		comment := prComment{
			Body:       requestAcknowledgedMsg("foo/two", "module/path/is/going/to/be/here", mustParseMetaTime("2023-04-02T15:04:05Z")),
			DatabaseID: 111,
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{
			Body: runOutputMsg("foo/two", "module/path/is/going/to/be/here", &v1beta1.Run{CommitHash: "hash1", Summary: "plan summary", Output: "terraform plan output"}),
		}
		wantOk := true

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})
}
