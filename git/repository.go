// modified version of https://github.com/utilitywarehouse/kube-applier/blob/master/git/repository.go
// Package git provides methods for manipulating and querying git repositories
// on disk.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

var (
	gitExecutablePath string
)

func init() {
	gitExecutablePath = exec.Command("git").String()
}

// RepositoryConfig defines a remote git repository.
type RepositoryConfig struct {
	Remote   string `yaml:"remote"`
	Branch   string `yaml:"branch"`
	Revision string `yaml:"revision"`
	Depth    int    `yaml:"depth"`
}

// SyncOptions encapsulates options about how a Repository should be fetched
// from the remote.
type SyncOptions struct {
	GitSSHKeyPath        string
	GitSSHKnownHostsPath string
	WithCheckout         bool
	CloneTimeout         time.Duration
	Interval             time.Duration
}

// gitSSHCommand returns the environment variable to be used for configuring
// git over ssh.
func (so SyncOptions) gitSSHCommand() string {
	sshKeyPath := so.GitSSHKeyPath
	if sshKeyPath == "" {
		sshKeyPath = "/dev/null"
	}
	knownHostsOptions := "-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no"
	if so.GitSSHKeyPath != "" && so.GitSSHKnownHostsPath != "" {
		knownHostsOptions = fmt.Sprintf("-o UserKnownHostsFile=%s", so.GitSSHKnownHostsPath)
	}
	return fmt.Sprintf(`GIT_SSH_COMMAND=ssh -q -F none -o IdentitiesOnly=yes -o IdentityFile=%s %s`, sshKeyPath, knownHostsOptions)
}

// Repository defines a remote git repository that should be synced regularly
// and is the source of truth for a cluster. Changes in this repository trigger
// GitPolling type runs for namespaces. The implementation borrows heavily from
// git-sync.
type Repository struct {
	lock             sync.RWMutex
	name             string
	path             string
	repositoryConfig RepositoryConfig
	running          bool
	stop, stopped    chan bool
	syncOptions      *SyncOptions
	log              hclog.Logger
}

// NewRepository initialises a Repository struct.
func NewRepository(name, path string, repositoryConfig RepositoryConfig, syncOptions *SyncOptions, log hclog.Logger) (*Repository, error) {
	if name == "" {
		return nil, fmt.Errorf("cannot create Repository without name")
	}
	if path == "" {
		return nil, fmt.Errorf("cannot create Repository with empty local path")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("Repository path must be absolute")
	}
	if repositoryConfig.Remote == "" {
		return nil, fmt.Errorf("cannot create Repository with empty remote")
	}
	if repositoryConfig.Depth < 0 {
		return nil, fmt.Errorf("Repository depth cannot be negative")
	}
	if repositoryConfig.Branch == "" {
		log.Info("Defaulting repository branch to 'master'")
		repositoryConfig.Branch = "master"
	}
	if repositoryConfig.Revision == "" {
		log.Info("Defaulting repository revision to 'HEAD'")
		repositoryConfig.Revision = "HEAD"
	}
	if syncOptions.CloneTimeout == 0 {
		log.Info("Defaulting clone timeout to 5 minute")
		syncOptions.CloneTimeout = time.Minute * 5
	}
	if syncOptions.Interval == 0 {
		log.Info("Defaulting Interval to 30 seconds")
		syncOptions.Interval = time.Second * 30
	}
	return &Repository{
		name:             name,
		path:             path,
		repositoryConfig: repositoryConfig,
		syncOptions:      syncOptions,
		lock:             sync.RWMutex{},
		log:              log,
		stop:             make(chan bool),
		stopped:          make(chan bool),
	}, nil
}

// StartSync begins syncing from the remote git repository. first sync/clone is done with
// cloneTimeout value as it might take longer than usual depending on the size of the repository.
func (r *Repository) StartSync(ctx context.Context) error {
	if r.running {
		return fmt.Errorf("sync has already been started")
	}
	r.running = true
	r.log.Info("waiting for the repository to complete initial sync")

	// The first sync is done outside of the syncLoop (and a separate timeout).
	// The first clone might take longer than usual depending on the size of the repository.
	// Additionally it runs in the foreground which simplifies startup since caller might
	// require a repository clone to exist before starting up properly.
	cloneCtx, cancel := context.WithTimeout(ctx, r.syncOptions.CloneTimeout)
	defer cancel()
	if err := r.sync(cloneCtx); err != nil {
		return err
	}
	go r.syncLoop(ctx)
	return nil
}

