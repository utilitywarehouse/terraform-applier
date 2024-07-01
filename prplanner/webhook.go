package prplanner

import (
	"context"
	"encoding/json"
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

	// TODO: handle authentication

	// Get the X-GitHub-Event header
	event := r.Header.Get("X-GitHub-Event")

	// Parse the JSON payload
	var payload GitHubWebhook
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Failed to decode JSON payload", http.StatusBadRequest)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if (event == "pull_request" && payload.Action == "opened") ||
		(event == "pull_request" && payload.Action == "synchronize") ||
		(event == "pull_request" && payload.Action == "reopened") {

		go p.processPRWebHookEvent(payload, payload.Number)
		w.WriteHeader(http.StatusOK)
		return
	}

	if event == "issue_comment" && payload.Action == "created" {
		// we know the body we still need to know the module user is
		// requesting belongs to this PR hence we need to do full reconcile of PR
		go p.processPRWebHookEvent(payload, payload.Issue.Number)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusBadRequest)
}

func (p *Planner) processPRWebHookEvent(payload GitHubWebhook, prNumber int) {
	ctx := context.Background()

	mirrorRepo, err := p.Repos.Repository(payload.Repository.URL)
	if err != nil {
		p.Log.Error("unable to get repository from url", "url", payload.Repository.URL, "pr", prNumber, "err", err)
		return
	}

	// trigger mirror
	err = mirrorRepo.Mirror(ctx)
	if err != nil {
		p.Log.Error("unable to mirror repository", "err", err)
		return
	}

	pr, err := p.github.PR(ctx, payload.Repository.Owner.Login, payload.Repository.Name, prNumber)
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
