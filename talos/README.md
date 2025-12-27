# Talos Configuration for Homelab

## Status

⚠️ **NOT READY** - Talos does not have stable RPi5 support yet.

These configs are prepared for future migration when RPi5 support is available.

## Key Decisions

| Component | Choice | Reason |
|-----------|--------|--------|
| CNI | `none` → Cilium | Full control, kube-proxy replacement |
| kube-proxy | `disabled` | Cilium handles all |
| CoreDNS | Talos-managed | Simpler, can switch to own later |
| Cluster domain | `k8s.home.lex.la` | Match current K3s |
| Pod CIDR | `10.42.0.0/16` | Match current K3s |
| Service CIDR | `10.43.0.0/16` | Match current K3s |

## Files

- `controlplane.yaml.example` - Control plane node config
- `worker.yaml.example` - Worker node config template

## Cilium Compatibility

Current `values/cilium.yaml` needs these changes for Talos:

```yaml
# K3s specific (REMOVE for Talos)
# k8sServiceHost: 172.16.101.1  # Not needed, Talos uses KubePrism
# k8sServicePort: 6443

# Talos specific (ADD)
securityContext:
  capabilities:
    ciliumAgent:
      - CHOWN
      - KILL
      - NET_ADMIN
      - NET_RAW
      - IPC_LOCK
      - SYS_ADMIN
      - SYS_RESOURCE
      - DAC_OVERRIDE
      - FOWNER
      - SETGID
      - SETUID
    cleanCiliumState:
      - NET_ADMIN
      - SYS_ADMIN
      - SYS_RESOURCE

cgroup:
  autoMount:
    enabled: false
  hostRoot: /sys/fs/cgroup

# Talos mounts
extraHostPathMounts:
  - name: host-proc-sys-net
    hostPath: /proc/sys/net
    mountPath: /host/proc/sys/net
  - name: host-proc-sys-kernel
    hostPath: /proc/sys/kernel
    mountPath: /host/proc/sys/kernel
```

## Bootstrap Sequence

1. Flash Talos to SD cards
2. Boot nodes, apply configs via `talosctl`
3. Bootstrap control plane: `talosctl bootstrap`
4. Install Cilium (CNI) - **MUST BE FIRST**
5. Install CoreDNS (if using own)
6. Install ArgoCD
7. Apply `argocd/meta/meta.yaml` - GitOps takes over

## Differences from K3s

| Aspect | K3s | Talos |
|--------|-----|-------|
| SSH access | Yes | **No** - use `talosctl` |
| Package manager | apt | **None** - immutable |
| Config changes | Edit files | Machine config YAML |
| Upgrades | `apt upgrade` + k3s upgrade | `talosctl upgrade` |
| Node access | SSH | `talosctl` API |
| Logs | `journalctl` | `talosctl logs` |
| Shell | `bash` | `talosctl shell` (containers only) |

## Useful Commands

```bash
# Get kubeconfig
talosctl kubeconfig --nodes 172.16.101.1

# Node health
talosctl --nodes 172.16.101.1 health

# View logs
talosctl --nodes 172.16.101.1 logs kubelet
talosctl --nodes 172.16.101.1 logs etcd

# Kernel messages
talosctl --nodes 172.16.101.1 dmesg

# Reboot node
talosctl --nodes 172.16.101.1 reboot

# Upgrade Talos
talosctl --nodes 172.16.101.1 upgrade --image ghcr.io/siderolabs/installer:vX.Y.Z

# etcd snapshot
talosctl --nodes 172.16.101.1 etcd snapshot /tmp/etcd.snapshot

# Apply config changes
talosctl --nodes 172.16.101.1 apply-config --file controlplane.yaml
```

## RPi5 Status

Track: https://github.com/siderolabs/talos/issues?q=raspberry+pi+5

Current blockers:
- Official RPi5 image not available
- May need custom kernel/firmware overlay
- Community images exist but are experimental

## References

- [Talos Documentation](https://www.talos.dev/v1.9/)
- [Talos + Cilium Guide](https://www.talos.dev/v1.9/kubernetes-guides/network/deploying-cilium/)
- [Talos Machine Config Reference](https://www.talos.dev/v1.9/reference/configuration/)