func (r *Repository) syncLoop(ctx context.Context) {
	defer func() {
		r.running = false
		close(r.stopped)
	}()

	r.running = true
	ticker := time.NewTicker(r.syncOptions.Interval)
	defer ticker.Stop()
	r.log.Info("started repository sync loop", "interval", r.syncOptions.Interval)
	for {
		select {
		case <-ticker.C:
			syncCtx, cancel := context.WithTimeout(ctx, r.syncOptions.Interval-time.Second)
			err := r.sync(syncCtx)
			if err != nil {
				r.log.Error("could not sync git repository", "error", err)
			}
			recordGitSync(r.name, err == nil)
			cancel()
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		}
	}
}

// StopSync stops the syncing process.
func (r *Repository) StopSync() {
	if !r.running {
		r.log.Info("Sync has not been started, will not do anything")
		return
	}
	close(r.stop)
	<-r.stopped
}

func (r *Repository) runGitCommand(ctx context.Context, environment []string, cwd string, args ...string) (string, error) {
	cmdStr := gitExecutablePath + " " + strings.Join(args, " ")
	r.log.Debug("running command", "cwd", cwd, "cmd", cmdStr)

	cmd := exec.CommandContext(ctx, gitExecutablePath, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	outbuf := bytes.NewBuffer(nil)
	errbuf := bytes.NewBuffer(nil)
	cmd.Stdout = outbuf
	cmd.Stderr = errbuf

	env := []string{
		fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
		fmt.Sprintf("SSH_AUTH_SOCK=%s", os.Getenv("SSH_AUTH_SOCK")),
		r.syncOptions.gitSSHCommand(),
	}

	cmd.Env = env
	if len(environment) > 0 {
		cmd.Env = append(cmd.Env, environment...)
	}

	err := cmd.Run()
	stdout := outbuf.String()
	stderr := errbuf.String()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("Run(%s): %w: { stdout: %q, stderr: %q }", cmdStr, ctx.Err(), stdout, stderr)
	}
	if err != nil {
		return "", fmt.Errorf("Run(%s): %w: { stdout: %q, stderr: %q }", cmdStr, err, stdout, stderr)
	}
	r.log.Debug("command result", "stdout", stdout, "stderr", stderr)

	return stdout, nil
}

// localHash returns the locally known hash for the configured Revision.
func (r *Repository) localHash(ctx context.Context) (string, error) {
	output, err := r.runGitCommand(ctx, nil, r.path, "rev-parse", r.repositoryConfig.Revision)
	if err != nil {
		return "", err
	}
	return strings.Trim(string(output), "\n"), nil
}

// localHashForPath returns the hash of the configured revision for the
// specified path.
func (r *Repository) localHashForPath(ctx context.Context, path string) (string, error) {
	output, err := r.runGitCommand(ctx, nil, r.path, "log", "--pretty=format:%h", "-n", "1", "--", path)
	if err != nil {
		return "", err
	}
	return strings.Trim(string(output), "\n"), nil
}

// remoteHash returns the upstream hash for the ref that corresponds to the
// configured Revision.
func (r *Repository) remoteHash(ctx context.Context) (string, error) {
	// Build a ref string, depending on whether the user asked to track HEAD or
	// a tag.
	ref := ""
	if r.repositoryConfig.Revision == "HEAD" {
		ref = "refs/heads/" + r.repositoryConfig.Branch
	} else {
		ref = "refs/tags/" + r.repositoryConfig.Revision
	}
	// git ls-remote -q origin refs/XXX/XXX
	output, err := r.runGitCommand(ctx, nil, r.path, "ls-remote", "-q", "origin", ref)
	if err != nil {
		return "", err
	}
	parts := strings.Split(string(output), "\t")
	return parts[0], nil
}

func (r *Repository) sync(ctx context.Context) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	defer updateSyncLatency(r.name, time.Now())

	gitRepoPath := filepath.Join(r.path, ".git")
	_, err := os.Stat(gitRepoPath)
	switch {
	case os.IsNotExist(err):
		// First time. Just clone it and get the hash.
		return r.cloneRemote(ctx)
	case err != nil:
		return fmt.Errorf("error checking if repo exists %q: %v", gitRepoPath, err)
	default:
		// Not the first time. Figure out if the ref has changed.
		local, err := r.localHash(ctx)
		if err != nil {
			return err
		}
		remote, err := r.remoteHash(ctx)
		if err != nil {
			return err
		}
		if local == remote {
			r.log.Info("no update required", "rev", r.repositoryConfig.Revision, "local", local, "remote", remote)
			return nil
		}
		r.log.Info("update required", "rev", r.repositoryConfig.Revision, "local", local, "remote", remote)
	}

	r.log.Info("syncing git", "branch", r.repositoryConfig.Branch, "rev", r.repositoryConfig.Revision)
	args := []string{"fetch", "-f", "--tags"}
	if r.repositoryConfig.Depth != 0 {
		args = append(args, "--depth", strconv.Itoa(r.repositoryConfig.Depth))
	}
	args = append(args, "origin", r.repositoryConfig.Branch)
	// Update from the remote.
	// git fetch -f --tags --depth x origin <branch>
	if _, err := r.runGitCommand(ctx, nil, r.path, args...); err != nil {
		return err
	}
	// GC clone
	// git gc --prune=all
	if _, err := r.runGitCommand(ctx, nil, r.path, "gc", "--prune=all"); err != nil {
		commitGraphLock := filepath.Join(gitRepoPath, "objects/info/commit-graph.lock")
		if strings.Contains(err.Error(), fmt.Sprintf("Unable to create '%s': File exists.", commitGraphLock)) {
			if e := os.Remove(commitGraphLock); e != nil {
				r.log.Error("possible git crash detected but could not remove commit graph lock", "path", commitGraphLock, "error", e)
			} else {
				r.log.Error("possible git crash detected, commit graph lock removed and next attempt should succeed", "path", commitGraphLock)
			}
		}
		return err
	}
	// Reset HEAD
	args = []string{"reset", fmt.Sprintf("origin/%s", r.repositoryConfig.Branch)}
	if r.syncOptions.WithCheckout {
		args = append(args, "--hard")
	} else {
		args = append(args, "--soft")
	}
	// git reset --soft origin/<branch>
	if _, err = r.runGitCommand(ctx, nil, r.path, args...); err != nil {
		return err
	}
	return nil
}

