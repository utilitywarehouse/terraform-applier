# Policy-clean module used by the runner policy integration tests.
#
# Every resource here is allowlisted in src/policies/data/null_resources.json
# and carries no credential-leak marker, so a plan of this module passes both
# the hard and soft policy tiers without any override.
resource "null_resource" "echo_hw" {
  provisioner "local-exec" {
    command = "echo 'Hello World'"
  }
}

resource "null_resource" "echo_v1" {
  provisioner "local-exec" {
    command = "echo ${var.variable1}"
  }

  depends_on = [
    null_resource.echo_hw
  ]
}
