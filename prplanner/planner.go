package prplanner

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Planner struct {
	GitMirror   mirror.RepoPoolConfig
	ClusterClt  client.Client
	Repos       git.Repositories
	RedisClient sysutil.RedisInterface
	github      *gitHubClient
	Interval    time.Duration
	Log         *slog.Logger
}

func (p *Planner) Init(token string) {
	p.github = &gitHubClient{
		rootURL: "https://api.github.com",
		http: &http.Client{
			Timeout: 3 * time.Minute,
		},
		token: token,
	}
}

func (p *Planner) Start(ctx context.Context) {
	ticker := time.NewTicker(p.Interval) // TODO: Adjust this as needed
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:

			kubeModuleList := &tfaplv1beta1.ModuleList{}
			if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
				p.Log.Error("error retrieving list of modules", err)
				return
			}

			for _, repoConf := range p.GitMirror.Repositories {

				repo, err := mirror.ParseGitURL(repoConf.Remote)
				if err != nil {
					p.Log.Error("unable to parse repo url", err)
					return
				}

				// Make a GraphQL query to fetch all open Pull Requests from Github
				prs, err := p.github.openPRs(ctx, repo)
				if err != nil {
					p.Log.Error("error making GraphQL request:", err)
					return
				}

				// Loop through all open PRs
				for _, pr := range prs {
					// 1. Verify if pr belongs to module based on files changed
					prModules, err := p.getPRModuleList(pr.Files.Nodes, kubeModuleList)
					if err != nil {
						p.Log.Error("error getting a list of modules in PR", err)
					}

					if len(prModules) == 0 {
						// no modules are affected by this PR
						continue
					}

					// 2. compare PR and local repos last commit hashes
					if !p.isLocalRepoUpToDate(ctx, repoConf.Remote, pr) {
						// skip as local repo isn't yet in sync with the remote
						continue
					}

					// 1. ensure plan requests
					p.ensurePlanRequests(ctx, repo, pr, prModules)

					// 2. look for pending output updates
					p.uploadRequestOutput(ctx, repo, pr)
				}
			}
		}
	}
}

func (p *Planner) isLocalRepoUpToDate(ctx context.Context, repo string, pr *pr) bool {
	if len(pr.Commits.Nodes) == 0 {
		return false
	}

	latestCommit := pr.Commits.Nodes[len(pr.Commits.Nodes)-1].Commit.Oid
	err := p.Repos.ObjectExists(ctx, repo, latestCommit)
	return err == nil
}

func (p *Planner) getPRModuleList(prFiles prFiles, kubeModules *tfaplv1beta1.ModuleList) ([]types.NamespacedName, error) {
	var pathList []string

	for _, file := range prFiles {
		pathList = append(pathList, file.Path)
	}

	var modulesUpdated []types.NamespacedName

	for _, kubeModule := range kubeModules.Items {
		if pathBelongsToModule(pathList, kubeModule) {
			modulesUpdated = append(modulesUpdated, kubeModule.NamespacedName())
		}
	}

	return modulesUpdated, nil
}

func pathBelongsToModule(pathList []string, module tfaplv1beta1.Module) bool {
	for _, path := range pathList {
		if strings.HasPrefix(path, module.Spec.Path) {
			return true
		}
	}
	return false
}