func (r *Repository) cloneRemote(ctx context.Context) error {
	args := []string{"clone", "-b", r.repositoryConfig.Branch}
	if r.repositoryConfig.Depth != 0 {
		args = append(args, "--depth", strconv.Itoa(r.repositoryConfig.Depth))
	}
	if !r.syncOptions.WithCheckout {
		args = append(args, "--no-checkout")
	}
	args = append(args, r.repositoryConfig.Remote, r.path)
	r.log.Info("cloning repo", "origin", r.repositoryConfig.Remote, "path", r.path)

	_, err := r.runGitCommand(ctx, nil, "", args...)
	if err != nil {
		if strings.Contains(err.Error(), "already exists and is not an empty directory") {
			// Maybe a previous run crashed?  Git won't use this dir.
			r.log.Info("git root exists and is not empty (previous crash?), cleaning up", "path", r.path)
			err := os.RemoveAll(r.path)
			if err != nil {
				return err
			}
			_, err = r.runGitCommand(ctx, nil, "", args...)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

// CloneLocal creates a clone of the existing repository to a new location on
// disk and only checkouts the specified subpath. On success, it returns the
// hash of the new repository clone's HEAD.
func (r *Repository) CloneLocal(ctx context.Context, subpath, dst string, envs []string) (string, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	hash, err := r.localHashForPath(ctx, subpath)
	if err != nil {
		return "", err
	}

	// git clone --no-checkout src dst
	if _, err := r.runGitCommand(ctx, nil, "", "clone", "--no-checkout", r.path, dst); err != nil {
		return "", err
	}

	// git checkout HEAD -- ./path
	if _, err := r.runGitCommand(ctx, envs, dst, "checkout", r.repositoryConfig.Revision, "--", subpath); err != nil {
		return "", err
	}
	return hash, nil
}

// CopyPath get read lock and then copies given subpath to new location.
// WithCheckout must be set to use this function
func (r *Repository) CopyPath(ctx context.Context, subpath, dst string) error {
	r.lock.RLock()
	defer r.lock.RUnlock()

	if !r.syncOptions.WithCheckout {
		return fmt.Errorf("'WithCheckout' option is disabled there are no sub paths on the repo to copy. use 'CloneLocal()'")
	}

	return sysutil.CopyDir(filepath.Join(r.path, subpath), dst)
}

// HashForPath returns the hash of the configured revision for the specified path.
func (r *Repository) HashForPath(ctx context.Context, path string) (string, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.localHashForPath(ctx, path)
}

// LogMsgForPath returns the formatted log subject with author info of the configured revision for the specified path.
func (r *Repository) LogMsgForPath(ctx context.Context, path string) (string, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	output, err := r.runGitCommand(ctx, nil, r.path, "log", "--pretty=format:'%s (%an)'", "-n", "1", "--", path)
	if err != nil {
		return "", err
	}
	return strings.Trim(string(output), "'\n"), nil
}

// HasChangesForPath returns true if there are changes that have been committed
// since the commit hash provided, under the specified path.
func (r *Repository) HasChangesForPath(ctx context.Context, path, sinceHash string) (bool, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	cmd := []string{"diff", "--quiet", sinceHash, r.repositoryConfig.Revision, "--", path}
	_, err := r.runGitCommand(ctx, nil, r.path, cmd...)
	if err == nil {
		return false, nil
	}
	var e *exec.ExitError
	if errors.As(err, &e) && e.ExitCode() == 1 {
		return true, nil
	}
	return false, err
}
