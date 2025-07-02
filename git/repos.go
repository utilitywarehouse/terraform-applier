package git

import (
	"context"

	"github.com/utilitywarehouse/git-mirror/repository"
)

//go:generate go run github.com/golang/mock/mockgen -package git -destination repos_mock.go github.com/utilitywarehouse/terraform-applier/git Repositories

// Repositories allows for mocking out the functionality of git-mirror when
// testing the full process of an apply run
// mirror.RepoPool satisfies this interface and drop in replacement
type Repositories interface {
	Clone(ctx context.Context, remote, dst, reference string, pathspecs []string, rmGitDir bool) (string, error)
	Hash(ctx context.Context, remote, ref, path string) (string, error)
	Mirror(ctx context.Context, remote string) error
	Subject(ctx context.Context, remote, hash string) (string, error)
	BranchCommits(ctx context.Context, remote, branch string) ([]repository.CommitInfo, error)
	MergeCommits(ctx context.Context, remote, mergeCommitHash string) ([]repository.CommitInfo, error)
}
