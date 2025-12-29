# ========================================
# Cluster Configuration
# ========================================

variable "cluster_name" {
  description = "Name of the Talos cluster"
  type        = string
  default     = "homelab"
}

variable "cluster_endpoint" {
  description = "VIP endpoint for Kubernetes API server"
  type        = string
  default     = "https://172.16.101.100:6443"
}

variable "cluster_vip" {
  description = "Shared VIP IP for control plane HA"
  type        = string
  default     = "172.16.101.100"
}

variable "cluster_domain" {
  description = "Kubernetes cluster domain"
  type        = string
  default     = "k8s.home.lex.la"
}

# ========================================
# Network Configuration
# ========================================

variable "gateway" {
  description = "Default gateway for nodes"
  type        = string
  default     = "172.16.101.254"
}

variable "nameservers" {
  description = "DNS servers for nodes"
  type        = list(string)
  default     = ["172.16.0.1"]
}

variable "pod_cidr" {
  description = "Pod network CIDR"
  type        = string
  default     = "10.42.0.0/16"
}

variable "service_cidr" {
  description = "Service network CIDR"
  type        = string
  default     = "10.43.0.0/16"
}

variable "node_subnet" {
  description = "Node network subnet for kubelet"
  type        = string
  default     = "172.16.101.0/24"
}

variable "network_interface" {
  description = "Network interface name (eth0 for RPi, enp0s1 for VMs)"
  type        = string
  default     = "eth0"
}

# ========================================
# Node Configuration
# ========================================

variable "nodes" {
  description = "Map of control plane nodes"
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

# ========================================
# Version Configuration
# ========================================

variable "talos_version" {
  description = "Talos Linux version"
  type        = string
  default     = "v1.12.0"
}

variable "kubernetes_version" {
  description = "Kubernetes version"
  type        = string
  default     = "1.35.0"
}

# ========================================
# Time Configuration
# ========================================

variable "ntp_servers" {
  description = "NTP servers for time sync"
  type        = list(string)
  default     = ["time.cloudflare.com"]
}

# ========================================
# Storage Configuration
# ========================================

variable "etcd_disk" {
  description = "Separate disk for etcd data (improves performance on slow boot disks)"
  type        = string
  default     = ""
}

variable "data_disk" {
  description = "Separate disk for ephemeral data (/var). If set, Talos will use this disk for containerd, kubelet, and other runtime data"
  type        = string
  default     = ""
}
