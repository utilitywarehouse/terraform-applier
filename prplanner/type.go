package prplanner

const queryRepoPRs = `
query ($owner: String!,$repoName: String! ) {
  repository(owner: $owner, name: $repoName) {
    pullRequests(states: OPEN, last: 20) {
      nodes { ` + prFieldQuery + `}}}}`

const queryRepoPR = `
query ($owner: String!,$repoName: String!, $prNumber: Int! ) {
  repository(owner: $owner, name: $repoName ) {
    pullRequest(number: $prNumber) {
     ` + prFieldQuery + `}}}`

const prFieldQuery = `
baseRefName
baseRepository {
  name
  owner {
    login
  }
  url
}
number
headRefName
isDraft
closed
merged
mergeCommit {
  oid
}
author {
  login
}
comments(last:50) {
  nodes {
    databaseId
    body
    author {
      login
    }
  }
}
`

type gitPRRequest struct {
	Query     string `json:"query,omitempty"`
	Variables struct {
		Owner    string `json:"owner"`
		RepoName string `json:"repoName"`
		PRNumber int    `json:"prNumber"`
	} `json:"variables,omitempty"`
}

type Error struct {
	Message   string   `json:"message"`
	Path      []string `json:"path"`
	Locations []struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"locations"`
}

type gitPRsResponse struct {
	Data struct {
		Repository struct {
			PullRequests struct {
				Nodes []*pr `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
	Errors []Error `json:"errors"`
}

type gitPRResponse struct {
	Data struct {
		Repository struct {
			PullRequest *pr `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	Errors []Error `json:"errors"`
}

type pr struct {
	Number         int    `json:"number"`
	BaseRefName    string `json:"baseRefName"`
	BaseRepository prRepo `json:"baseRepository"`
	HeadRefName    string `json:"headRefName"`
	IsDraft        bool   `json:"isDraft"`
	Closed         bool   `json:"closed"`
	Merged         bool   `json:"merged"`
	MergeCommit    Commit `json:"mergeCommit"`
	Author         author `json:"author"`
	Comments       struct {
		Nodes []prComment `json:"nodes"`
	} `json:"comments"`
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

type Commit struct {
	Oid string `json:"oid"`
}

type prComment struct {
	DatabaseID int    `json:"databaseId"`
	Author     author `json:"author"`
	Body       string `json:"body"`
}

type GitHubWebhook struct {
	Action string `json:"action"`
	Number int    `json:"number"`

	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		URL string `json:"html_url"`
	} `json:"repository"`

	PullRequest struct {
		Number         int    `json:"number"`
		State          string `json:"state"`
		Draft          bool   `json:"draft"`
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
	} `json:"pull_request"`

	// only for comments
	Issue struct {
		Number int  `json:"number"`
		Draft  bool `json:"draft"`
	} `json:"issue"`

	Comment struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Body string `json:"body"`
	} `json:"comment"`
}
