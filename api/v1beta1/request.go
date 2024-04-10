package v1beta1

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Request represents terraform run request
type Request struct {
	types.NamespacedName
	ID          string       `json:"id,omitempty"`
	RequestedAt *metav1.Time `json:"reqAt,omitempty"`
	Type        string       `json:"type,omitempty"`
	PR          *PullRequest `json:"pr,omitempty"`
}

type PullRequest struct {
	Number     int    `json:"num,omitempty"`
	HeadBranch string `json:"headBranch,omitempty"`
	CommentID  string `json:"commentID,omitempty"`
}

func (req *Request) Validate() error {
	if req.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.RequestedAt.IsZero() {
		return fmt.Errorf("valid timestamp is required for 'RequestedAt'")
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
	// for scheduled and polling run we respect module spec
	if req.Type == ScheduledRun ||
		req.Type == PollingRun {
		return module.IsPlanOnly()
	}

	// this is override triggered by user
	if req.Type == ForcedApply {
		return false
	}

	// its always safe to default to plan-only
	return true
}

// GetRepoRef returns the revision of the repository for the module source code
// based on request type
func (req *Request) GetRepoRef(module *Module) string {
	// this is override triggered by user
	if req.Type == PRPlan {
		return req.PR.HeadBranch
	}

	return module.Spec.RepoRef
}

// NewRequestID generates random string as ID
func NewRequestID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
