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
	AddRepository(identifier string, repositoryConfig RepositoryConfig, syncOptions *SyncOptions) error
	Repository(identifier string) (*Repository, error)
	RepositoryConfig(identifier string) (RepositoryConfig, error)

	CloneLocal(ctx context.Context, identifier string, subpath string, dst string, envs []string) (string, error)
	CopyPath(ctx context.Context, identifier string, subpath string, dst string) error

	HasChangesForPath(ctx context.Context, identifier string, path, sinceHash string) (bool, error)
	HashForPath(ctx context.Context, identifier string, path string) (string, error)
	LogMsgForPath(ctx context.Context, identifier string, path string) (string, error)
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

// AddRepository creates new repository object adds to sync pool after starting its sync loop
// identifier is the text identifier of the repository. ideally it should be name of the repository.
// identifier will be used on all other helper call to identify/select particular repo from sync pool
// if syncOptions is nil then it will use default sync objects syncOptions
// since AddRepository calls StartSync it will block until initial clone is done
func (s *SyncPool) AddRepository(identifier string, repositoryConfig RepositoryConfig, syncOptions *SyncOptions) error {

	if syncOptions == nil {
		syncOptions = s.defaultSyncOptions
	}

	if _, ok := s.repos[identifier]; ok {
		return fmt.Errorf("repository with identifier '%s' already exists", identifier)
	}

	repo, err := NewRepository(filepath.Join(s.root, identifier), repositoryConfig, syncOptions, s.log.Named(identifier))
	if err != nil {
		return err
	}

	s.repos[identifier] = repo

	return repo.StartSync(s.ctx)
}

// Repository returns Repository object if its added to sync pool
func (s *SyncPool) Repository(identifier string) (*Repository, error) {
	repo, ok := s.repos[identifier]
	if !ok {
		return nil, fmt.Errorf("repository with identifier '%s' is not yet added by admin", identifier)
	}
	return repo, nil
}

// Repository returns Repository config if its added to sync pool
func (s *SyncPool) RepositoryConfig(identifier string) (RepositoryConfig, error) {
	repo, ok := s.repos[identifier]
	if !ok {
		return RepositoryConfig{}, fmt.Errorf("repository with identifier '%s' is not yet added by admin", identifier)
	}
	return repo.repositoryConfig, nil
}

// CloneLocal creates a clone of the existing repository to a new location on
// disk and only checkouts the specified subpath. On success, it returns the
// hash of the new repository clone's HEAD.
func (s *SyncPool) CloneLocal(ctx context.Context, identifier string, subpath string, dst string, envs []string) (string, error) {
	repo, ok := s.repos[identifier]
	if !ok {
		return "", fmt.Errorf("repository with identifier '%s' is not yet added by admin", identifier)
	}
	return repo.CloneLocal(ctx, subpath, dst, envs)
}

// CopyPath get read lock and then copies given subpath to new location.
// WithCheckout must be set to use this function
func (s *SyncPool) CopyPath(ctx context.Context, identifier string, subpath string, dst string) error {
	repo, ok := s.repos[identifier]
	if !ok {
		return fmt.Errorf("repository with identifier '%s' is not yet added by admin", identifier)
	}
	return repo.CopyPath(ctx, subpath, dst)
}

// HasChangesForPath returns true if there are changes that have been committed
// since the commit hash provided, under the specified path.
func (s *SyncPool) HasChangesForPath(ctx context.Context, identifier string, path, sinceHash string) (bool, error) {
	repo, ok := s.repos[identifier]
	if !ok {
		return false, fmt.Errorf("repository with identifier '%s' is not yet added by admin", identifier)
	}
	return repo.HasChangesForPath(ctx, path, sinceHash)
}

// HashForPath returns the hash of the configured revision for the specified
// path.
func (s *SyncPool) HashForPath(ctx context.Context, identifier string, path string) (string, error) {
	repo, ok := s.repos[identifier]
	if !ok {
		return "", fmt.Errorf("repository with identifier '%s' is not yet added by admin", identifier)
	}
	return repo.HashForPath(ctx, path)
}

// LogMsgForPath returns the formatted log subject with author info of the configured revision for the specified path.
func (s *SyncPool) LogMsgForPath(ctx context.Context, identifier string, path string) (string, error) {
	repo, ok := s.repos[identifier]
	if !ok {
		return "", fmt.Errorf("repository with identifier '%s' is not yet added by admin", identifier)
	}
	return repo.LogMsgForPath(ctx, path)
}
