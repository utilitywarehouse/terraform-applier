package prplanner

const queryRepoPRs = `
query ($owner: String!,$repoName: String! ) {
  repository(owner: $owner, name: $repoName) {
    pullRequests(states: OPEN, last: 20) {
      nodes {
	  	baseRefName
        baseRepository {
          name
          url
        }
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
	Number         int    `json:"number"`
	BaseRefName    string `json:"baseRefName"`
	BaseRepository prRepo `json:"baseRepository"`
	HeadRefName    string `json:"headRefName"`
	IsDraft        bool   `json:"isDraft"`
	Author         author `json:"author"`
	Commits        struct {
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

type prRepo struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
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

type GitHubWebhook struct {
	Action string `json:"action"`
	Number int    `json:"number"`
	Issue  struct {
		Number int `json:"number"`
	} `json:"issue"`
	Repository struct {
		FullName string `json:"full_name"`
		GitURL   string `json:"clone_url"`
	} `json:"repository"`
}
