package prplanner

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
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
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "one"},
		Spec: tfaplv1beta1.ModuleSpec{
			RepoURL: "https://github.com/owner-a/repo-a.git",
			Path:    "foo/one",
		},
	}

	t.Run("plan out posted for module", func(t *testing.T) {
		pr := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{
				fmt.Sprintf(outputBodyTml, "foo/two", "hash2", "tf plan output"),
				fmt.Sprintf(outputBodyTml, "foo/three", "hash3", "tf plan output"),
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
				fmt.Sprintf("@terraform-applier plan two"),
				fmt.Sprintf("@terraform-applier plan foo/two"),
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
				fmt.Sprintf("@terraform-applier plan foo/two"),
				fmt.Sprintf("@terraform-applier plan foo/one"),
				fmt.Sprintf("@terraform-applier plan foo/three"),
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
				Number:        123,
				HeadBranch:    "ref1",
				CommentID:     111,
				GitCommitHash: "hash3",
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
			[]string{"hash1", "hash2", "hash3", "hash4"},
			[]string{
				fmt.Sprintf("@terraform-applier plan foo/two"),
				fmt.Sprintf("@terraform-applier plan one"),
				fmt.Sprintf("@terraform-applier plan foo/three"),
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
				Number:        123,
				HeadBranch:    "ref1",
				CommentID:     111,
				GitCommitHash: "hash4",
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
				fmt.Sprintf("@terraform-applier plan foo/two"),
				fmt.Sprintf("@terraform-applier plan bar/one"),
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
			[]string{"hash1", "hash2", "hash3", "hash4"},
			[]string{
				fmt.Sprintf("@terraform-applier plan foo/two"),
				fmt.Sprintf("@terraform-applier plan one please"),
				fmt.Sprintf("@terraform-applier plan foo/three"),
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
				Number:        123,
				HeadBranch:    "ref1",
				CommentID:     111,
				GitCommitHash: "hash4",
			},
		}

		if diff := cmp.Diff(wantReq, gotReq, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommits() mismatch (-want +got):\n%s", diff)
		}
	})
}
