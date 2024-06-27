package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/apimachinery/pkg/types"
)

func mustParseTime(str string) *time.Time {
	at, err := time.Parse(time.RFC3339, str)
	if err != nil {
		panic(err)
	}
	return &at
}

func Test_requestAcknowledgedCommentInfo(t *testing.T) {
	type args struct {
		commentBody string
	}
	tests := []struct {
		name       string
		args       args
		wantModule types.NamespacedName
		wantReqAt  *time.Time
	}{
		{
			name:       "Empty string",
			args:       args{commentBody: ""},
			wantModule: types.NamespacedName{},
			wantReqAt:  nil,
		},
		{
			name:       "NamespacedName + Requested At",
			args:       args{commentBody: "Received terraform plan request. Module: `foo/one` Requested At: `2006-01-02T15:04:05+07:00`"},
			wantModule: types.NamespacedName{Namespace: "foo", Name: "one"},
			wantReqAt:  mustParseTime("2006-01-02T15:04:05+07:00"),
		},
		{
			name:       "NamespacedName + Requested At UTC",
			args:       args{commentBody: "Received terraform plan request. Module: `foo/one` Requested At: `2023-04-02T15:04:05Z`"},
			wantModule: types.NamespacedName{Namespace: "foo", Name: "one"},
			wantReqAt:  mustParseTime("2023-04-02T15:04:05Z"),
		},
		{
			name:       "Name + Requested At",
			args:       args{commentBody: "Received terraform plan request. Module: `one` Requested At: `2023-04-02T15:04:05Z`"},
			wantModule: types.NamespacedName{Name: "one"},
			wantReqAt:  mustParseTime("2023-04-02T15:04:05Z"),
		},
		{
			name:       "missing Requested At",
			args:       args{commentBody: "Received terraform plan request. Module: `foo/one` Requested At: ``"},
			wantModule: types.NamespacedName{Namespace: "foo", Name: "one"},
			wantReqAt:  nil,
		},
		{
			name:       "Missing module",
			args:       args{commentBody: "Received terraform plan request. Module: `` Requested At: `2006-01-02T15:04:05+07:00`"},
			wantModule: types.NamespacedName{},
			wantReqAt:  nil,
		},
		{
			name:       "Terraform plan output for module",
			args:       args{commentBody: "Terraform plan output for module `foo/one`"},
			wantModule: types.NamespacedName{},
			wantReqAt:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModule, gotReqAt := requestAcknowledgedCommentInfo(tt.args.commentBody)
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("requestAcknowledgedCommentInfo() gotModule = %v, want %v", gotModule, tt.wantModule)
			}
			if diff := cmp.Diff(tt.wantReqAt, gotReqAt); diff != "" {
				t.Errorf("requestAcknowledgedCommentInfo() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_checkPRCommentForOutputRequests(t *testing.T) {
	ctx := context.Background()
	goMockCtrl := gomock.NewController(t)

	planner := &Planner{
		Log: slog.Default(),
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	t.Run("terraform plan output comment", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		planner.RedisClient = testRedis

		comment := prComment{
			Body: fmt.Sprintf(outputBodyTml, "foo/two", "hash1", "terraform plan output"),
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

	t.Run("empty commit id", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		planner.RedisClient = testRedis

		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedTml, "foo/two", "2023-04-02T15:04:05Z"),
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

	t.Run("key not found in redis", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		planner.RedisClient = testRedis

		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedTml, "foo/two", "2023-04-02T15:04:05Z"),
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash1").
			Return(nil, sysutil.ErrKeyNotFound)

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
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		planner.RedisClient = testRedis

		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedTml, "foo/two", "2023-04-02T15:04:05Z"),
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash1").
			Return(&v1beta1.Run{Output: ""}, nil)

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
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		planner.RedisClient = testRedis

		comment := prComment{
			Body:       fmt.Sprintf(requestAcknowledgedTml, "foo/two", "2023-04-02T15:04:05Z"),
			DatabaseID: 111,
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash1").
			Return(&v1beta1.Run{CommitHash: "hash1", Output: "terraform plan output"}, nil)

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, comment)

		wantOut := prComment{Body: fmt.Sprintf(
			outputBodyTml, types.NamespacedName{Namespace: "foo", Name: "two"},
			"hash1", "terraform plan output"),
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
