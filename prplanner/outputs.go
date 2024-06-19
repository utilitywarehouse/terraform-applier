package prplanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func (ps *Server) getPendinPRUpdates(ctx context.Context, outputs []output, pr pr, prModules []tfaplv1beta1.Module) []output {
	// Go through PR comments in reverse order
	for _, module := range prModules {

		for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
			comment := pr.Comments.Nodes[i]

			if strings.Contains(comment.Body, "Received terraform plan request") {
				prCommentModule, prCommentReqID, err := ps.findModuleNameInComment(comment.Body)
				if err != nil {
					ps.Log.Error("error getting module name and req ID from PR comment", err)
					return nil
				}

				if module.Name == prCommentModule {
					planOutput, err := ps.getPlanOutputFromRedis(ctx, module, prCommentReqID, pr)
					if err != nil {
						ps.Log.Error("can't check plan output in Redis:", err)
						break
					}

					if planOutput == "" {
						break // plan output is not ready yet
					}

					commentBody := prComment{
						Body: fmt.Sprintf(
							"Terraform plan output for module `%s`\n```terraform\n%s\n```",
							module.Name,
							planOutput,
						),
					}
					newOutput := output{
						module:    module,
						commentID: comment.DatabaseID,
						prNumber:  pr.Number,
						body:      commentBody,
					}
					outputs = append(outputs, newOutput)
					break
				}
			}
		}
	}

	return outputs
}

func (ps *Server) findModuleNameInComment(commentBody string) (string, string, error) {
	// Search for module name and req ID
	re1 := regexp.MustCompile(`Module: ` + "`" + `(.+?)` + "`" + ` Request ID: ` + "`" + `(.+?)` + "`")
	matches1 := re1.FindStringSubmatch(commentBody)

	if len(matches1) == 3 {
		return matches1[1], matches1[2], nil
	}

	// Search for module name only
	re2 := regexp.MustCompile("`([^`]*)`")
	matches2 := re2.FindStringSubmatch(commentBody)

	if len(matches2) > 1 {
		return matches2[1], "", nil
	}

	return "", "", fmt.Errorf("module data not found")
}

func (ps *Server) postPlanOutput(outputs []output) {
	for _, output := range outputs {
		_, err := ps.postToGitHub(output.module.Spec.RepoURL, "PATCH", output.commentID, output.prNumber, output.body)
		if err != nil {
			ps.Log.Error("error posting PR comment:", err)
		}
	}
}

func (ps *Server) getPlanOutputFromRedis(ctx context.Context, module tfaplv1beta1.Module, prCommentReqID string, pr pr) (string, error) {
	lastRun, err := ps.RedisClient.PRLastRun(ctx, module.NamespacedName(), pr.Number)
	if err != nil {
		return "", err
	}

	if lastRun == nil {
		return "", nil
	}

	if prCommentReqID == lastRun.Request.ID {
		return lastRun.Output, nil
	}

	return "", nil
}
func (ps *Server) postToGitHub(repoURL, method string, commentID, prNumber int, commentBody prComment) (int, error) {
	// TODO: Update credentials
	// Temporarily using my own github user and token
	username := "DTLP"
	token := os.Getenv("GITHUB_TOKEN")

	repoName := repoNameFromURL(repoURL)

	// Post a comment
	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repoName, prNumber)
	if method == "PATCH" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", repoName, commentID)
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

func repoNameFromURL(url string) string {
	trimmedURL := strings.TrimSuffix(url, ".git")
	parts := strings.Split(trimmedURL, ":")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}
