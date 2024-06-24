package prplanner

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func Test_findOutputRequestDataInComment(t *testing.T) {
	type args struct {
		commentBody string
	}
	tests := []struct {
		name       string
		args       args
		wantModule types.NamespacedName
		wantCommit string
	}{
		// requestAcknowledgedRegex = regexp.MustCompile("Received terraform plan request. Module: `(.+)` Request ID: `(.+)` Commit ID: `(.+)`")
		// TODO: Add test cases.
		{
			name:       "NamespaceNameName + Request ID + Commit ID",
			args:       args{commentBody: "Received terraform plan request. Module: `terraform/my-module` Request ID: `a1b2c3d4` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{Namespace: "terraform", Name: "my-module"},
			wantCommit: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModule, gotCommit := findOutputRequestDataInComment(tt.args.commentBody)
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("findOutputRequestDataInComment() got = %v, want %v", gotModule, tt.wantModule)
			}
			if gotCommit != tt.wantCommit {
				t.Errorf("findOutputRequestDataInComment() got1 = %v, want %v", gotCommit, tt.wantCommit)
			}
		})
	}
}
