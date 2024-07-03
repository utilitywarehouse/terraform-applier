package prplanner

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_webhook(t *testing.T) {
	planner := &Planner{
		WebhookSecret: "a1b2c3d4e5",
	}

	body := []byte(`{"foo":"bar", "action": "foo"}`)

	t.Run("valid signature", func(t *testing.T) {

		expSig := planner.computeHMAC(body, planner.WebhookSecret)

		req := httptest.NewRequest("POST", "/github-events", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", expSig)

		gotOk := planner.isValidSignature(req, body, planner.WebhookSecret)

		wantOk := true

		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("isValidSignature() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {

		expSig := planner.computeHMAC(body, "invalidSignature")

		req := httptest.NewRequest("POST", "/github-events", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", expSig)

		gotOk := planner.isValidSignature(req, body, planner.WebhookSecret)

		wantOk := false

		if diff := cmp.Diff(wantOk, gotOk, cmpIgnoreRandFields); diff != "" {
			t.Errorf("isValidSignature() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("invalid method", func(t *testing.T) {

		expSig := planner.computeHMAC(body, planner.WebhookSecret)

		server := httptest.NewServer(http.HandlerFunc(planner.handleWebhook))
		defer server.Close()

		req, err := http.NewRequest("GET", server.URL, strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("Failed to make a request: %v", err)
		}
		req.Header.Set("X-Hub-Signature-256", expSig)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("Expected status %v, got %v", http.StatusMethodNotAllowed, resp.StatusCode)
		}
	})

	t.Run("invalid event", func(t *testing.T) {

		expSig := planner.computeHMAC(body, planner.WebhookSecret)

		server := httptest.NewServer(http.HandlerFunc(planner.handleWebhook))
		defer server.Close()

		req, err := http.NewRequest("POST", server.URL, strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("Failed to make a request: %v", err)
		}
		req.Header.Set("X-Hub-Signature-256", expSig)
		req.Header.Set("X-GitHub-Event", "foo")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %v, got %v", http.StatusBadRequest, resp.StatusCode)
		}
	})

	t.Run("invalid action", func(t *testing.T) {

		expSig := planner.computeHMAC(body, planner.WebhookSecret)

		server := httptest.NewServer(http.HandlerFunc(planner.handleWebhook))
		defer server.Close()

		req, err := http.NewRequest("POST", server.URL, strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("Failed to make a request: %v", err)
		}
		req.Header.Set("X-Hub-Signature-256", expSig)
		req.Header.Set("X-GitHub-Event", "pull_request")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status %v, got %v", http.StatusBadRequest, resp.StatusCode)
		}
	})
}
