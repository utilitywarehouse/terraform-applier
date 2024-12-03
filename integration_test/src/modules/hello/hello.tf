module "age_secret" {
  source = "./age-sec"
}

output "top_secret_age" {
  value = module.age_secret.top_secret_age
}

resource "null_resource" "echo_hw" {
  provisioner "local-exec" {
    command = "echo 'Hello World'"
  }
}

resource "null_resource" "echo_v1" {
  provisioner "local-exec" {
    command = "echo ${var.variable1}"
  }

  # just to print value in seq
  depends_on = [
    null_resource.echo_hw
  ]
}

resource "null_resource" "echo_v2" {
  provisioner "local-exec" {
    command = "echo ${var.variable2}"
  }

  depends_on = [
    null_resource.echo_v1
  ]
}

resource "null_resource" "echo_v3" {
  provisioner "local-exec" {
    command = "echo ${var.variable3}"
  }

  depends_on = [
    null_resource.echo_v2
  ]
}


resource "null_resource" "echo_env1" {
  provisioner "local-exec" {
    command = "echo $TF_ENV_1"
  }

  depends_on = [
    null_resource.echo_v3
  ]
}

resource "null_resource" "echo_env2" {
  provisioner "local-exec" {
    command = "echo $TF_ENV_2"
  }

  depends_on = [
    null_resource.echo_env1
  ]
}

resource "null_resource" "echo_env3" {
  provisioner "local-exec" {
    command = "echo $TF_ENV_3"
  }

  depends_on = [
    null_resource.echo_env2
  ]
}


resource "null_resource" "echo_AWS_KEY" {
  provisioner "local-exec" {
    command = "echo $AWS_ACCESS_KEY_ID"
  }

  depends_on = [
    null_resource.echo_env3
  ]
}

data "google_client_openid_userinfo" "sa" {}

output "google_sa_email" {
  value = data.google_client_openid_userinfo.sa.email
}

resource "null_resource" "slow_provider" {
  provisioner "local-exec" {
    command = "echo progressing...;sleep ${var.sleep};echo done"
  }

  depends_on = [
    null_resource.echo_env3
  ]
}
