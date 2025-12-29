terraform {
  required_version = "1.14.3"

  # Optional: Terraform Cloud
  # cloud {
  #   organization = "lexfrei"
  #   workspaces {
  #     name = "talos-homelab"
  #   }
  # }

  required_providers {
    talos = {
      source  = "siderolabs/talos"
      version = "0.10.0"
    }
  }
}

provider "talos" {}
