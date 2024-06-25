package prplanner

import (
	"k8s.io/apimachinery/pkg/types"
)

// TODO: Add isDraft to filter out draft PRs in the PR loop
// skip all pullRequests.nodes.isDraft = true
//
//	query {
//	  repository(owner: "utilitywarehouse", name: "tf_okta") {
//	    pullRequests(states: OPEN, last: 20) {
//	      nodes {
//	        number
//	        isDraft
//	      }
//	    }
//	  }
//	}
const queryRepoPRs = `
query ($owner: String!,$repoName: String! ) {
  repository(owner: $owner, name: $repoName) {
    pullRequests(states: OPEN, last: 20) {
      nodes {
        number
        headRefName
        commits(last: 20) {
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

type gitPRRequest struct {
	Query     string `json:"query,omitempty"`
	Variables struct {
		Owner    string `json:"owner"`
		RepoName string `json:"repoName"`
	} `json:"variables,omitempty"`
}

type gitPRResponse struct {
	Data struct {
		Repository struct {
			PullRequests struct {
				Nodes []*pr `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
}

type pr struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	Commits     struct {
		Nodes []prCommit `json:"nodes"`
	} `json:"commits"`
	Comments struct {
		Nodes []prComment `json:"nodes"`
	} `json:"comments"`
	Files struct {
		Nodes []prFiles `json:"nodes"`
	} `json:"files"`
}

type prCommit struct {
	Commit struct {
		Oid string `json:"oid"`
	} `json:"commit"`
}

type prComment struct {
	DatabaseID int    `json:"databaseId"`
	Body       string `json:"body"`
}

type prFiles struct {
	Path string `json:"path"`
}

type output struct {
	module    types.NamespacedName
	body      prComment
	commentID int
	prNumber  int
}
