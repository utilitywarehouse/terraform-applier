package prplanner

import (
	"context"
	"log/slog"
	"testing"

	gomock "github.com/golang/mock/gomock"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func Test_processPullRequest(t *testing.T) {
	ctx := context.Background()
	kubeModuleList := &tfaplv1beta1.ModuleList{
		Items: []tfaplv1beta1.Module{
			{Spec: tfaplv1beta1.ModuleSpec{Path: "one", RepoURL: "https://github.com/utilitywarehouse/foo.git"}},
			{Spec: tfaplv1beta1.ModuleSpec{Path: "two", RepoURL: "https://github.com/utilitywarehouse/foo.git"}},
			{Spec: tfaplv1beta1.ModuleSpec{Path: "three", RepoURL: "https://github.com/utilitywarehouse/foo.git"}},
			{Spec: tfaplv1beta1.ModuleSpec{Path: "four", RepoURL: "https://github.com/utilitywarehouse/foo.git"}},
			{Spec: tfaplv1beta1.ModuleSpec{Path: "five", RepoURL: "https://github.com/utilitywarehouse/foo.git"}},
			{Spec: tfaplv1beta1.ModuleSpec{Path: "six", RepoURL: "https://github.com/utilitywarehouse/foo.git"}},
		},
	}

	planner := &Planner{
		Log: slog.Default(),
	}

	t.Run("skip draft PR", func(t *testing.T) {
		p := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", "random comment", "random comment"},
			[]string{"foo1/bar1", "foo2/bar2"},
		)
		p.IsDraft = true

		planner.processPullRequest(ctx, p, kubeModuleList)
	})

	t.Run("len PR modules == 0", func(t *testing.T) {
		kubeModuleList := &tfaplv1beta1.ModuleList{}
		p := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", "random comment", "random comment"},
			[]string{},
		)

		planner.processPullRequest(ctx, p, kubeModuleList)
	})

	t.Run("len PR modules > 5 + module limit comment not posted", func(t *testing.T) {
		goMockCtrl := gomock.NewController(t)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		p := generateMockPR(123, "ref1",
			[]string{"hash1", "hash2", "hash3"},
			[]string{"random comment", "random comment", "random comment"},
			[]string{"one", "two", "three", "four", "five", "six"},
		)
		p.BaseRepository.Owner.Login = "utilitywarehouse"
		p.BaseRepository.Name = "foo"
		p.BaseRepository.URL = "git@github.com:utilitywarehouse/foo.git"

		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				t.Skip("comment posted. test passed")
				return 111, nil
			})

		planner.processPullRequest(ctx, p, kubeModuleList)
	})
}
