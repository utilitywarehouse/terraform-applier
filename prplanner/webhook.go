package prplanner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func (p *Planner) startWebhook() {
	http.HandleFunc("/github-events", p.handleWebhook)
	if err := http.ListenAndServe(p.ListenAddress, nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
		p.Log.Error("unable to start server", "err", err)
	}
}

func (p *Planner) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		p.Log.Error("cannot read request body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !p.isValidSignature(r, body, p.WebhookSecret) {
		p.Log.Error("invalid signature")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get the X-GitHub-Event header
	event := r.Header.Get("X-GitHub-Event")

	// Parse the JSON payload
	var payload GitHubWebhook
	if err := json.Unmarshal(body, &payload); err != nil {
		p.Log.Error("cannot unmarshal json payload", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if event == "ping" {
		w.Write([]byte("pong"))
		return
	}

	if event == "pull_request" {
		if payload.PullRequest.Draft {
			return
		}

		switch payload.Action {
		case "opened", "synchronize", "reopened":
			go p.processPRWebHookEvent(payload, payload.Number)
		case "closed":
			if !payload.PullRequest.Merged {
				return
			}
			go p.processPRWebHookEvent(payload, payload.Number)
		}
	}

	if event == "issue_comment" {
		if payload.Issue.Draft {
			return
		}
		if isSelfComment(payload.Comment.Body) {
			return
		}

		switch payload.Action {
		case "created", "edited":
			// we know the body, but we still need to know the module user is requesting
			// plan run for belongs to this PR hence we need to do full reconcile of PR
			go p.processPRWebHookEvent(payload, payload.Issue.Number)
		}
	}
}

func (p *Planner) processPRWebHookEvent(event GitHubWebhook, prNumber int) {
	ctx := context.Background()

	err := p.Repos.Mirror(ctx, event.Repository.URL)
	if err != nil {
		p.Log.Error("unable to mirror repository", "url", event.Repository.URL, "pr", prNumber, "err", err)
		return
	}

	pr, err := p.github.PR(ctx, event.Repository.Owner.Login, event.Repository.Name, prNumber)
	if err != nil {
		p.Log.Error("unable to get PR info", "pr", prNumber, "err", err)
		return
	}

	kubeModuleList := &tfaplv1beta1.ModuleList{}
	if err := p.ClusterClt.List(ctx, kubeModuleList); err != nil {
		p.Log.Error("error retrieving list of modules", "pr", prNumber, "error", err)
		return
	}

	p.Log.Debug("processing PR event", "pr", prNumber)
	p.processPullRequest(ctx, pr, kubeModuleList)
}

func (p *Planner) isValidSignature(r *http.Request, message []byte, secret string) bool {
	gotSignature := r.Header.Get("X-Hub-Signature-256")

	expSignature := p.computeHMAC(message, secret)
	if expSignature == "" {
		return false
	}

	return hmac.Equal([]byte(gotSignature), []byte(expSignature))
}

func (p *Planner) computeHMAC(message []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))

	if _, err := mac.Write(message); err != nil {
		p.Log.Error("cannot compute hmac for request", "error", err)
		return ""
	}

	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
