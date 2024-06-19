package prplanner

import (
	"context"
	"fmt"
	"strings"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"k8s.io/apimachinery/pkg/types"
)

func (ps *Server) getPendingPlans(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo gitHubRepo, prModules []tfaplv1beta1.Module) {
	// 1. Check if module is annotated
	annotated, err := ps.isModuleAnnotated(ctx, module.NamespacedName())
	if err != nil {
		ps.Log.Error("error checking module annotation", err)
	}

	if annotated {
		break // no need to proceed if there's already a plan request for the module
	}

	planRequests := make(map[string]*tfaplv1beta1.Request)
	// 2. loop through commits from latest to oldest
	ps.checkPRCommits(ctx, &planRequests)

	// 3. loop through comments
	var outputs []output
	ps.checkPRComments(ctx, outputs, pr, prModules)

}

func (ps *Server) checkPRCommits() {
	// 0. check comments if output posted for the commit hash
	// if no comment posted:
	//
	// 1. check commit hashes in redis
	// if missing:
	//
	// 2. verify module needs to be planned based on files changed
	// if matching:
	//
	// 3. request run
}

func (ps *Server) checkPRComments() {
	// 1. check if user requested run
	// if yes:
	//
	// 2. request run
}

// func (ps *Server) getPendingPlans(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo gitHubRepo, prModules []tfaplv1beta1.Module) {
// 	if ps.isNewPR(pr.Comments.Nodes) {
// 		for _, module := range prModules {
// 			annotated, err := ps.isModuleAnnotated(ctx, module.NamespacedName())
// 			if err != nil {
// 				ps.Log.Error("error retreiving module annotation", err)
// 			}
//
// 			if annotated {
// 				continue // Skip annotated modules
// 			}
//
// 			ps.addNewRequest(ctx, planRequests, module, pr, repo)
// 			ps.Log.Debug("new pr found. creating new plan request", "namespace", module.ObjectMeta.Namespace, "module", module.Name)
// 		}
// 		return
// 	}
//
// 	ps.checkLastPRCommit(ctx, planRequests, pr, repo, prModules)
// 	ps.analysePRCommentsForRun(ctx, planRequests, pr, repo, prModules)
// }

func (ps *Server) pathBelongsToModule(pathList []string, module tfaplv1beta1.Module) bool {
	for _, path := range pathList {
		if strings.Contains(path, module.Spec.Path) {
			return true
		}
	}
	return false
}

func (ps *Server) isNewPR(prComments []prComment) bool {
	// A PR is considered new when there are no comments posted by terraform-applier
	for _, comment := range prComments {
		if strings.Contains(comment.Body, "Received terraform plan request") || strings.Contains(comment.Body, "Terraform plan output for module") {
			return false
		}
	}

	return true
}

func (ps *Server) addNewRequest(ctx context.Context, requests *map[string]*tfaplv1beta1.Request, module tfaplv1beta1.Module, pr pr, repo gitHubRepo) {
	if _, exists := (*requests)[module.Name]; exists {
		return // module is already in the requests list
	}

	newReq := module.NewRunRequest(tfaplv1beta1.PRPlan)

	commentBody := prComment{
		Body: fmt.Sprintf("Received terraform plan request. Module: `%s` Request ID: `%s`", module.Name, newReq.ID),
	}
	commentID, err := ps.postToGitHub(module.Spec.RepoURL, "POST", 0, pr.Number, commentBody)
	if err != nil {
		ps.Log.Error("error posting PR comment:", err)
	}

	ps.Log.Debug("posted request id to github", "namespace", module.ObjectMeta.Namespace, "module", module.Name, "requestID", newReq.ID, "repo", repo.name, "pr", pr.Number)

	err = ps.ClusterClt.Get(ctx, module.NamespacedName(), &module)
	if err != nil {
		ps.Log.Error("cannot find module", err)
	}

	newReq.PR = &tfaplv1beta1.PullRequest{
		Number:        pr.Number,
		HeadBranch:    pr.HeadRefName,
		CommentID:     commentID,
		GitCommitHash: pr.Commits.Nodes[0].Commit.Oid,
	}

	moduleKey := module.ObjectMeta.Namespace + "/" + module.Name
	(*requests)[moduleKey] = newReq

}

