package prplanner

import (
	"regexp"
)

var (
	terraformPlanRequestRegex = regexp.MustCompile(`@terraform-applier plan ([\w'-]+\/?[\w'-]+)`)

	requestAcknowledgedTml   = "Received terraform plan request. Module: `%s` Requested At: `%s`"
	requestAcknowledgedRegex = regexp.MustCompile("Received terraform plan request. Module: `(.+)` Requested At: `(.+)`")

	outputBodyTml         = "Terraform plan output for module `%s` Commit ID: `%s`\n```terraform\n%s\n```"
	terraformPlanOutRegex = regexp.MustCompile("Terraform plan output for module `(.+?)` Commit ID: `(.+?)`")
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
        isDraft
        author {
          login
        }
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
            author {
              login
            }
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
	IsDraft     bool   `json:"isDraft"`
	Author      author `json:"author"`
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

type author struct {
	Login string `json:"login"`
}

type prCommit struct {
	Commit struct {
		Oid string `json:"oid"`
	} `json:"commit"`
}

type prComment struct {
	DatabaseID int    `json:"databaseId"`
	Author     author `json:"author"`
	Body       string `json:"body"`
}

type prFiles struct {
	Path string `json:"path"`
}
