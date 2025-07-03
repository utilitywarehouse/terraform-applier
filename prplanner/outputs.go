package prplanner

import (
	"context"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/utilitywarehouse/git-mirror/giturl"
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
		p.Log.Info("run output uploaded", "module", moduleNamespacedName, "pr", pr.Number)
	}
}

func (p *Planner) processRedisKeySetMsg(ctx context.Context, ch <-chan *redis.Message) {
	p.Log.Info("starting redis update watcher")
	defer p.Log.Error("stopping redis update watcher")

	for msg := range ch {
		// make sure we only process `set` keyevent
		if msg.Channel != "__keyevent@0__:set" {
			continue
		}

		key := msg.Payload

		// skip non run related keys
		// and process default output only once
		if !strings.Contains(key, ":default:lastRun") &&
			!strings.Contains(key, ":PR:") {
			continue
		}

		run, err := p.RedisClient.Run(ctx, key)
		if err != nil {
			p.Log.Error("unable to get run output", "key", key, "err", err)
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
		if prNum == 0 && strings.Contains(key, "default:lastRun") {
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
			p.Log.Error("unable to get module", "module", run.Module, "pr", prNum, "error", err)
			continue
		}

		comment := prComment{
			Body: runOutputMsg(p.ClusterEnvName, run.Module.String(), module.Spec.Path, run),
		}

		repo, err := giturl.Parse(module.Spec.RepoURL)
		if err != nil {
			p.Log.Error("unable to parse repo url", "module", run.Module, "pr", prNum, "error", err)
			continue
		}

		_, err = p.github.postComment(repo.Path, strings.TrimSuffix(repo.Repo, ".git"), CommentID, prNum, comment)
		if err != nil {
			p.Log.Error("error posting PR comment:", "module", run.Module, "pr", prNum, "error", err)
			continue
		}

		p.Log.Info("run output posted", "module", run.Module, "pr", prNum)

		// if apply output is posted then clean up PR runs
		if strings.Contains(key, "default:lastRun") {
			if err := p.RedisClient.CleanupPRKeys(ctx, run.Module, prNum, run.CommitHash); err != nil {
				p.Log.Error("error cleaning PR keys:", "module", run.Module, "pr", prNum, "error", err)
				continue
			}
		}
	}
}
