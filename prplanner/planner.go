package prplanner

import (
	"context"
	"fmt"
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

	return nil
}

func (p *Planner) processPullRequest(ctx context.Context, repo *mirror.GitURL, repoString string, pr *pr, kubeModuleList *tfaplv1beta1.ModuleList) {
	// skip draft PRs
	if pr.IsDraft {
		return
	}

	// 1. verify if PR belongs to module based on files changed
	prModules, err := p.getPRModuleList(repo, pr, kubeModuleList)
	if err != nil {
		p.Log.Error("error getting a list of modules in PR", "error", err)
	}

	if len(prModules) == 0 {
		// no modules are affected by this PR
		return
	}

	// 2. compare PR and local repos last commit hashes
	if !p.isLocalRepoUpToDate(ctx, repo, pr) {
		// skip as local repo isn't yet in sync with the remote
		return
	}

	// 1. ensure plan requests
	p.ensurePlanRequests(ctx, repo, pr, prModules)
}

func (p *Planner) isLocalRepoUpToDate(ctx context.Context, repo *mirror.GitURL, pr *pr) bool {
	if len(pr.Commits.Nodes) == 0 {
		return false
	}

	latestCommit := pr.Commits.Nodes[len(pr.Commits.Nodes)-1].Commit.Oid
	repoString := fmt.Sprintf("%s://%s/%s/%s", repo.Scheme, repo.Host, repo.Path, repo.Repo)
	err := p.Repos.ObjectExists(ctx, repoString, latestCommit)
	return err == nil
}

func (p *Planner) getPRModuleList(repo *mirror.GitURL, pr *pr, kubeModules *tfaplv1beta1.ModuleList) ([]types.NamespacedName, error) {
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
