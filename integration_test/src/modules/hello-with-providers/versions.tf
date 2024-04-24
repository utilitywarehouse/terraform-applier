
terraform {
  backend "local" {}
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
    google = {
      source = "hashicorp/google"
    }
    google-beta = {
      source = "hashicorp/google-beta"
    }
    okta = {
      source = "okta/okta"
    }
  }
}
