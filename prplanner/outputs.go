package prplanner

import (
	"context"
	"fmt"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"

	"k8s.io/apimachinery/pkg/types"
)

func (p *Planner) uploadRequestOutput(ctx context.Context, repo *mirror.GitURL, pr *pr) {
	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]
		commentBody, ok := p.checkPRCommentForOutputRequests(ctx, comment)
		if ok {
			_, err := p.github.postComment(repo, comment.DatabaseID, pr.Number, commentBody)
			if err != nil {
				p.Log.Error("error posting PR comment:", "error", err)
				continue
			}
		}
	}
}

func (p *Planner) checkPRCommentForOutputRequests(ctx context.Context, comment prComment) (prComment, bool) {
	moduleNamespacedName, path, requestedAt := requestAcknowledgedCommentInfo(comment.Body)
	if requestedAt == nil {
		return prComment{}, false
	}

	// ger run output from Redis
	runs, err := p.RedisClient.Runs(ctx, moduleNamespacedName)
	if err != nil {
		return prComment{}, false
	}

	for _, run := range runs {
		if run.Request.RequestedAt.Compare(*requestedAt) == 0 {

			if run.Output == "" {
				return prComment{}, false
			}

			return prComment{
				Body: outputBody(moduleNamespacedName.String(), path, run),
			}, true
		}
	}

	return prComment{}, false
}

func requestAcknowledgedCommentInfo(commentBody string) (types.NamespacedName, string, *time.Time) {
	matches := requestAcknowledgedRegex.FindStringSubmatch(commentBody)
	if len(matches) == 4 {
		t, err := time.Parse(time.RFC3339, matches[3])
		if err == nil {
			return parseNamespaceName(matches[1]), matches[2], &t
		}
		return parseNamespaceName(matches[1]), matches[2], nil
	}

	return types.NamespacedName{}, "", nil
}

func outputBody(module, path string, run *v1beta1.Run) string {
	// TODO: max character limit needs to be set for run.Output
	// https://github.com/orgs/community/discussions/27190
	return fmt.Sprintf(
		outputBodyTml,
		module, path, run.CommitHash, run.Summary, run.Output,
	)
}
