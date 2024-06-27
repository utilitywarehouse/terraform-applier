package prplanner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func (p *Planner) ensurePlanRequests(ctx context.Context, repo *mirror.GitURL, pr *pr, prModules []types.NamespacedName) {
	var skipCommitRun bool
	if len(prModules) > 5 {
		skipCommitRun = true
	}

	for _, moduleName := range prModules {

		// 1. Check if module has any pending plan request
		var module tfaplv1beta1.Module
		err := p.ClusterClt.Get(ctx, moduleName, &module)
		if err != nil {
			p.Log.Error("unable to get module", "module", moduleName, "error", err)
			continue
		}
		_, ok := module.PendingRunRequest()
		if ok {
			continue
		}

		req, err := p.ensurePlanRequest(ctx, repo, pr, module, skipCommitRun)
		if err != nil {
			p.Log.Error("unable to generate new plan request", "module", moduleName, "error", err)
			continue
		}
		if req != nil {
			err = sysutil.EnsureRequest(ctx, p.ClusterClt, module.NamespacedName(), req)
			if err != nil {
				p.Log.Error("failed to request plan job", "error", err)
				continue
			}
		}
	}
}

func (p *Planner) ensurePlanRequest(ctx context.Context, repo *mirror.GitURL, pr *pr, module tfaplv1beta1.Module, skipCommitRun bool) (*tfaplv1beta1.Request, error) {
	if !skipCommitRun {
		// 1. loop through commits from latest to oldest
		req, err := p.checkPRCommits(ctx, repo, pr, module)
		if err != nil {
			return req, err
		}
		if req != nil {
			return req, nil
		}
	}

	// 2. loop through comments
	return p.checkPRCommentsForPlanRequests(pr, repo, module)
}

func (p *Planner) checkPRCommits(ctx context.Context, repo *mirror.GitURL, pr *pr, module tfaplv1beta1.Module) (*tfaplv1beta1.Request, error) {
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
		outputPosted := isPlanOutputPostedForCommit(pr, commit.Oid, module.NamespacedName())
		if outputPosted {
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
		return p.addNewRequest(module, pr, repo)
	}

	return nil, nil
}

func (p *Planner) isModuleUpdated(ctx context.Context, commitHash string, module tfaplv1beta1.Module) (bool, error) {
	filesChangedInCommit, err := p.Repos.ChangedFiles(ctx, module.Spec.RepoURL, commitHash)
	if err != nil {
		return false, fmt.Errorf("error getting commit info: %w", err)
	}

	return pathBelongsToModule(filesChangedInCommit, module), nil
}

func (p *Planner) checkPRCommentsForPlanRequests(pr *pr, repo *mirror.GitURL, module tfaplv1beta1.Module) (*tfaplv1beta1.Request, error) {
	// Go through PR comments in reverse order
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		// Skip if request already acknowledged for module
		commentModule, _, _ := requestAcknowledgedCommentInfo(comment.Body)
		if commentModule == module.NamespacedName() {
			return nil, nil
		}

		// Skip if terraform plan output is already posted
		commentModule, _ = getPostedRunOutputInfo(comment.Body)
		if commentModule == module.NamespacedName() {
			return nil, nil
		}

		// Check if user requested terraform plan run
		// '@terraform-applier plan [<module namespace>]/<module name>'
		commentModule = getRunRequestFromComment(comment.Body)
		if commentModule.Name != module.Name {
			continue
		}
		// commented module's namespace needs to match as well if its given by user
		if commentModule.Namespace != "" && commentModule.Namespace != module.Namespace {
			continue
		}

		p.Log.Info("triggering plan requested via comment", "module", module.NamespacedName(), "pr", pr.Number, "author", comment.Author.Login)
		return p.addNewRequest(module, pr, repo)
	}

	return nil, nil
}

// isPlanOutputPostedForCommit loops through all the comments to check if given commit
// ids plan output is already posted
func isPlanOutputPostedForCommit(pr *pr, commitID string, module types.NamespacedName) bool {
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		commentModule, commentCommitID := getPostedRunOutputInfo(comment.Body)
		if commentModule == module && commentCommitID == commitID {
			return true
		}
	}

	return false
}

func getPostedRunOutputInfo(comment string) (module types.NamespacedName, commit string) {
	matches := terraformPlanOutRegex.FindStringSubmatch(comment)
	if len(matches) == 4 {
		return parseNamespaceName(matches[1]), matches[3]
	}

	return types.NamespacedName{}, ""
}

// TODO: comment can be with or without namespaced name
func getRunRequestFromComment(commentBody string) types.NamespacedName {
	matches := terraformPlanRequestRegex.FindStringSubmatch(commentBody)

	if len(matches) == 2 && matches[1] != "" {
		return parseNamespaceName(matches[1])
	}

	return types.NamespacedName{}
}

func parseNamespaceName(str string) types.NamespacedName {
	namespacedName := strings.Split(str, "/")
	if len(namespacedName) == 2 {
		return types.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}
	}

	if len(namespacedName) == 1 {
		return types.NamespacedName{Name: namespacedName[0]}
	}

	return types.NamespacedName{}
}

func (p *Planner) addNewRequest(module tfaplv1beta1.Module, pr *pr, repo *mirror.GitURL) (*tfaplv1beta1.Request, error) {
	req := module.NewRunRequest(tfaplv1beta1.PRPlan)

	commentBody := prComment{
		Body: requestAcknowledgedMsg(module.NamespacedName().String(), module.Spec.Path, req.RequestedAt),
	}

	commentID, err := p.github.postComment(repo, 0, pr.Number, commentBody)
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

func requestAcknowledgedMsg(module, path string, reqAt *metav1.Time) string {
	return fmt.Sprintf(requestAcknowledgedTml, module, path, reqAt.Format(time.RFC3339))
}
