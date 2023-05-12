package git

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
)

//go:generate go run github.com/golang/mock/mockgen -package git -destination syncpool_mock.go github.com/utilitywarehouse/terraform-applier/git SyncInterface

// SyncInterface allows for mocking out the functionality of SyncPool when
// testing the full process of an apply run.
type SyncInterface interface {
	AddRepository(repoName string, repositoryConfig RepositoryConfig, syncOptions *SyncOptions) error
	Repository(repoName string) (*Repository, error)
	RepositoryConfig(repoName string) (RepositoryConfig, error)

	CloneLocal(ctx context.Context, repoName string, subpath string, dst string, envs []string) (string, error)
	CopyPath(ctx context.Context, repoName string, subpath string, dst string) error

	HasChangesForPath(ctx context.Context, repoName string, path, sinceHash string) (bool, error)
	HashForPath(ctx context.Context, repoName string, path string) (string, error)
	LogMsgForPath(ctx context.Context, repoName string, path string) (string, error)
}

type SyncPool struct {
	ctx                context.Context
	log                hclog.Logger
	defaultSyncOptions *SyncOptions
	root               string
	// using normal map assuming that all repositories will be added
	// initially and then only accessed without deleting
	repos map[string]*Repository
}

// NewSync returns syncPool which is a collection of repository sync
// SyncPool object provides helper wrapper functions over Repository objects specially for multiple repo sync
func NewSyncPool(ctx context.Context, rootPath string, syncOptions SyncOptions, log hclog.Logger) (*SyncPool, error) {
	if !filepath.IsAbs(rootPath) {
		return nil, fmt.Errorf("Repository root path must be absolute")
	}

	if syncOptions.CloneTimeout == 0 {
		log.Info("Defaulting clone timeout to 5 minute")
		syncOptions.CloneTimeout = time.Minute * 5
	}
	if syncOptions.Interval == 0 {
		log.Info("Defaulting Interval to 30 seconds")
		syncOptions.Interval = time.Second * 30
	}

	return &SyncPool{
		ctx:                ctx,
		log:                log,
		defaultSyncOptions: &syncOptions,
		root:               rootPath,
		repos:              make(map[string]*Repository),
	}, nil

}

// AddRepository creates new repository object adds to sync pool after starting its sync loop.
// repo dir will be created under syncPool 'root' path with same name as repoName.
// repoName will be used on all other helper call to identify/select particular repo from sync pool.
// if syncOptions is nil then it will use default sync objects syncOptions
// since AddRepository calls StartSync it will block until initial clone is done
func (s *SyncPool) AddRepository(repoName string, repositoryConfig RepositoryConfig, syncOptions *SyncOptions) error {

	if syncOptions == nil {
		syncOptions = s.defaultSyncOptions
	}

	if _, ok := s.repos[repoName]; ok {
		return fmt.Errorf("repository with repoName '%s' already exists", repoName)
	}

	repo, err := NewRepository(filepath.Join(s.root, repoName), repositoryConfig, syncOptions, s.log.Named(repoName))
	if err != nil {
		return err
	}

	s.repos[repoName] = repo

	return repo.StartSync(s.ctx)
}

// Repository returns Repository object if its added to sync pool
func (s *SyncPool) Repository(repoName string) (*Repository, error) {
	repo, ok := s.repos[repoName]
	if !ok {
		return nil, fmt.Errorf("repository with repoName '%s' is not yet added by admin", repoName)
	}
	return repo, nil
}

// Repository returns Repository config if its added to sync pool
func (s *SyncPool) RepositoryConfig(repoName string) (RepositoryConfig, error) {
	repo, ok := s.repos[repoName]
	if !ok {
		return RepositoryConfig{}, fmt.Errorf("repository with repoName '%s' is not yet added by admin", repoName)
	}
	return repo.repositoryConfig, nil
}

// CloneLocal creates a clone of the existing repository to a new location on
// disk and only checkouts the specified subpath. On success, it returns the
// hash of the new repository clone's HEAD.
func (s *SyncPool) CloneLocal(ctx context.Context, repoName string, subpath string, dst string, envs []string) (string, error) {
	repo, ok := s.repos[repoName]
	if !ok {
		return "", fmt.Errorf("repository with repoName '%s' is not yet added by admin", repoName)
	}
	return repo.CloneLocal(ctx, subpath, dst, envs)
}

// CopyPath get read lock and then copies given subpath to new location.
// WithCheckout must be set to use this function
func (s *SyncPool) CopyPath(ctx context.Context, repoName string, subpath string, dst string) error {
	repo, ok := s.repos[repoName]
	if !ok {
		return fmt.Errorf("repository with repoName '%s' is not yet added by admin", repoName)
	}
	return repo.CopyPath(ctx, subpath, dst)
}

// HasChangesForPath returns true if there are changes that have been committed
// since the commit hash provided, under the specified path.
func (s *SyncPool) HasChangesForPath(ctx context.Context, repoName string, path, sinceHash string) (bool, error) {
	repo, ok := s.repos[repoName]
	if !ok {
		return false, fmt.Errorf("repository with repoName '%s' is not yet added by admin", repoName)
	}
	return repo.HasChangesForPath(ctx, path, sinceHash)
}

// HashForPath returns the hash of the configured revision for the specified
// path.
func (s *SyncPool) HashForPath(ctx context.Context, repoName string, path string) (string, error) {
	repo, ok := s.repos[repoName]
	if !ok {
		return "", fmt.Errorf("repository with repoName '%s' is not yet added by admin", repoName)
	}
	return repo.HashForPath(ctx, path)
}

// LogMsgForPath returns the formatted log subject with author info of the configured revision for the specified path.
func (s *SyncPool) LogMsgForPath(ctx context.Context, repoName string, path string) (string, error) {
	repo, ok := s.repos[repoName]
	if !ok {
		return "", fmt.Errorf("repository with repoName '%s' is not yet added by admin", repoName)
	}
	return repo.LogMsgForPath(ctx, path)
}
