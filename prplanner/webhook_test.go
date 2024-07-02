package prplanner

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_isValidSignature(t *testing.T) {
	planner := &Planner{
		Log: slog.Default(),
	}

	slog.SetLogLoggerLevel(slog.LevelDebug)

	t.Run("Github's example test", func(t *testing.T) {

		secret := "It's a Secret to Everybody"
		body := []byte("Hello, World!")
		expSig := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"

		req := httptest.NewRequest("POST", "/events", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", expSig)

		gotOk := planner.isValidSignature(req, body, secret)

		wantOk := true

		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Generate hash", func(t *testing.T) {

		secret := "a1b2c3d4"
		body := []byte(`{"foo":"bar"}`)

		hash := hmac.New(sha256.New, []byte(secret))
		hash.Write(body)
		expectedSignature := hex.EncodeToString(hash.Sum(nil))
		expSig := "sha256=" + expectedSignature

		req := httptest.NewRequest("POST", "/github-events", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", expSig)

		gotOk := planner.isValidSignature(req, body, secret)

		wantOk := true

		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("checkPRCommentForOutputRequests() mismatch (-want +got):\n%s", diff)
		}
	})
}