func (ps *Server) checkLastPRCommit(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo gitHubRepo, prModules []tfaplv1beta1.Module) {
	for _, module := range prModules {
		// TODO: Change order of checks?
		// What's the point in doing all of that if module might be annotated at the end?
		//
		prLastCommitHash := pr.Commits.Nodes[0].Commit.Oid
		localRepoCommitHash, err := ps.Repos.Hash(ctx, module.Spec.RepoURL, pr.HeadRefName, module.Spec.Path)
		key := sysutil.DefaultPRLastRunsKey(module.NamespacedName(), pr.Number)
		redisCommitHash, err := ps.RedisClient.GetCommitHash(ctx, key)
		if err != nil {
			ps.Log.Error("error getting module data from redis", err)
		}

		// TODO:
		// Get PR last commit hash
		// Find plan output in redis for the same commit

		if prLastCommitHash != localRepoCommitHash {
			// Skip check as local git repo is not up-to-date yet
			continue
		}

		if prLastCommitHash != redisCommitHash {
			// Request plan run if corresponding plan output is not yet in redis
			moduleUpdated, err := ps.isModuleUpdated(repo, prLastCommitHash, module)
			if err != nil {
				ps.Log.Error("error getting a list of modules", err)
			}

			if moduleUpdated {
				// Continue if this commit changes the files related to current module
				annotated, err := ps.isModuleAnnotated(ctx, module.NamespacedName())
				if err != nil {
					ps.Log.Error("error retreiving module annotation", err)
				}

				if !annotated {
					// Create a new request if kube module is not yet annotated
					ps.addNewRequest(ctx, planRequests, module, pr, repo)
					ps.Log.Debug("new commit found. creating new plan request", "namespace", module.ObjectMeta.Namespace, "module", module.Name)
				}
			}
		}
	}
}

func (ps *Server) isModuleAnnotated(ctx context.Context, key types.NamespacedName) (bool, error) {
	module, err := sysutil.GetModule(ctx, ps.ClusterClt, key)
	if err != nil {
		return false, err
	}

	for _, value := range module.ObjectMeta.Annotations {
		if strings.Contains(value, "terraform-applier.uw.systems/run-request") {
			return true, nil
		}
	}

	return false, nil
}

func (ps *Server) isModuleUpdated(repo gitHubRepo, commitHash string, module tfaplv1beta1.Module) (bool, error) {
	filesChangedInCommit, err := ps.getCommitFilesChanged(repo.name, commitHash)
	if err != nil {
		return false, fmt.Errorf("error getting commit info: %w", err)
	}

	moduleUpdated := ps.pathBelongsToModule(filesChangedInCommit, module)
	if !moduleUpdated {
		return false, nil
	}

	return true, nil
}

func (ps *Server) getCommitFilesChanged(repoName, commitHash string) ([]string, error) {
	// TODO: Replace go-git package with git command
	repoPath := "/tmp/src/" + repoName + ".git" // TODO: Replace with REPOS_ROOT_PATH var
	githubRepo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	commit, err := githubRepo.CommitObject(plumbing.NewHash(commitHash))
	if err != nil {
		return nil, fmt.Errorf("commit hash provided not found")
	}

	files, err := commit.Stats()
	if err != nil {
		return nil, fmt.Errorf("error getting commmit stats")
	}

	var filesChanged []string
	for _, file := range files {
		filesChanged = append(filesChanged, file.Name)
	}

	return filesChanged, nil
}

func (ps *Server) analysePRCommentsForRun(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo gitHubRepo, prModules []tfaplv1beta1.Module) {
	for _, module := range prModules {

		// Go through PR comments in reverse order
		for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
			comment := pr.Comments.Nodes[i]

			// Skip module if plan job request is already received or completed
			if strings.Contains(comment.Body, "Received terraform plan request") || strings.Contains(comment.Body, "Terraform plan output for module") {
				prCommentModule, _, err := ps.findModuleNameInComment(comment.Body)
				if err != nil {
					ps.Log.Error("error getting module name from PR comment", err)
				}

				if module.Name == prCommentModule {
					break // skip as plan output or request request ack is already posted for the module
				}
			}

			// Check if user requested terraform plan run
			if strings.Contains(comment.Body, "@terraform-applier plan") {
				// Give users ability to request plan for all modules or just one
				// terraform-applier plan [`<module name>`]
				prCommentModule, _, _ := ps.findModuleNameInComment(comment.Body)

				if prCommentModule != "" && module.Name != prCommentModule {
					break
				}

				ps.addNewRequest(ctx, planRequests, module, pr, repo)
				ps.Log.Debug("new plan request received. creating new plan request", "namespace", module.ObjectMeta.Namespace, "module", module.Name)
			}
		}
	}
}

func (ps *Server) requestPlan(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo gitHubRepo) {
	for module, req := range *planRequests {

		parts := strings.Split(module, "/")
		moduleNamespace := parts[0]
		moduleName := parts[1]
		namespacedName := types.NamespacedName{
			Namespace: moduleNamespace,
			Name:      moduleName,
		}

		err := sysutil.EnsureRequest(ctx, ps.ClusterClt, namespacedName, req)
		if err != nil {
			ps.Log.Error("failed to request plan job", err)
		}

		ps.Log.Debug("requested terraform plan", "namespace", moduleNamespace, "module", moduleName)
	}
}
