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

	"k8s.io/apimachinery/pkg/types"
)

func (ps *Server) serveOutputRequests(ctx context.Context, repo gitHubRepo, pr pr) {
	outputs := ps.getPendinPRUpdates(ctx, pr)

	for _, output := range outputs {
		ps.postPlanOutput(output, repo)
	}
}

func (ps *Server) getPendinPRUpdates(ctx context.Context, pr pr) []output {
	var outputs []output
	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]
		ps.checkPRCommentsForOutputRequests(ctx, &outputs, pr, comment)
	}

	return outputs
}

func (ps *Server) checkPRCommentsForOutputRequests(ctx context.Context, outputs *[]output, pr pr, comment prComment) {

	if strings.Contains(comment.Body, "Received terraform plan request") {
		prCommentModule, err := ps.findModuleNameInComment(comment.Body)
		if err != nil {
			ps.Log.Error("error getting module name and req ID from PR comment", err)
			return
		}

		// if module.Name == prCommentModule {
		planOutput, err := ps.getPlanOutputFromRedis(ctx, pr, "", prCommentModule)
		if err != nil {
			ps.Log.Error("can't check plan output in Redis:", err)
			return
		}

		if planOutput == "" {
			return // plan output is not ready yet
		}

		commentBody := prComment{
			Body: fmt.Sprintf(
				"Terraform plan output for module `%s`\n```terraform\n%s\n```",
				prCommentModule,
				planOutput,
			),
		}
		newOutput := output{
			module:    prCommentModule,
			commentID: comment.DatabaseID,
			prNumber:  pr.Number,
			body:      commentBody,
		}
		*outputs = append(*outputs, newOutput)
		// }
	}
}

// TODO: move re1 outside of func
func (ps *Server) findModuleNameInComment(commentBody string) (types.NamespacedName, error) {
	// Search for module name and req ID
	re1 := regexp.MustCompile(`Module: ` + "`" + `(.+?)` + "`" + ` Request ID: ` + "`" + `(.+?)` + "`")

	matches := re1.FindStringSubmatch(commentBody)

	if len(matches) == 3 {
		namespacedName := strings.Split(matches[1], "/")
		return types.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}, nil

	}

	return types.NamespacedName{}, nil
}

func (ps *Server) findModuleNameInRunRequestComment(commentBody string) (string, error) {
	// TODO: Match "@terraform-applier plan "
	// Search for module name only
	re2 := regexp.MustCompile("`([^`]*)`")
	matches := re2.FindStringSubmatch(commentBody)

	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("module data not found")
}

func (ps *Server) postPlanOutput(output output, repo gitHubRepo) {
	_, err := ps.postToGitHub(repo, "PATCH", output.commentID, output.prNumber, output.body)
	if err != nil {
		ps.Log.Error("error posting PR comment:", err)
	}
}

func (ps *Server) getPlanOutputFromRedis(ctx context.Context, pr pr, prCommentReqID string, module types.NamespacedName) (string, error) {
	moduleRuns, err := ps.RedisClient.Runs(ctx, module)
	if err != nil {
		return "", err
	}

	for _, run := range moduleRuns {
		if run.Request.ID == prCommentReqID {
			return run.Output, nil
		}
	}

	return "", nil
}

func (ps *Server) postToGitHub(repo gitHubRepo, method string, commentID, prNumber int, commentBody prComment) (int, error) {
	// TODO: Update credentials
	// Temporarily using my own github user and token
	username := "DTLP"
	token := os.Getenv("GITHUB_TOKEN")

	// Post a comment
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", repo.owner, repo.name, prNumber)
	if method == "PATCH" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/%/issues/comments/%d", repo.owner, repo.name, commentID)
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

// func repoNameFromURL(url string) string {
// 	trimmedURL := strings.TrimSuffix(url, ".git")
// 	parts := strings.Split(trimmedURL, ":")
// 	if len(parts) < 2 {
// 		return ""
// 	}
// 	return parts[1]
// }
