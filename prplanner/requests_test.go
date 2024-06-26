package prplanner

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

func Test_getPostedRunOutputInfo(t *testing.T) {
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
			args:       args{comment: "Terraform plan output for module `terraform/my-module` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{Namespace: "terraform", Name: "my-module"},
			wantCommit: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
		},
		{
			name:       "NamespaceName only",
			args:       args{comment: "Terraform plan output for module `terraform/my-module` Commit ID: ``"},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Commit ID only",
			args:       args{comment: "Terraform plan output for module `` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{},
			wantCommit: "",
		},
		{
			name:       "Name + Commit ID",
			args:       args{comment: "Terraform plan output for module `my-module` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`"},
			wantModule: types.NamespacedName{Name: "my-module"},
			wantCommit: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
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
			gotModule, gotCommit := getPostedRunOutputInfo(tt.args.comment)
			if !reflect.DeepEqual(gotModule, tt.wantModule) {
				t.Errorf("getPostedRunOutputInfo() gotModule = %v, want %v", gotModule, tt.wantModule)
			}
			if gotCommit != tt.wantCommit {
				t.Errorf("getPostedRunOutputInfo() gotCommit = %v, want %v", gotCommit, tt.wantCommit)
			}
		})
	}
}

func Test_getRunRequestFromComment(t *testing.T) {
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
			if got := getRunRequestFromComment(tt.args.commentBody); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getRunRequestFromComment() = %v, want %v", got, tt.want)
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
						Body:       "Terraform plan output for module `foo/one` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
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
						Body:       "Terraform plan output for module `one` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
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
						Body:       "Terraform plan output for module `foo/one` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
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
						Body:       "Terraform plan output for module `bar/one` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
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
						Body:       "Received terraform plan request. Module: `foo/one` Request ID: `a1b2c3d4` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
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
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
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
