# Soft-deny: unapproved null_resource command execution.
#
# null_resources with local-exec provisioners run arbitrary shell commands on
# the applier runner. These are an anti-pattern that should require an admin
# override unless the resource is explicitly approved. The `hello-with-providers`
# module is the intentionally "requires override" one: its `exec_provider`
# resource runs an arbitrary user-supplied command (var.command) and is not in
# the allowlist, while `slow_provider` (which just sleeps) is approved.
#
# The allowlist comes from data/null_resources.json, a conftest --data JSON
# file. conftest merges the file's top-level keys into the data document, so
# `{"allowed_null_resources": [...]}` is reachable as data.allowed_null_resources.
# Reads input.resource_changes (tfjson shape: type, address, change.actions).
package main

import rego.v1

# Last segment of a resource address (the local name terraform uses to refer to
# the resource, e.g. null_resource.exec_provider -> "exec_provider").
last_segment(addr) := seg if {
	parts := split(addr, ".")
	count(parts) > 0
	seg := parts[count(parts) - 1]
}

deny contains {"rule": "null-resource-unapproved-exec", "msg": msg} if {
	rc := input.resource_changes[_]
	rc.type == "null_resource"
	# Ignore deletions.
	not "delete" in rc.change.actions
	name := last_segment(rc.address)
	# The allowlist is a direct reference (not object.get over the whole data
	# document, which would be recursive from within data.main.deny). conftest
	# fails at load time when the configured --data file is missing, so a
	# present data.allowed_null_resources is guaranteed here.
	allowed := data.allowed_null_resources
	not name in allowed
	msg := sprintf("null_resource %s runs an unapproved local-exec command; override required (approved: %v)", [rc.address, allowed])
}
