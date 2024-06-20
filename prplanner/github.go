package prplanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
)

func (ps *Planner) getOpenPullRequests(ctx context.Context, repo *mirror.GitURL) ([]pr, error) {
	url := "https://api.github.com/graphql"
	query := `
	query {
		repository(owner: "` + repo.Path + `", name: "` + repo.Repo + `") {
			pullRequests(states: OPEN, last: 100) {
				nodes {
					number
					headRefName
					commits(last: 20) {
						nodes {
							commit {
								oid
							}
						}
					}
					comments(last:20) {
						nodes {
							databaseId
							body
						}
					}
					files(first: 100) {
						nodes {
							path
						}
					}
				}
			}
		}
	}`

	q := gitPRRequest{Query: query}

	resp, err := ps.github.http.R().
		SetContext(ctx).
		SetBody(q).
		SetResult(&gitPRResponse{}).
		Post(url)

	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("http status %d Error: %s", resp.StatusCode(), resp.Body())
	}

	result := resp.Result().(*gitPRResponse)

	return result.Data.Repository.PullRequests.Nodes, nil
}

func (ps *Planner) postToGitHub(repo *mirror.GitURL, method string, commentID, prNumber int, commentBody prComment) (int, error) {
	// TODO: Update credentials
	// Temporarily using my own github user and token
	username := "DTLP"
	token := os.Getenv("GITHUB_TOKEN")

	// Post a comment
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", repo.Path, repo.Repo, prNumber)
	if method == "PATCH" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%/issues/comments/%d", repo.Path, repo.Repo, commentID)
	}

	// Marshal the comment object to JSON
	commentJSON, err := json.Marshal(commentBody)
	if err != nil {
		return 0, fmt.Errorf("error marshalling comment to JSON: %w", err)
	}

	// Create a new HTTP request
	req, err := http.NewRequest(method, url, bytes.NewBuffer(commentJSON))
	if err != nil {
		return 0, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(username, token)

	// Send the HTTP request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error sending HTTP request: %w", err)
	}
	defer resp.Body.Close()

	var commentResponse struct {
		ID int `json:"id"`
	}

	err = json.NewDecoder(resp.Body).Decode(&commentResponse)
	if err != nil {
		return 0, err
	}

	// Check the response status
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return 0, fmt.Errorf("error creating PR comment: %s", resp.Status)
	}

	return commentResponse.ID, nil
}
