package prplanner

import (
	"context"
	"errors"
	"fmt"
	"time"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/apimachinery/pkg/types"
)

// 3. loop through pr modules:
//   1. check if modules is annotated
//   if not:
//   no need for this??? 1. verify if pr is new by looking at comments

//   2. loop through commits from latest to oldest:
//     0. verify modules needs to be planned based on files changed
//     if matching:

//     1. check comments if output posted for the commit hash
//     if no comment posted:

//     2. check commit hashes in redis
//     if missing:

//     3. request run

//   3. loop through comments
//     1. check if user requested run
//     if yes:

// 2. request run
func (p *Planner) ensurePlanRequests(ctx context.Context, pr *pr, prModules []types.NamespacedName) {
	var skipCommitRun bool
	if len(prModules) > 5 && !p.isModuleLimitReachedCommentPosted(pr.Comments.Nodes) {
		comment := prComment{
			Body: moduleLimitReachedTml,
		}
		p.github.postComment(pr.BaseRepository.Owner.Login, pr.BaseRepository.Name, 0, pr.Number, comment)

		skipCommitRun = true
	}

	for _, moduleName := range prModules {

		// 1. Check if module has any pending plan request
		module, err := sysutil.GetModule(ctx, p.ClusterClt, moduleName)
		if err != nil {
			p.Log.Error("unable to get module", "module", moduleName, "error", err)
			continue
		}

		req, err := p.ensurePlanRequest(ctx, pr, module, skipCommitRun)
		if err != nil {
			p.Log.Error("unable to generate new plan request", "module", moduleName, "error", err)
			continue
		}
		if req != nil {
			run := tfaplv1beta1.NewRun(module, req)
			cancelChan := make(chan struct{})
			go p.Runner.Start(&run, cancelChan)
		}
	}
}

func (p *Planner) ensurePlanRequest(ctx context.Context, pr *pr, module *tfaplv1beta1.Module, skipCommitRun bool) (*tfaplv1beta1.Request, error) {
	if !skipCommitRun {
		// 1. loop through commits from latest to oldest
		req, err := p.checkPRCommits(ctx, pr, module)
		if err != nil {
			return req, err
		}
		if req != nil {
			return req, nil
		}
	}

	// 2. loop through comments
	return p.checkPRCommentsForPlanRequests(pr, module)
}

func (p *Planner) checkPRCommits(ctx context.Context, pr *pr, module *tfaplv1beta1.Module) (*tfaplv1beta1.Request, error) {
	// loop through commits to check if module path is updated
	for i := len(pr.Commits.Nodes) - 1; i >= 0; i-- {
		commit := pr.Commits.Nodes[i].Commit

		// 1. check if module path is updated in this commit
		updated, err := p.isModuleUpdated(ctx, commit.Oid, module)
		if err != nil {
			return nil, err
		}
		if !updated {
			continue
		}

		// 2. check if we have already processed (uploaded output) this commit
		if isPlanOutputPostedForCommit(pr, commit.Oid, module.NamespacedName()) {
			return nil, nil
		}

		if isPlanRequestAckPostedForCommit(pr, commit.Oid, module.NamespacedName()) {
			return nil, nil
		}

		// 3. check if run is already completed for this commit
		runOutput, err := p.RedisClient.PRRun(ctx, module.NamespacedName(), pr.Number, commit.Oid)
		if err != nil && !errors.Is(err, sysutil.ErrKeyNotFound) {
			return nil, err
		}

		if runOutput != nil && runOutput.CommitHash == commit.Oid {
			return nil, nil
		}

		// 4. request run
		p.Log.Info("triggering plan due to new commit", "module", module.NamespacedName(), "pr", pr.Number, "author", pr.Author.Login)
		return p.addNewRequest(module, pr, commit.Oid)
	}

	return nil, nil
}

func (p *Planner) isModuleUpdated(ctx context.Context, commitHash string, module *tfaplv1beta1.Module) (bool, error) {
	filesChangedInCommit, err := p.Repos.ChangedFiles(ctx, module.Spec.RepoURL, commitHash)
	if err != nil {
		return false, fmt.Errorf("error getting commit info: %w", err)
	}

	return pathBelongsToModule(filesChangedInCommit, module), nil
}

func (p *Planner) checkPRCommentsForPlanRequests(pr *pr, module *tfaplv1beta1.Module) (*tfaplv1beta1.Request, error) {
	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		// Skip if request already acknowledged for module
		commentModule, _, _, reqAt := parseRequestAcknowledgedMsg(comment.Body)
		if commentModule == module.NamespacedName() &&
			reqAt != nil && time.Until(*reqAt) < 10*time.Minute {
			return nil, nil
		}

		// Skip if terraform plan output is already posted
		commentModule, _ = parseRunOutputMsg(comment.Body)
		if commentModule == module.NamespacedName() {
			return nil, nil
		}

		// Check if user requested terraform plan run
		// '@terraform-applier plan [<module namespace>]/<module name>'
		commentModule = parsePlanReqMsg(comment.Body)
		if commentModule.Name != module.Name {
			continue
		}
		// commented module's namespace needs to match as well if its given by user
		if commentModule.Namespace != "" && commentModule.Namespace != module.Namespace {
			continue
		}

		modulePathHash, err := p.Repos.Hash(context.Background(), module.Spec.RepoURL, pr.HeadRefName, module.Spec.Path)
		if err != nil {
			return nil, err
		}

		p.Log.Info("triggering plan requested via comment", "module", module.NamespacedName(), "pr", pr.Number, "author", comment.Author.Login)
		return p.addNewRequest(module, pr, modulePathHash)
	}

	return nil, nil
}

// isPlanOutputPostedForCommit loops through all the comments to check if given commit
// ids plan output is already posted
func isPlanOutputPostedForCommit(pr *pr, commitID string, module types.NamespacedName) bool {
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		commentModule, commentCommitID := parseRunOutputMsg(comment.Body)
		if commentModule == module && commentCommitID == commitID {
			return true
		}
	}

	return false
}

// isPlanRequestAckPostedForCommit loops through all the comments to check if given commit
// ids plan request is already acknowledged
func isPlanRequestAckPostedForCommit(pr *pr, commitID string, module types.NamespacedName) bool {
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		commentModule, _, commentCommitID, reqAt := parseRequestAcknowledgedMsg(comment.Body)
		if commentModule == module &&
			commentCommitID == commitID &&
			reqAt != nil && time.Until(*reqAt) < 10*time.Minute {
			return true
		}
	}

	return false
}

func (p *Planner) addNewRequest(module *tfaplv1beta1.Module, pr *pr, commitID string) (*tfaplv1beta1.Request, error) {
	req := module.NewRunRequest(tfaplv1beta1.PRPlan)

	commentBody := prComment{
		Body: requestAcknowledgedMsg(module.NamespacedName().String(), module.Spec.Path, commitID, req.RequestedAt),
	}

	commentID, err := p.github.postComment(pr.BaseRepository.Owner.Login, pr.BaseRepository.Name, 0, pr.Number, commentBody)
	if err != nil {
		return req, fmt.Errorf("unable to post pending request comment: %w", err)
	}

	req.PR = &tfaplv1beta1.PullRequest{
		Number:     pr.Number,
		HeadBranch: pr.HeadRefName,
		CommentID:  commentID,
	}

	return req, nil
}

func (p *Planner) isModuleLimitReachedCommentPosted(prComments []prComment) bool {
	for _, comment := range prComments {
		matches := moduleLimitReachedRegex.FindStringSubmatch(comment.Body)
		if len(matches) == 1 {
			return true
		}
	}

	return false
}
