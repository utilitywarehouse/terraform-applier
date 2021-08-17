package git

import (
	"os/exec"
	"strings"
)

// UtilInterface allows for mocking out the functionality of GitUtil when
// testing the full process of an apply run.
type UtilInterface interface {
	HeadCommitLogForPaths(args ...string) (string, error)
	HeadHashForPaths(args ...string) (string, error)
	IsRepo() (bool, error)
}

// Util allows for fetching information about a Git repository using Git CLI
// commands.
type Util struct {
	Path string
}

// HeadHashForPaths returns the hash of the current HEAD commit for the
// filtered directories
func (g *Util) HeadHashForPaths(args ...string) (string, error) {
	cmd := []string{"log", "--pretty=format:'%h'", "-n", "1", "--"}
	cmd = append(cmd, args...)
	hash, err := runGitCmd(g.Path, cmd...)
	return strings.Trim(hash, "'\n"), err
}

// HeadCommitLogForPaths returns the log of the current HEAD commit for the filtered directories
func (g *Util) HeadCommitLogForPaths(args ...string) (string, error) {
	cmd := []string{"log", "-1", "--name-status", "--"}
	cmd = append(cmd, args...)
	log, err := runGitCmd(g.Path, cmd...)
	return log, err
}

func (g *Util) IsRepo() (bool, error) {
	cmd := []string{"rev-parse", "--git-dir"}
	if _, err := runGitCmd(g.Path, cmd...); err != nil {
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
	var cmd *exec.Cmd
	cmd = exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
