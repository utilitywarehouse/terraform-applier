package prplanner

import (
	"context"
	"fmt"
	"regexp"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"

	"k8s.io/apimachinery/pkg/types"
)

var (
	outputBodyTml         = "Terraform plan output for module `%s` Commit ID: `%s`\n```terraform\n%s\n```"
	terraformPlanOutRegex = regexp.MustCompile("Terraform plan output for module `(.+?)` Commit ID: `(.+?)`")
)

func (p *Planner) uploadRequestOutput(ctx context.Context, repo *mirror.GitURL, pr *pr) {
	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]
		output, ok := p.checkPRCommentForOutputRequests(ctx, pr, comment)
		if ok {
			p.postPlanOutput(output, repo)
		}
	}
}

func (p *Planner) checkPRCommentForOutputRequests(ctx context.Context, pr *pr, comment prComment) (output, bool) {
	moduleNamespacedName, commitID := findOutputRequestDataInComment(comment.Body)
	if commitID == "" {
		return output{}, false
	}

	// ger run output from Redis
	run, err := p.RedisClient.PRRun(ctx, moduleNamespacedName, pr.Number, commitID)
	if err != nil {
		return output{}, false
	}

	if run.Output == "" {
		return output{}, false
	}

	commentBody := prComment{
		Body: fmt.Sprintf(
			outputBodyTml,
			moduleNamespacedName,
			run.CommitHash,
			run.Output,
		),
	}

	return output{
		module:    moduleNamespacedName,
		commentID: comment.DatabaseID,
		prNumber:  pr.Number,
		body:      commentBody,
	}, true
}

func findOutputRequestDataInComment(commentBody string) (types.NamespacedName, string) {
	matches := requestAcknowledgedRegex.FindStringSubmatch(commentBody)
	if len(matches) == 4 {
		return parseNamespaceName(matches[1]), matches[3]
	}

	return types.NamespacedName{}, ""
}

func (p *Planner) postPlanOutput(output output, repo *mirror.GitURL) {
	_, err := p.github.postComment(repo, output.commentID, output.prNumber, output.body)
	if err != nil {
		p.Log.Error("error posting PR comment:", "error", err)
	}
}
