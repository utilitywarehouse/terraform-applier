
resource "null_resource" "slow_provider" {
  provisioner "local-exec" {
    command = "echo progressing...;sleep ${var.sleep};echo done"
  }
}
