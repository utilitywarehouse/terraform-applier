package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_checkPRCommentsForPlanRequests(t *testing.T) {
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

	module := tfaplv1beta1.Module{
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
				fmt.Sprintf(requestAcknowledgedTml, "foo/two", "reqID2", "hash2"),
				fmt.Sprintf(requestAcknowledgedTml, "foo/three", "reqID3", "hash3"),
			},
			nil,
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, repoURL, module)
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
				outputBody("foo/two", "module/path/is/going/to/be/here", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
				outputBody("foo/three", "module/path/is/going/to/be/here", &v1beta1.Run{CommitHash: "hash3", Summary: "Plan: x to add, x to change, x to destroy.", Output: "tf plan output"}),
			},
			nil,
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, repoURL, module)
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
				fmt.Sprintf("@terraform-applier plan one"),
				fmt.Sprintf("@terraform-applier plan foo/one"),
				fmt.Sprintf("@terraform-applier plan foo/three"),
			},
			nil,
		)

		gotReq, err := planner.checkPRCommentsForPlanRequests(pr, repoURL, module)
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
				fmt.Sprintf("@terraform-applier plan foo/one"),
				fmt.Sprintf("@terraform-applier plan foo/two"),
				fmt.Sprintf("@terraform-applier plan foo/three"),
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
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, repoURL, module)
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
				fmt.Sprintf("@terraform-applier plan foo/one"),
				fmt.Sprintf("@terraform-applier plan two"),
				fmt.Sprintf("@terraform-applier plan three"),
			},
			nil,
		)

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
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, repoURL, module)
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
				fmt.Sprintf("@terraform-applier plan foo/one"),
				fmt.Sprintf("@terraform-applier plan bar/two"),
				fmt.Sprintf("@terraform-applier plan foo/three"),
			},
			nil,
		)

		// Call Test function
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, repoURL, module)
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
				fmt.Sprintf("@terraform-applier plan foo/one"),
				fmt.Sprintf("@terraform-applier plan two please"),
				fmt.Sprintf("@terraform-applier plan three"),
			},
			nil,
		)

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
		gotReq, err := planner.checkPRCommentsForPlanRequests(p, repoURL, module)
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