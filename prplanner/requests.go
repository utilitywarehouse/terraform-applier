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
func (ps *Planner) ensurePlanRequests(ctx context.Context, repo *mirror.GitURL, pr pr, prModules []types.NamespacedName) {
	for _, moduleName := range prModules {
		fmt.Println("§§§ module:", moduleName)

		var module tfaplv1beta1.Module
		err := ps.ClusterClt.Get(ctx, moduleName, &module)
		if err != nil {
			ps.Log.Error("unable to get module", "module", moduleName, "error", err)
			continue
		}

		// 1. Check if module is annotated
		// no need to proceed if there's already a plan request for the module
		_, ok := module.PendingRunRequest()
		if ok {
			continue
		}

		err = ps.ensurePlanRequest(ctx, repo, pr, module)
		if err != nil {
			ps.Log.Error("unable to get module", "module", moduleName, "error", err)
			continue
		}
	}
}

func (ps *Planner) ensurePlanRequest(ctx context.Context, repo *mirror.GitURL, pr pr, module tfaplv1beta1.Module) error {
	fmt.Println("§§§ getPendingPlans")

	// 2. loop through commits from latest to oldest
	ok, err := ps.checkPRCommits(ctx, repo, pr, module)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	// 3. loop through comments
	fmt.Println("§§§ getPendingPlans 2")
	_, err = ps.checkPRCommentsForPlanRequests(ctx, pr, repo, module)
	return err
}

func (ps *Planner) checkPRCommits(ctx context.Context, repo *mirror.GitURL, pr pr, module tfaplv1beta1.Module) (bool, error) {
	// loop through commits to check if module path is updated
	for i := len(pr.Commits.Nodes) - 1; i >= 0; i-- {
		commit := pr.Commits.Nodes[i].Commit

		// 1. check if module path is updated in this commit
		updated, err := ps.isModuleUpdated(ctx, commit.Oid, module)
		if err != nil {
			return false, err
		}
		if !updated {
			continue
		}

		fmt.Println("§§§ checkPRCommits")
		// 2. check if we have already processed (uploaded output) this commit
		outputPosted := ps.commentPostedForCommit(pr, commit.Oid, module.NamespacedName())
		if outputPosted {
			return false, nil
		}

		fmt.Println("§§§ checkPRCommits 0")
		// 3. check if run is already completed for this commit
		_, err = ps.RedisClient.PRRun(ctx, module.NamespacedName(), pr.Number, commit.Oid)
		if err != nil {
			return false, nil
		}

		// 3. request run
		return true, ps.addNewRequest(ctx, module, pr, repo, commit.Oid)
	}

	return false, nil
}

func (ps *Planner) commentPostedForCommit(pr pr, commitID string, module types.NamespacedName) bool {
	// TODO: Dirty prototype
	// Improve flow and formatting if this works
	for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
		comment := pr.Comments.Nodes[i]

		searchPattern := fmt.Sprintf("Terraform plan output for module `(%s)`. Commit ID: `(%s)`", module, commitID)
		re := regexp.MustCompile(searchPattern)
		matches := re.FindStringSubmatch(comment.Body)
		fmt.Printf("§§§ matches: %+v\n", matches)
		fmt.Println("§§§ len matches:", len(matches))
		// searchString := regexp.MustCompile(`Module: ` + "`" + `(.+?)` + "`" + ` Request ID: ` + "`" + `(.+?)` + "`")
		// if strings.Contains(comment.Body, commitID) {
		if len(matches) == 3 {
			return true
		}

		searchPattern = fmt.Sprintf("Received terraform plan request. Module: `(%s)`", module)
		re = regexp.MustCompile(searchPattern)
		matches = re.FindStringSubmatch(comment.Body)
		fmt.Printf("§§§ matches: %+v\n", matches)
		fmt.Println("§§§ len matches:", len(matches))
		// searchString := regexp.MustCompile(`Module: ` + "`" + `(.+?)` + "`" + ` Request ID: ` + "`" + `(.+?)` + "`")
		// if strings.Contains(comment.Body, commitID) {
		if len(matches) == 2 {
			return true
		}
	}

	return false
}

func (ps *Planner) isModuleUpdated(ctx context.Context, commitHash string, module tfaplv1beta1.Module) (bool, error) {
	filesChangedInCommit, err := ps.Repos.ChangedFiles(ctx, module.Spec.RepoURL, commitHash)
	if err != nil {
		return false, fmt.Errorf("error getting commit info: %w", err)
	}

	return pathBelongsToModule(filesChangedInCommit, module), nil
}

func (ps *Planner) checkPRCommentsForPlanRequests(ctx context.Context, pr pr, repo *mirror.GitURL, module tfaplv1beta1.Module) (bool, error) {
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
			prCommentModule, _ := ps.findModuleNameInComment(comment.Body)

			if prCommentModule.Name != "" && module.Name != prCommentModule.Name {
				continue
			}

			ps.Log.Debug("new plan request received. creating new plan request", "namespace", module.ObjectMeta.Namespace, "module", module.Name)
			return true, ps.addNewRequest(ctx, module, pr, repo, pr.Commits.Nodes[len(pr.Commits.Nodes)-1].Commit.Oid)
		}
	}

	return false, nil
}

// func (ps *Server) getPendingPlans(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo *mirror.GitURL, prModules []tfaplv1beta1.Module) {
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

