package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
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
		gotReq, err := planner.checkPRCommits(ctx, p, module)
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
		gotReq, err := planner.checkPRCommits(ctx, p, module)
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

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "one"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/one",
			},
		}

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, module)
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
			[]string{"random comment", runOutputMsg("foo/two", "foo/two", &tfaplv1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "some output"}), "random comment"},

			nil,
		)

		module := &tfaplv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "https://github.com/owner-a/repo-a.git",
				Path:    "foo/two",
			},
		}

		// Call Test function
		gotReq, err := planner.checkPRCommits(ctx, p, module)
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
		gotReq, err := planner.checkPRCommits(ctx, p, module)
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
		gotReq, err := planner.checkPRCommits(ctx, p, module)
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
		Repos: testGit,
		Log:   slog.Default(),
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	module := &tfaplv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "two"},
		Spec: tfaplv1beta1.ModuleSpec{
			RepoURL: "https://github.com/owner-a/repo-a.git",
			Path:    "foo/two",
		},
	}

	t.Run("request acknowledged for module", func(t *testing.T) {
		// avoid generating another request from `@terraform-applier plan` comment
		// is there's already a request ID posted for the module
		// module might not be annotated by the time the loop checks it, which in this
		// case would mean plan out is ready ot be posted and NOT run hasn't been requested yet
		pr := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				"@terraform-applier plan foo/two",
				requestAcknowledgedMsg("foo/two", "module/path/is/going/to/be/here", "hash2", mustParseMetaTime("2023-04-02T15:04:05Z")),
				requestAcknowledgedMsg("foo/three", "module/path/is/going/to/be/here", "hash3", mustParseMetaTime("2023-04-02T15:04:05Z")),
			},
			nil,
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan out posted for module", func(t *testing.T) {
		pr := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				runOutputMsg("foo/two", "module/path/is/going/to/be/here", &tfaplv1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
				runOutputMsg("foo/three", "module/path/is/going/to/be/here", &tfaplv1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
			},
			nil,
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan run is not requested for module", func(t *testing.T) {
		pr := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				"@terraform-applier plan one",
				"@terraform-applier plan foo/one",
				"@terraform-applier plan foo/three",
			},
			nil,
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, module)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if gotReq != nil {
			t.Errorf("checkPRCommentsForPlanRequests() returner non-nil Request")
		}
	})

	t.Run("plan run is requested for module using correct NamespacedName", func(t *testing.T) {
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		p := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan foo/two",
				"@terraform-applier plan foo/three",
			},
			nil,
		)

		// Mock Repo calls with files changed
		testGit.EXPECT().ChangedFiles(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _, hash string) ([]string, error) {
				switch hash {
				case "hash1":
					return []string{"foo/one"}, nil
				case "hash2":
					return []string{"foo/two"}, nil
				case "hash3":
					return []string{"foo/one", "foo/three"}, nil
				default:
					return nil, fmt.Errorf("hash not found")
				}
			}).AnyTimes()

		testGit.EXPECT().Hash(gomock.Any(), gomock.Any(), "ref1", "foo/two").
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
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("plan run is requested for module using correct Name", func(t *testing.T) {
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		p := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan two",
				"@terraform-applier plan three",
			},
			nil,
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

		testGit.EXPECT().Hash(gomock.Any(), gomock.Any(), "ref1", "foo/two").
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
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("plan run is requested for module in different Namespace", func(t *testing.T) {
		p := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan bar/two",
				"@terraform-applier plan foo/three",
			},
			nil,
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
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				"@terraform-applier plan foo/one",
				"@terraform-applier plan two please",
				"@terraform-applier plan three",
			},
			nil,
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

		testGit.EXPECT().Hash(gomock.Any(), gomock.Any(), "ref1", "foo/two").
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
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})
}

func Test_isPlanOutputPostedForCommit(t *testing.T) {
	type args struct {
		pr       *pr
		commitID string
		module   types.NamespacedName
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
						Body:       runOutputMsg("foo/one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."}),
					},
				}}},
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: true,
		},
		{
			name: "Matching Name and Commit ID",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       runOutputMsg("one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."}),
					},
				}}},
				commitID: "hash2",
				module:   types.NamespacedName{Name: "one"},
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
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlanOutputPostedForCommit(tt.args.pr, tt.args.commitID, tt.args.module); got != tt.want {
				t.Errorf("isPlanOutputPostedForCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}
