package prplanner

import (
	"context"
	"fmt"
	"strings"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"

	"k8s.io/apimachinery/pkg/types"
)

func (p *Planner) uploadRequestOutput(ctx context.Context, repo *mirror.GitURL, pr *pr) {
	outputs := p.getPendinPRUpdates(ctx, pr)

	for _, output := range outputs {
		p.postPlanOutput(output, repo)
	}
}

func (p *Planner) getPendinPRUpdates(ctx context.Context, pr *pr) []output {
	var outputs []output
	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]
		p.checkPRCommentForOutputRequests(ctx, &outputs, pr, comment)
	}

	return outputs
}

func (p *Planner) checkPRCommentForOutputRequests(ctx context.Context, outputs *[]output, pr *pr, comment prComment) {
	fmt.Println("checkPRCommentsForOutputRequests")
	outputRequest := p.findOutputRequestDataInComment(comment.Body)
	if len(outputRequest) != 4 {
		return
	}

	moduleName := outputRequest[1]
	moduleSplit := strings.Split(moduleName, "/")
	moduleNamespacedName := types.NamespacedName{
		Namespace: moduleSplit[0],
		Name:      moduleSplit[1],
	}
	commitID := outputRequest[3]

	// ger run output from Redis
	run, err := p.RedisClient.PRRun(ctx, moduleNamespacedName, pr.Number, commitID)
	if err != nil {
		return
	}

	if run.Output == "" {
		return // plan output is not ready yet
	}

	commentBody := prComment{
		Body: fmt.Sprintf(
			"Terraform plan output for module `%s`. Commit ID: `%s`\n```terraform\n%s\n```",
			moduleName,
			run.CommitHash,
			run.Output,
		),
	}

	newOutput := output{
		module:    moduleNamespacedName,
		commentID: comment.DatabaseID,
		prNumber:  pr.Number,
		body:      commentBody,
	}
	*outputs = append(*outputs, newOutput)
}

func (p *Planner) findOutputRequestDataInComment(commentBody string) []string {
	matches := requestAcknowledgedRegex.FindStringSubmatch(commentBody)
	if len(matches) != 4 {
		return []string{}
	}

	return matches
}

func (p *Planner) findModuleNameInPlanRequestComment(commentBody string) (bool, types.NamespacedName) {
	matches := terraformPlanRequestRegex.FindStringSubmatch(commentBody)

	if len(matches) == 0 {
		return false, types.NamespacedName{}
	}

	if len(matches) == 3 && matches[2] != "" {
		namespacedName := strings.Split(matches[1], "/")
		return true, types.NamespacedName{Namespace: namespacedName[1], Name: namespacedName[2]}
	}

	return true, types.NamespacedName{}
}

func (p *Planner) postPlanOutput(output output, repo *mirror.GitURL) {
	_, err := p.github.postComment(repo, output.commentID, output.prNumber, output.body)
	if err != nil {
		p.Log.Error("error posting PR comment:", err)
	}
}

func (p *Planner) getRunFromRedis(ctx context.Context, pr pr, prCommentReqID string, module types.NamespacedName) (*tfaplv1beta1.Run, error) {
	moduleRuns, err := p.RedisClient.Runs(ctx, module)
	if err != nil {
		return &tfaplv1beta1.Run{}, err
	}

	for _, run := range moduleRuns {
		if run.Request.ID == prCommentReqID {
			return run, nil
		}
	}

	return &tfaplv1beta1.Run{}, nil
}

// func repoNameFromURL(url string) string {
// 	trimmedURL := strings.TrimSuffix(url, ".git")
// 	parts := strings.Split(trimmedURL, ":")
// 	if len(parts) < 2 {
// 		return ""
// 	}
// 	return parts[1]
// }
