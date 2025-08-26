package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/utilitywarehouse/git-mirror/repopool"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/prplanner"
)

var expectedEvents = []string{"ping", "pull_request", "issue_comment", "push"}

type Webhook struct {
	ListenAddress         string
	WebhookSecret         string
	SkipWebhookValidation bool
	Repos                 git.Repositories

	PRPlanner *prplanner.Planner
	Log       *slog.Logger
}

func (wh *Webhook) Start() {
	http.HandleFunc("/github-events", wh.handleWebhook)
	if err := http.ListenAndServe(wh.ListenAddress, nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
		wh.Log.Error("unable to start server", "err", err)
	}

	wh.Log.Error("webhook listener stopped")
}

func (wh *Webhook) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get the X-GitHub-Event header
	event := r.Header.Get("X-GitHub-Event")

	if !slices.Contains(expectedEvents, event) {
		// exit early
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		wh.Log.Error("cannot read request body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !wh.SkipWebhookValidation && !wh.isValidSignature(r, body, wh.WebhookSecret) {
		wh.Log.Error("invalid signature")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Parse the JSON payload
	var payload GitHubEvent
	if err := json.Unmarshal(body, &payload); err != nil {
		wh.Log.Error("cannot unmarshal json payload", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if event == "ping" {
		w.Write([]byte("pong"))
		return
	}

	// only process event if its from synced repository
	if _, err := wh.Repos.Repository(payload.Repository.URL); err != nil {
		return
	}

	if event == "push" {
		go wh.processPushEvent(payload)
		return
	}

	if event == "pull_request" {
		if wh.PRPlanner == nil || payload.PullRequest.Draft {
			return
		}

		switch payload.Action {
		case "opened", "reopened":
			go wh.PRPlanner.ProcessPRWebHookEvent(toPRPlannerEvent(payload), payload.Number)
		case "synchronize":
			// synchronize and push events are triggered at the same time, so sleep
			// here ensures push event is action or at least mirror process started
			time.Sleep(5 * time.Second)
			go wh.PRPlanner.ProcessPRWebHookEvent(toPRPlannerEvent(payload), payload.Number)
		case "closed":
			if !payload.PullRequest.Merged {
				return
			}
			// closed and push (on default branch) events are triggered at the same time,
			// so sleep here ensures push event is action or at least mirror process started
			time.Sleep(5 * time.Second)
			go wh.PRPlanner.ProcessPRCloseEvent(toPRPlannerEvent(payload))
		}
	}

	if event == "issue_comment" {
		if wh.PRPlanner == nil || prplanner.IsSelfComment(payload.Comment.Body) {
			return
		}

		switch payload.Action {
		case "created", "edited":
			// we know the body, but we still need to know the module user is requesting
			// plan run for belongs to this PR hence we need to do full reconcile of PR
			go wh.PRPlanner.ProcessPRWebHookEvent(toPRPlannerEvent(payload), payload.Issue.Number)
		}
	}
}

func toPRPlannerEvent(event GitHubEvent) prplanner.GitHubWebhook {
	e := prplanner.GitHubWebhook{
		Action: event.Action,
		Number: event.Number,
	}

	e.Repository.Name = event.Repository.Name
	e.Repository.URL = event.Repository.URL
	e.Repository.Owner.Login = event.Repository.Owner.Login
	e.PullRequest.Draft = event.PullRequest.Draft
	e.PullRequest.Merged = event.PullRequest.Merged
	e.PullRequest.MergeCommitSHA = event.PullRequest.MergeCommitSHA
	return e
}

func (wh *Webhook) processPushEvent(event GitHubEvent) {
	err := wh.Repos.Mirror(context.Background(), event.Repository.URL)
	if err != nil {
		if errors.Is(err, repopool.ErrNotExist) {
			return
		}
		wh.Log.Error("unable to process push event", "repo", event.Repository.URL, "err", err)
		return
	}
}

func (wh *Webhook) isValidSignature(r *http.Request, message []byte, secret string) bool {
	gotSignature := r.Header.Get("X-Hub-Signature-256")

	expSignature := wh.computeHMAC(message, secret)
	if expSignature == "" {
		return false
	}

	return hmac.Equal([]byte(gotSignature), []byte(expSignature))
}

func (wh *Webhook) computeHMAC(message []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))

	if _, err := mac.Write(message); err != nil {
		wh.Log.Error("cannot compute hmac for request", "error", err)
		return ""
	}

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
