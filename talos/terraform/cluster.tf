# ========================================
# Cluster Secrets
# ========================================

resource "talos_machine_secrets" "this" {}

# ========================================
# Machine Configuration
# ========================================

data "talos_machine_configuration" "controlplane" {
  for_each = var.nodes

  cluster_name     = var.cluster_name
  cluster_endpoint = var.cluster_endpoint
  machine_type     = "controlplane"
  machine_secrets  = talos_machine_secrets.this.machine_secrets

  talos_version      = var.talos_version
  kubernetes_version = var.kubernetes_version
}

# ========================================
# Apply Configuration
# ========================================

resource "talos_machine_configuration_apply" "node" {
  for_each = var.nodes

  client_configuration        = talos_machine_secrets.this.client_configuration
  machine_configuration_input = data.talos_machine_configuration.controlplane[each.key].machine_configuration
  node                        = each.value.ip
  endpoint                    = each.value.ip
  apply_mode                  = "reboot"

  config_patches = concat([
    yamlencode({
      machine = {
        network = {
          interfaces = [merge(
            {
              interface = var.network_interface
              addresses = ["${each.value.ip}/24"]
              mtu       = 1400
              routes = [{
                network = "0.0.0.0/0"
                gateway = var.gateway
              }]
            },
            var.cluster_vip != "" ? { vip = { ip = var.cluster_vip } } : {}
          )]
          nameservers = ["8.8.8.8", "8.8.4.4"]
        }

        time = {
          servers = var.ntp_servers
        }

        install = {
          disk  = each.value.disk
          image = "ghcr.io/siderolabs/installer:${var.talos_version}"
          extensions = [
            { image = "ghcr.io/siderolabs/iscsi-tools:${var.talos_version}" },
          ]
        }

        kubelet = {
          extraArgs = {
            rotate-server-certificates = "true"
          }
          nodeIP = {
            validSubnets = [var.node_subnet]
          }
        }

        kernel = {
          modules = [
            { name = "br_netfilter" },
            { name = "xt_socket" }
          ]
        }

        sysctls = {
          # Cilium/BPF requirements
          "net.core.bpf_jit_enable"             = "1"
          "net.ipv4.ip_forward"                 = "1"
          "net.ipv6.conf.all.forwarding"        = "1"
          "net.bridge.bridge-nf-call-iptables"  = "1"
          "net.bridge.bridge-nf-call-ip6tables" = "1"
          "kernel.unprivileged_bpf_disabled"    = "1"
          "net.core.netdev_max_backlog"         = "5000"

          # Filesystem optimization
          "fs.file-max"                   = "2097152"
          "fs.inotify.max_user_watches"   = "524288"
          "fs.inotify.max_user_instances" = "512"
          "fs.inotify.max_queued_events"  = "65536"
          "fs.aio-max-nr"                 = "1048576"

          # Memory management
          "vm.swappiness"                = "1"
          "vm.dirty_background_ratio"    = "5"
          "vm.dirty_ratio"               = "10"
          "vm.dirty_expire_centisecs"    = "1500"
          "vm.dirty_writeback_centisecs" = "500"
          "vm.overcommit_memory"         = "1"

          # Kernel panic behavior
          "kernel.panic"         = "10"
          "kernel.panic_on_oops" = "1"
          "vm.panic_on_oom"      = "0"
          # Note: kernel.hung_task_panic not available in Apple Virtualization VMs
        }

        features = {
          rbac = true
          kubePrism = {
            enabled = true
            port    = 7445
          }
        }
      }

      cluster = {
        network = {
          cni = {
            name = "none"
          }
          podSubnets     = [var.pod_cidr]
          serviceSubnets = [var.service_cidr]
          dnsDomain      = var.cluster_domain
        }

        proxy = {
          disabled = true
        }

        apiServer = {
          certSANs = concat(
            [var.cluster_vip, "api.${var.cluster_domain}"],
            [for n in var.nodes : n.ip],
            [for n in var.nodes : n.hostname]
          )
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
  ],
  # Add VolumeConfig for EPHEMERAL on separate disk (contains etcd data)
  var.etcd_disk != "" ? [yamlencode({
    apiVersion = "v1alpha1"
    kind       = "VolumeConfig"
    name       = "EPHEMERAL"
    provisioning = {
      diskSelector = {
        match = "!system_disk"
      }
    }
  })] : [])
}

# ========================================
# Bootstrap
# ========================================

resource "talos_machine_bootstrap" "this" {
  depends_on = [talos_machine_configuration_apply.node]

  client_configuration = talos_machine_secrets.this.client_configuration
  node                 = var.nodes["cp-01"].ip
}

# ========================================
# Client Configuration
# ========================================

data "talos_client_configuration" "this" {
  cluster_name         = var.cluster_name
  client_configuration = talos_machine_secrets.this.client_configuration
  nodes                = [for n in var.nodes : n.ip]
  endpoints            = var.cluster_vip != "" ? [var.cluster_vip] : [for n in var.nodes : n.ip]
}

# ========================================
# Kubeconfig
# ========================================

resource "talos_cluster_kubeconfig" "this" {
  depends_on = [talos_machine_bootstrap.this]

  client_configuration = talos_machine_secrets.this.client_configuration
  node                 = var.nodes["cp-01"].ip
}
