# Soft-deny: large-scale resource destruction.
#
# Fires when a single plan destroys more than `max_destroy` resources. Large
# batch deletions are almost always unintentional (a refactor that drops a
# whole subtree, a wrong path, an accidental `terraform destroy`), so they
# require an admin override even though they are not a per-resource violation.
#
# Reads the tfjson plan shape: input.resource_changes[_] with type, address,
# change.actions.
package main

import rego.v1

# Destructive mass-deletion threshold.
max_destroy := 3

deny contains {"rule": "large-scale-destruction", "msg": msg} if {
	destroyed := [addr | rc := input.resource_changes[_]; "delete" in rc.change.actions; addr := rc.address]
	count(destroyed) > max_destroy
	msg := sprintf("plan destroys %d resources, exceeding the threshold of %d; likely unintended, override required", [count(destroyed), max_destroy])
}
