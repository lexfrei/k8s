# Kyverno Policy Exceptions

This directory contains PolicyException resources that exclude system infrastructure components from Pod Security Standards baseline policies.

## Purpose

These exceptions allow legitimate system components to use elevated privileges required for their operation while maintaining security policy enforcement for user workloads.

## Policy Exceptions

### kube-system-infrastructure

**Location:** `kube-system.yaml`

**Applies to:**
- Cilium CNI (DaemonSet, Deployment, Pods)
- kube-vip control plane HA (DaemonSet, Pods)
- CSI NFS driver (DaemonSet, Deployment, Pods)

**Exempted Policies:**
- `disallow-capabilities`: Network and storage drivers require additional Linux capabilities
- `disallow-host-namespaces`: CNI requires host network access for node networking
- `disallow-host-path`: Access to host filesystem for CNI configuration and storage mounting
- `disallow-privileged-containers`: Init containers need privileged mode for system setup
- `disallow-selinux`: SELinux types required for container runtime integration
- `restrict-seccomp`: Custom seccomp profiles for network packet processing
- `disallow-host-ports`: Direct port binding for load balancer functionality

**Justification:**
- Cilium CNI manages cluster networking and requires deep system integration
- kube-vip provides control plane HA via VIP management
- CSI drivers require host access for volume mounting and management

---

### longhorn-system-storage

**Location:** `longhorn-system.yaml`

**Applies to:**
- Longhorn manager (DaemonSet, Pods)
- Longhorn CSI plugin (DaemonSet, Pods)
- Engine images (DaemonSet, Pods)
- Instance managers (Pods)
- CSI controllers (Deployments, Pods)

**Exempted Policies:**
- `disallow-capabilities`: Storage management requires CAP_SYS_ADMIN and CAP_IPC_LOCK
- `disallow-host-path`: Direct access to host storage devices and mount points
- `disallow-privileged-containers`: Block device management requires privileged access

**Justification:**
- Longhorn provides distributed block storage for the cluster
- Requires privileged access to manage block devices, filesystems, and iSCSI
- Engine instances run on host network for direct storage access

---

### monitoring-observability

**Location:** `monitoring.yaml`

**Applies to:**
- fluent-bit log collector (DaemonSet, Pods)
- node-exporter metrics exporter (DaemonSet, Pods)

**Exempted Policies:**
- `disallow-host-namespaces`: Access to host PID/IPC namespaces for system metrics
- `disallow-host-path`: Read access to host logs and system metrics
- `disallow-privileged-containers`: System-level observability requires elevated access
- `disallow-host-ports`: node-exporter exposes metrics on host port

**Justification:**
- fluent-bit collects container logs from host filesystem
- node-exporter reads system metrics from /proc and /sys
- Both require host-level access for complete observability

---

### system-upgrade-controller

**Location:** `system-upgrade.yaml`

**Applies to:**
- system-upgrade-controller (Deployment, Pods)

**Exempted Policies:**
- `disallow-host-path`: Access to host filesystem for system upgrades

**Justification:**
- Manages Kubernetes node OS upgrades
- Requires access to host system for upgrade execution

---

## Exception Namespace

All PolicyException resources are created in the `security` namespace as configured in Kyverno:

```yaml
features:
  policyExceptions:
    enabled: true
    namespace: security
```

## Scope

PolicyExceptions apply to:
- **Kinds:** Pod, DaemonSet, Deployment
- **Namespaces:** Specific system namespaces only (kube-system, longhorn-system, monitoring, system-upgrade)
- **Name patterns:** Wildcards for component families (e.g., `cilium*`, `longhorn-*`)

ReplicaSets are excluded from background scanning via `resourceFilters` to reduce audit log noise.

## Maintenance

When adding new system components:

1. Identify required privileged access
2. Create PolicyException in appropriate namespace
3. Use specific name patterns, avoid wildcards where possible
4. Document justification in this README
5. Apply via ArgoCD: exceptions are auto-synced from this directory

## Monitoring

PolicyException effectiveness is monitored via:
- PolicyReports in each namespace (check `kubectl get policyreport -A`)
- Kyverno metrics exported to Prometheus
- ServiceMonitors for Grafana dashboards

Target: 0 Pod-level policy violations for excepted resources.
