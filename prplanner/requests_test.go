package prplanner

import (
	"context"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/utilitywarehouse/git-mirror/pkg/mirror"
	tfaplv1beta1 "github.com/utilitywarehouse/terraform-applier/api/v1beta1"
	"github.com/utilitywarehouse/terraform-applier/git"
	"github.com/utilitywarehouse/terraform-applier/sysutil"
	"golang.org/x/vuln/client"
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
			args: args{commentBody: "@terraform-applier plan terraform/my-module"},
			want: types.NamespacedName{Namespace: "terraform", Name: "my-module"},
		},
		{
			name: "Name only",
			args: args{commentBody: "@terraform-applier plan my-module"},
			want: types.NamespacedName{Name: "my-module"},
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
			args: args{str: "terraform/my-module"},
			want: types.NamespacedName{Namespace: "terraform", Name: "my-module"},
		},
		{
			name: "Namespace only",
			args: args{str: "terraform/"},
			want: types.NamespacedName{Namespace: "terraform"},
		},
		{
			name: "Name only",
			args: args{str: "my-module"},
			want: types.NamespacedName{Name: "my-module"},
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
			args: args{str: "/my-module"},
			want: types.NamespacedName{Name: "my-module"},
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
						Body:       "Terraform plan output for module `terraform/my-module` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
				module:   types.NamespacedName{Namespace: "terraform", Name: "my-module"},
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
						Body:       "Terraform plan output for module `my-module` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
				module:   types.NamespacedName{Name: "my-module"},
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
						Body:       "Terraform plan output for module `terraform/my-module` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3aaaa",
				module:   types.NamespacedName{Namespace: "terraform", Name: "my-module"},
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
						Body:       "Terraform plan output for module `not-terraform/my-module` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
				module:   types.NamespacedName{Namespace: "terraform", Name: "my-module"},
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
						Body:       "Received terraform plan request. Module: `terraform/my-module` Request ID: `a1b2c3d4` Commit ID: `e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7`",
					},
				}}},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
				module:   types.NamespacedName{Namespace: "terraform", Name: "my-module"},
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
				module:   types.NamespacedName{Namespace: "terraform", Name: "my-module"},
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

func Test_addNewRequest(t *testing.T) {
	type fields struct {
		GitMirror   mirror.RepoPoolConfig
		ClusterClt  client.Client
		Repos       git.Repositories
		RedisClient sysutil.RedisInterface
		github      *gitHubClient
		Interval    time.Duration
		Log         *slog.Logger
	}
	type args struct {
		ctx      context.Context
		module   tfaplv1beta1.Module
		pr       *pr
		repo     *mirror.GitURL
		commitID string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *tfaplv1beta1.Request
		wantErr bool
	}{
		{
			name:   "test1",
			fields: fields{},
			args: args{
				ctx:      context.Background(),
				module:   tfaplv1beta1.Module{ObjectMeta: {Namespace: "terraform", Name: "my-module"}},
				pr:       &pr{},
				repo:     &mirror.GitURL{},
				commitID: "e3c7d4a60b8c9b4c9211a7b4e1a837e9e9c3b5f7",
			},
			want:    &tfaplv1beta1.Request{},
			wantErr: false,
		},
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Planner{
				GitMirror:   tt.fields.GitMirror,
				ClusterClt:  tt.fields.ClusterClt,
				Repos:       tt.fields.Repos,
				RedisClient: tt.fields.RedisClient,
				github:      tt.fields.github,
				Interval:    tt.fields.Interval,
				Log:         tt.fields.Log,
			}
			got, err := p.addNewRequest(tt.args.ctx, tt.args.module, tt.args.pr, tt.args.repo, tt.args.commitID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Planner.addNewRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Planner.addNewRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}
