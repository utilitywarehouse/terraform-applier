package prplanner

import (
	"context"
	"fmt"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func (p *Planner) ProcessPRWebHookEvent(event GitHubWebhook, prNumber int) {
	ctx := context.Background()

	pr, err := p.github.PR(ctx, event.Repository.Owner.Login, event.Repository.Name, prNumber)
	if err != nil {
		p.Log.Error("unable to get PR info", "pr", prNumber, "err", err)
		return
	}

	kubeModuleList := &tfaplv1beta1.ModuleList{}
	if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
		p.Log.Error("error retrieving list of modules", "pr", prNumber, "error", err)
		return
	}

	p.Log.Debug("processing PR event", "pr", prNumber)
	p.processPullRequest(ctx, pr, kubeModuleList)
}

func (p *Planner) ProcessPRCloseEvent(e GitHubWebhook) {
	ctx := context.Background()

	if e.Action != "closed" ||
		e.PullRequest.Draft ||
		!e.PullRequest.Merged {
		return
	}

	kubeModuleList := &tfaplv1beta1.ModuleList{}
	if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
		p.Log.Error("error retrieving list of modules", "pr", e.Number, "error", err)
		return
	}

	// get list of commits and changed file for the merged commit
	commitsInfo, err := p.Repos.MergeCommits(ctx, e.Repository.URL, e.PullRequest.MergeCommitSHA)
	if err != nil {
		p.Log.Error("unable to commit info", "repo", e.Repository.URL, "pr", e.Number, "mergeCommit", e.PullRequest.MergeCommitSHA, "error", err)
		return
	}

	for _, module := range kubeModuleList.Items {
		// make sure there was actually plan runs on the PR
		// this is to avoid uploading apply output on filtered PR
		runs, _ := p.RedisClient.Runs(ctx, module.NamespacedName(), fmt.Sprintf("PR:%d:*", e.Number))
		if len(runs) == 0 {
			continue
		}

		for _, commit := range commitsInfo {
			if !isModuleUpdated(&module, commit) {
				continue
			}

			err := p.RedisClient.SetPendingApplyUpload(ctx, module.NamespacedName(), commit.Hash, e.Number)
			if err != nil {
				p.Log.Error("unable to set pending apply upload", "module", module.NamespacedName(), "repo", e.Repository.URL, "pr", e.Number, "mergeCommit", e.PullRequest.MergeCommitSHA, "error", err)
				break
			}
			// only process 1 latest commit /module
			break
		}
	}

}
