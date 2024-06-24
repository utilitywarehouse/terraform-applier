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
		// TODO: Add test cases.
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
			args: args{commentBody: "@terraform-applier plan utilitywarehouse/terraform"},
			want: types.NamespacedName{Namespace: "utilitywarehouse", Name: "terraform"},
		},
		{
			name: "Name only",
			args: args{commentBody: "@terraform-applier plan terraform"},
			want: types.NamespacedName{Name: "terraform"},
		},
		{
			name: "Empty string",
			args: args{commentBody: ""},
			want: types.NamespacedName{},
		},
		{
			name: "Multiple slashes",
			args: args{commentBody: "namespace/name/extra"},
			want: types.NamespacedName{},
		},
		{
			name: "Leading slash",
			args: args{commentBody: "/name"},
			want: types.NamespacedName{},
		},
		{
			name: "Trailing slash",
			args: args{commentBody: "namespace/"},
			want: types.NamespacedName{Namespace: "namespace"},
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
			name: "Namespace and Name",
			args: args{str: "utilitywarehouse/terraform"},
			want: types.NamespacedName{Namespace: "utilitywarehouse", Name: "terraform"},
		},
		{
			name: "Name only",
			args: args{str: "terraform"},
			want: types.NamespacedName{Name: "terraform"},
		},
		{
			name: "Empty string",
			args: args{str: ""},
			want: types.NamespacedName{},
		},
		{
			name: "Multiple slashes",
			args: args{str: "namespace/name/extra"},
			want: types.NamespacedName{},
		},
		{
			name: "Leading slash",
			args: args{str: "/name"},
			want: types.NamespacedName{},
		},
		{
			name: "Trailing slash",
			args: args{str: "namespace/"},
			want: types.NamespacedName{Namespace: "namespace"},
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
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPlanOutputPostedForCommit(tt.args.pr, tt.args.commitID, tt.args.module); got != tt.want {
				t.Errorf("isPlanOutputPostedForCommit() = %v, want %v", got, tt.want)
			}
		})
	}
}
