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
				Path:     "foo/admins",
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState: "Running",
				RunStartedAt: getMetaTime(10, 30, 1),
				StateMessage: "Initialising",
			},
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "users", Namespace: "foo"},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "*/30 * * * *",
				Path:     "foo/users",
				PlanOnly: &boolTrue,
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:  "Errored",
				RunStartedAt:  getMetaTime(10, 30, 1),
				RunFinishedAt: getMetaTime(10, 35, 30),
				StateMessage:  `some very long error message with \n  Terraform has created a lock file .terraform.lock.hcl to record the provider selections it made above. Include this file in your version control repository so that Terraform can guarantee to make the same selections by default when you run "terraform init" in the future`,
			},
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "application", Namespace: "bar"},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "00 06 * * *",
				Path:     "dev/application",
				Suspend:  &boolTrue,
			},
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "groups", Namespace: "bar"},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "00 */2 * * *",
				Path:     "dev/groups",
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:  "Ready",
				RunStartedAt:  getMetaTime(2, 10, 1),
				RunFinishedAt: getMetaTime(2, 15, 30),

				StateMessage:  "Apply complete! Resources: 7 added, 0 changed, 0 destroyed",
				RemoteURL:     "github.com/org/repo",
				RunCommitHash: "abcccf2a0f758ba0d8e88a834a2acdba5885577c",
				RunCommitMsg:  `initial commit (john)`,
				LastDriftInfo: tfaplv1beta1.OutputStats{
					CommitHash: "abcccf2a0f758ba0d8e88a834a2acdba5885577c",
					Timestamp:  getMetaTime(2, 15, 19),
					Output: `2

Terraform used the selected providers to generate the following execution
plan. Resource actions are indicated with the following symbols:
+ create

Terraform will perform the following actions:

# null_resource.echo will be created
+ resource "null_resource" "echo" {
	+ id = (known after apply)
	}

# null_resource.env1 will be created
+ resource "null_resource" "env1" {
	+ id = (known after apply)
	}

# null_resource.env2 will be created
+ resource "null_resource" "env2" {
	+ id = (known after apply)
	}

Plan: 7 to add, 0 to change, 0 to destroy.`,
				},
				LastApplyInfo: tfaplv1beta1.OutputStats{
					CommitHash: "abcccf2a0f758ba0d8e88a834a2acdba5885577c",
					Timestamp:  getMetaTime(2, 15, 39),
					Output: `
null_resource.echo: Creating...
null_resource.env3: Creating...
null_resource.env3: Provisioning with 'local-exec'...
null_resource.echo: Provisioning with 'local-exec'...
null_resource.echo (local-exec): Executing: ["/bin/sh" "-c" "echo 'Hello World'"]
null_resource.env3 (local-exec): Executing: ["/bin/sh" "-c" "echo $TF_ENV_3"]
null_resource.env3 (local-exec): env-value-from-config3
null_resource.env3: Creation complete after 0s [id=892200032364971021]
null_resource.echo (local-exec): Hello World
null_resource.echo: Creation complete after 0s [id=7774449071607325874]
null_resource.variable1: Creating...

Apply complete! Resources: 7 added, 0 changed, 0 destroyed.`,
				},
			},
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "users", Namespace: "bar"},
			Spec: tfaplv1beta1.ModuleSpec{
				Schedule: "*/15 * * * ",
				Path:     "dev/users",
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:  "Errored",
				RunStartedAt:  getMetaTime(10, 30, 1),
				RunFinishedAt: getMetaTime(10, 35, 30),
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

	// uncomment to load index.html file locally after running test
	// if err := os.WriteFile("index.html", rendered.Bytes(), 0666); err != nil {
	// 	t.Errorf("error reading test file:  %v\n", err)
	// 	return
	// }
}
