package v1beta1

import (
	"slices"
	"strings"
)

const RoleAdmin = "Admin"

func CanForceRun(email string, groups []string, module *Module) bool {
	// remove empty values
	groups = slices.DeleteFunc(groups, func(g string) bool {
		return strings.TrimSpace(g) == ""
	})

	for _, rbac := range module.Spec.RBAC {
		// only module "Admins" are allowed to do force run
		if rbac.Role == RoleAdmin {
			for _, subject := range rbac.Subjects {
				// check if email matching and its not a empty value
				if subject.Kind == "User" &&
					subject.Name == email &&
					strings.TrimSpace(email) != "" {
					return true
				}
				if subject.Kind == "Group" &&
					slices.Contains(groups, subject.Name) {
					return true
				}
			}
		}
	}

	return false
}
