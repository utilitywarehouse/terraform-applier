package prplanner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func (p *Planner) startWebhook() {
	http.HandleFunc("/github-events", p.handleWebhook)
	http.ListenAndServe(p.ListenAddress, nil)
}

func (p *Planner) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		p.Log.Error("cannot read request body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !p.isValidSignature(r, body, p.WebhookSecret) {
		p.Log.Error("invalid signature", "error", err)
		http.Error(w, "Wrong signature", http.StatusUnauthorized)
		return
	}

	// Get the X-GitHub-Event header
	event := r.Header.Get("X-GitHub-Event")

	// Parse the JSON payload
	var payload GitHubWebhook
	if err := json.Unmarshal(body, &payload); err != nil {
		p.Log.Error("cannot unmarshal json payload", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Verify event and action
	if (event == "pull_request" && payload.Action == "opened") ||
		(event == "pull_request" && payload.Action == "synchronize") ||
		(event == "pull_request" && payload.Action == "reopened") {

		go p.processPRWebHookEvent(payload, payload.Number)
		w.WriteHeader(http.StatusOK)
		return
	}

	if event == "pull_request" && payload.Action == "closed" {
		// TODO:clean-up: remove run from Redis
		w.WriteHeader(http.StatusOK)
		return
	}

	if event == "issue_comment" && payload.Action == "created" ||
		event == "issue_comment" && payload.Action == "edited" {
		// we know the body, but we still need to know the module user is requesting
		// plan run for belongs to this PR hence we need to do full reconcile of PR
		go p.processPRWebHookEvent(payload, payload.Issue.Number)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusBadRequest)
}

func (p *Planner) processPRWebHookEvent(event GitHubWebhook, prNumber int) {
	ctx := context.Background()

	err := p.Repos.Mirror(ctx, event.Repository.URL)
	if err != nil {
		p.Log.Error("unable to mirror repository", "url", event.Repository.URL, "pr", prNumber, "err", err)
		return
	}

	pr, err := p.github.PR(ctx, event.Repository.Owner.Login, event.Repository.Name, prNumber)
	if err != nil {
		p.Log.Error("unable to get PR info", "pr", prNumber, "err", err)
		return
	}

	kubeModuleList := &tfaplv1beta1.ModuleList{}
	if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
		p.Log.Error("error retrieving list of modules", "pr", prNumber, "error", err)
		return
	}

	p.processPullRequest(ctx, pr, kubeModuleList)
}

func (p *Planner) isValidSignature(r *http.Request, message []byte, secret string) bool {
	gotSignature := r.Header.Get("X-Hub-Signature-256")

	expSignature := p.computeHMAC(message, secret)
	if expSignature == "" {
		return false
	}

	return hmac.Equal([]byte(gotSignature), []byte(expSignature))
}

func (p *Planner) computeHMAC(message []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))

	if _, err := mac.Write(message); err != nil {
		p.Log.Error("cannot compute hmac for request", err)
		return ""
	}

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
