# Hard-deny: null_resource that echoes cloud credentials.
#
# The integration-test modules are all-null_resource workloads. The `hello`
# module is the intentionally "bad" one: it wires up null_resources that echo
# cloud credentials into their local-exec commands (`echo $AWS_ACCESS_KEY_ID`
# via `echo_AWS_KEY`, and a token-verification call via `verify_gcp_token`).
# Printing cloud credentials to a local-exec log is a credential-leak
# anti-pattern, so any plan that creates/updates such a resource is an
# unconditional, never-bypassable violation.
#
# The local-exec `command` itself is not present in the tfjson plan `after`
# state (only `triggers` is), so this rule keys off the resource's local name —
# which the modules deliberately make descriptive. Reads input.resource_changes
# (tfjson shape: type, address, change.actions).
package main

import rego.v1

# Credential-related markers found in the integration modules' resource names
# (matched case-insensitively, e.g. echo_AWS_KEY and verify_gcp_token).
credential_markers := ["aws_key", "gcp_token"]

# Last segment of a resource address (the local name terraform uses to refer to
# the resource, e.g. null_resource.echo_AWS_KEY -> "echo_AWS_KEY").
last_segment(addr) := seg if {
	parts := split(addr, ".")
	count(parts) > 0
	seg := parts[count(parts) - 1]
}

deny contains {"rule": "null-resource-credential-echo", "msg": msg} if {
	rc := input.resource_changes[_]
	rc.type == "null_resource"
	# Ignore deletions: a destroyed resource echoes nothing.
	not "delete" in rc.change.actions
	name := last_segment(rc.address)
	contains(lower(name), credential_markers[_])
	msg := sprintf("null_resource %s echoes cloud credentials into a local-exec command; credential leak risk", [rc.address])
}
