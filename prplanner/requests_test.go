package prplanner

import (
	"testing"

	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

func Test_isPlanOutputPostedForCommit(t *testing.T) {
	type args struct {
		pr       *pr
		commitID string
		module   types.NamespacedName
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// func isPlanOutputPostedForCommit(pr *pr, commitID string, module types.NamespacedName) bool {
		{
			name: "Matching NamespacedName and Commit ID",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       runOutputMsg("foo/one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."}),
					},
				}}},
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: true,
		},
		{
			name: "Matching Name and Commit ID",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       runOutputMsg("one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."}),
					},
				}}},
				commitID: "hash2",
				module:   types.NamespacedName{Name: "one"},
			},
			want: true,
		},
		{
			name: "Wrong Commit ID",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "Terraform plan output for module `foo/one` Commit ID: `hash2`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3aaaa",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
		{
			name: "Wrong Namespace",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "Terraform plan output for module `bar/one` Commit ID: `hash2`",
					},
				}}},
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
		{
			name: "Received terraform plan request",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "Received terraform plan request. Module: `foo/one` Request ID: `a1b2c3d4` Commit ID: `hash2`",
					},
				}}},
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
		{
			name: "Empty string",
			args: args{
				pr: &pr{Comments: struct {
					Nodes []prComment `json:"nodes"`
				}{Nodes: []prComment{
					{
						DatabaseID: 01234567,
						Body:       "",
					},
				}}},
				commitID: "hash2",
				module:   types.NamespacedName{Namespace: "foo", Name: "one"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlanOutputPostedForCommit(tt.args.pr, tt.args.commitID, tt.args.module); got != tt.want {
				t.Errorf("isPlanOutputPostedForCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}
