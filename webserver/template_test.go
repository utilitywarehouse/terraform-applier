package webserver

import (
	"bytes"
	"os"
	"testing"
	"time"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	boolTrue = true
)

func getMetaTime(h, m, s int) *metav1.Time {
	return &metav1.Time{Time: time.Date(2022, 02, 01, h, m, s, 0000, time.UTC)}
}

func Test_ExecuteTemplate(t *testing.T) {
	modules := []tfaplv1beta1.Module{
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "admins", Namespace: "foo"},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "00 */1 * * *",
				RepoURL:  "https://github.com/utilitywarehouse/terraform-applier.git",
				RepoRef:  "prj-dev",
				Path:     "foo/admins",
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:            "Running",
				LastDefaultRunStartedAt: getMetaTime(10, 30, 1),
				StateMessage:            "Initialising",
				StateReason:             tfaplv1beta1.ReasonForcedPlanTriggered,
			},
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "users", Namespace: "foo"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL:  "git@github.com:utilitywarehouse/terraform-applier.git",
				RepoRef:  "master",
				Path:     "foo/users",
				PlanOnly: &boolTrue,
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:            "Errored",
				StateReason:             tfaplv1beta1.ReasonSpecsParsingFailure,
				LastRunType:             tfaplv1beta1.PollingRun,
				LastDefaultRunStartedAt: getMetaTime(10, 30, 1),
				StateMessage:            `some very long error message with \n  Terraform has created a lock file .terraform.lock.hcl to record the provider selections it made above. Include this file in your version control repository so that Terraform can guarantee to make the same selections by default when you run "terraform init" in the future`,
			},
		},
		{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "groups", Namespace: "bar",
				Annotations: map[string]string{tfaplv1beta1.RunRequestAnnotationKey: `'{"id":"VMqlQIIX","reqAt":"2024-04-11T15:05:46Z","type":"ForcedPlan"}'`},
			},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL:  "ssh://git@github.com/utilitywarehouse/terraform-applier.git",
				RepoRef:  "as-test-module",
				Schedule: "00 */2 * * *",
				Path:     "integration_test/src/modules/hello",
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:            "Ready",
				StateReason:             tfaplv1beta1.ReasonApplied,
				LastDefaultRunStartedAt: getMetaTime(2, 10, 1),

				StateMessage:             "Apply complete! Resources: 7 added, 0 changed, 0 destroyed",
				LastDefaultRunCommitHash: "abcccf2a0f758ba0d8e88a834a2acdba5885577c",
				LastRunType:              tfaplv1beta1.ScheduledRun,
			},
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "users", Namespace: "bar"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL:  "git@github.com:utilitywarehouse/terraform-applier.git",
				Schedule: "*/15 * * * ",
				Path:     "dev/users",
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:            "Errored",
				StateReason:             tfaplv1beta1.ReasonApplyFailed,
				LastDefaultRunStartedAt: getMetaTime(10, 30, 1),
				StateMessage: `unparseable schedule "*/04 * * * ": expected exactly 5 fields, found
				4: [*/04 * * *]`,
			},
		},
	}

	result := createNamespaceMap(modules)

	statusHTML, err := os.ReadFile("templates/status.html")
	if err != nil {
		t.Errorf("error reading template: %v\n", err)
		return
	}

	templt, err := createTemplate(string(statusHTML))
	if err != nil {
		t.Errorf("error parsing template: %v\n", err)
		return
	}

	rendered := &bytes.Buffer{}
	err = templt.ExecuteTemplate(rendered, "index", result)
	if err != nil {
		t.Errorf("error executing template: %v\n", err)
		return
	}

	// open index.html in browser to view test output
	if err := os.WriteFile("index.html", rendered.Bytes(), 0666); err != nil {
		t.Errorf("error reading test file:  %v\n", err)
		return
	}
}
