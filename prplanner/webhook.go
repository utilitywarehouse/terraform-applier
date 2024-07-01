package prplanner

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func (p *Planner) startWebhook() {
	http.HandleFunc("/events", p.handleWebhook)
	http.ListenAndServe(p.ListenAddress, nil)
}

func (p *Planner) handleWebhook(w http.ResponseWriter, r *http.Request) {
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

	var prNumber int
	if (event == "pull_request" && payload.Action == "opened") ||
		(event == "pull_request" && payload.Action == "synchronize") ||
		(event == "pull_request" && payload.Action == "reopened") {
		prNumber = payload.Number
	}
	if event == "issue_comment" && payload.Action == "created" {
		prNumber = payload.Issue.Number
	}

	if prNumber == 0 {
		return
	}

	ctx := context.Background()
	repo, err := giturl.Parse(payload.Repository.GitURL)
	if err != nil {
		p.Log.Error("error", err)
		return
	}
	// repoFullName := payload.Repository.FullName

	// Respond with 200 OK
	w.WriteHeader(http.StatusOK)

	kubeModuleList := &tfaplv1beta1.ModuleList{}
	if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
		p.Log.Error("error retrieving list of modules", "error", err)
		return
	}

	// Make a GraphQL query to fetch all open Pull Requests from Github
	prs, err := p.github.openPRs(ctx, repo.Path, repo.Repo)
	if err != nil {
		p.Log.Error("error making GraphQL request:", "error", err)
		return
	}

	// Loop through all open PRs
	for _, pr := range prs {
		p.processPullRequest(ctx, pr, kubeModuleList)
	}
}
