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
	planReqMsgRegex = regexp.MustCompile(`@terraform-applier plan ([\w'-]+\/?[\w'-]+)`)

	requestAcknowledgedMsgTml = "Received terraform plan request\n" +
		"```\n" +
		"Module: %s\n" +
		"Path: %s\n" +
		"Requested At: %s\n" +
		"```\n" +
		"Do not edit this comment"

	requestAcknowledgedMsgRegex = regexp.MustCompile(`Received terraform plan request\n\x60{3}\nModule: (.+)\nPath: (.+)\nRequested At: (.+)`)

	runOutputMsgTml = "Terraform plan output for\n" +
		"```\n" +
		"Module: %s\n" +
		"Path: %s\n" +
		"Commit ID: %s\n" +
		"```\n" +
		"<details><summary><b>Run Status: %s, Run Summary: %s</b></summary>" +
		"\n\n```terraform\n%s\n```\n</details>"

	runOutputMsgRegex = regexp.MustCompile(`Terraform plan output for\n\x60{3}\nModule: (.+)\nPath: (.+)\nCommit ID: (.+)\n`)
)

func parsePlanReqMsg(commentBody string) types.NamespacedName {
	matches := planReqMsgRegex.FindStringSubmatch(commentBody)

	if len(matches) == 2 && matches[1] != "" {
		return parseNamespaceName(matches[1])
	}

	return types.NamespacedName{}
}

func requestAcknowledgedMsg(module, path string, reqAt *metav1.Time) string {
	return fmt.Sprintf(requestAcknowledgedMsgTml, module, path, reqAt.Format(time.RFC3339))
}

func parseRequestAcknowledgedMsg(commentBody string) (types.NamespacedName, string, *time.Time) {
	matches := requestAcknowledgedMsgRegex.FindStringSubmatch(commentBody)
	if len(matches) == 4 {
		t, err := time.Parse(time.RFC3339, matches[3])
		if err == nil {
			return parseNamespaceName(matches[1]), matches[2], &t
		}
		return parseNamespaceName(matches[1]), matches[2], nil
	}

	return types.NamespacedName{}, "", nil
}

func parseRunOutputMsg(comment string) (module types.NamespacedName, commit string) {
	matches := runOutputMsgRegex.FindStringSubmatch(comment)
	if len(matches) == 4 {
		return parseNamespaceName(matches[1]), matches[3]
	}

	return types.NamespacedName{}, ""
}

func runOutputMsg(module, path string, run *v1beta1.Run) string {
	// https://github.com/orgs/community/discussions/27190
	characterLimit := 65000
	runOutput := run.Output
	runes := []rune(runOutput)

	if len(runes) > characterLimit {
		runOutput = "Plan output has reached the max character limit of " + fmt.Sprintf("%d", characterLimit) + " characters. " +
			"The output is truncated from the top.\n" + string(runes[characterLimit:])
	}

	return fmt.Sprintf(runOutputMsgTml, module, path, run.CommitHash, run.Status, run.Summary, runOutput)
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
