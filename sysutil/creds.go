package sysutil

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/utilitywarehouse/git-mirror/auth"
)

const (
	gitAskPassScriptName = `tf-git-ask-pass.sh`
	gitAskPassScript     = `#!/bin/sh

# git runs this script with following argument to get username
# tf-git-ask-pass.sh 'Username for '\''https://github.com'\'': '

# and then once again for password with given username
# tf-git-ask-pass.sh 'Password for '\''https://<username>@github.com'\'': '

case "$1" in
  Username*github.com*) echo "$GITHUB_REPO_USERNAME" ;;
  Password*github.com*) echo "$GITHUB_REPO_PASSWORD" ;;
esac
`
)

//go:generate go run github.com/golang/mock/mockgen -package sysutil -destination creds_mock.go github.com/utilitywarehouse/terraform-applier/sysutil CredsProvider

// CredsProvider allows for mocking out the functionality of generating creds/token
// return password is either user's password or access token
type CredsProvider interface {
	Creds(ctx context.Context) (username string, password string, err error)
}

type GithubCredProvider struct {
	staticToken string
	app         *GithubAPP

	log *slog.Logger
}

type GithubAPP struct {
	id             string
	installID      string
	privateKeyPath string
	permissions    map[string]string

	token      string
	tokenExpAt time.Time
}

func NewGithubCredProvider(token, appID, appInstallID, appPrivateKey string, permissions map[string]string, log *slog.Logger) (*GithubCredProvider, error) {
	gh := &GithubCredProvider{
		staticToken: token,
		log:         log,
	}
	if appID == "" {
		return gh, nil
	}
	if appInstallID == "" || appPrivateKey == "" {
		return nil, fmt.Errorf("github app ID, installation ID and key path are required but only ID is set")
	}

	gh.app = &GithubAPP{
		id:             appID,
		installID:      appInstallID,
		privateKeyPath: appPrivateKey,
		permissions:    permissions,
	}
	return gh, nil
}

func (gh *GithubCredProvider) Creds(ctx context.Context) (string, string, error) {
	username := "-" // username is required

	// use static token if its configured
	if gh.staticToken != "" {
		return username, gh.staticToken, nil
	}

	if gh.app == nil {
		return "", "", fmt.Errorf("static token or github app is not set")
	}

	// return app token if current token is valid for next 20 min
	if gh.app.tokenExpAt.After(time.Now().UTC().Add(20 * time.Minute)) {
		return username, gh.app.token, nil
	}

	token, err := auth.GithubAppInstallationToken(
		ctx,
		gh.app.id,
		gh.app.installID,
		gh.app.privateKeyPath,
		auth.GithubAppTokenReqPermissions{Permissions: gh.app.permissions},
	)
	if err != nil {
		return "", "", err
	}

	gh.app.token = token.Token
	gh.app.tokenExpAt = token.ExpiresAt

	gh.log.Debug("new github app access token created")

	return username, gh.app.token, nil
}

// GitSSHCommand returns string which is used to set 'GIT_SSH_COMMAND'
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

	return fmt.Sprintf(`ssh -q -F none -o IdentitiesOnly=yes -i %s %s`, sshKeyPath, knownHostsFragment), nil
}

// EnsureGitCredsLoader will create a script which can be used by git to get creds
// returned path is used to set 'GIT_ASKPASS'
func EnsureGitCredsLoader() (string, error) {
	path := filepath.Join(os.TempDir(), gitAskPassScriptName)
	_, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		if err := os.WriteFile(path, []byte(gitAskPassScript), 0750); err != nil {
			return "", err
		}
	case err != nil:
		return "", fmt.Errorf("unable to check if script file exits err:%w", err)
	}

	return path, nil
}
