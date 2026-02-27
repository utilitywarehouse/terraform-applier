package prplanner

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// Metadata Prefix to identify our comments
	metaStart = "<!-- terraform-applier-pr-planner-metadata:"
	metaEnd   = " -->"
)

var (
	planReqMsgRegex = regexp.MustCompile("^`?@terraform-applier plan `?([\\w-.\\/]+)`?$")

	// find our hidden JSON block
	metadataRegex = regexp.MustCompile(fmt.Sprintf(`(?s)%s(.*?)%s`, regexp.QuoteMeta(metaStart), regexp.QuoteMeta(metaEnd)))

	autoPlanDisabledTml = "Auto plan is disabled for this PR.\n" +
		"Please post `@terraform-applier plan <module_name>` as comment if you want to request terraform plan for a particular module."

	requestAcknowledgedMsgTml = "Received terraform plan request\n" +
		"```\n" +
		"Cluster: %s\n" +
		"Module: %s\n" +
		"Path: %s\n" +
		"Commit ID: %s\n" +
		"Requested At: %s\n" +
		"```\n" +
		"Do not edit this comment. This message will be updated once the plan run is completed.\n" +
		"To manually trigger plan again please post `@terraform-applier plan %s` as comment."

	runOutputMsgTml = "Terraform run output for\n" +
		"```\n" +
		"Cluster: %s\n" +
		"Module: %s\n" +
		"Path: %s\n" +
		"Commit ID: %s\n" +
		"```\n" +
		"<details><summary><b>%s Run Status: %s, Run Summary: %s</b></summary>" +
		"\n\n```terraform\n%s\n```\n</details>\n" +
		"\n> To manually trigger plan again please post `@terraform-applier plan %s` as comment."
)

type MsgType string

const (
	MsgTypePlanRequest      MsgType = "PlanRequest"
	MsgTypeRunOutput        MsgType = "RunOutput"
	MsgTypeAutoPlanDisabled MsgType = "AutoPlanDisabled"
)

// CommentMetadata is the hidden JSON structure
type CommentMetadata struct {
	Type     MsgType `json:"type"`
	Cluster  string  `json:"cluster,omitempty"`
	Module   string  `json:"module,omitempty"` // Stores "Namespace/Name"
	Path     string  `json:"path,omitempty"`
	CommitID string  `json:"commit_id,omitempty"`
	ReqAt    string  `json:"req_at,omitempty"` // RFC3339 String
}

// embedMetadata serializes the struct into a hidden HTML comment
func embedMetadata(meta CommentMetadata) string {
	b, err := json.Marshal(meta)
	if err != nil {
		panic(fmt.Sprintf("unable to marshal pr comment metadata: %v", meta))
	}
	return fmt.Sprintf("\n\n%s %s %s", metaStart, string(b), metaEnd)
}

// extractMetadata parses the hidden JSON from a comment body
func extractMetadata(commentBody string) *CommentMetadata {
	matches := metadataRegex.FindStringSubmatch(commentBody)
	if len(matches) < 2 {
		return nil
	}

	rawJson := matches[1]

	// GitHub markdown/browsers often convert spaces to Non-Breaking Spaces (\u00A0)
	// or inject odd formatting.
	rawJson = strings.ReplaceAll(rawJson, "\u00A0", "")
	rawJson = strings.TrimSpace(rawJson)

	var meta CommentMetadata
	if err := json.Unmarshal([]byte(rawJson), &meta); err != nil {
		slog.Default().Error("unable to parse PR comment metadata json", "logger", "pr-planner", "err", err)
		return nil
	}
	return &meta
}

func parsePlanReqMsg(commentBody string) string {
	matches := planReqMsgRegex.FindStringSubmatch(commentBody)

	if len(matches) == 2 {
		return matches[1]
	}

	return ""
}

func requestAcknowledgedMsg(cluster, module, path, commitID string, reqAt *metav1.Time) string {
	display := fmt.Sprintf(requestAcknowledgedMsgTml, cluster, module, path, commitID, reqAt.Format(time.RFC3339), path)

	meta := CommentMetadata{
		Type:     MsgTypePlanRequest,
		Cluster:  cluster,
		Module:   module,
		Path:     path,
		CommitID: commitID,
		ReqAt:    reqAt.Format(time.RFC3339),
	}

	return display + embedMetadata(meta)
}

func parseRequestAcknowledgedMsg(commentBody string) (cluster string, module types.NamespacedName, path string, commID string, ReqAt *time.Time) {
	meta := extractMetadata(commentBody)
	if meta == nil || meta.Type != MsgTypePlanRequest {
		return
	}

	if t, err := time.Parse(time.RFC3339, meta.ReqAt); err == nil {
		ReqAt = &t
	}

	return meta.Cluster, parseNamespaceName(meta.Module), meta.Path, meta.CommitID, ReqAt
}

func parseRunOutputMsg(comment string) (cluster string, module types.NamespacedName, path string, commit string) {
	meta := extractMetadata(comment)
	if meta == nil || meta.Type != MsgTypeRunOutput {
		return
	}
	return meta.Cluster, parseNamespaceName(meta.Module), meta.Path, meta.CommitID
}

func runOutputMsg(cluster, module, path string, run *v1beta1.Run) string {
	// https://github.com/orgs/community/discussions/27190
	characterLimit := 65000

	statusSymbol := "✅"

	runOutput := run.Output
	// when run fails upload init output as well since it may contain
	// reason of the failure
	if run.Status == v1beta1.StatusErrored {
		statusSymbol = "⛔"
		runOutput = run.InitOutput + "\n" + run.Output
	}

	runes := []rune(runOutput)

	if len(runes) > characterLimit {
		runOutput = "Plan output has reached the max character limit of " + fmt.Sprintf("%d", characterLimit) + " characters. " +
			"The output is truncated from the top.\n" + string(runes[(len(runes)-characterLimit):])
	}

	display := fmt.Sprintf(runOutputMsgTml, cluster, module, path, run.CommitHash, statusSymbol, run.Status, run.Summary, runOutput, path)

	meta := CommentMetadata{
		Type:     MsgTypeRunOutput,
		Cluster:  cluster,
		Module:   module,
		Path:     path,
		CommitID: run.CommitHash,
	}

	return display + embedMetadata(meta)
}

func parseNamespaceName(str string) types.NamespacedName {
	namespacedName := strings.Split(str, "/")

	if len(namespacedName) == 2 {
		return types.NamespacedName{Namespace: namespacedName[0], Name: namespacedName[1]}
	}

	if len(namespacedName) == 1 {
		return types.NamespacedName{Name: namespacedName[0]}
	}

	return types.NamespacedName{}
}

func isAutoPlanDisabledCommentPosted(prComments []prComment) bool {
	for _, comment := range prComments {
		meta := extractMetadata(comment.Body)
		if meta != nil && meta.Type == MsgTypeAutoPlanDisabled {
			return true
		}
	}
	return false
}

// IsSelfComment will return true if comments matches TF applier comment templates
func IsSelfComment(comment string) bool {
	return strings.Contains(comment, metaStart)
}
