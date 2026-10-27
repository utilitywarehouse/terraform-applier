package prplanner

import (
	"context"
	"log/slog"
	"testing"
	"time"

	gomock "github.com/golang/mock/gomock"
	"github.com/utilitywarehouse/git-mirror/repository"
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
		p.UpdatedAt = time.Now().Add(-time.Hour)
		p.IsDraft = true
		p.BaseRepository.Owner.Login = "utilitywarehouse"
		p.BaseRepository.Name = "foo"
		p.BaseRepository.URL = "git@github.com:utilitywarehouse/foo.git"

		testGit.EXPECT().BranchCommits(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _, hash string) ([]repository.CommitInfo, error) {
				return []repository.CommitInfo{
					{Hash: "hash1", ChangedFiles: []string{"one", "six"}},
				}, nil
			})

		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(ctx context.Context, repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				t.Skip("comment posted. test passed")
				return 111, nil
			})

		planner.processPullRequest(ctx, p, kubeModuleList)
	})

	t.Run("skip old PR", func(t *testing.T) {
		goMockCtrl := gomock.NewController(t)
		testGithub := NewMockGithubInterface(goMockCtrl)
		planner.github = testGithub
		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)
		p.UpdatedAt = time.Now().Add(-25 * time.Hour)
		p.IsDraft = true
		p.BaseRepository.Owner.Login = "utilitywarehouse"
		p.BaseRepository.Name = "foo"
		p.BaseRepository.URL = "git@github.com:utilitywarehouse/foo.git"

		planner.processPullRequest(ctx, p, kubeModuleList)
	})

	t.Run("len PR modules == 0", func(t *testing.T) {
		kubeModuleList := &tfaplv1beta1.ModuleList{}
		p := generateMockPR(123, "ref1",
			[]string{"random comment", "random comment", "random comment"},
		)
		p.UpdatedAt = time.Now().Add(-time.Hour)

		testGit.EXPECT().BranchCommits(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _, hash string) ([]repository.CommitInfo, error) {
				return []repository.CommitInfo{
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
		p.UpdatedAt = time.Now().Add(-time.Hour)
		p.BaseRepository.Owner.Login = "utilitywarehouse"
		p.BaseRepository.Name = "foo"
		p.BaseRepository.URL = "git@github.com:utilitywarehouse/foo.git"

		testGit.EXPECT().BranchCommits(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _, hash string) ([]repository.CommitInfo, error) {
				return []repository.CommitInfo{
					{Hash: "hash3", ChangedFiles: []string{"four", "three"}},
					{Hash: "hash2", ChangedFiles: []string{"two", "five"}},
					{Hash: "hash1", ChangedFiles: []string{"one", "six"}},
				}, nil
			})

		testGithub.EXPECT().postComment(gomock.Any(), gomock.Any(), gomock.Any(), 0, 123, gomock.Any()).
			DoAndReturn(func(ctx context.Context, repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
				t.Skip("comment posted. test passed")
				return 111, nil
			})

		planner.processPullRequest(ctx, p, kubeModuleList)
	})
}

func Test_isModuleUpdated(t *testing.T) {
	tests := []struct {
		name         string
		modulePath   string
		changedFiles []string
		wantUpdated  bool
	}{
		{
			name:         "file in module path",
			modulePath:   "foo/one",
			changedFiles: []string{"foo/one/main.tf"},
			wantUpdated:  true,
		},
		{
			name:         "file in nested directory of module path",
			modulePath:   "foo/one",
			changedFiles: []string{"foo/one/modules/db/main.tf"},
			wantUpdated:  true,
		},
		{
			name:         "module path itself is the changed file",
			modulePath:   "foo/one",
			changedFiles: []string{"foo/one"},
			wantUpdated:  true,
		},
		{
			name:         "module path with trailing slash",
			modulePath:   "foo/one/",
			changedFiles: []string{"foo/one/main.tf"},
			wantUpdated:  true,
		},
		{
			name:         "sibling module sharing a name prefix",
			modulePath:   "foo/one",
			changedFiles: []string{"foo/one-extra/main.tf"},
			wantUpdated:  false,
		},
		{
			name:         "unrelated module",
			modulePath:   "foo/one",
			changedFiles: []string{"foo/two/main.tf"},
			wantUpdated:  false,
		},
		{
			name:         "empty module path matches every change",
			modulePath:   "",
			changedFiles: []string{"foo/one/main.tf"},
			wantUpdated:  true,
		},
		{
			name:         "no changed files",
			modulePath:   "foo/one",
			changedFiles: nil,
			wantUpdated:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			module := &tfaplv1beta1.Module{Spec: tfaplv1beta1.ModuleSpec{Path: tt.modulePath}}
			commit := repository.CommitInfo{Hash: "hash1", ChangedFiles: tt.changedFiles}

			if got := isModuleUpdated(module, commit); got != tt.wantUpdated {
				t.Errorf("isModuleUpdated() = %v, want %v", got, tt.wantUpdated)
			}
		})
	}
}
