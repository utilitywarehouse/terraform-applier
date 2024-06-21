package prplanner

import (
	"context"
	"fmt"
	"regexp"
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
	// TODO: Find "Received terraform plan request" comment using regex
	// requestAckRegex := regexp.MustCompile(`Module: ` + "`" + `(.+?)` + "`" + ` Request ID: ` + "`" + `(.+?)` + "`")
	//
	//
	//
	//
	outputRequest := p.findOutputRequestDataInComment(comment.Body)
	fmt.Println("outputRequest:", outputRequest)
	fmt.Println("len outputRequest:", outputRequest)
	// if strings.Contains(comment.Body, "Received terraform plan request") {
	// 	prCommentModule, err := ps.findModuleNameInComment(comment.Body)
	// 	if err != nil {
	// 		ps.Log.Error("error getting module name and req ID from PR comment", err)
	// 		return
	// 	}
	//
	// 	// if module.Name == prCommentModule {
	// 	run, err := ps.getRunFromRedis(ctx, pr, "", prCommentModule)
	// 	if err != nil {
	// 		ps.Log.Error("can't check plan output in Redis:", err)
	// 		return
	// 	}
	//
	// 	if run.Output == "" {
	// 		return // plan output is not ready yet
	// 	}
	//
	// 	commentBody := prComment{
	// 		Body: fmt.Sprintf(
	// 			"Terraform plan output for module `%s`. Commit ID: `%s`\n```terraform\n%s\n```",
	// 			prCommentModule,
	// 			run.CommitHash,
	// 			run.Output,
	// 		),
	// 	}
	//
	// 	newOutput := output{
	// 		module:    prCommentModule,
	// 		commentID: comment.DatabaseID,
	// 		prNumber:  pr.Number,
	// 		body:      commentBody,
	// 	}
	// 	*outputs = append(*outputs, newOutput)
	// 	// }
	// }
}

func (p *Planner) findOutputRequestDataInComment(commentBody string) []string {
	// TODO: Dirty prototype
	// Improve flow and formatting if this works
	searchPattern := fmt.Sprintf("Received terraform plan request. Module: `(.+?)` Request ID: `(.+?)` Commit ID: `(.+?)`")
	re := regexp.MustCompile(searchPattern)
	matches := re.FindStringSubmatch(commentBody)
	// searchString := regexp.MustCompile(`Module: ` + "`" + `(.+?)` + "`" + ` Request ID: ` + "`" + `(.+?)` + "`")
	// if strings.Contains(comment.Body, commitID) {
	if len(matches) == 4 {
		return matches
	}

	return []string{}
}

// TODO: regex should be compiled outside of func
func (p *Planner) findModuleNameInComment(commentBody string) (types.NamespacedName, error) {
	// Search for module name and req ID
	re1 := regexp.MustCompile(`Module: ` + "`" + `(.+?)` + "`" + ` Request ID: ` + "`" + `(.+?)` + "`")

	matches := re1.FindStringSubmatch(commentBody)

	if len(matches) == 3 {
		namespacedName := strings.Split(matches[1], "/")
		return types.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}, nil

	}

	return types.NamespacedName{}, nil
}

func (p *Planner) findModuleNameInRunRequestComment(commentBody string) (string, error) {
	// TODO: Match "@terraform-applier plan "
	// Search for module name only
	re2 := regexp.MustCompile("`([^`]*)`")
	matches := re2.FindStringSubmatch(commentBody)

	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("module data not found")
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
