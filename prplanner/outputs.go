package prplanner

import (
	"context"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
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
	moduleNamespacedName, path, requestedAt := parseRequestAcknowledgedMsg(comment.Body)
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
				Body: runOutputMsg(moduleNamespacedName.String(), path, run),
			}, true
		}
	}

	return prComment{}, false
}
