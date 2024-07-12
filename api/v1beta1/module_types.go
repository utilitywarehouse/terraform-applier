/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"encoding/json"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	RunRequestAnnotationKey = `terraform-applier.uw.systems/run-request`
)

// The potential reasons for events and current state
const (
	ReasonRunTriggered = "RunTriggered"

	ReasonRunPreparationFailed = "RunPreparationFailed"
	ReasonDelegationFailed     = "DelegationFailed"
	ReasonControllerShutdown   = "ControllerShutdown"
	ReasonSpecsParsingFailure  = "SpecsParsingFailure"
	ReasonGitFailure           = "GitFailure"
	ReasonUnknown              = "Unknown"
	ReasonInitialiseFailed     = "InitialiseFailed"
	ReasonPlanFailed           = "PlanFailed"
	ReasonApplyFailed          = "ApplyFailed"

	ReasonInitialised     = "Initialised"
	ReasonDriftDetected   = "DriftDetected"
	ReasonNoDriftDetected = "NoDriftDetected"
	ReasonApplied         = "Applied"
)

const (
	// following runs are called 'default runs' because it happens on revision
	// specified by user in spec
	//
	// ScheduledRun indicates a scheduled, regular terraform run.
	ScheduledRun = "ScheduledRun"
	// PollingRun indicated a run triggered by changes in the git repository.
	PollingRun = "PollingRun"
	// ForcedPlan indicates a forced (triggered on the UI) terraform plan.
	ForcedPlan = "ForcedPlan"
	// ForcedApply indicates a forced (triggered on the UI) terraform apply.
	ForcedApply = "ForcedApply"

	// non-default run happens on PR branch instead
	// PRPlan indicates terraform plan trigged by PullRequest on modules repo path.
	PRPlan = "PullRequestPlan"
)

// Overall state of Module run
type state string

const (
	// 'Running' -> module is in running state
	StatusRunning state = "Running"
	// 'OK' -> last run finished successfully and no drift detected
	StatusOk state = "Ok"
	// 'Drift_Detected' -> last run finished successfully and drift detected
	StatusDriftDetected state = "Drift_Detected"
	// 'Errored' -> last run finished with Error
	StatusErrored state = "Errored"
)

// ModuleSpec defines the desired state of Module
type ModuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// URL to the repository containing Terraform module source code.
	RepoURL string `json:"repoURL"`

	// The RepoRef specifies the revision of the repository for the module source code.
	// this can be tag or branch. If not specified, this defaults to "HEAD" (repo's default branch)
	// +optional
	// +kubebuilder:default=HEAD
	RepoRef string `json:"repoRef,omitempty"`

	// Path to the directory containing Terraform Root Module (.tf) files.
	Path string `json:"path"`

	// The schedule in Cron format. Module will do periodic run for a given schedule
	// if no schedule provided then module will only run if new PRs are added to given module path
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// +optional
	PlanOnly *bool `json:"planOnly,omitempty"`

	// if PlanOnPR is true, plan-on-pr feature will be enabled for this module
	// +optional
	// +kubebuilder:default=true
	PlanOnPR *bool `json:"planOnPR,omitempty"`

	// List of backend config attributes passed to the Terraform init
	// for terraform backend configuration
	// +optional
	Backend []corev1.EnvVar `json:"backend,omitempty"`

	// List of environment variables passed to the Terraform execution.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// List of input variables passed to the Terraform execution.
	// +optional
	Var []corev1.EnvVar `json:"var,omitempty"`

	// VaultRequests specifies credential generate requests from the vault
	// configured on the controller
	// +optional
	VaultRequests *VaultRequests `json:"vaultRequests,omitempty"`

	// PollInterval specifies the interval at which the Git repository must be checked.
	// +optional
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=60
	PollInterval int `json:"pollInterval,omitempty"`

	// DelegateServiceAccountSecretRef references a Secret of type
	// kubernetes.io/service-account-token in the same namespace as the Module
	// that will be used to fetch secrets, configmaps from modules' namespace.
	// if vaultRequests are specified, the service account's jwt will be used for vault authentication.
	// +optional
	// +kubebuilder:default=terraform-applier-delegate-token
	// +kubebuilder:validation:MinLength=1
	DelegateServiceAccountSecretRef string `json:"delegateServiceAccountSecretRef,omitempty"`

	// RunTimeout specifies the timeout in sec for performing a complete TF run (init,plan and apply if required).
	// +optional
	// +kubebuilder:default=900
	// +kubebuilder:validation:Maximum=1800
	RunTimeout int `json:"runTimeout,omitempty"`

	// List of roles and subjects assigned to that role for the module.
	// +optional
	RBAC []RBAC `json:"rbac,omitempty"`
}

