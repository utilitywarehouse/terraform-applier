package prplanner

import (
	"fmt"
	reflect "reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

func Test_parsePlanReqMsg(t *testing.T) {
	type args struct {
		commentBody string
	}
	tests := []struct {
		name string
		args args
		want types.NamespacedName
	}{
		{
			name: "Namespace and Name",
			args: args{commentBody: "@terraform-applier plan foo/one"},
			want: types.NamespacedName{Namespace: "foo", Name: "one"},
		},
		{
			name: "Namespace and Name",
			args: args{commentBody: "@terraform-applier plan foo/two\n@terraform-applier plan foo/one"},
			want: types.NamespacedName{Namespace: "foo", Name: "two"},
		},
		{
			name: "Name only",
			args: args{commentBody: "@terraform-applier plan one"},
			want: types.NamespacedName{Name: "one"},
		},
		{
			name: "Empty string",
			args: args{commentBody: ""},
			want: types.NamespacedName{},
		},
		{
			name: "Multiple slashes",
			args: args{commentBody: "foo/one/extra"},
			want: types.NamespacedName{},
		},
		{
			name: "Leading slash",
			args: args{commentBody: "/one"},
			want: types.NamespacedName{},
		},
		{
			name: "Trailing slash",
			args: args{commentBody: "foo/"},
			want: types.NamespacedName{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePlanReqMsg(tt.args.commentBody); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePlanReqMsg() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseRequestAcknowledgedMsg(t *testing.T) {
	type args struct {
		commentBody string
	}
	tests := []struct {
		name       string
		args       args
		wantModule types.NamespacedName
		wantPath   string
		wantHash   string
		wantReqAt  *time.Time
	}{
		{
			name:       "Empty string",
			args:       args{commentBody: ""},
			wantModule: types.NamespacedName{},
			wantReqAt:  nil,
		},
		{
			name:       "NamespacedName + Requested At",
			args:       args{commentBody: requestAcknowledgedMsg("foo/one", "path/to/module/one", "hash1", mustParseMetaTime("2006-01-02T15:04:05+07:00"))},
			wantModule: types.NamespacedName{Namespace: "foo", Name: "one"},
			wantPath:   "path/to/module/one",
			wantHash:   "hash1",
			wantReqAt:  mustParseTime("2006-01-02T15:04:05+07:00"),
		},
		{
			name:       "NamespacedName + Requested At UTC",
			args:       args{commentBody: requestAcknowledgedMsg("foo/one", "foo/one", "hash2", mustParseMetaTime("2023-04-02T15:04:05Z"))},
			wantModule: types.NamespacedName{Namespace: "foo", Name: "one"},
			wantPath:   "foo/one",
			wantHash:   "hash2",
			wantReqAt:  mustParseTime("2023-04-02T15:04:05Z"),
		},
		{
			name:       "Name + Requested At",
			args:       args{commentBody: requestAcknowledgedMsg("one", "foo/one", "hash3", mustParseMetaTime("2023-04-02T15:04:05Z"))},
			wantModule: types.NamespacedName{Name: "one"},
			wantPath:   "foo/one",
			wantHash:   "hash3",
			wantReqAt:  mustParseTime("2023-04-02T15:04:05Z"),
		},
		{
			name:       "missing Requested At",
			args:       args{commentBody: fmt.Sprintf(requestAcknowledgedMsgTml, "foo/one", "foo/one", "")},
			wantModule: types.NamespacedName{},
			wantPath:   "",
			wantReqAt:  nil,
		},
		{
			name:       "Missing module",
			args:       args{commentBody: "Received terraform plan request. Module: `` Requested At: `2006-01-02T15:04:05+07:00`"},
			wantModule: types.NamespacedName{},
			wantPath:   "",
			wantReqAt:  nil,
		},
		{
			name:       "Terraform plan output for module",
			args:       args{commentBody: "Terraform plan output for module `foo/one`"},
			wantModule: types.NamespacedName{},
			wantPath:   "",
			wantReqAt:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModule, gotPath, gotHash, gotReqAt := parseRequestAcknowledgedMsg(tt.args.commentBody)
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("parseRequestAcknowledgedMsg() gotModule = %v, want %v", gotModule, tt.wantModule)
			}
			if diff := cmp.Diff(tt.wantPath, gotPath); diff != "" {
				t.Errorf("parseRequestAcknowledgedMsg() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantHash, gotHash); diff != "" {
				t.Errorf("parseRequestAcknowledgedMsg() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantReqAt, gotReqAt); diff != "" {
				t.Errorf("parseRequestAcknowledgedMsg() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_parseRunOutputMsg(t *testing.T) {
	type args struct {
		comment string
	}
	tests := []struct {
		name       string
		args       args
		wantModule types.NamespacedName
		wantCommit string
	}{
		{
			name:       "NamespaceName + Commit ID",
			args:       args{comment: runOutputMsg("foo/one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule: types.NamespacedName{Namespace: "foo", Name: "one"},
			wantCommit: "hash2",
		},
		{
			name:       "NamespaceName only",
			args:       args{comment: runOutputMsg("one", "foo/one", &v1beta1.Run{Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "missing name",
			args:       args{comment: runOutputMsg("", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Name + Commit ID",
			args:       args{comment: runOutputMsg("one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule: types.NamespacedName{Name: "one"},
			wantCommit: "hash2",
		},
		{
			name:       "@terraform-applier plan only",
			args:       args{comment: "@terraform-applier plan"},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Empty string",
			args:       args{comment: ""},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModule, gotCommit := parseRunOutputMsg(tt.args.comment)
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("parseRunOutputMsg() gotModule = %v, want %v", gotModule, tt.wantModule)
			}
			if gotCommit != tt.wantCommit {
				t.Errorf("parseRunOutputMsg() gotCommit = %v, want %v", gotCommit, tt.wantCommit)
			}
		})
	}
}

func Test_parseNamespaceName(t *testing.T) {
	type args struct {
		str string
	}
	tests := []struct {
		name string
		args args
		want types.NamespacedName
	}{
		{
			name: "NamespacedName",
			args: args{str: "foo/one"},
			want: types.NamespacedName{Namespace: "foo", Name: "one"},
		},
		{
			name: "Namespace only",
			args: args{str: "foo/"},
			want: types.NamespacedName{Namespace: "foo"},
		},
		{
			name: "Name only",
			args: args{str: "one"},
			want: types.NamespacedName{Name: "one"},
		},
		{
			name: "Empty string",
			args: args{str: ""},
			want: types.NamespacedName{},
		},
		{
			name: "Multiple slashes",
			args: args{str: "foo/one/extra"},
			want: types.NamespacedName{},
		},
		{
			name: "Leading slash",
			args: args{str: "/one"},
			want: types.NamespacedName{Name: "one"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseNamespaceName(tt.args.str); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseNamespaceName() = %v, want %v", got, tt.want)
			}
		})
	}
}
