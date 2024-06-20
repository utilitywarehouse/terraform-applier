package prplanner

import (
	"context"
	"log/slog"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-resty/resty/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Planner struct {
	GitMirror   mirror.RepoPoolConfig
	ClusterClt  client.Client
	Repos       git.Repositories
	RedisClient sysutil.RedisInterface
	github      *graphqlClient
	Interval    time.Duration
	Log         *slog.Logger
}

func (p *Planner) Init(username, token string) {
	c := &graphqlClient{
		url:  "https://api.github.com/graphql",
		http: resty.New(),
	}
	c.http.SetTimeout(5 * time.Minute)
	c.http.SetHeader("Accept", "application/vnd.github.v3+json")
	c.http.SetBasicAuth(username, token)

	p.github = c
}

func (ps *Planner) Start(ctx context.Context) {
	ticker := time.NewTicker(ps.Interval) // TODO: Adjust this as needed
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:

			kubeModuleList := &tfaplv1beta1.ModuleList{}
			if err := ps.ClusterClt.List(ctx, kubeModuleList); err != nil {
				ps.Log.Error("error retrieving list of modules", err)
				return
			}

			for _, repoConf := range ps.GitMirror.Repositories {

				repo, err := mirror.ParseGitURL(repoConf.Remote)
				if err != nil {
					ps.Log.Error("unable to parse repo url", err)
					return
				}

				// Make a GraphQL query to fetch all open Pull Requests from Github
				prs, err := ps.getOpenPullRequests(ctx, repo)
				if err != nil {
					ps.Log.Error("error making GraphQL request:", err)
					return
				}

				// Loop through all open PRs
				for _, pr := range prs {
					// 1. Verify if pr belongs to module based on files changed
					prModules, err := ps.getPRModuleList(pr.Files.Nodes, kubeModuleList)
					if err != nil {
						ps.Log.Error("error getting a list of modules in PR", err)
					}

					if len(prModules) == 0 {
						// no modules are affected by this PR
						continue
					}

					// 2. compare PR and local repos last commit hashes
					if !ps.isLocalRepoUpToDate(ctx, repoConf.Remote, pr) {
						// skip as local repo isn't yet in sync with the remote
						continue
					}

					// 1. ensure plan requests
					ps.ensurePlanRequests(ctx, repo, pr, prModules)

					// 2. look for pending output updates
					ps.uploadRequestOutput(ctx, repo, pr)
				}
			}
		}
	}
}

func (ps *Planner) isLocalRepoUpToDate(ctx context.Context, repo string, pr pr) bool {
	if len(pr.Comments.Nodes) == 0 {
		return false
	}
	latestCommit := pr.Commits.Nodes[len(pr.Comments.Nodes)-1].Commit.Oid
	err := ps.Repos.ObjectExists(ctx, repo, latestCommit)
	return err == nil
}

func (ps *Planner) getPRModuleList(prFiles prFiles, kubeModules *tfaplv1beta1.ModuleList) ([]types.NamespacedName, error) {
	var pathList []string

	for _, file := range prFiles {
		pathList = append(pathList, file.Path)
	}

	var modulesUpdated []types.NamespacedName

	for _, kubeModule := range kubeModules.Items {
		if ps.pathBelongsToModule(pathList, kubeModule) {
			modulesUpdated = append(modulesUpdated, kubeModule.NamespacedName())
		}
	}

	return modulesUpdated, nil
}
