package prplanner

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
)

func (p *Planner) uploadRequestOutput(ctx context.Context, pr *pr) {
	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		cluster, moduleNamespacedName, path, commitID, requestedAt := parseRequestAcknowledgedMsg(comment.Body)
		if requestedAt == nil {
			continue
		}

		if cluster != p.ClusterEnvName {
			continue
		}

		// ger run output from Redis
		run, err := p.RedisClient.PRRun(ctx, moduleNamespacedName, pr.Number, commitID)
		if err != nil {
			continue
		}

		if run.Output == "" {
			continue
		}

		payload := prComment{
			Body: runOutputMsg(p.ClusterEnvName, moduleNamespacedName.String(), path, run),
		}

		_, err = p.github.postComment(pr.BaseRepository.Owner.Login, pr.BaseRepository.Name, comment.DatabaseID, pr.Number, payload)
		if err != nil {
			p.Log.Error("error posting PR comment:", "error", err)
			continue
		}
		p.Log.Info("run output posted", "module", moduleNamespacedName, "pr", pr)
	}
}

func (p *Planner) processRedisKeySetMsg(ctx context.Context, ch <-chan *redis.Message) {
	p.Log.Info("starting redis update watcher")
	defer p.Log.Info("stopping redis update watcher")

	for msg := range ch {
		// make sure we only process `set` keyevent
		if msg.Channel != "__keyevent@0__:set" {
			continue
		}

		// make sure updated key is PR run output
		moduleName, pr, hash, err := sysutil.ParsePRRunsKey(msg.Payload)
		if err != nil {
			continue
		}

		run, err := p.RedisClient.PRRun(ctx, moduleName, pr, hash)
		if err != nil {
			p.Log.Error("unable to get run output", "key", msg.Payload, "err", err)
			continue
		}

		if run.Output == "" {
			continue
		}

		var module tfaplv1beta1.Module
		err = p.ClusterClt.Get(ctx, moduleName, &module)
		if err != nil {
			p.Log.Error("unable to get module", "module", moduleName, "error", err)
			continue
		}

		comment := prComment{
			Body: runOutputMsg(p.ClusterEnvName, moduleName.String(), module.Spec.Path, run),
		}

		repo, err := giturl.Parse(module.Spec.RepoURL)
		if err != nil {
			p.Log.Error("unable to parse repo url", "module", moduleName, "error", err)
			continue
		}

		_, err = p.github.postComment(repo.Path, strings.TrimSuffix(repo.Repo, ".git"), run.Request.PR.CommentID, pr, comment)
		if err != nil {
			p.Log.Error("error posting PR comment:", "error", err)
			continue
		}
		p.Log.Info("run output posted", "module", moduleName, "pr", pr)
	}
}
