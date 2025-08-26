package webhook

type GitHubEvent struct {
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
		Head           struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
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
