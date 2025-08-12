package git

import (
	"context"
	"log/slog"
	"time"

	"github.com/utilitywarehouse/git-mirror/auth"
)

//go:generate go run github.com/golang/mock/mockgen -package git -destination github_mock.go github.com/utilitywarehouse/terraform-applier/git TokenGenerator

// TokenGenerator allows for mocking out the functionality of generating github app token
type TokenGenerator interface {
	Token(ctx context.Context, permissions map[string]string) (string, error)
}

type GithubApp struct {
	ID             string
	InstallID      string
	PrivateKeyPath string

	log        *slog.Logger
	token      string
	tokenExpAt time.Time
}

func (app *GithubApp) Token(ctx context.Context, permissions map[string]string) (string, error) {
	// return token if current token is valid for next 20 min
	if app.tokenExpAt.After(time.Now().UTC().Add(20 * time.Minute)) {
		return app.token, nil
	}

	token, err := auth.GithubAppInstallationToken(
		ctx,
		app.ID,
		app.InstallID,
		app.PrivateKeyPath,
		auth.GithubAppTokenReqPermissions{Permissions: permissions},
	)
	if err != nil {
		return "", nil
	}

	app.token = token.Token
	app.tokenExpAt = token.ExpiresAt

	app.log.Debug("new github app access token created")

	return app.token, nil
}
