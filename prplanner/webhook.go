package prplanner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"net/http"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func (p *Planner) startWebhook() {
	http.HandleFunc("/github-events", p.handleWebhook)
	http.ListenAndServe(p.ListenAddress, nil)
}

func (p *Planner) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if !p.isValidSignature(r, p.WebhookSecret) {
		http.Error(w, "Wrong signature", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the X-GitHub-Event header
	event := r.Header.Get("X-GitHub-Event")

	// Parse the JSON payload
	var payload GitHubWebhook
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Failed to decode JSON payload", http.StatusBadRequest)
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

	// ??
	// edited

	if event == "pull_request" && payload.Action == "closed" {
		// TODO:clean-up: remove run from Redis
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

func (p *Planner) isValidSignature(r *http.Request, key string) bool {
	gotSignature := r.Header.Get("X-Hub-Signature-256")

	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		p.Log.Error("cannot read request body", err)
		return false
	}

	hash := hmac.New(sha256.New, []byte(key))
	if _, err := hash.Write(b); err != nil {
		p.Log.Error("cannot comput hmac for request", err)
		return false
	}

	expSignature := "sha256=" + hex.EncodeToString(hash.Sum(nil))

	return hmac.Equal([]byte(gotSignature), []byte(expSignature))
}
