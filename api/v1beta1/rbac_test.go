package v1beta1

import "testing"

func TestCanForceRun(t *testing.T) {
	type args struct {
		email  string
		groups []string
		module *Module
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"no rbac",
			args{
				"u1", []string{"group1", "group2"},
				&Module{Spec: ModuleSpec{RepoURL: "test"}},
			},
			false,
		}, {
			"no Admin role",
			args{
				"u1", []string{"group1", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{Role: "View", Subjects: []Subject{{Kind: "User", Name: "u1"}}}},
					}},
			},
			false,
		}, {
			"email or group doesn't match",
			args{
				"u1", []string{"group1", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "group3"},
							}}}}},
			},
			false,
		}, {
			"email match",
			args{
				"u1", []string{"group1", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "u1"},
								{Kind: "Group", Name: "group3"},
							}}}}},
			},
			true,
		}, {
			"email match",
			args{
				"u1", []string{"group1", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "group3"},
								{Kind: "User", Name: "u1"},
							}}}}},
			},
			true,
		}, {
			"empty email match",
			args{
				"  ", []string{"group1", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "  "},
								{Kind: "Group", Name: "group3"},
								{Kind: "User", Name: "u1"},
							}}}}},
			},
			false,
		}, {
			"group match",
			args{
				"u1", []string{"group1", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "group2"},
							}}}}},
			},
			true,
		}, {
			"empty group match",
			args{
				"u1", []string{"  ", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "  "},
							}}}}},
			},
			false,
		}, {
			"with multiple roles",
			args{
				"u1", []string{"group1", "group2"},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "View",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "group2"},
							}}, {
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "group2"},
							}},
						}}},
			},
			true,
		}, {
			"no_user_groups",
			args{
				"u1", []string{},
				&Module{
					Spec: ModuleSpec{
						RBAC: []RBAC{{
							Role: "View",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "group2"},
							}}, {
							Role: "Admin",
							Subjects: []Subject{
								{Kind: "User", Name: "u3"},
								{Kind: "Group", Name: "group2"},
							}},
						}}},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanForceRun(tt.args.email, tt.args.groups, tt.args.module); got != tt.want {
				t.Errorf("CanForceRun() = %v, want %v", got, tt.want)
			}
		})
	}
}