// ModuleStatus defines the observed state of Module
type ModuleStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ObservedGeneration is the last reconciled generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// CurrentState denotes current overall status of module run
	// it will be either
	// 'Running' -> Module is in running state
	// 'OK' -> last run finished successfully and no drift detected
	// 'Drift_Detected' -> last run finished successfully and drift detected
	// 'Errored' -> last run finished with Error
	CurrentState string `json:"currentState,omitempty"`

	// StateReason is potential reason associated with current state.
	// +optional
	StateReason string `json:"stateReason,omitempty"`

	// LastRunType is a short description of the kind of terraform run that was attempted.
	// +optional
	LastRunType string `json:"runType,omitempty"`

	// LastDefaultRunStartedAt when was the last time the run was started.
	// Default Runs are runs happens on default repo ref set by user.
	// This field used in Reconcile loop
	// +optional
	LastDefaultRunStartedAt *metav1.Time `json:"lastDefaultRunStartedAt,omitempty"`

	// LastDefaultRunCommitHash is the hash of git commit of last run.
	// Default Runs are runs happens on default repo ref set by user.
	// This field used in Reconcile loop
	// +optional
	LastDefaultRunCommitHash string `json:"lastDefaultRunCommitHash,omitempty"`

	// Information when was the last time the module was successfully applied.
	// +optional
	LastAppliedAt *metav1.Time `json:"lastAppliedAt,omitempty"`

	// LastAppliedCommitHash is the hash of git commit of last successful apply.
	// +optional
	LastAppliedCommitHash string `json:"lastAppliedCommitHash,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule",description=""
//+kubebuilder:printcolumn:name="PlanOnly",type="string",JSONPath=".spec.planOnly",description=""
//+kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.currentState",description=""
//+kubebuilder:printcolumn:name="Reason",type="string",JSONPath=".status.stateReason",description=""
//+kubebuilder:printcolumn:name="Last Run Started At",type="string",JSONPath=`.status.lastDefaultRunStartedAt`,description=""
//+kubebuilder:printcolumn:name="Last Applied At",type="string",JSONPath=`.status.lastAppliedAt`,description=""
//+kubebuilder:printcolumn:name="Commit",type="string",JSONPath=`.status.lastDefaultRunCommitHash`,description="",priority=10
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="",priority=20

// Module is the Schema for the modules API
type Module struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModuleSpec   `json:"spec,omitempty"`
	Status ModuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ModuleList contains a list of Module
type ModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Module `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Module{}, &ModuleList{})
}

type VaultRequests struct {
	// aws specifies vault credential generation request for AWS secrets engine
	// If specified, controller will request AWS creds from vault and set
	// AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY and AWS_SESSION_TOKEN envs during
	// terraform run.
	// 'VAULT_AWS_ENG_PATH' env set on controller will be used as credential path
	// +optional
	AWS *VaultAWSRequest `json:"aws,omitempty"`
}

type VaultAWSRequest struct {
	// VaultRole Specifies the name of the vault role to generate credentials against.
	// +required
	VaultRole string `json:"vaultRole,omitempty"`

	// CredentialType specifies the type of credential to be used when retrieving credentials from the role.
	// Must be one of iam_user, assumed_role, or federation_token.
	// +kubebuilder:validation:Enum=iam_user;assumed_role;federation_token
	// +kubebuilder:default=assumed_role
	// +optional
	CredentialType string `json:"credentialType,omitempty"`

	// The ARN of the role to assume if credential_type on the Vault role is assumed_role.
	// Optional if the Vault role only allows a single AWS role ARN.
	// +optional
	RoleARN string `json:"roleARN,omitempty"`
}

type RBAC struct {
	// Name of the role. Allowed value at the moment is just "Admin"
	// +required
	// +kubebuilder:validation:Enum=Admin
	Role string `json:"role,omitempty"`
	// Subjects holds references to the objects the role applies to.
	// +required
	Subjects []Subject `json:"subjects,omitempty"`
}
type Subject struct {
	// Kind of object being referenced. Allowed values are "User" & "Group"
	// +required
	// +kubebuilder:validation:Enum=User;Group
	Kind string `json:"kind,omitempty"`
	// Name of the object being referenced. For "User" kind value should be email
	// +required
	Name string `json:"name,omitempty"`
}

func (m *Module) NamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: m.Namespace,
		Name:      m.Name,
	}
}

func (m *Module) IsPlanOnly() bool {
	return m.Spec.PlanOnly != nil && *m.Spec.PlanOnly
}

func (m *Module) NewRunRequest(reqType string) *Request {
	req := Request{
		RequestedAt: &metav1.Time{Time: time.Now()},
		ID:          NewRequestID(),
		Type:        reqType,
	}

	return &req
}

// PendingRunRequest returns pending requests if any from module's annotation.
func (m *Module) PendingRunRequest() (*Request, bool) {
	valueString, exists := m.ObjectMeta.Annotations[RunRequestAnnotationKey]
	if !exists {
		return nil, false
	}
	value := Request{}
	if err := json.Unmarshal([]byte(valueString), &value); err != nil {
		// unmarshal errors are ignored as it should not happen and if it does
		// it can be treated as no request pending and module can override it
		// with new valid request
		return nil, false
	}
	return &value, true
}
