# Talos Configuration for Homelab

## Status

⚠️ **NOT READY** - Talos does not have stable RPi5 support yet.

## Architecture

**3x Control Plane** - all nodes are control plane, workloads scheduled on all.

| Node | IP | Role |
|------|-----|------|
| k8s-cp-01 | 172.16.101.1 | control-plane |
| k8s-cp-02 | 172.16.101.2 | control-plane |
| k8s-cp-03 | 172.16.101.3 | control-plane |
| VIP | 172.16.101.100 | API endpoint |

**No dedicated workers** — `allowSchedulingOnControlPlanes: true`

## Key Decisions

| Component | Choice | Reason |
|-----------|--------|--------|
| CNI | `none` → Cilium | Full control |
| kube-proxy | `disabled` | Cilium replaces |
| VIP | Talos native | No vipalived needed |
| CoreDNS | Talos-managed | Simpler |
| Cluster domain | `k8s.home.lex.la` | Match current |
| Pod CIDR | `10.42.0.0/16` | Match current |
| Service CIDR | `10.43.0.0/16` | Match current |

## Files

```
talos/
├── controlplane.yaml.example    # Base config for all nodes
├── patches/
│   ├── node-01.yaml            # k8s-cp-01 specific (hostname, IP)
│   ├── node-02.yaml            # k8s-cp-02 specific
│   └── node-03.yaml            # k8s-cp-03 specific
└── README.md
```

## Bootstrap Procedure

```bash
# 1. Generate secrets (ONCE, keep safe!)
talosctl gen secrets --output-file secrets.yaml

# 2. Generate base config
talosctl gen config homelab https://172.16.101.100:6443 \
  --with-secrets secrets.yaml \
  --output-dir generated/

# 3. Create per-node configs
talosctl machineconfig patch generated/controlplane.yaml \
  --patch @patches/node-01.yaml --output cp-01.yaml
talosctl machineconfig patch generated/controlplane.yaml \
  --patch @patches/node-02.yaml --output cp-02.yaml
talosctl machineconfig patch generated/controlplane.yaml \
  --patch @patches/node-03.yaml --output cp-03.yaml

# 4. Flash Talos to SD cards, boot nodes

# 5. Apply configs
talosctl apply-config --nodes 172.16.101.1 --file cp-01.yaml --insecure
talosctl apply-config --nodes 172.16.101.2 --file cp-02.yaml --insecure
talosctl apply-config --nodes 172.16.101.3 --file cp-03.yaml --insecure

# 6. Bootstrap first node (creates etcd cluster)
talosctl bootstrap --nodes 172.16.101.1

# 7. Wait for cluster
talosctl --nodes 172.16.101.100 health

# 8. Get kubeconfig
talosctl kubeconfig --nodes 172.16.101.100
```

## Post-Bootstrap: Install Components

```bash
# Cilium FIRST (CNI required for pods)
helm install cilium cilium/cilium \
  --namespace kube-system \
  --values values/cilium-talos.yaml

# Wait for Cilium
kubectl wait --for=condition=ready pod \
  -l app.kubernetes.io/name=cilium-agent \
  -n kube-system --timeout=300s

# Apply Cilium L2/Gateway
kubectl apply -f manifests/cilium/

# ArgoCD
helm install argocd argo/argo-cd \
  --namespace argocd --create-namespace \
  --values values/argocd.yaml

# GitOps takes over
kubectl apply -f argocd/meta/meta.yaml

# Secrets
for f in secrets/*.asc; do gpg -d "$f" | kubectl apply -f -; done
```

## Talos VIP vs vipalived

Talos has **built-in VIP** support:

```yaml
machine:
  network:
    interfaces:
      - interface: eth0
        vip:
          ip: 172.16.101.100  # Shared across all CP nodes
```

- No external DaemonSet needed
- Uses etcd for leader election
- Automatic failover
- One less thing to manage

## Cilium Changes for Talos

Create `values/cilium-talos.yaml` based on current `values/cilium.yaml` with:

```yaml
# Remove K3s-specific
# k8sServiceHost: ...  # Not needed, Talos uses KubePrism

# Add Talos-specific
cgroup:
  autoMount:
    enabled: false
  hostRoot: /sys/fs/cgroup

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
```

## Useful Commands

```bash
# Health check
talosctl --nodes 172.16.101.100 health

# Logs
talosctl --nodes NODE logs kubelet
talosctl --nodes NODE logs etcd

# etcd status
talosctl --nodes NODE etcd members
talosctl --nodes NODE etcd status

# etcd snapshot
talosctl --nodes NODE etcd snapshot db.snapshot

# Upgrade
talosctl --nodes NODE upgrade --image ghcr.io/siderolabs/installer:vX.Y.Z

# Reboot
talosctl --nodes NODE reboot

# Dashboard (TUI)
talosctl --nodes NODE dashboard
```

## RPi5 Status

Track: https://github.com/siderolabs/talos/issues?q=raspberry+pi+5

When ready, may need:
- Custom installer image with RPi5 firmware
- Device tree overlays
- Boot partition adjustments
