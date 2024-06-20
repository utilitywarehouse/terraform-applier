package prplanner

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"

	"k8s.io/apimachinery/pkg/types"
)

var (
	terraformPlanOutRegex    = regexp.MustCompile("Terraform plan output for module `(.+?)`. Commit ID: `(.+?)`")
	requestAcknowledgedRegex = regexp.MustCompile("Received terraform plan request. Module: `(.+?)`")
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
func (p *Planner) ensurePlanRequests(ctx context.Context, repo *mirror.GitURL, pr pr, prModules []types.NamespacedName) {
	for _, moduleName := range prModules {

		var module tfaplv1beta1.Module
		err := p.ClusterClt.Get(ctx, moduleName, &module)
		if err != nil {
			p.Log.Error("unable to get module", "module", moduleName, "error", err)
			continue
		}

		// 1. Check if module is annotated
		// no need to proceed if there's already a plan request for the module
		_, ok := module.PendingRunRequest()
		if ok {
			continue
		}

		err = p.ensurePlanRequest(ctx, repo, pr, module)
		if err != nil {
			p.Log.Error("unable to generate new plan request", "module", moduleName, "error", err)
			continue
		}
	}
}

func (p *Planner) ensurePlanRequest(ctx context.Context, repo *mirror.GitURL, pr pr, module tfaplv1beta1.Module) error {
	// 2. loop through commits from latest to oldest
	ok, err := p.checkPRCommits(ctx, repo, pr, module)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	// 3. loop through comments
	_, err = p.checkPRCommentsForPlanRequests(ctx, pr, repo, module)
	return err
}

func (p *Planner) checkPRCommits(ctx context.Context, repo *mirror.GitURL, pr pr, module tfaplv1beta1.Module) (bool, error) {
	// loop through commits to check if module path is updated
	for i := len(pr.Commits.Nodes) - 1; i >= 0; i-- {
		commit := pr.Commits.Nodes[i].Commit

		// 1. check if module path is updated in this commit
		updated, err := p.isModuleUpdated(ctx, commit.Oid, module)
		if err != nil {
			return false, err
		}
		if !updated {
			continue
		}

		// 2. check if we have already processed (uploaded output) this commit
		outputPosted := p.planOutputPostedForCommit(pr, commit.Oid, module.NamespacedName())
		if outputPosted {
			return false, nil
		}

		requestAcknowledged := p.planRequestAcknowledgedForCommit(pr, module.NamespacedName())
		if requestAcknowledged {
			return false, nil
		}

		// 3. check if run is already completed for this commit
		_, err = p.RedisClient.PRRun(ctx, module.NamespacedName(), pr.Number, commit.Oid)
		if err != nil && err.Error() != "unable to get value err:redis: nil" {
			return false, nil
		}

		// 3. request run
		return true, p.addNewRequest(ctx, module, pr, repo, commit.Oid)
	}

	return false, nil
}

// TODO: Move regex patterns compiles outside of these functions and store in one place
func (p *Planner) planOutputPostedForCommit(pr pr, commitID string, module types.NamespacedName) bool {
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		matches := terraformPlanOutRegex.FindStringSubmatch(comment.Body)
		if len(matches) != 3 {
			return false
		}
		// TODO: S1025: should use String() instead of fmt.Sprintf
		if matches[1] == fmt.Sprintf("%s", module) && matches[2] == commitID {
			return true
		}
	}

	return false
}

func (p *Planner) planRequestAcknowledgedForCommit(pr pr, module types.NamespacedName) bool {
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		matches := requestAcknowledgedRegex.FindStringSubmatch(comment.Body)
		if len(matches) != 2 {
			return false
		}
		// TODO: S1025: should use String() instead of fmt.Sprintf
		if matches[1] == fmt.Sprintf("%s", module) {
			return true
		}
	}

	return false
}

func (p *Planner) isModuleUpdated(ctx context.Context, commitHash string, module tfaplv1beta1.Module) (bool, error) {
	filesChangedInCommit, err := p.Repos.ChangedFiles(ctx, module.Spec.RepoURL, commitHash)
	if err != nil {
		return false, fmt.Errorf("error getting commit info: %w", err)
	}
	fmt.Println("filesChangedInCommit:", filesChangedInCommit)

	return pathBelongsToModule(filesChangedInCommit, module), nil
}

func (p *Planner) checkPRCommentsForPlanRequests(ctx context.Context, pr pr, repo *mirror.GitURL, module tfaplv1beta1.Module) (bool, error) {
	// TODO: Allow users manually request plan runs for PRs with a large number of modules,
	// but only ONE module at a time

	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		// TODO: 1st check if plan output for THIS module is uploaded/requested
		// if find return

		// Check if user requested terraform plan run
		if strings.Contains(comment.Body, "@terraform-applier plan") {
			// Give users ability to request plan for all modules or just one
			// terraform-applier plan [`<module name>`]
			prCommentModule, _ := p.findModuleNameInComment(comment.Body)

			if prCommentModule.Name != "" && module.Name != prCommentModule.Name {
				continue
			}

			p.Log.Debug("new plan request received. creating new plan request", "namespace", module.ObjectMeta.Namespace, "module", module.Name)
			return true, p.addNewRequest(ctx, module, pr, repo, pr.Commits.Nodes[len(pr.Commits.Nodes)-1].Commit.Oid)
		}
	}

	return false, nil
}

func (p *Planner) addNewRequest(ctx context.Context, module tfaplv1beta1.Module, pr pr, repo *mirror.GitURL, commitID string) error {
	req := module.NewRunRequest(tfaplv1beta1.PRPlan)

	commentBody := prComment{
		// TODO: put all comments message together as template/var
		Body: fmt.Sprintf("Received terraform plan request. Module: `%s` Request ID: `%s` Commit ID: `%s`", module.NamespacedName(), req.ID, commitID),
	}

	commentID, err := p.postToGitHub(repo, "POST", 0, pr.Number, commentBody)
	if err != nil {
		return fmt.Errorf("unable to post pending request comment: %w", err)
	}

	req.PR = &tfaplv1beta1.PullRequest{
		Number:        pr.Number,
		HeadBranch:    pr.HeadRefName,
		CommentID:     commentID,
		GitCommitHash: commitID,
	}

	// TODO: Test what happens if we upload but we annotation fails

	err = sysutil.EnsureRequest(ctx, p.ClusterClt, module.NamespacedName(), req)
	if err != nil {
		p.Log.Error("failed to request plan job", err)
	}

	p.Log.Info("requested terraform plan for the PR", "module", module.NamespacedName(), "requestID", req.ID, "pr", pr.Number)
	return nil
}
