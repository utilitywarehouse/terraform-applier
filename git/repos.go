package git

import (
	"context"
)

//go:generate go run github.com/golang/mock/mockgen -package git -destination repos_mock.go github.com/utilitywarehouse/terraform-applier/git Repositories

// Repositories allows for mocking out the functionality of git-mirror when
// testing the full process of an apply run
// mirror.RepoPool satisfies this interface and drop in replacement
type Repositories interface {
	Hash(ctx context.Context, remote, ref, path string) (string, error)
	LogMsg(ctx context.Context, remote, ref, path string) (string, error)
	Clone(ctx context.Context, remote, dst, branch, pathspec string, rmGitDir bool) (string, error)
	Subject(ctx context.Context, remote, hash string) (string, error)
	ChangedFiles(ctx context.Context, remote, hash string) ([]string, error)
	ObjectExists(ctx context.Context, remote, obj string) error
}
