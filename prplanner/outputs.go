package prplanner

import (
	"context"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
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

		run, err := p.RedisClient.Run(ctx, msg.Payload)
		if err != nil {
			p.Log.Error("unable to get run output", "key", msg.Payload, "err", err)
			continue
		}

		if run.Output == "" {
			continue
		}

		var prNum, CommentID int

		if run.Request.PR != nil {
			prNum = run.Request.PR.Number
			CommentID = run.Request.PR.CommentID
		}

		// if its not a PR run then also
		// check if there is pending task for output upload
		if prNum == 0 {
			if pr, err := p.RedisClient.PendingApplyUploadPR(ctx, run.Module, run.CommitHash); err == nil {
				prNum, _ = strconv.Atoi(pr)
			}
		}

		// this is required in case this run is not a PR run && not apply run
		if prNum == 0 {
			continue
		}

		var module tfaplv1beta1.Module
		err = p.ClusterClt.Get(ctx, run.Module, &module)
		if err != nil {
			p.Log.Error("unable to get module", "module", run.Module, "error", err)
			continue
		}

		comment := prComment{
			Body: runOutputMsg(p.ClusterEnvName, run.Module.String(), module.Spec.Path, run),
		}

		repo, err := giturl.Parse(module.Spec.RepoURL)
		if err != nil {
			p.Log.Error("unable to parse repo url", "module", run.Module, "error", err)
			continue
		}

		_, err = p.github.postComment(repo.Path, strings.TrimSuffix(repo.Repo, ".git"), CommentID, prNum, comment)
		if err != nil {
			p.Log.Error("error posting PR comment:", "error", err)
			continue
		}
		p.Log.Info("run output posted", "module", run.Module, "pr", prNum)
	}
}
