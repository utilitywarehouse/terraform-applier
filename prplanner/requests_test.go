package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/utilitywarehouse/git-mirror/repository"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var cmpIgnoreRandFields = cmpopts.IgnoreFields(tfaplv1beta1.Request{}, "RequestedAt")

func generateMockPR(num int, ref string, comments []string) *pr {
	p := &pr{
		Number:      num,
		HeadRefName: ref,
	}

	for _, v := range comments {
		p.Comments.Nodes = append(p.Comments.Nodes, prComment{1, author{}, v})
	}

	return p
}

func TestCheckPRCommits(t *testing.T) {
	ctx := context.Background()
	goMockCtrl := gomock.NewController(t)

	testGit := git.NewMockRepositories(goMockCtrl)

	planner := &Planner{
		ClusterEnvName: "default",
		Repos:          testGit,
		Log:            slog.Default(),
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	t.Run("generate req for updated module", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)

		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "one"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/one",
			},
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "one"}, 123, "hash1").
			Return(nil, sysutil.ErrKeyNotFound)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		wantReq := &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("multiple commit updating a module", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)

		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash3").
			Return(nil, sysutil.ErrKeyNotFound)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		wantReq := &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("module path is not updated", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", runOutputMsg("default", "foo/two", "foo/two", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "some output"}), "random comment"},
		)

		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "one"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/one",
			},
		}

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq *tfaplv1beta1.Request

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("module output is already uploaded", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", runOutputMsg("default", "foo/two", "foo/two", &tfaplv1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "some output"}), "random comment"},
		)
		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq *tfaplv1beta1.Request

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("module output uploaded by diff cluster", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", runOutputMsg("diff-cluster", "foo/two", "foo/two", &tfaplv1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "some output"}), "random comment"},
		)
		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash3").
			Return(nil, sysutil.ErrKeyNotFound)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq = &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("module run request is pending", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", requestAcknowledgedMsg("default", "foo/two", "foo/two", "hash3", &metav1.Time{Time: time.Now()}), "random comment"},
		)
		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq *tfaplv1beta1.Request

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("module run request is pending by diff cluster", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", requestAcknowledgedMsg("diff-cluster", "foo/two", "foo/two", "hash3", &metav1.Time{Time: time.Now()}), "random comment"},
		)
		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash3").
			Return(nil, sysutil.ErrKeyNotFound)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq = &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("old commit run output uploaded and new commit added", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", runOutputMsg("default", "foo/two", "foo/two", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "some output"}), "random comment"},
		)
		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// mock db call with no result found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash3").
			Return(nil, sysutil.ErrKeyNotFound)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		wantReq := &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("module run finished but output is not yet uploaded", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)
		commitsInfo := []repository.CommitInfo{
			{Hash: "hash3", ChangedFiles: []string{"foo/two", "foo/three"}},
			{Hash: "hash2", ChangedFiles: []string{"foo/two"}},
			{Hash: "hash1", ChangedFiles: []string{"foo/one"}},
		}

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// mock db call with output found
		testRedis.EXPECT().PRRun(gomock.Any(),
			types.NamespacedName{Namespace: "foo", Name: "two"}, 123, "hash3").
			Return(&tfaplv1beta1.Run{CommitHash: "hash3"}, nil)

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, commitsInfo, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq *tfaplv1beta1.Request

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_checkPRCommentsForPlanRequests(t *testing.T) {
	goMockCtrl := gomock.NewController(t)

	testGit := git.NewMockRepositories(goMockCtrl)

	planner := &Planner{
		ClusterEnvName: "default",
		Repos:          testGit,
		Log:            slog.Default(),
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	module := &tfaplv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
		Spec: tfaplv1beta1.ModuleSpec{
			RepoURL: "https://github.com/owner-a/repo-a.git",
			Path:    "path/foo/two",
		},
	}

	t.Run("request acknowledged for module (using name)", func(t *testing.T) {
		// avoid generating another request from `@terraform-applier plan` comment
		// is there's already a request ID posted for the module
		// module might not be annotated by the time the loop checks it, which in this
		// case would mean plan out is ready ot be posted and NOT run hasn't been requested yet
		pr := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan two",
				requestAcknowledgedMsg("default", "foo/two", "path/foo/two", "hash2", mustParseMetaTime("2023-04-02T15:04:05Z")),
				requestAcknowledgedMsg("default", "foo/three", "path/foo/three", "hash3", mustParseMetaTime("2023-04-02T15:04:05Z")),
			},
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("request acknowledged for module (using path)", func(t *testing.T) {
		// avoid generating another request from `@terraform-applier plan` comment
		// is there's already a request ID posted for the module
		// module might not be annotated by the time the loop checks it, which in this
		// case would mean plan out is ready ot be posted and NOT run hasn't been requested yet
		pr := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan path/foo/two",
				requestAcknowledgedMsg("default", "foo/two", "path/foo/two", "hash2", mustParseMetaTime("2023-04-02T15:04:05Z")),
				requestAcknowledgedMsg("default", "foo/three", "path/foo/three", "hash3", mustParseMetaTime("2023-04-02T15:04:05Z")),
			},
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan out posted for module (by name)", func(t *testing.T) {
		pr := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan two",
				runOutputMsg("default", "foo/two", "path/foo/two", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
				runOutputMsg("default", "foo/three", "path/foo/three", &tfaplv1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
			},
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan out posted for module (by path)", func(t *testing.T) {
		pr := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan path/foo/two",
				runOutputMsg("default", "foo/two", "path/foo/two", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
				runOutputMsg("default", "foo/three", "path/foo/three", &tfaplv1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
			},
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan run is not requested for current module", func(t *testing.T) {
		pr := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan one",
				"@terraform-applier plan path/foo/one",
				"@terraform-applier plan path/foo/three",
			},
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan run is requested for module using correct module path", func(t *testing.T) {
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		p := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan path/foo/two",
				"@terraform-applier plan foo/three",
			},
		)

		testGit.EXPECT().Hash(gomock.Any(), gomock.Any(), "ref1", "path/foo/two").
			Return("hash1", nil)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		wantReq := &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentsForPlanRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("plan run is requested for module using correct Name", func(t *testing.T) {
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		p := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan two",
				"@terraform-applier plan three",
			},
		)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		testGit.EXPECT().Hash(gomock.Any(), gomock.Any(), "ref1", "path/foo/two").
			Return("hash1", nil)

		// Call Test function
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		wantReq := &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentsForPlanRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("request acknowledged for module by diff cluster", func(t *testing.T) {
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		// avoid generating another request from `@terraform-applier plan` comment
		// is there's already a request ID posted for the module
		// module might not be annotated by the time the loop checks it, which in this
		// case would mean plan out is ready ot be posted and NOT run hasn't been requested yet
		pr := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan two",
				requestAcknowledgedMsg("diff-cluster", "foo/two", "path/foo/two", "hash2", mustParseMetaTime("2023-04-02T15:04:05Z")),
				requestAcknowledgedMsg("default", "foo/three", "path/foo/three", "hash3", mustParseMetaTime("2023-04-02T15:04:05Z")),
			},
		)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		testGit.EXPECT().Hash(gomock.Any(), gomock.Any(), "ref1", "path/foo/two").
			Return("hash1", nil)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		wantReq := &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentsForPlanRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("plan out posted for module by diff cluster", func(t *testing.T) {
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		pr := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan two",
				runOutputMsg("diff-cluster", "foo/two", "path/foo/two", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
				runOutputMsg("default", "foo/three", "path/foo/three", &tfaplv1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
			},
		)

		// mock github API Call adding new request info
		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedMsgRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		testGit.EXPECT().Hash(gomock.Any(), gomock.Any(), "ref1", "path/foo/two").
			Return("hash1", nil)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		wantReq := &tfaplv1beta1.Request{
			Type: "PullRequestPlan",
			PR: &tfaplv1beta1.PullRequest{
				Number:     123,
				HeadBranch: "ref1",
				CommentID:  111,
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentsForPlanRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("plan run is requested for module in different Namespace or diff path", func(t *testing.T) {
		p := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan bar/two",
				"@terraform-applier plan path/bar/two",
				"@terraform-applier plan path/foo",
				"@terraform-applier plan foo/three",
			},
		)

		// Call Test function
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan run is requested for module using correct Name with a random suffix", func(t *testing.T) {
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		p := generateMockPR(123, "ref1",
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan two please",
				"@terraform-applier plan three",
			},
		)

		// Call Test function
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})
}

func Test_isPlanOutputPostedForCommit(t *testing.T) {
	type args struct {
		cluster    string
		pr         *pr
		commitID   string
		modulePath string
		module     types.NamespacedName
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// func isPlanOutputPostedForCommit(pr *pr, commitID string, module types.NamespacedName) bool {
		{
			name: "Matching NamespacedName and Commit ID",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       runOutputMsg("default", "foo/one", "foo/one", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: true,
		},
		{
			name: "Matching NamespacedName and Commit ID - diff cluster",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       runOutputMsg("diff-cluster", "foo/one", "foo/one", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
		{
			name: "Matching Name and Commit ID",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       runOutputMsg("default", "one", "foo/one", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "foo/one",
				module:     types.NamespacedName{Name: "one"},
			},
			want: true,
		},
		{
			name: "Wrong Commit ID",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "Terraform plan output for module `foo/one` Commit ID: `hash2`",
					},
				}}},
				cluster:  "default",
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3aaaa",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
		{
			name: "Wrong Namespace",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "Terraform plan output for module `bar/one` Commit ID: `hash2`",
					},
				}}},
				cluster:  "default",
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
		{
			name: "Received terraform plan request",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "Received terraform plan request. Module: `foo/one` Request ID: `a1b2c3d4` Commit ID: `hash2`",
					},
				}}},
				cluster:  "default",
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
		{
			name: "Empty string",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "",
					},
				}}},
				cluster:  "default",
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlanOutputPostedForCommit(tt.args.cluster, tt.args.pr, tt.args.commitID, tt.args.modulePath, tt.args.module); got != tt.want {
				t.Errorf("isPlanOutputPostedForCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isPlanRequestAckPostedForCommit(t *testing.T) {
	type args struct {
		cluster    string
		pr         *pr
		commitID   string
		modulePath string
		module     types.NamespacedName
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "Empty string",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "",
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		}, {
			name: "Matching NamespacedName and Commit ID and req is current",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       requestAcknowledgedMsg("default", "foo/one", "foo/one", "hash2", &metav1.Time{Time: time.Now()}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: true,
		}, {
			name: "Matching NamespacedName and Commit ID and req is current from diff cluster",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       requestAcknowledgedMsg("diff-cluster", "foo/one", "foo/one", "hash2", &metav1.Time{Time: time.Now()}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		}, {
			name: "Matching NamespacedName and Commit ID and req is old",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       requestAcknowledgedMsg("default", "foo/one", "foo/one", "hash2", &metav1.Time{Time: time.Now().Add(-30 * time.Minute)}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		}, {
			name: "Matching NamespacedName and Commit ID and req is future",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       requestAcknowledgedMsg("default", "foo/one", "foo/one", "hash2", &metav1.Time{Time: time.Now().Add(5 * time.Minute)}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash2",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		}, {
			name: "wrong commit id",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       requestAcknowledgedMsg("default", "foo/one", "foo/one", "hash2", &metav1.Time{Time: time.Now()}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash3",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		}, {
			name: "wrong NS",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       requestAcknowledgedMsg("default", "foo/two", "foo/two", "hash3", &metav1.Time{Time: time.Now()}),
					},
				}}},
				cluster:    "default",
				commitID:   "hash3",
				modulePath: "foo/one",
				module:     types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlanRequestAckPostedForCommit(tt.args.cluster, tt.args.pr, tt.args.commitID, tt.args.modulePath, tt.args.module); got != tt.want {
				t.Errorf("isPlanRequestAckPostedForCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}
