package webserver

import (
	"testing"

	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
)

func Test_pendingOverride(t *testing.T) {
	overrideRequired := tfaplv1beta1.ModuleStatus{CurrentState: string(tfaplv1beta1.StatusOverrideRequired)}

	hardDeny := &tfaplv1beta1.PolicyEvalResult{
		HardDenies: []tfaplv1beta1.PolicyViolation{{Msg: "blocked"}},
	}
	softDeny := &tfaplv1beta1.PolicyEvalResult{
		SoftDenies: []tfaplv1beta1.PolicyViolation{{Msg: "warn"}},
	}
	softDenyOverridden := &tfaplv1beta1.PolicyEvalResult{
		SoftDenies: []tfaplv1beta1.PolicyViolation{{Msg: "warn"}},
		Overridden: true,
	}

	tests := []struct {
		name    string
		status  tfaplv1beta1.ModuleStatus
		lastRun *tfaplv1beta1.Run
		want    bool
	}{
		{name: "status demands override", status: overrideRequired, want: true},
		{name: "hard deny beats stale override status", status: overrideRequired, lastRun: &tfaplv1beta1.Run{PolicyResult: hardDeny}, want: false},
		{name: "hard deny without override status", lastRun: &tfaplv1beta1.Run{PolicyResult: hardDeny}, want: false},
		{name: "soft deny pending", lastRun: &tfaplv1beta1.Run{PolicyResult: softDeny}, want: true},
		{name: "soft deny already overridden", lastRun: &tfaplv1beta1.Run{PolicyResult: softDenyOverridden}, want: false},
		{name: "no last run", want: false},
		{name: "last run without policy result", lastRun: &tfaplv1beta1.Run{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pendingOverride(tt.status, tt.lastRun); got != tt.want {
				t.Errorf("pendingOverride() = %v, want %v", got, tt.want)
			}
		})
	}
}
