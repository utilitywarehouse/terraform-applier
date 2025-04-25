package prplanner

import (
	"context"
	"log/slog"
	"testing"

	gomock "github.com/golang/mock/gomock"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
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
	goMockCtrl := gomock.NewController(t)
	testGit := git.NewMockRepositories(goMockCtrl)

	planner := &Planner{
		Log:   slog.Default(),
		Repos: testGit,
	}

	t.Run("skip draft PR", func(t *testing.T) {
		goMockCtrl := gomock.NewController(t)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)
		p.IsDraft = true
		p.BaseRepository.Owner.Login = "utilitywarehouse"
		p.BaseRepository.Name = "foo"
		p.BaseRepository.URL = "git@github.com:utilitywarehouse/foo.git"

		testGit.EXPECT().BranchCommits(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _, hash string) ([]mirror.CommitInfo, error) {
				return []mirror.CommitInfo{
					{Hash: "hash1", ChangedFiles: []string{"one", "six"}},
				}, nil
			})

		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				t.Skip("comment posted. test passed")
				return 111, nil
			})

		planner.processPullRequest(ctx, p, kubeModuleList)
	})

	t.Run("len PR modules == 0", func(t *testing.T) {
		kubeModuleList := &tfaplv1beta1.ModuleList{}
		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)

		testGit.EXPECT().BranchCommits(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _, hash string) ([]mirror.CommitInfo, error) {
				return []mirror.CommitInfo{
					{Hash: "hash3"},
					{Hash: "hash2"},
					{Hash: "hash1"},
				}, nil
			})

		planner.processPullRequest(ctx, p, kubeModuleList)
	})

	t.Run("len PR modules > 5 + module limit comment not posted", func(t *testing.T) {
		goMockCtrl := gomock.NewController(t)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub

		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)
		p.BaseRepository.Owner.Login = "utilitywarehouse"
		p.BaseRepository.Name = "foo"
		p.BaseRepository.URL = "git@github.com:utilitywarehouse/foo.git"

		testGit.EXPECT().BranchCommits(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _, hash string) ([]mirror.CommitInfo, error) {
				return []mirror.CommitInfo{
					{Hash: "hash3", ChangedFiles: []string{"four", "three"}},
					{Hash: "hash2", ChangedFiles: []string{"two", "five"}},
					{Hash: "hash1", ChangedFiles: []string{"one", "six"}},
				}, nil
			})

		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				t.Skip("comment posted. test passed")
				return 111, nil
			})

		planner.processPullRequest(ctx, p, kubeModuleList)
	})
}
