package prplanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

//go:generate go run github.com/golang/mock/mockgen -package prplanner -destination github_mock.go github.com/utilitywarehouse/terraform-applier/prplanner GithubInterface

// GithubInterface allows for mocking out the functionality of GitHub API Calls
type GithubInterface interface {
	openPRs(ctx context.Context, repo *mirror.GitURL) ([]*pr, error)
	postComment(repo *mirror.GitURL, commentID, prNumber int, commentBody prComment) (int, error)
}

type gitHubClient struct {
	rootURL string
	http    *http.Client
	token   string
}

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
	repoURL, err := mirror.ParseGitURL(payload.Repository.GitURL)
	if err != nil {
		p.Log.Error("error", err)
		return
	}
	repoFullName := payload.Repository.FullName

	// Respond with 200 OK
	w.WriteHeader(http.StatusOK)

	kubeModuleList := &tfaplv1beta1.ModuleList{}
	if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
		p.Log.Error("error retrieving list of modules", "error", err)
		return
	}

	// Make a GraphQL query to fetch all open Pull Requests from Github
	prs, err := p.github.openPRs(ctx, repoURL)
	if err != nil {
		p.Log.Error("error making GraphQL request:", "error", err)
		return
	}

	// Loop through all open PRs
	for _, pr := range prs {
		p.processPullRequest(ctx, repoURL, repoFullName, pr, kubeModuleList)
	}
}

func (gc *gitHubClient) openPRs(ctx context.Context, repo *mirror.GitURL) ([]*pr, error) {
	repoName := strings.TrimSuffix(repo.Repo, ".git")

	q := gitPRRequest{Query: queryRepoPRs}
	q.Variables.Owner = repo.Path
	q.Variables.RepoName = repoName

	payload, err := json.Marshal(q)
	if err != nil {
		return nil, fmt.Errorf("error marshalling pr query err:%w", err)
	}

	// Create a new HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", gc.rootURL+"/graphql", bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+gc.token)

	// Send the HTTP request
	resp, err := gc.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check the response status
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("http error getting prs: %s", resp.Status)
	}

	var result gitPRResponse

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, err
	}

	return result.Data.Repository.PullRequests.Nodes, nil
}

func (gc *gitHubClient) postComment(repo *mirror.GitURL, commentID, prNumber int, commentBody prComment) (int, error) {
	repoName := strings.TrimSuffix(repo.Repo, ".git")

	method := "POST"
	reqURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", gc.rootURL, repo.Path, repoName, prNumber)

	// if comment ID provided update same comment
	if commentID != 0 {
		method = "PATCH"
		reqURL = fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d", gc.rootURL, repo.Path, repoName, commentID)
	}

	payload, err := json.Marshal(commentBody)
	if err != nil {
		return 0, fmt.Errorf("error marshalling comment to JSON: %w", err)
	}

	// Create a new HTTP request
	req, err := http.NewRequest(method, reqURL, bytes.NewBuffer(payload))
	if err != nil {
		return 0, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+gc.token)

	// Send the HTTP request
	resp, err := gc.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Check the response status
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("error posting PR comment: %s", resp.Status)
	}

	var commentResponse struct {
		ID int `json:"id"`
	}

	err = json.NewDecoder(resp.Body).Decode(&commentResponse)
	if err != nil {
		return 0, err
	}

	return commentResponse.ID, nil
}
