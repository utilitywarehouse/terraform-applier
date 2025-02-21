package webserver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	boolTrue = true
)

func getMetaTime(h, m, s int) *metav1.Time {
	return &metav1.Time{Time: time.Date(2022, 02, 01, h, m, s, 0000, time.UTC)}
}

func Test_ExecuteTemplate(t *testing.T) {
	statusHTML, err := os.ReadFile("templates/status.html")
	if err != nil {
		t.Errorf("error reading template: %v\n", err)
		return
	}
	statusTempt, err := createTemplate(string(statusHTML))
	if err != nil {
		t.Errorf("error parsing template: %v\n", err)
		return
	}
	moduleHTML, err := os.ReadFile("templates/module.html")
	if err != nil {
		t.Errorf("error reading template: %v\n", err)
		return
	}
	moduleTempt, err := createTemplate(string(moduleHTML))
	if err != nil {
		t.Errorf("error parsing template: %v\n", err)
		return
	}

	testRedis := sysutil.NewMockRedisInterface(gomock.NewController(t))

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
				StateReason:             tfaplv1beta1.ReasonRunTriggered,
			},
		},
		{
			TypeMeta: metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{
				Name: "users", Namespace: "foo",
				Annotations: map[string]string{tfaplv1beta1.RunRequestAnnotationKey: `'{"id":"VMqlQIIX","reqAt":"2024-04-11T15:05:46Z","type":"ForcedPlan"}'`},
			},
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
				CurrentState:             string(tfaplv1beta1.StatusDriftDetected),
				StateReason:              tfaplv1beta1.ReasonApplied,
				LastDefaultRunStartedAt:  getMetaTime(2, 10, 1),
				LastDefaultRunCommitHash: "abcccf2a0f758ba0d8e88a834a2acdba5885577c",
				LastRunType:              tfaplv1beta1.ScheduledRun,
			},
		},
		{
			TypeMeta:   metav1.TypeMeta{APIVersion: "terraform-applier.uw.systems/v1beta1", Kind: "Module"},
			ObjectMeta: metav1.ObjectMeta{Name: "users", Namespace: "bar"},
			Spec: tfaplv1beta1.ModuleSpec{
				RepoURL: "git@github.com:utilitywarehouse/terraform-applier.git",
				Path:    "dev/users",
			},
			Status: tfaplv1beta1.ModuleStatus{
				CurrentState:            string(tfaplv1beta1.StatusOk),
				StateReason:             tfaplv1beta1.ReasonApplyFailed,
				LastDefaultRunStartedAt: getMetaTime(10, 30, 1),
			},
		},
	}

	testRedis.EXPECT().Runs(gomock.Any(), gomock.Any(), "*").DoAndReturn(
		func(ctx context.Context, module types.NamespacedName, patternSuffix string) ([]*tfaplv1beta1.Run, error) {
			switch module {
			case types.NamespacedName{Name: "admins", Namespace: "foo"}:
				return nil, nil
			case types.NamespacedName{Name: "users", Namespace: "bar"}:
				return nil, nil
			case types.NamespacedName{Name: "users", Namespace: "foo"}:
				return []*tfaplv1beta1.Run{
					{
						Module:    types.NamespacedName{Name: "users", Namespace: "foo"},
						Request:   &tfaplv1beta1.Request{Type: tfaplv1beta1.PollingRun},
						StartedAt: getMetaTime(2, 10, 1),
						Duration:  60 * time.Second,
						Status:    tfaplv1beta1.StatusErrored,
					},
				}, nil
			case types.NamespacedName{Name: "groups", Namespace: "bar"}:
				return []*tfaplv1beta1.Run{
					{
						Module:     types.NamespacedName{Name: "groups", Namespace: "bar"},
						Request:    &tfaplv1beta1.Request{Type: tfaplv1beta1.PollingRun, RequestedAt: getMetaTime(3, 4, 2)},
						Status:     tfaplv1beta1.StatusOk,
						StartedAt:  getMetaTime(11, 10, 1),
						Duration:   60 * time.Second,
						CommitHash: "abcccf2a0f758ba0d8e88a834a2acdba5885577c",
						CommitMsg:  `initial commit (john)`,
						InitOutput: `{
  "terraform_version": "1.8.2",
  "platform": "linux_amd64",
  "provider_selections": {},
  "terraform_outdated": false
}

Initializing the backend...

Successfully configured the backend "local"! Terraform will automatically
use this backend unless the backend configuration changes.

Initializing provider plugins...
- Finding latest version of hashicorp/null...
- Using hashicorp/null v3.2.2 from the shared cache directory

Terraform has created a lock file .terraform.lock.hcl to record the provider
selections it made above. Include this file in your version control repository
so that Terraform can guarantee to make the same selections by default when
you run "terraform init" in the future.


Warning: Incomplete lock file information for providers

Due to your customized provider installation methods, Terraform was forced to
calculate lock file checksums locally for the following providers:
- hashicorp/null

The current .terraform.lock.hcl file only includes checksums for linux_amd64,
so Terraform running on another platform will fail to install these
providers.

To calculate additional checksums for another platform, run:
terraform providers lock -platform=linux_amd64
(where linux_amd64 is the platform to generate)

Terraform has been successfully initialized!
`,
						Output: `
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
					{
						Module:       types.NamespacedName{Name: "groups", Namespace: "bar"},
						Request:      &tfaplv1beta1.Request{Type: tfaplv1beta1.PollingRun, RequestedAt: getMetaTime(3, 1, 2)},
						StartedAt:    getMetaTime(10, 30, 1),
						Status:       tfaplv1beta1.StatusOk,
						Duration:     60 * time.Second,
						CommitHash:   "abcccf2a0f758ba0d8e88a834a2acdba5885577c",
						CommitMsg:    `initial commit (john)`,
						DiffDetected: true,
						InitOutput: `
{
  "terraform_version": "1.8.2",
  "platform": "linux_amd64",
  "provider_selections": {},
  "terraform_outdated": false
}

Initializing the backend...

Successfully configured the backend "local"! Terraform will automatically
use this backend unless the backend configuration changes.

Initializing provider plugins...
- Finding latest version of hashicorp/google-beta...
- Finding latest version of okta/okta...
- Finding latest version of hashicorp/aws...
- Finding latest version of hashicorp/google...
- Finding latest version of hashicorp/null...
- Using hashicorp/google v5.26.0 from the shared cache directory
- Using hashicorp/null v3.2.2 from the shared cache directory
- Using hashicorp/google-beta v5.26.0 from the shared cache directory
- Using okta/okta v4.8.1 from the shared cache directory
- Using hashicorp/aws v5.47.0 from the shared cache directory

Terraform has created a lock file .terraform.lock.hcl to record the provider
selections it made above. Include this file in your version control repository
so that Terraform can guarantee to make the same selections by default when
you run "terraform init" in the future.


Warning: Incomplete lock file information for providers

Due to your customized provider installation methods, Terraform was forced to
calculate lock file checksums locally for the following providers:
- hashicorp/aws
- hashicorp/google
- hashicorp/google-beta
- hashicorp/null
- okta/okta

The current .terraform.lock.hcl file only includes checksums for linux_amd64,
so Terraform running on another platform will fail to install these
providers.

To calculate additional checksums for another platform, run:
terraform providers lock -platform=linux_amd64
(where linux_amd64 is the platform to generate)

Terraform has been successfully initialized!`,
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
					{
						Module:     types.NamespacedName{Name: "groups", Namespace: "bar"},
						Request:    &tfaplv1beta1.Request{Type: tfaplv1beta1.PRPlan, RequestedAt: getMetaTime(1, 4, 2), PR: &tfaplv1beta1.PullRequest{Number: 345, HeadBranch: "dev"}},
						StartedAt:  getMetaTime(2, 3, 1),
						Status:     tfaplv1beta1.StatusOk,
						Duration:   60 * time.Second,
						CommitHash: "ba417c83b281eb71cdc6766dbd935b5bda7319f4",
						CommitMsg:  `update tf applier to v2.1.1 (john)`,
						Output: `
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
				}, nil

			default:
				return nil, fmt.Errorf("key not found")
			}
		}).AnyTimes()

	result := createNamespaceMap(modules)

	rendered := &bytes.Buffer{}
	err = statusTempt.ExecuteTemplate(rendered, "index", result)
	if err != nil {
		t.Errorf("error executing template: %v\n", err)
		return
	}

	// open index.html in browser to view test output
	if err := os.WriteFile("test-data/index.html", rendered.Bytes(), 0666); err != nil {
		t.Errorf("error writing test file:  %v\n", err)
		return
	}

	for i, m := range modules {
		module := &Module{Module: m}
		module.Runs = runInfo(context.Background(), testRedis, m.NamespacedName())

		rendered := &bytes.Buffer{}
		err = moduleTempt.ExecuteTemplate(rendered, "module", module)
		if err != nil {
			t.Errorf("error executing template: %v\n", err)
			return
		}

		// open index.html in browser to view test output
		if err := os.WriteFile(fmt.Sprintf("test-data/module%d.html", i), rendered.Bytes(), 0666); err != nil {
			t.Errorf("error writing test file:  %v\n", err)
			return
		}
	}
}
