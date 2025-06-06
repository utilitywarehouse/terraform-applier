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

// Run represents a complete run result of the terraform run
type Run struct {
	Module  types.NamespacedName `json:"module,omitempty"`
	Request *Request             `json:"request,omitempty"`

	Status       state         `json:"status,omitempty"` // 'Running','Success','Error'
	StartedAt    *metav1.Time  `json:"startedAT,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
	PlanOnly     bool          `json:"planOnly,omitempty"`
	RepoRef      string        `json:"repoRef,omitempty"`
	CommitHash   string        `json:"commitHash,omitempty"`
	CommitMsg    string        `json:"commitMsg,omitempty"`
	DiffDetected bool          `json:"diffDetected,omitempty"`
	InitOutput   string        `json:"initOutput,omitempty"`
	Output       string        `json:"output,omitempty"`
	Summary      string        `json:"summary,omitempty"`
}

func NewRun(module *Module, req *Request) Run {
	run := Run{
		Module: types.NamespacedName{
			Namespace: module.Namespace,
			Name:      module.Name,
		},
		Request: req,
	}

	run.PlanOnly = req.IsPlanOnly(module)
	run.RepoRef = req.RepoRef(module)
	return run
}

// Request represents terraform run request
type Request struct {
	RequestedAt *metav1.Time `json:"reqAt,omitempty"`
	Type        string       `json:"type,omitempty"`
	PR          *PullRequest `json:"pr,omitempty"`
	LockID      string       `json:"lockID,omitempty"`
}

type PullRequest struct {
	Number     int    `json:"num,omitempty"`
	HeadBranch string `json:"headBranch,omitempty"`
	CommentID  int    `json:"commentID,omitempty"`
}

func (req *Request) Validate() error {
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

	return nil
}

// IsPlanOnly will return is req is plan-only
func (req *Request) IsPlanOnly(module *Module) bool {
	// for scheduled and polling run respect module spec
	if req.Type == ScheduledRun || req.Type == PollingRun {
		return module.IsPlanOnly()
	}

	// this is override triggered by user
	if req.Type == ForcedApply {
		return false
	}

	// these are plan only override requests
	if req.Type == PRPlan ||
		req.Type == ForcedPlan {
		return true
	}

	// its always safe to default to plan-only
	return true
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
