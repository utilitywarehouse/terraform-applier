package prplanner

import (
	"github.com/go-resty/resty/v2"
	"k8s.io/apimachinery/pkg/types"
)

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
	module    types.NamespacedName
	body      prComment
	commentID int
	prNumber  int
}

type graphqlClient struct {
	url  string
	http *resty.Client
}
