package prplanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

//go:generate go run github.com/golang/mock/mockgen -package prplanner -destination github_mock.go github.com/utilitywarehouse/terraform-applier/prplanner GithubInterface

// GithubInterface allows for mocking out the functionality of GitHub API Calls
type GithubInterface interface {
	openPRs(ctx context.Context, repoOwner, repoName string) ([]*pr, error)
	PR(ctx context.Context, repoOwner, repoName string, prNumber int) (*pr, error)
	postComment(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error)
}

type gitHubClient struct {
	rootURL       string
	http          *http.Client
	credsProvider sysutil.CredsProvider
}

func (gc *gitHubClient) openPRs(ctx context.Context, repoOwner, repoName string) ([]*pr, error) {
	repoName = strings.TrimSuffix(repoName, ".git")
	q := gitPRRequest{Query: queryRepoPRs}
	q.Variables.Owner = repoOwner
	q.Variables.RepoName = repoName

	var result gitPRsResponse

	err := gc.query(ctx, q, &result)
	if err != nil {
		return nil, fmt.Errorf("unable to get PRs, error :%w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("api error %+v", result.Errors)
	}

	return result.Data.Repository.PullRequests.Nodes, nil
}

func (gc *gitHubClient) PR(ctx context.Context, repoOwner, repoName string, prNumber int) (*pr, error) {
	repoName = strings.TrimSuffix(repoName, ".git")
	q := gitPRRequest{Query: queryRepoPR}
	q.Variables.Owner = repoOwner
	q.Variables.RepoName = repoName
	q.Variables.PRNumber = prNumber

	var result gitPRResponse

	err := gc.query(ctx, q, &result)
	if err != nil {
		return nil, fmt.Errorf("unable to get PR, err:%w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("api error %+v", result.Errors)
	}

	return result.Data.Repository.PullRequest, nil
}

func (gc *gitHubClient) query(ctx context.Context, q gitPRRequest, result any) error {
	payload, err := json.Marshal(q)
	if err != nil {
		return err
	}

	// Create a new HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", gc.rootURL+"/graphql", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set headers
	_, token, err := gc.credsProvider.Creds(ctx)
	if err != nil {
		return fmt.Errorf("unable to provide creds err:%w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Send the HTTP request
	resp, err := gc.http.Do(req)
	if err != nil {
		return fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// Check the response status
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("HTTP error: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(&result)
}

func (gc *gitHubClient) postComment(repoOwner, repoName string, commentID, prNumber int, commentBody prComment) (int, error) {
	repoName = strings.TrimSuffix(repoName, ".git")
	method := "POST"
	reqURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", gc.rootURL, repoOwner, repoName, prNumber)

	// if comment ID provided update same comment
	if commentID != 0 {
		method = "PATCH"
		reqURL = fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d", gc.rootURL, repoOwner, repoName, commentID)
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
	_, token, err := gc.credsProvider.Creds(context.Background())
	if err != nil {
		return 0, fmt.Errorf("unable to provide creds err:%w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	// Send the HTTP request
	resp, err := gc.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

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
