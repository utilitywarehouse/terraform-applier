package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
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
		case "opened", "synchronize", "reopened":
			go wh.processPRWebHookEvent(payload)
		case "closed":
			if !payload.PullRequest.Merged {
				return
			}
			go wh.processPRCloseEvent(payload)
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
	// to avoid simultaneous fetch calls from all tf-appliers
	time.Sleep(time.Duration(rand.Float64() * float64(time.Minute)))

	err := wh.Repos.Mirror(context.Background(), event.Repository.URL)
	if err != nil {
		if errors.Is(err, repopool.ErrNotExist) {
			return
		}
		wh.Log.Error("unable to process push event", "repo", event.Repository.Name, "err", err)
		return
	}
}

func (wh *Webhook) processPRWebHookEvent(event GitHubEvent) {
	if err := wh.waitForHeadCommitSync(event); err != nil {
		wh.Log.Error("unable to process pr event", "repo", event.Repository.Name, "number", event.Number, "err", err)
		return
	}
	wh.PRPlanner.ProcessPRWebHookEvent(toPRPlannerEvent(event), event.Number)
}

func (wh *Webhook) processPRCloseEvent(event GitHubEvent) {
	if err := wh.waitForHeadCommitSync(event); err != nil {
		wh.Log.Error("unable to process pr close event", "repo", event.Repository.Name, "number", event.Number, "err", err)
		return
	}
	wh.PRPlanner.ProcessPRCloseEvent(toPRPlannerEvent(event))
}

// waitForHeadCommitSync will check if head SHA commit is mirrored
func (wh *Webhook) waitForHeadCommitSync(event GitHubEvent) error {
	// not all event will have head sha value
	if event.PullRequest.Head.SHA == "" {
		return nil
	}

	// push event has 1min jitter
	timeout := 2 * time.Minute

	start := time.Now()
	for {
		err := wh.Repos.ObjectExists(context.Background(), event.Repository.URL, event.PullRequest.Head.SHA)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, repopool.ErrNotExist):
			return fmt.Errorf("unable to check of new commit exits err:%w", err)
		case time.Since(start) > timeout:
			return fmt.Errorf("unable to check of new commit exits: timed out err:%w", err)
		default:
			time.Sleep(5 * time.Second)
		}
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
