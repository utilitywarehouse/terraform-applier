package prplanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
)

type gitHubClient struct {
	rootURL string
	http    *http.Client
	token   string
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
