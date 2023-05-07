package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:generate go run github.com/golang/mock/mockgen -package git -destination mock_gitutil.go github.com/utilitywarehouse/terraform-applier/git UtilInterface

// UtilInterface allows for mocking out the functionality of GitUtil when
// testing the full process of an apply run.
type UtilInterface interface {
	HeadCommitHashAndLog(name, path string) (string, string, error)
	RemoteURL(name string) (string, error)
	IsRepo(name string) (bool, error)
}

// Util allows for fetching information about a Git repository using Git CLI
// commands.
type Util struct {
	RootPath string
}

// HeadCommitHashAndLog returns the hash and the log of the current HEAD commit for the given path
func (g *Util) HeadCommitHashAndLog(name, path string) (string, string, error) {
	// get commit hash
	cmd := []string{"log", "--pretty=format:'%H'", "-n", "1", "--", path}
	hash, err := runGitCmd(filepath.Join(g.RootPath, name), cmd...)
	if err != nil {
		return "", "", err
	}

	// get commit message
	cmd = []string{"log", "--pretty=format:'%s (%an)'", "-n", "1", "--", path}
	log, err := runGitCmd(g.RootPath, cmd...)
	return strings.Trim(hash, "'\n"), strings.Trim(log, "'\n"), err
}

func (g *Util) RemoteURL(name string) (string, error) {
	cmd := []string{"remote", "get-url", "origin"}
	rURL, err := runGitCmd(filepath.Join(g.RootPath, name), cmd...)
	if err != nil {
		return "", err
	}

	rURL = strings.TrimSpace(rURL)
	rURL = strings.TrimPrefix(rURL, "https://")
	rURL = strings.TrimPrefix(rURL, "git@")
	rURL = strings.TrimSuffix(rURL, ".git")
	rURL = strings.ReplaceAll(rURL, ":", "/")

	return rURL, nil
}

func (g *Util) IsRepo(name string) (bool, error) {
	cmd := []string{"rev-parse", "--git-dir"}
	if _, err := runGitCmd(filepath.Join(g.RootPath, name), cmd...); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if exiterr.ExitCode() == 128 {
				return false, nil
			}
		}
		return false, err
	}

	return true, nil
}

// runGitCmd runs git
func runGitCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func GitSSHCommand(sshKeyPath, knownHostsFilePath string, verifyKnownHosts bool) (string, error) {
	knownHostsFragment := `-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no`

	if _, err := os.Stat(sshKeyPath); err != nil {
		return "", fmt.Errorf("can't access SSH key file %s: %w", sshKeyPath, err)
	}

	if verifyKnownHosts {
		if _, err := os.Stat(knownHostsFilePath); err != nil {
			return "", fmt.Errorf("can't access SSH known_hosts file %s: %w", knownHostsFilePath, err)
		}
		knownHostsFragment = fmt.Sprintf("-o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s", knownHostsFilePath)
	}

	return fmt.Sprintf(`ssh -i %s %s`, sshKeyPath, knownHostsFragment), nil
}