// func (ps *Server) isNewPR(prComments []prComment) bool {
// 	// A PR is considered new when there are no comments posted by terraform-applier
// 	for _, comment := range prComments {
// 		if strings.Contains(comment.Body, "Received terraform plan request") || strings.Contains(comment.Body, "Terraform plan output for module") {
// 			return false
// 		}
// 	}
//
// 	return true
// }

func (ps *Planner) addNewRequest(ctx context.Context, module tfaplv1beta1.Module, pr pr, repo *mirror.GitURL, commitID string) error {

	req := module.NewRunRequest(tfaplv1beta1.PRPlan)

	commentBody := prComment{
		// TODO: put all comments message together as template/var
		Body: fmt.Sprintf("Received terraform plan request. Module: `%s` Request ID: `%s` Commit iD: `%s`", module.NamespacedName(), req.ID, commitID),
	}

	commentID, err := ps.postToGitHub(repo, "POST", 0, pr.Number, commentBody)
	if err != nil {
		return fmt.Errorf("unable to post pending request comment:", err)
	}

	req.PR = &tfaplv1beta1.PullRequest{
		Number:        pr.Number,
		HeadBranch:    pr.HeadRefName,
		CommentID:     commentID,
		GitCommitHash: commitID,
	}

	// TODO: Test what happens if we upload but we annotation fails

	err = sysutil.EnsureRequest(ctx, ps.ClusterClt, module.NamespacedName(), req)
	if err != nil {
		ps.Log.Error("failed to request plan job", err)
	}

	ps.Log.Info("requested terraform plan for the PR", "module", module.NamespacedName(), "requestID", req.ID, "pr", pr.Number)
	return nil
}

// func (ps *Server) checkLastPRCommit(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo *mirror.GitURL, prModules []tfaplv1beta1.Module) {
// 	for _, module := range prModules {
// 		// TODO: Change order of checks?
// 		// What's the point in doing all of that if module might be annotated at the end?
// 		//
// 		prLastCommitHash := pr.Commits.Nodes[0].Commit.Oid
// 		localRepoCommitHash, err := ps.Repos.Hash(ctx, module.Spec.RepoURL, pr.HeadRefName, module.Spec.Path)
// 		key := sysutil.DefaultPRLastRunsKey(module.NamespacedName(), pr.Number)
// 		redisCommitHash, err := ps.RedisClient.GetCommitHash(ctx, key)
// 		if err != nil {
// 			ps.Log.Error("error getting module data from redis", err)
// 		}
//
// 		// TODO:
// 		// Get PR last commit hash
// 		// Find plan output in redis for the same commit
//
// 		if prLastCommitHash != localRepoCommitHash {
// 			// Skip check as local git repo is not up-to-date yet
// 			continue
// 		}
//
// 		if prLastCommitHash != redisCommitHash {
// 			// Request plan run if corresponding plan output is not yet in redis
// 			moduleUpdated, err := ps.isModuleUpdated(repo, prLastCommitHash, module)
// 			if err != nil {
// 				ps.Log.Error("error getting a list of modules", err)
// 			}
//
// 			if moduleUpdated {
// 				// Continue if this commit changes the files related to current module
// 				annotated, err := ps.isModuleAnnotated(ctx, module.NamespacedName())
// 				if err != nil {
// 					ps.Log.Error("error retreiving module annotation", err)
// 				}
//
// 				if !annotated {
// 					// Create a new request if kube module is not yet annotated
// 					ps.addNewRequest(ctx, planRequests, module, pr, repo)
// 					ps.Log.Debug("new commit found. creating new plan request", "namespace", module.ObjectMeta.Namespace, "module", module.Name)
// 				}
// 			}
// 		}
// 	}
// }

// func (ps *Server) analysePRCommentsForRun(ctx context.Context, planRequests *map[string]*tfaplv1beta1.Request, pr pr, repo *mirror.GitURL, prModules []tfaplv1beta1.Module) {
// 	for _, module := range prModules {
//
// 		// Go through PR comments in reverse order
// 		for i := len(pr.Comments.Nodes) - 1; i >= 0; i-- {
// 			comment := pr.Comments.Nodes[i]
//
// 			// Skip module if plan job request is already received or completed
// 			if strings.Contains(comment.Body, "Received terraform plan request") || strings.Contains(comment.Body, "Terraform plan output for module") {
// 				prCommentModule, _, err := ps.findModuleNameInComment(comment.Body)
// 				if err != nil {
// 					ps.Log.Error("error getting module name from PR comment", err)
// 				}
//
// 				if module.Name == prCommentModule {
// 					break // skip as plan output or request request ack is already posted for the module
// 				}
// 			}
//
// 			// Check if user requested terraform plan run
// 			if strings.Contains(comment.Body, "@terraform-applier plan") {
// 				// Give users ability to request plan for all modules or just one
// 				// terraform-applier plan [`<module name>`]
// 				prCommentModule, _, _ := ps.findModuleNameInComment(comment.Body)
//
// 				if prCommentModule != "" && module.Name != prCommentModule {
// 					break
// 				}
//
// 				ps.addNewRequest(ctx, planRequests, module, pr, repo)
// 				ps.Log.Debug("new plan request received. creating new plan request", "namespace", module.ObjectMeta.Namespace, "module", module.Name)
// 			}
// 		}
// 	}
// }
