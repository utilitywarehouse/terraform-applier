package prplanner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"

	"github.com/go-resty/resty/v2"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Server struct {
	ClusterClt    client.Client
	KubeClient    kubernetes.Interface
	Repos         git.Repositories
	RedisClient   sysutil.RedisInterface
	GraphqlClient *graphqlClient
	Log           *slog.Logger
}

type gitHubRepo struct {
	name  string
	owner string
}

type gitPRRequest struct {
	Query     string `json:"query,omitempty"`
	Variables struct {
		Slug  string `json:"slug"`
		After string `json:"after,omitempty"`
	} `json:"variables,omitempty"`
}

type gitPRResponse struct {
	Data struct {
		Repository struct {
			PullRequests struct {
				Nodes []pr `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
}

type pr struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	Commits     struct {
		Nodes []struct {
			Commit struct {
				Oid string `json:"oid"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	Comments struct {
		Nodes []prComment `json:"nodes"`
	} `json:"comments"`
	Files struct {
		Nodes prFiles `json:"nodes"`
	} `json:"files"`
}

type prComment struct {
	DatabaseID int    `json:"databaseId"`
	Body       string `json:"body"`
}

type prFiles []struct {
	Path string `json:"path"`
}

type output struct {
	module    tfaplv1beta1.Module
	body      prComment
	commentID int
	prNumber  int
}

type graphqlClient struct {
	url  string
	http *resty.Client
}

func NewGraphqlClient(username, token string) *graphqlClient {
	c := &graphqlClient{
		url:  "https://api.github.com/graphql",
		http: resty.New(),
	}
	c.http.SetTimeout(5 * time.Minute)
	c.http.SetHeader("Accept", "application/vnd.github.v3+json")
	c.http.SetBasicAuth(username, token)
	return c
}

func (ps *Server) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second) // TODO: Adjust this as needed
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ps.Log.Debug("starting github PR polling run...")

			kubeModuleList, err := ps.getKubeModuleList(ctx)
			if err != nil {
				ps.Log.Error("error retreiving list of modules", err)
			}

			repoList := getRepoList(kubeModuleList)
			for _, repo := range repoList {

				// Make a GraphQL query to fetch all open Pull Requests from Github
				response, err := ps.getOpenPullRequests(ctx, repo.owner, repo.name)
				if err != nil {
					ps.Log.Error("error making GraphQL request:", err)
				}

				// Loop through all open PRs
				for _, pr := range response.Data.Repository.PullRequests.Nodes {

					// 1. Verify if pr belongs to module based on files changed
					prModules, err := ps.getPRModuleList(pr.Files.Nodes, kubeModuleList)
					if err != nil {
						ps.Log.Error("error getting a list of modules in PR", err)
					}

					// 2. compare remote and local repos last commit hashes
					upToDate, err := ps.isLocalRepoUpToDate(pr)
					if err != nil {
						ps.Log.Error("error fetching local repo last commit hash", err)
					}

					if !upToDate {
						break // skip as local repo isn't yet in sync with the remote
					}

					// 3. loop through pr modules
					for _, module := range prModules {
						ps.actionOnPRModule(ctx, module)
					}

					// planRequests := make(map[string]*tfaplv1beta1.Request)
					// ps.getPendingPlans(ctx, &planRequests, pr, repo, prModules)
					// ps.requestPlan(ctx, &planRequests, pr, repo)
					//
					// var outputs []output
					// outputs = ps.getPendinPRUpdates(ctx, outputs, pr, prModules)
					// ps.postPlanOutput(outputs)
				}
			}
		}
	}
}

func (ps *Server) actionOnPRModule(ctx context.Context, module tfaplv1beta1.Module) {

	fmt.Println("module:", module.Name)

	// 1. look for pending plan requests
	planRequests := make(map[string]*tfaplv1beta1.Request)
	ps.getPendingPlans(ctx, &planRequests, pr, repo, prModules)
	ps.requestPlan(ctx, &planRequests, pr, repo)

	// 2. look for pending outputs
	var outputs []output
	outputs = ps.getPendinPRUpdates(ctx, outputs, pr, prModules)
	ps.postPlanOutput(outputs)
}

func (ps *Server) isLocalRepoUpToDate(pr pr) (bool, error) {
	prLastCommitHash := pr.Commits.Nodes[0].Commit.Oid
	localRepoCommitHash, err := ps.Repos.Hash(ctx, module.Spec.RepoURL, pr.HeadRefName, module.Spec.Path)
	if err != nil {
		return false, nil
	}

	if prLastCommitHash != localRepoCommitHash {
		return false, nil
	}

	return true, nil
}

func (ps *Server) getKubeModuleList(ctx context.Context) ([]tfaplv1beta1.Module, error) {
	moduleList := &tfaplv1beta1.ModuleList{}
	modules := []tfaplv1beta1.Module{}

	if err := ps.ClusterClt.List(ctx, moduleList); err != nil {
		return nil, err
	}

	for _, module := range moduleList.Items {
		modules = append(modules, module)
	}
	return modules, nil
}

func getRepoList(moduleList []tfaplv1beta1.Module) []gitHubRepo {
	var repoList = []gitHubRepo{}
	var repoURLs = []string{}
	var m = make(map[string]bool)

	for _, module := range moduleList {
		if m[module.Spec.RepoURL] {
			continue
		}
		repoURLs = append(repoURLs, module.Spec.RepoURL)
		m[module.Spec.RepoURL] = true
	}

	for _, repoURL := range repoURLs {
		var repo gitHubRepo
		parts := strings.Split(strings.Split(repoURL, ":")[1], "/")
		repo.name = strings.TrimSuffix(parts[1], ".git")
		repo.owner = parts[0]

		repoList = append(repoList, repo)
	}

	return repoList
}

func (ps *Server) getOpenPullRequests(ctx context.Context, repoOwner, repoName string) (*gitPRResponse, error) {
	url := "https://api.github.com/graphql"

	query := `
	query {
		repository(owner: "` + repoOwner + `", name: "` + repoName + `") {
			pullRequests(states: OPEN, last: 100) {
				nodes {
					number
					headRefName
					commits(last: 1) {
						nodes {
							commit {
								oid
							}
						}
					}
					comments(last:20) {
						nodes {
							databaseId
							body
						}
					}
					files(first: 100) {
						nodes {
							path
						}
					}
				}
			}
		}
	}`

	q := gitPRRequest{Query: query}

	resp, err := ps.GraphqlClient.http.R().
		SetContext(ctx).
		SetBody(q).
		SetResult(&gitPRResponse{}).
		Post(url)

	var errorResponse *gitPRResponse

	if err != nil {
		return errorResponse, err
	}
	if resp.StatusCode() != http.StatusOK {
		return errorResponse, fmt.Errorf("http status %d Error: %s", resp.StatusCode(), resp.Body())
	}

	result := resp.Result().(*gitPRResponse)

	return result, nil
}

func (ps *Server) getPRModuleList(prFiles prFiles, kubeModules []tfaplv1beta1.Module) ([]tfaplv1beta1.Module, error) {
	var pathList []string
	for _, file := range prFiles {
		pathList = append(pathList, file.Path)
	}

	var modulesUpdated []tfaplv1beta1.Module
	for _, kubeModule := range kubeModules {
		moduleUpdated := ps.pathBelongsToModule(pathList, kubeModule)
		if moduleUpdated {
			modulesUpdated = append(modulesUpdated, kubeModule)
		}
	}

	return modulesUpdated, nil
}
