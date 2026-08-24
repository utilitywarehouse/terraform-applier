package v1beta1

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	ErrRunRequestExist    = fmt.Errorf("another pending run request found")
	ErrNoRunRequestFound  = fmt.Errorf("no pending run requests found")
	ErrRunRequestMismatch = fmt.Errorf("run request ID doesn't match pending request id")
)

type RunMode string

const (
	ModePlanOnly RunMode = "Plan_Only"
	ModeApply    RunMode = "Apply"
)

// Run represents a complete run result of the terraform run
type Run struct {
	Module  types.NamespacedName `json:"module,omitempty"`
	Request *Request             `json:"request,omitempty"`

	Status       state         `json:"status,omitempty"` // 'Running','Success','Error'
	StartedAt    *metav1.Time  `json:"startedAT,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
	Mode         RunMode       `json:"mode,omitempty"`
	RepoRef      string        `json:"repoRef,omitempty"`
	CommitHash   string        `json:"commitHash,omitempty"`
	CommitMsg    string        `json:"commitMsg,omitempty"`
	DiffDetected bool          `json:"diffDetected,omitempty"`
	InitOutput   string        `json:"initOutput,omitempty"`

	Output       string            `json:"output,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	PolicyResult *PolicyEvalResult `json:"policyResult,omitempty"`
}

func NewRun(module *Module, req *Request) Run {
	return Run{
		Module: types.NamespacedName{
			Namespace: module.Namespace,
			Name:      module.Name,
		},
		Request: req,
		Mode:    req.GetRunMode(module),
		RepoRef: req.RepoRef(module),
	}
}

// PolicyEvalResult captures the outcome of an OPA policy evaluation for a run.
type PolicyEvalResult struct {
	Allowed    bool              `json:"allowed"`
	HardDenies []PolicyViolation `json:"hardDenies,omitempty"`
	SoftDenies []PolicyViolation `json:"softDenies,omitempty"`
	// Overridden records that a soft_deny gate was bypassed by an admin.
	Overridden bool `json:"overridden,omitempty"`
	// Warnings are advisory conftest warn-rule results. They never block the
	// run or affect Allowed; they are surfaced for operator visibility only.
	Warnings []PolicyViolation `json:"warnings,omitempty"`
}

// PolicyViolation is a single deny-set element from a policy tier, shaped
// like conftest's failure entries: the message plus metadata passthrough.
type PolicyViolation struct {
	Msg      string            `json:"msg"`
	Metadata map[string]string `json:"metadata,omitempty"` // element fields minus "msg", plus "query"
}

// Request represents terraform run request
type Request struct {
	RequestedAt *metav1.Time `json:"reqAt,omitempty"`
	Type        string       `json:"type,omitempty"`
	PR          *PullRequest `json:"pr,omitempty"`
	LockID      string       `json:"lockID,omitempty"`

	// PolicyOverride requests that the soft_deny policy gate be bypassed for this run (admin-only).
	PolicyOverride bool `json:"policyOverride,omitempty"`
	// OverrideReason is the admin-provided justification for a policy override.
	OverrideReason string `json:"overrideReason,omitempty"`
	// OverriddenBy is the identity of the admin who requested the override.
	OverriddenBy string `json:"overriddenBy,omitempty"`
	// OverriddenHash is the module commit this override applies to. The runner
	// verifies it matches the commit being run before honoring the override.
	OverriddenHash string `json:"overriddenHash,omitempty"`
}

type PullRequest struct {
	Number     int    `json:"num,omitempty"`
	HeadBranch string `json:"headBranch,omitempty"`
	CommentID  int    `json:"commentID,omitempty"`
}

func (req *Request) Validate(module *Module) error {
	if req.RequestedAt.IsZero() {
		return fmt.Errorf("'reqAt' is required and must be in the '2006-01-02T15:04:05Z' format")
	}

	switch req.Type {
	case ScheduledRun,
		PollingRun,
		ForcedPlan,
		ForcedApply,
		PRPlan:
	default:
		return fmt.Errorf("unknown Request type provided")
	}

	// PRPlan requests must carry PR info
	if req.Type == PRPlan && (req.PR == nil || req.PR.HeadBranch == "") {
		return fmt.Errorf("'pr' with 'headBranch' is required for %q request type", PRPlan)
	}

	// reject request if apply req is downgraded to plan only to avoid confusion
	if req.Type == ForcedApply && !req.IsApply(module) {
		return fmt.Errorf("Manual Apply rejected: Module.Spec.PlanOnly is true")
	}

	return nil
}

func (req *Request) GetRunMode(module *Module) RunMode {
	if req.IsApply(module) {
		return ModeApply
	}
	return ModePlanOnly
}

// IsApply determines the final run mode based on the trigger type
// and the module's safety/automation settings.
func (req *Request) IsApply(module *Module) bool {
	// If the module is locked to PlanOnly, all request should be plan only.
	// Even a 'ForcedApply' from the GUI should be downgraded to a Plan.
	if module.IsPlanOnly() {
		return false
	}

	// for scheduled and polling run respect module spec
	if req.Type == ScheduledRun || req.Type == PollingRun {
		return module.IsAutoApply()
	}

	// this is override triggered by user
	if req.Type == ForcedApply {
		return true
	}

	// these are plan only override requests
	if req.Type == PRPlan ||
		req.Type == ForcedPlan {
		return false
	}

	return false
}

// SkipStatusUpdate will return if run info/stats needs to be added to CRD
// and stored in etcd
func (req *Request) SkipStatusUpdate() bool {
	return req.Type == PRPlan
}

// RepoRef returns the revision of the repository for the module source code
// based on request type
func (req *Request) RepoRef(module *Module) string {
	// this is override triggered by user
	if req.Type == PRPlan {
		return req.PR.HeadBranch
	}

	return module.Spec.RepoRef
}
