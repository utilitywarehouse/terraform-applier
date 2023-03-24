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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ModuleSpec defines the desired state of Module
type ModuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Path to the directory containing Terraform Root Module (.tf) files.
	Path string `json:"path"`

	// The schedule in Cron format. Module will do periodic run for a given schedule
	// if no schedule provided then module will only run if new PRs are added to given module path
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// This flag tells the controller to suspend all subsequent runs, it does
	// not apply to already started run. Defaults to false.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// +optional
	PlanOnly *bool `json:"planOnly,omitempty"`

	// List of environment variables passed to the Terraform execution.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// List of input variables passed to the Terraform execution.
	// +optional
	Var []corev1.EnvVar `json:"var,omitempty"`

	// PollInterval specifies the interval at which the Git repository must be checked.
	// +optional
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=60
	PollInterval int `json:"pollInterval,omitempty"`

	// DelegateServiceAccountSecretRef references a Secret of type
	// kubernetes.io/service-account-token in the same namespace as the Module
	// that will be used to fetch secrets from namespace
	// runs.
	// +optional
	// +kubebuilder:default=terraform-applier-delegate-token
	// +kubebuilder:validation:MinLength=1
	DelegateServiceAccountSecretRef string `json:"delegateServiceAccountSecretRef,omitempty"`

	// RunTimeout specifies the timeout in sec for performing a complete TF run (init,plan and apply if required).
	// +optional
	// +kubebuilder:default=900
	// +kubebuilder:validation:Maximum=1800
	RunTimeout int `json:"runTimeout,omitempty"`
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
	// 'Ready' -> last run finished successfully and its waiting for next run/event
	// 'Errored' -> last run finished with Error and its waiting for next run/event
	CurrentState string `json:"currentState,omitempty"`

	// StateMessage is a human readable message indicating details about current state.
	// +optional
	StateMessage string `json:"stateMessage,omitempty"`

	// Information when was the last time the run was started.
	// +optional
	RunStartedAt *metav1.Time `json:"runStartedAt,omitempty"`
	// Information when was the last time the run was finished.
	// +optional
	RunFinishedAt *metav1.Time `json:"runFinishedAt,omitempty"`

	// RunCommitHash is the hash of git commit of last run.
	// +optional
	RunCommitHash string `json:"runCommitHash,omitempty"`
	// RunCommitMsg is the message of git commit of last run.
	// +optional
	RunCommitMsg string `json:"runCommitMsg,omitempty"`

	// LastDriftInfo is the stdout of plan command.  it may contain error stream
	// +optional
	LastDriftInfo OutputStats `json:"lastDriftInfo,omitempty"`
	// LastApplyInfo is the stdout of apply command. it may contain error stream
	// +optional
	LastApplyInfo OutputStats `json:"lastApplyInfo,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule",description=""
//+kubebuilder:printcolumn:name="Suspend",type="string",JSONPath=".spec.suspend",description=""
//+kubebuilder:printcolumn:name="PlanOnly",type="string",JSONPath=".spec.planOnly",description=""
//+kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.currentState",description=""
//+kubebuilder:printcolumn:name="Started At",type="string",JSONPath=`.status.runStartedAt`,description=""
//+kubebuilder:printcolumn:name="Finished At",type="string",JSONPath=`.status.runFinishedAt`,description=""
//+kubebuilder:printcolumn:name="Commit",type="string",JSONPath=`.status.runCommitHash`,description="",priority=10
//+kubebuilder:printcolumn:name="Path",type="string",JSONPath=`.spec.path`,description="",priority=20
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="",priority=20
//+kubebuilder:printcolumn:name="Last Message",type="string",JSONPath=".status.stateMessage",description=""

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

type OutputStats struct {
	// Timestamp when logs was captured.
	// +optional
	Timestamp *metav1.Time `json:"timestamp,omitempty"`

	// CommitHash is the hash of git commit of this output.
	// +optional
	CommitHash string `json:"hash,omitempty"`

	// Output is the stdout of terraform command. it may contain error stream
	// +optional
	Output string `json:"output,omitempty"`
}

// Overall state of Module run
type state string

const (
	// 'Running' -> module is in running state
	StatusRunning state = "Running"
	// 'Ready' -> last run finished successfully and its waiting on next run/event
	StatusReady state = "Ready"
	// 'Errored' -> last run finished with Error and its waiting on next run/event
	StatusErrored state = "Errored"
)

func SetModuleStatusProgressing(m *Module, msg string) {
	m.Status.CurrentState = string(StatusRunning)
	m.Status.StateMessage = msg
}

func SetModuleStatusRunStarted(m *Module, msg, commitHash, commitMsg string, now time.Time) {
	m.Status.CurrentState = string(StatusRunning)
	m.Status.RunStartedAt = &metav1.Time{Time: now}
	m.Status.RunFinishedAt = nil
	m.Status.ObservedGeneration = m.Generation
	m.Status.RunCommitHash = commitHash
	m.Status.RunCommitMsg = commitMsg

	SetModuleStatusProgressing(m, msg)
}

func SetModuleStatusRunFinished(m *Module, msg string, now time.Time) {
	m.Status.CurrentState = string(StatusReady)
	m.Status.RunFinishedAt = &metav1.Time{Time: now}
	m.Status.StateMessage = msg
}

func SetModuleStatusFailed(m *Module, msg string, now time.Time) {
	m.Status.CurrentState = string(StatusErrored)
	m.Status.RunFinishedAt = &metav1.Time{Time: now}
	m.Status.StateMessage = msg
}