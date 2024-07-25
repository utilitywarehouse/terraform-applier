package prplanner

import (
	"fmt"
	reflect "reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func Test_parsePlanReqMsg(t *testing.T) {
	type args struct {
		commentBody string
	}
	tests := []struct {
		name                 string
		args                 args
		wantNamespace        types.NamespacedName
		wantModuleNameOrPath string
	}{
		{
			name:                 "Correct path given",
			args:                 args{commentBody: "@terraform-applier plan foo/one"},
			wantModuleNameOrPath: "foo/one",
		},
		{
			name:                 "relative path with dot",
			args:                 args{commentBody: "@terraform-applier plan ./foo/one"},
			wantModuleNameOrPath: "./foo/one",
		},
		{
			name:                 "Request made twice",
			args:                 args{commentBody: "@terraform-applier plan foo/one\n@terraform-applier plan foo/baz"},
			wantModuleNameOrPath: "",
		},
		{
			name:                 "Name only",
			args:                 args{commentBody: "@terraform-applier plan one"},
			wantModuleNameOrPath: "one",
		},
		{
			name:                 "Empty string",
			args:                 args{commentBody: ""},
			wantModuleNameOrPath: "",
		},
		{
			name:                 "Too many slashes",
			args:                 args{commentBody: "@terraform-applier plan foo/one/baz"},
			wantModuleNameOrPath: "foo/one/baz",
		},
		{
			name:                 "dot as path",
			args:                 args{commentBody: "@terraform-applier plan ."},
			wantModuleNameOrPath: ".",
		},
		{
			name:                 "Trailing slash",
			args:                 args{commentBody: "@terraform-applier plan foo/"},
			wantModuleNameOrPath: "foo/",
		},
		{
			name:                 "do not trigger plan on module limit comment",
			args:                 args{commentBody: moduleLimitReachedTml},
			wantModuleNameOrPath: "",
		},
		{
			name:                 "do not trigger plan on our module request Acknowledged Msg",
			args:                 args{commentBody: requestAcknowledgedMsg("default", "foo/one", "path/to/module/one", "hash1", mustParseMetaTime("2006-01-02T15:04:05+07:00"))},
			wantModuleNameOrPath: "",
		},
		{
			name:                 "do not trigger plan on our module run Output Msg",
			args:                 args{commentBody: runOutputMsg("default", "foo/one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModuleNameOrPath: "",
		},
		{
			name:                 "with surrounding `",
			args:                 args{commentBody: "`@terraform-applier plan foo-baz/one`"},
			wantModuleNameOrPath: "foo-baz/one",
		},
		{
			name:                 "with ` surrounding only name",
			args:                 args{commentBody: "@terraform-applier plan `foo_bar/one`"},
			wantModuleNameOrPath: "foo_bar/one",
		},
		{
			name:                 "correct Name with a random suffix",
			args:                 args{commentBody: "@terraform-applier plan two please"},
			wantModuleNameOrPath: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModuleNameOrPath := parsePlanReqMsg(tt.args.commentBody)
			if !reflect.DeepEqual(gotModuleNameOrPath, tt.wantModuleNameOrPath) {
				t.Errorf("parsePlanReqMsg() = %v, wantModuleNameOrPath %v", gotModuleNameOrPath, tt.wantModuleNameOrPath)
			}
		})
	}
}

func Test_requestAcknowledgedMsg(t *testing.T) {
	type args struct {
		cluster  string
		module   string
		path     string
		commitID string
		reqAt    *metav1.Time
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"1",
			args{cluster: "default", module: "foo/one", path: "path/to/module/one", commitID: "hash1", reqAt: mustParseMetaTime("2006-01-02T15:04:05+07:00")},
			"Received terraform plan request\n" +
				"```\n" +
				"Cluster: default\n" +
				"Module: foo/one\n" +
				"Path: path/to/module/one\n" +
				"Commit ID: hash1\n" +
				"Requested At: 2006-01-02T15:04:05+07:00\n" +
				"```\n" +
				"Do not edit this comment. This message will be updated once the plan run is completed.\n" +
				"To manually trigger plan again please post `@terraform-applier plan path/to/module/one` as comment.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestAcknowledgedMsg(tt.args.cluster, tt.args.module, tt.args.path, tt.args.commitID, tt.args.reqAt)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("requestAcknowledgedMsg() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_parseRequestAcknowledgedMsg(t *testing.T) {
	type args struct {
		commentBody string
	}
	tests := []struct {
		name        string
		args        args
		wantCluster string
		wantModule  types.NamespacedName
		wantPath    string
		wantHash    string
		wantReqAt   *time.Time
	}{
		{
			name:       "Empty string",
			args:       args{commentBody: ""},
			wantModule: types.NamespacedName{},
			wantReqAt:  nil,
		},
		{
			name:        "NamespacedName + Requested At",
			args:        args{commentBody: requestAcknowledgedMsg("default", "foo/one", "path/to/module/one", "hash1", mustParseMetaTime("2006-01-02T15:04:05+07:00"))},
			wantModule:  types.NamespacedName{Namespace: "foo", Name: "one"},
			wantCluster: "default",
			wantPath:    "path/to/module/one",
			wantHash:    "hash1",
			wantReqAt:   mustParseTime("2006-01-02T15:04:05+07:00"),
		},
		{
			name:        "cluster env with spec char",
			args:        args{commentBody: requestAcknowledgedMsg("clusterEnv-with_", "foo/one", "path/to/module/one", "hash1", mustParseMetaTime("2006-01-02T15:04:05+07:00"))},
			wantModule:  types.NamespacedName{Namespace: "foo", Name: "one"},
			wantCluster: "clusterEnv-with_",
			wantPath:    "path/to/module/one",
			wantHash:    "hash1",
			wantReqAt:   mustParseTime("2006-01-02T15:04:05+07:00"),
		},
		{
			name:        "NamespacedName + Requested At UTC",
			args:        args{commentBody: requestAcknowledgedMsg("default", "foo/one", "foo/one", "hash2", mustParseMetaTime("2023-04-02T15:04:05Z"))},
			wantModule:  types.NamespacedName{Namespace: "foo", Name: "one"},
			wantCluster: "default",
			wantPath:    "foo/one",
			wantHash:    "hash2",
			wantReqAt:   mustParseTime("2023-04-02T15:04:05Z"),
		},
		{
			name:        "Name + Requested At",
			args:        args{commentBody: requestAcknowledgedMsg("default", "one", "foo/one", "hash3", mustParseMetaTime("2023-04-02T15:04:05Z"))},
			wantModule:  types.NamespacedName{Name: "one"},
			wantCluster: "default",
			wantPath:    "foo/one",
			wantHash:    "hash3",
			wantReqAt:   mustParseTime("2023-04-02T15:04:05Z"),
		},
		{
			name:       "missing Requested At",
			args:       args{commentBody: fmt.Sprintf(requestAcknowledgedMsgTml, "default", "foo/one", "foo/one", "")},
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
			gotCluster, gotModule, gotPath, gotHash, gotReqAt := parseRequestAcknowledgedMsg(tt.args.commentBody)
			if !reflect.DeepEqual(gotCluster, tt.wantCluster) {
				t.Errorf("parseRequestAcknowledgedMsg() gotCluster = %v, want %v", gotCluster, tt.wantModule)
			}
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

func Test_runOutputMsg(t *testing.T) {
	type args struct {
		cluster string
		module  string
		path    string
		run     *v1beta1.Run
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"1",
			args{cluster: "default", module: "baz/one", path: "path/baz/one", run: &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy.", Output: "Terraform apply output...."}},
			"Terraform plan output for\n" +
				"```\n" +
				"Cluster: default\n" +
				"Module: baz/one\n" +
				"Path: path/baz/one\n" +
				"Commit ID: hash2\n" +
				"```\n" +
				"<details><summary><b>Run Status: , Run Summary: Plan: x to add, x to change, x to destroy.</b></summary>\n\n" +
				"```" +
				"terraform\n" +
				"Terraform apply output....\n" +
				"```\n" +
				"</details>\n" +
				"To manually trigger plan again please post `@terraform-applier plan path/baz/one` as comment.",
		},
		{
			"2",
			args{cluster: "default", module: "baz/one", path: "path/baz/one", run: &v1beta1.Run{Status: v1beta1.StatusErrored, CommitHash: "hash2", Summary: "unable to plan module", InitOutput: "Some Init Output...", Output: "Some TF Output ....."}},
			"Terraform plan output for\n" +
				"```\n" +
				"Cluster: default\n" +
				"Module: baz/one\n" +
				"Path: path/baz/one\n" +
				"Commit ID: hash2\n" +
				"```\n" +
				"<details><summary><b>Run Status: Errored, Run Summary: unable to plan module</b></summary>\n\n" +
				"```" +
				"terraform\n" +
				"Some Init Output...\nSome TF Output .....\n" +
				"```\n" +
				"</details>\n" +
				"To manually trigger plan again please post `@terraform-applier plan path/baz/one` as comment.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runOutputMsg(tt.args.cluster, tt.args.module, tt.args.path, tt.args.run)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("runOutputMsg() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_parseRunOutputMsg(t *testing.T) {
	type args struct {
		comment string
	}
	tests := []struct {
		name        string
		args        args
		wantCluster string
		wantModule  types.NamespacedName
		wantPath    string
		wantCommit  string
	}{
		{
			name:        "NamespaceName + Commit ID",
			args:        args{comment: runOutputMsg("default", "baz/one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule:  types.NamespacedName{Namespace: "baz", Name: "one"},
			wantCluster: "default",
			wantPath:    "foo/one",
			wantCommit:  "hash2",
		},
		{
			name:        "cluster env with spec char",
			args:        args{comment: runOutputMsg("default_-", "baz/one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule:  types.NamespacedName{Namespace: "baz", Name: "one"},
			wantCluster: "default_-",
			wantPath:    "foo/one",
			wantCommit:  "hash2",
		},
		{
			name:       "Module Name only",
			args:       args{comment: runOutputMsg("default", "one", "foo/one", &v1beta1.Run{Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule: types.NamespacedName{},
			wantPath:   "",
			wantCommit: "",
		},
		{
			name:       "Module Name missing",
			args:       args{comment: runOutputMsg("default", "", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule: types.NamespacedName{},
			wantPath:   "",
			wantCommit: "",
		},
		{
			name:        "Module Name + Commit ID",
			args:        args{comment: runOutputMsg("default", "one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule:  types.NamespacedName{Name: "one"},
			wantCluster: "default",
			wantPath:    "foo/one",
			wantCommit:  "hash2",
		},
		{
			name:        "Path cluster only",
			args:        args{comment: runOutputMsg("default", "baz/one", "foo/", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule:  types.NamespacedName{Namespace: "baz", Name: "one"},
			wantCluster: "default",
			wantPath:    "foo/",
			wantCommit:  "hash2",
		},
		{
			name:        "Path Module Name only",
			args:        args{comment: runOutputMsg("default", "baz/one", "/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule:  types.NamespacedName{Namespace: "baz", Name: "one"},
			wantCluster: "default",
			wantPath:    "/one",
			wantCommit:  "hash2",
		},
		{
			name:        "Path one word only",
			args:        args{comment: runOutputMsg("default", "baz/one", "one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})},
			wantModule:  types.NamespacedName{Namespace: "baz", Name: "one"},
			wantCluster: "default",
			wantPath:    "one",
			wantCommit:  "hash2",
		},
		{
			name:       "@terraform-applier plan only",
			args:       args{comment: "@terraform-applier plan"},
			wantModule: types.NamespacedName{},
			wantPath:   "",
			wantCommit: "",
		},
		{
			name:       "Empty string",
			args:       args{comment: ""},
			wantModule: types.NamespacedName{},
			wantPath:   "",
			wantCommit: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCluster, gotModule, gotPath, gotCommit := parseRunOutputMsg(tt.args.comment)
			if !reflect.DeepEqual(gotCluster, tt.wantCluster) {
				t.Errorf("parseRunOutputMsg() gotCluster = %v, want %v", gotCluster, tt.wantCluster)
			}
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("parseRunOutputMsg() gotModule = %v, want %v", gotModule, tt.wantModule)
			}
			if gotPath != tt.wantPath {
				t.Errorf("parseRunOutputMsg() gotPath = %v, want %v", gotPath, tt.wantPath)
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

func Test_isModuleLimitReachedCommentPosted(t *testing.T) {
	type args struct {
		prComments []prComment
	}

	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "comment not posted",
			args: args{prComments: []prComment{{Body: "random comment"}, {Body: "another random comment"}}},
			want: false,
		},
		{
			name: "comment posted",
			args: args{prComments: []prComment{{Body: moduleLimitReachedTml}, {Body: "random comment"}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isModuleLimitReachedCommentPosted(tt.args.prComments); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseNamespaceName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_isSelfAddedComment(t *testing.T) {
	type args struct {
		comment string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{"empty", args{""}, false},
		{"moduleLimitReachedTml", args{moduleLimitReachedTml}, true},
		{"requestAcknowledgedMsg", args{requestAcknowledgedMsg("default", "foo/one", "path/to/module/one", "hash1", mustParseMetaTime("2006-01-02T15:04:05+07:00"))}, true},
		{"runOutputMsg", args{runOutputMsg("default", "one", "foo/one", &v1beta1.Run{CommitHash: "hash2", Summary: "Plan: x to add, x to change, x to destroy."})}, true},
		{"other", args{"other"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSelfComment(tt.args.comment); got != tt.want {
				t.Errorf("isSelfAddedComment() = %v, want %v", got, tt.want)
			}
		})
	}
}
