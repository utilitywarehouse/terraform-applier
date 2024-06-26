package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/apimachinery/pkg/types"
)

func Test_findOutputRequestDataInComment(t *testing.T) {
	type args struct {
		commentBody string
	}
	tests := []struct {
		name       string
		args       args
		wantModule types.NamespacedName
		wantCommit string
	}{
		{
			name:       "NamespacedName + Request ID + Commit ID",
			args:       args{commentBody: "Received terraform plan request. Module: `terraform/my-module` Request ID: `a1b2c3d4` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{Namespace: "terraform", Name: "my-module"},
			wantCommit: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
		},
		{
			name:       "Missing Commit ID",
			args:       args{commentBody: "Received terraform plan request. Module: `terraform/my-module` Request ID: `a1b2c3d4` Commit ID: ``"},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Missing module",
			args:       args{commentBody: "Received terraform plan request. Module: `` Request ID: `a1b2c3d4` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Missing Request ID",
			args:       args{commentBody: "Received terraform plan request. Module: `my-module` Request ID: `` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Terraform plan output for module",
			args:       args{commentBody: "Terraform plan output for module `terraform/my-module` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Empty string",
			args:       args{commentBody: ""},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModule, gotCommit := findOutputRequestDataInComment(tt.args.commentBody)
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("findOutputRequestDataInComment() gotModule = %v, want %v", gotModule, tt.wantModule)
			}
			if gotCommit != tt.wantCommit {
				t.Errorf("findOutputRequestDataInComment() gotCommit = %v, want %v", gotCommit, tt.wantCommit)
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
		pr := generateMockPR(123, "ref1", []string{}, []string{}, nil)

		comment := prComment{
			Body: fmt.Sprintf(outputBodyTml, "foo/two", "hash1", "terraform plan output"),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, pr, comment)

		wantOut := output{}
		wantOk := false

		if diff := cmp.Diff(wantOut, gotOut, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("empty commit id", func(t *testing.T) {
		pr := generateMockPR(123, "ref1", []string{}, []string{}, nil)

		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedTml, "foo/two", "reqID1", ""),
		}

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, pr, comment)

		wantOut := output{}
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

		pr := generateMockPR(123, "ref1", []string{}, []string{}, nil)

		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedTml, "foo/two", "reqID1", "hash1"),
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash1").
			Return(nil, sysutil.ErrKeyNotFound)

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, pr, comment)

		wantOut := output{}
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

		pr := generateMockPR(123, "ref1", []string{}, []string{}, nil)

		comment := prComment{
			Body: fmt.Sprintf(requestAcknowledgedTml, "foo/two", "reqID1", "hash1"),
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash1").
			Return(&v1beta1.Run{Output: ""}, nil)

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, pr, comment)

		wantOut := output{}
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

		pr := generateMockPR(123, "ref1", []string{}, []string{}, nil)

		comment := prComment{
			Body:       fmt.Sprintf(requestAcknowledgedTml, "foo/two", "reqID1", "hash1"),
			DatabaseID: 111,
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash1").
			Return(&v1beta1.Run{CommitHash: "hash1", Output: "terraform plan output"}, nil)

		gotOut, gotOk := planner.checkPRCommentForOutputRequests(ctx, pr, comment)

		wantOut := output{
			Module: types.NamespacedName{Namespace: "foo", Name: "two"},
			Body: prComment{Body: fmt.Sprintf(
				outputBodyTml, types.NamespacedName{Namespace: "foo", Name: "two"},
				"hash1", "terraform plan output")},
			CommentID: 111,
			PrNumber:  123,
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
