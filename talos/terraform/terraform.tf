terraform {
  required_version = ">= 1.0"

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
      version = "0.7.0"
    }
  }
}

provider "talos" {}
