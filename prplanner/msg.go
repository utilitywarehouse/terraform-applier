package prplanner

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	planReqMsgRegex = regexp.MustCompile("^`?@terraform-applier plan `?([\\w-.\\/]+)`?$")

	moduleLimitReachedTml = "A limit of 5 modules per PR has been reached, hence auto plan is disabled for this PR.\n" +
		"Please post `@terraform-applier plan <module_name>` as comment if you want to request terraform plan for a particular module."

	moduleLimitReachedRegex = regexp.MustCompile("A limit of 5 modules per PR has been reached")

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

	requestAcknowledgedMsgRegex = regexp.MustCompile(`Received terraform plan request\n\x60{3}\nCluster: (.+)\nModule: (.+)\nPath: (.+)\nCommit ID: (.+)\nRequested At: (.+)`)

	runOutputMsgTml = "Terraform plan output for\n" +
		"```\n" +
		"Cluster: %s\n" +
		"Module: %s\n" +
		"Path: %s\n" +
		"Commit ID: %s\n" +
		"```\n" +
		"<details><summary><b>Run Status: %s, Run Summary: %s</b></summary>" +
		"\n\n```terraform\n%s\n```\n</details>\n" +
		"To manually trigger plan again please post `@terraform-applier plan %s` as comment."

	runOutputMsgRegex = regexp.MustCompile(`Terraform plan output for\n\x60{3}\nCluster: (.+)\nModule: (.+)\nPath: (.+)\nCommit ID: (.+)\n`)
)

func parsePlanReqMsg(commentBody string) string {
	matches := planReqMsgRegex.FindStringSubmatch(commentBody)

	if len(matches) == 2 {
		return matches[1]
	}

	return ""
}

func requestAcknowledgedMsg(cluster, module, path, commitID string, reqAt *metav1.Time) string {
	return fmt.Sprintf(requestAcknowledgedMsgTml, cluster, module, path, commitID, reqAt.Format(time.RFC3339), path)
}

func parseRequestAcknowledgedMsg(commentBody string) (cluster string, module types.NamespacedName, path string, commID string, ReqAt *time.Time) {
	matches := requestAcknowledgedMsgRegex.FindStringSubmatch(commentBody)
	if len(matches) == 6 {
		t, err := time.Parse(time.RFC3339, matches[5])
		if err == nil {
			return matches[1], parseNamespaceName(matches[2]), matches[3], matches[4], &t
		}
		return matches[1], parseNamespaceName(matches[2]), matches[3], matches[4], nil
	}

	return
}

func parseRunOutputMsg(comment string) (cluster string, module types.NamespacedName, path string, commit string) {
	matches := runOutputMsgRegex.FindStringSubmatch(comment)
	if len(matches) == 5 {
		return matches[1], parseNamespaceName(matches[2]), matches[3], matches[4]
	}

	return
}

func runOutputMsg(cluster, module, path string, run *v1beta1.Run) string {
	// https://github.com/orgs/community/discussions/27190
	characterLimit := 65000

	runOutput := run.Output
	// when run fails upload init output as well since it may contain
	// reason of the failure
	if run.Status == v1beta1.StatusErrored {
		runOutput = run.InitOutput + "\n" + run.Output
	}

	runes := []rune(runOutput)

	if len(runes) > characterLimit {
		runOutput = "Plan output has reached the max character limit of " + fmt.Sprintf("%d", characterLimit) + " characters. " +
			"The output is truncated from the top.\n" + string(runes[characterLimit:])
	}

	return fmt.Sprintf(runOutputMsgTml, cluster, module, path, run.CommitHash, run.Status, run.Summary, runOutput, path)
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

func isModuleLimitReachedCommentPosted(prComments []prComment) bool {
	for _, comment := range prComments {
		if moduleLimitReachedRegex.MatchString(comment.Body) {
			return true
		}
	}
	return false
}
