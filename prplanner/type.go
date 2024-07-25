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
comments(last:50) {
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

	Issue struct {
		Number int `json:"number"`
	} `json:"issue"`

	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		URL string `json:"html_url"`
	} `json:"repository"`

	// only for comments
	Comment struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		Body string `json:"body"`
	} `json:"comment"`
}
