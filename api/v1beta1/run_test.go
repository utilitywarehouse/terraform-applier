package v1beta1_test

import (
	"testing"

	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func TestRequest_IsApply(t *testing.T) {
	tests := []struct {
		name          string
		requestType   string
		specPlanOnly  *bool
		specAutoApply *bool
		expected      bool
	}{
		{
			name:          "Global Lock: ForcedApply should be downgraded to Plan",
			requestType:   v1beta1.ForcedApply,
			specPlanOnly:  new(true),
			specAutoApply: new(true),
			expected:      false,
		}, {
			name:          "Global Lock: ScheduledRun should stay Plan",
			requestType:   v1beta1.ScheduledRun,
			specPlanOnly:  new(true),
			specAutoApply: new(true),
			expected:      false,
		}, {
			name:          "Auto-Apply Enabled: ScheduledRun should Apply (return false)",
			requestType:   v1beta1.ScheduledRun,
			specPlanOnly:  new(false),
			specAutoApply: new(true),
			expected:      true,
		}, {
			name:          "Auto-Apply Disabled: ScheduledRun should Plan",
			requestType:   v1beta1.ScheduledRun,
			specPlanOnly:  new(false),
			specAutoApply: new(false),
			expected:      false,
		}, {
			name:          "Auto-Apply Enabled: PollingRun should Apply (return false)",
			requestType:   v1beta1.PollingRun,
			specPlanOnly:  new(false),
			specAutoApply: new(true),
			expected:      true,
		}, {
			name:          "ForcedApply: Should Apply when no global lock exists",
			requestType:   v1beta1.ForcedApply,
			specPlanOnly:  new(false),
			specAutoApply: new(false),
			expected:      true,
		},
		{
			name:          "ForcedPlan: Should always be Plan",
			requestType:   v1beta1.ForcedPlan,
			specPlanOnly:  new(false),
			specAutoApply: new(true),
			expected:      false,
		}, {
			name:          "PR Plan: Should always be Plan regardless of AutoApply",
			requestType:   v1beta1.PRPlan,
			specPlanOnly:  new(false),
			specAutoApply: new(true),
			expected:      false,
		}, {
			name:          "Unknown Type: Should default to Plan",
			requestType:   "UnknownType",
			specPlanOnly:  new(false),
			specAutoApply: new(true),
			expected:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			module := &v1beta1.Module{
				Spec: v1beta1.ModuleSpec{
					PlanOnly:  tt.specPlanOnly,
					AutoApply: tt.specAutoApply,
				},
			}
			req := &v1beta1.Request{Type: tt.requestType}

			result := req.IsApply(module)

			if result != tt.expected {
				t.Errorf("IsPlanOnly() for %s: expected %v, got %v", tt.name, tt.expected, result)
			}
		})
	}
}
