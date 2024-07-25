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
	"github.com/utilitywarehouse/terraform-applier/runner"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Planner struct {
	ListenAddress  string
	WebhookSecret  string
	ClusterEnvName string
	GitMirror      mirror.RepoPoolConfig
	ClusterClt     client.Client
	Repos          git.Repositories
	RedisClient    sysutil.RedisInterface
	Runner         runner.RunnerInterface
	github         GithubInterface
	Interval       time.Duration
	Log            *slog.Logger
}

func (p *Planner) Init(ctx context.Context, token string, ch <-chan *redis.Message) error {
	p.github = &gitHubClient{
		rootURL: "https://api.github.com",
		http: &http.Client{
			Timeout: 15 * time.Second,
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
	p.Log.Info("starting PR Poller")
	defer p.Log.Info("stopping PR Poller")

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
				continue
			}

			for _, repoConf := range p.GitMirror.Repositories {

				err := p.Repos.Mirror(ctx, repoConf.Remote)
				if err != nil {
					p.Log.Error("unable to mirror repository", "url", repoConf.Remote, "error", err)
					continue
				}

				repo, err := giturl.Parse(repoConf.Remote)
				if err != nil {
					p.Log.Error("unable to parse repo url", "url", repoConf.Remote, "error", err)
					continue
				}

				// Make a GraphQL query to fetch all open Pull Requests from Github
				prs, err := p.github.openPRs(ctx, repo.Path, repo.Repo)
				if err != nil {
					p.Log.Error("error getting a list of open PRs:", "url", repo.Path, "error", err)
					continue
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

	// verify if PR belongs to module based on files changed
	prModules, err := p.getPRModuleList(pr, kubeModuleList)
	if err != nil {
		p.Log.Error("error getting a list of modules in PR", "repo", pr.BaseRepository.Name, "pr", pr.Number, "error", err)
		return
	}

	if len(prModules) == 0 {
		// no modules are affected by this PR
		return
	}

	var skipCommitRun bool
	if len(prModules) > 5 {
		skipCommitRun = true

		// add limit msg comment if not already added
		if !isModuleLimitReachedCommentPosted(pr.Comments.Nodes) {
			comment := prComment{Body: moduleLimitReachedTml}
			_, err := p.github.postComment(pr.BaseRepository.Owner.Login, pr.BaseRepository.Name, 0, pr.Number, comment)
			if err != nil {
				p.Log.Error("unable to post limit reached msg", "err", err)
			}
		}
	}

	// ensure plan requests
	p.ensurePlanRequests(ctx, pr, prModules, skipCommitRun)

	p.uploadRequestOutput(ctx, pr)
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

		if !pathBelongsToModule(pathList, &kubeModule) {
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

func pathBelongsToModule(pathList []string, module *tfaplv1beta1.Module) bool {
	for _, path := range pathList {
		if strings.HasPrefix(path, module.Spec.Path) {
			return true
		}
	}
	return false
}
