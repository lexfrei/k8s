terraform {
  required_version = ">= 1.0"

  required_providers {
    talos = {
      source  = "siderolabs/talos"
      version = "~> 0.7"
    }
  }

  # Optional: remote state
  # backend "s3" {
  #   bucket = "terraform-state"
  #   key    = "talos/homelab.tfstate"
  # }
}

provider "talos" {}

# ============================================================================
# Variables
# ============================================================================

variable "cluster_name" {
  type    = string
  default = "homelab"
}

variable "cluster_endpoint" {
  type        = string
  default     = "https://172.16.101.100:6443"
  description = "VIP endpoint for API server"
}

variable "cluster_vip" {
  type    = string
  default = "172.16.101.100"
}

variable "gateway" {
  type    = string
  default = "172.16.101.254"
}

variable "nodes" {
  type = map(object({
    ip       = string
    hostname = string
    disk     = string
  }))
  default = {
    "cp-01" = {
      ip       = "172.16.101.1"
      hostname = "k8s-cp-01"
      disk     = "/dev/mmcblk0"
    }
    "cp-02" = {
      ip       = "172.16.101.2"
      hostname = "k8s-cp-02"
      disk     = "/dev/mmcblk0"
    }
    "cp-03" = {
      ip       = "172.16.101.3"
      hostname = "k8s-cp-03"
      disk     = "/dev/mmcblk0"
    }
  }
}

variable "talos_version" {
  type    = string
  default = "v1.9.0"
}

variable "kubernetes_version" {
  type    = string
  default = "1.32.0"
}

# ============================================================================
# Secrets (generated once, stored in state)
# ============================================================================

resource "talos_machine_secrets" "this" {}

# ============================================================================
# Machine Configuration
# ============================================================================

data "talos_machine_configuration" "controlplane" {
  for_each = var.nodes

  cluster_name     = var.cluster_name
  cluster_endpoint = var.cluster_endpoint
  machine_type     = "controlplane"
  machine_secrets  = talos_machine_secrets.this.machine_secrets

  talos_version      = var.talos_version
  kubernetes_version = var.kubernetes_version
}

# ============================================================================
# Configuration Patches
# ============================================================================

data "talos_machine_configuration_patches" "node" {
  for_each = var.nodes

  patches = [
    # Common patches for all nodes
    yamlencode({
      machine = {
        network = {
          hostname = each.value.hostname
          interfaces = [{
            interface = "eth0"
            addresses = ["${each.value.ip}/24"]
            routes = [{
              network = "0.0.0.0/0"
              gateway = var.gateway
            }]
            vip = {
              ip = var.cluster_vip
            }
          }]
          nameservers = ["1.1.1.1", "8.8.8.8"]
        }

        time = {
          servers = ["time.cloudflare.com"]
        }

        install = {
          disk  = each.value.disk
          image = "ghcr.io/siderolabs/installer:${var.talos_version}"
        }

        kubelet = {
          extraArgs = {
            rotate-server-certificates = "true"
          }
          nodeIP = {
            validSubnets = ["172.16.101.0/24"]
          }
        }

        kernel = {
          modules = [
            { name = "br_netfilter" },
            { name = "xt_socket" }
          ]
        }

        sysctls = {
          "net.core.bpf_jit_enable"           = "1"
          "net.ipv4.ip_forward"               = "1"
          "net.ipv6.conf.all.forwarding"      = "1"
          "net.bridge.bridge-nf-call-iptables"  = "1"
          "net.bridge.bridge-nf-call-ip6tables" = "1"
          "kernel.unprivileged_bpf_disabled"  = "1"
          "fs.file-max"                       = "2097152"
          "fs.inotify.max_user_watches"       = "524288"
          "fs.inotify.max_user_instances"     = "8192"
        }

        features = {
          rbac           = true
          stableHostname = true
          kubePrism = {
            enabled = true
            port    = 7445
          }
        }
      }

      cluster = {
        network = {
          cni = {
            name = "none" # We install Cilium ourselves
          }
          podSubnets     = ["10.42.0.0/16"]
          serviceSubnets = ["10.43.0.0/16"]
          dnsDomain      = "k8s.home.lex.la"
        }

        proxy = {
          disabled = true # Cilium replaces kube-proxy
        }

        apiServer = {
          certSANs = [
            var.cluster_vip,
            "172.16.101.1",
            "172.16.101.2",
            "172.16.101.3",
            "k8s-cp-01",
            "k8s-cp-02",
            "k8s-cp-03",
            "api.k8s.home.lex.la"
          ]
        }

        controllerManager = {
          extraArgs = {
            bind-address = "0.0.0.0"
          }
        }

        scheduler = {
          extraArgs = {
            bind-address = "0.0.0.0"
          }
        }

        etcd = {
          extraArgs = {
            listen-metrics-urls       = "http://0.0.0.0:2381"
            quota-backend-bytes       = "536870912"
            auto-compaction-mode      = "periodic"
            auto-compaction-retention = "1h"
            snapshot-count            = "5000"
          }
        }

        coreDNS = {
          disabled = false
        }

        allowSchedulingOnControlPlanes = true
      }
    })
  ]
}

# ============================================================================
# Apply Configuration
# ============================================================================

resource "talos_machine_configuration_apply" "node" {
  for_each = var.nodes

  client_configuration        = talos_machine_secrets.this.client_configuration
  machine_configuration_input = data.talos_machine_configuration.controlplane[each.key].machine_configuration
  config_patches              = data.talos_machine_configuration_patches.node[each.key].patches
  node                        = each.value.ip

  # Don't apply until node is reachable
  # lifecycle {
  #   precondition {
  #     condition     = can(http_get("http://${each.value.ip}:50000/healthz"))
  #     error_message = "Node ${each.value.hostname} is not reachable"
  #   }
  # }
}

# ============================================================================
# Bootstrap (only first node)
# ============================================================================

resource "talos_machine_bootstrap" "this" {
  depends_on = [talos_machine_configuration_apply.node]

  client_configuration = talos_machine_secrets.this.client_configuration
  node                 = var.nodes["cp-01"].ip
}

# ============================================================================
# Outputs
# ============================================================================

output "talosconfig" {
  value     = talos_machine_secrets.this.talos_config
  sensitive = true
}

data "talos_cluster_kubeconfig" "this" {
  depends_on = [talos_machine_bootstrap.this]

  client_configuration = talos_machine_secrets.this.client_configuration
  node                 = var.nodes["cp-01"].ip
}

output "kubeconfig" {
  value     = data.talos_cluster_kubeconfig.this.kubeconfig_raw
  sensitive = true
}

# Helper outputs
output "cluster_endpoint" {
  value = var.cluster_endpoint
}

output "nodes" {
  value = {
    for k, v in var.nodes : k => {
      hostname = v.hostname
      ip       = v.ip
    }
  }
}
