package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var cmpIgnoreRandFields = cmpopts.IgnoreFields(tfaplv1beta1.Request{}, "ID", "RequestedAt")

func generateMockPR(num int, ref string, hash, comments, paths []string) *pr {
	p := &pr{
		Number:      num,
		HeadRefName: ref,
	}

	for _, v := range hash {
		pc := prCommit{}
		pc.Commit.Oid = v
		p.Commits.Nodes = append(p.Commits.Nodes, pc)
	}
	for _, v := range comments {
		p.Comments.Nodes = append(p.Comments.Nodes, prComment{1, author{}, v})
	}
	for _, v := range paths {
		p.Files.Nodes = append(p.Files.Nodes, prFiles{v})
	}
	return p
}

func TestCheckPRCommits(t *testing.T) {
	ctx := context.Background()
	goMockCtrl := gomock.NewController(t)

	testGit := git.NewMockRepositories(goMockCtrl)

	planner := &Planner{
		Repos: testGit,
		Log:   slog.Default(),
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	repoURL := &mirror.GitURL{
		Path: "owner-a",
		Repo: "repo-a",
	}

	// Mock Repo calls with files changed
	testGit.EXPECT().ChangedFiles(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, hash string) ([]string, error) {
			switch hash {
			case "hash1":
				return []string{"foo/one"}, nil
			case "hash2":
				return []string{"foo/two"}, nil
			case "hash3":
				return []string{"foo/two", "foo/three"}, nil
			default:
				return nil, fmt.Errorf("hash not found")
			}
		}).AnyTimes()

	t.Run("generate req for updated module", func(t *testing.T) {
		testRedis := sysutil.NewMockRedisInterface(goMockCtrl)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		planner.RedisClient = testRedis

		p := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", "random comment", "random comment"},
			nil,
		)

		module := tfaplv1beta1.Module{
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
		testGithub.EXPECT().postComment(gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repo *mirror.GitURL, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, repoURL, p, module)
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
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", "random comment", "random comment"},
			nil,
		)

		module := tfaplv1beta1.Module{
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
		testGithub.EXPECT().postComment(gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repo *mirror.GitURL, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, repoURL, p, module)
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
			[]string{"hash2", "hash3"},
			[]string{"random comment", "Terraform plan output for module `foo/two` Commit ID: `hash2`", "random comment"},
			nil,
		)

		module := tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "one"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/one",
			},
		}

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, repoURL, p, module)
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
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", fmt.Sprintf(outputBodyTml, "foo/two", "foo/two", "hash3", "Plan: x to add, x to change, x to destroy.", "some output"), "random comment"},
			nil,
		)

		module := tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, repoURL, p, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq *tfaplv1beta1.Request

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
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", "Terraform plan output for module `foo/two` Commit ID: `hash2`", "random comment"},
			nil,
		)

		module := tfaplv1beta1.Module{
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
		testGithub.EXPECT().postComment(gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repo *mirror.GitURL, commentID, prNumber int, commentBody prComment) (int, error) {
				// validate comment message
				if !requestAcknowledgedRegex.Match([]byte(commentBody.Body)) {
					return 0, fmt.Errorf("comment body doesn't match requestAcknowledgedRegex")
				}
				return 111, nil
			})

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, repoURL, p, module)
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
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", "random comment", "random comment"},
			nil,
		)

		module := tfaplv1beta1.Module{
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
		gotReq, err := planner.checkPRCommits(ctx, repoURL, p, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		var wantReq *tfaplv1beta1.Request

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})
}
