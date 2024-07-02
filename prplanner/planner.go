package prplanner

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/utilitywarehouse/git-mirror/pkg/giturl"
	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Planner struct {
	ListenAddress string
	WebhookSecret string
	GitMirror     mirror.RepoPoolConfig
	ClusterClt    client.Client
	Repos         git.Repositories
	RedisClient   sysutil.RedisInterface
	github        GithubInterface
	Interval      time.Duration
	Log           *slog.Logger
}

func (p *Planner) Init(ctx context.Context, token string, ch <-chan *redis.Message) error {
	p.github = &gitHubClient{
		rootURL: "https://api.github.com",
		http: &http.Client{
			Timeout: 3 * time.Minute,
		},
		token: token,
	}

	if ch != nil {
		go p.processRedisKeySetMsg(ctx, ch)
	}

	go p.startWebhook()

	go p.StartPRPoll(ctx)

	return nil
}

func (p *Planner) StartPRPoll(ctx context.Context) {
	ticker := time.NewTicker(p.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			kubeModuleList := &tfaplv1beta1.ModuleList{}
			if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
				p.Log.Error("error retrieving list of modules", "error", err)
				return
			}

			for _, repoConf := range p.GitMirror.Repositories {
				repo, err := giturl.Parse(repoConf.Remote)
				if err != nil {
					p.Log.Error("unable to parse repo url", "error", err)
					return
				}

				// Make a GraphQL query to fetch all open Pull Requests from Github
				prs, err := p.github.openPRs(ctx, repo.Path, repo.Repo)
				if err != nil {
					p.Log.Error("error making GraphQL request:", "error", err)
					return
				}

				// Loop through all open PRs
				for _, pr := range prs {
					p.processPullRequest(ctx, pr, kubeModuleList)
				}
			}
		}
	}
}

func (p *Planner) processPullRequest(ctx context.Context, pr *pr, kubeModuleList *tfaplv1beta1.ModuleList) {
	// skip draft PRs
	if pr.IsDraft {
		return
	}

	// 1. verify if PR belongs to module based on files changed
	prModules, err := p.getPRModuleList(pr, kubeModuleList)
	if err != nil {
		p.Log.Error("error getting a list of modules in PR", "error", err)
	}

	if len(prModules) == 0 {
		// no modules are affected by this PR
		return
	}

	// 2. compare PR and local repos last commit hashes
	if !p.isLocalRepoUpToDate(ctx, pr) {
		// skip as local repo isn't yet in sync with the remote
		return
	}

	// 1. ensure plan requests
	p.ensurePlanRequests(ctx, pr, prModules)
}

func (p *Planner) isLocalRepoUpToDate(ctx context.Context, pr *pr) bool {
	if len(pr.Commits.Nodes) == 0 {
		return false
	}
	latestCommit := pr.Commits.Nodes[len(pr.Commits.Nodes)-1].Commit.Oid
	err := p.Repos.ObjectExists(ctx, pr.BaseRepository.URL, latestCommit)
	return err == nil
}

func (p *Planner) getPRModuleList(pr *pr, kubeModules *tfaplv1beta1.ModuleList) ([]types.NamespacedName, error) {
	var pathList []string

	for _, file := range pr.Files.Nodes {
		pathList = append(pathList, file.Path)
	}

	var modulesUpdated []types.NamespacedName

	for _, kubeModule := range kubeModules.Items {
		if ok, _ := giturl.SameRawURL(kubeModule.Spec.RepoURL, pr.BaseRepository.URL); !ok {
			continue
		}

		if !pathBelongsToModule(pathList, kubeModule) {
			continue
		}

		// default value of RepoRef is 'HEAD', which is normally a master branch
		if kubeModule.Spec.RepoRef != pr.BaseRefName &&
			pr.BaseRefName != "master" && pr.BaseRefName != "main" {
			continue
		}

		modulesUpdated = append(modulesUpdated, kubeModule.NamespacedName())
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
