# ========================================
# Talos Outputs
# ========================================

output "talosconfig" {
  description = "Talos client configuration"
  value       = data.talos_client_configuration.this.talos_config
  sensitive   = true
}

# ========================================
# Kubernetes Outputs
# ========================================

output "kubeconfig" {
  description = "Kubernetes admin kubeconfig"
  value       = talos_cluster_kubeconfig.this.kubeconfig_raw
  sensitive   = true
}

# ========================================
# Cluster Information
# ========================================

output "cluster_endpoint" {
  description = "Kubernetes API endpoint"
  value       = var.cluster_endpoint
}

output "nodes" {
  description = "Node information map"
  value = {
    for k, v in var.nodes : k => {
      hostname = v.hostname
      ip       = v.ip
    }
  }
}
