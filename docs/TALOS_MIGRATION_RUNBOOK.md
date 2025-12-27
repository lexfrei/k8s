# K3s to Talos Migration Runbook

## Overview

Migration from K3s (Ubuntu 25.10) to Talos Linux for the homelab Kubernetes cluster.

**Current state:**
- 1 control plane: k8s-cp-01 (172.16.101.1)
- 2 workers: k8s-worker-01 (172.16.101.2), k8s-worker-02 (172.16.101.3)
- All nodes: Raspberry Pi (ARM64)

**Target state:**
- Talos Linux on same hardware
- Same IP addresses (recommended for minimal disruption)

## Pre-Migration Checklist

### 1. Verify DNS Owner IDs (Migration-Safe)

```yaml
# external-dns (Cloudflare)
txtOwnerId: cloudflare  ✅

# internal-dns (UniFi)
txtOwnerId: unifi  ✅
```

Both are static — DNS records will be adopted automatically.

### 2. Document NFS PV Paths

Current NFS volumes requiring migration:

| PVC | Namespace | NFS Path | Priority |
|-----|-----------|----------|----------|
| etcd-backup | kube-system | `/mnt/pool/k8s/pvc-4413fb39-bb9e-4a01-9374-26f8c4f850ed` | Low (recreate) |
| vmsingle-vmsingle | monitoring | `/mnt/pool/k8s/pvc-689043de-966f-4e86-99c2-3d043c515bd0` | Medium |
| transmission-config | transmission-system | `/mnt/pool/k8s/pvc-9b16c9f2-cd6d-4bab-8a8f-8f92808da932` | High |
| transmission-downloads-nfs | transmission-system | Manual PV (already static) | None |

### 3. Backup Current State

```bash
# etcd snapshot (already automated, but take fresh one)
ssh k8s-cp-01 "sudo k3s etcd-snapshot save --name pre-talos-migration"

# Export all secrets (encrypted)
for ns in $(kubectl get ns -o name | grep -v kube); do
  kubectl get secrets -n ${ns#namespace/} -o yaml > backup-secrets-${ns#namespace/}.yaml
done

# Verify git is up to date
git status  # should be clean
git push
```

### 4. Prepare Talos Configuration

```bash
# Generate Talos config
talosctl gen config homelab https://172.16.101.1:6443 \
  --output-dir talos \
  --with-docs=false \
  --with-examples=false

# Key customizations needed:
# - cluster.network.cni: none (we use Cilium)
# - cluster.proxy.disabled: true (Cilium replaces kube-proxy)
# - machine.network: static IPs for each node
```

## Migration Steps

### Phase 1: Prepare (Day Before)

1. **Notify users of maintenance window**

2. **Scale down stateful workloads:**
   ```bash
   kubectl scale statefulset --all --replicas=0 -n monitoring
   kubectl scale statefulset --all --replicas=0 -n paper
   kubectl scale deployment --all --replicas=0 -n transmission-system
   ```

3. **Verify NFS data is synced:**
   ```bash
   ssh truenas "ls -la /mnt/pool/k8s/"
   ```

4. **Create static PV manifests for migration:**
   ```bash
   mkdir -p manifests/nfs-migration
   ```

### Phase 2: Migration (Maintenance Window)

**Estimated time: 1-2 hours**

#### Step 1: Shutdown K3s Cluster (5 min)

```bash
# On each worker
ssh k8s-worker-01 "sudo systemctl stop k3s-agent"
ssh k8s-worker-02 "sudo systemctl stop k3s-agent"

# On control plane (last)
ssh k8s-cp-01 "sudo systemctl stop k3s"
```

#### Step 2: Install Talos on Nodes (30 min)

```bash
# Flash Talos to SD cards or USB drives
# Boot each node with Talos

# Apply config to each node
talosctl apply-config --nodes 172.16.101.1 --file talos/controlplane.yaml --insecure
talosctl apply-config --nodes 172.16.101.2 --file talos/worker.yaml --insecure
talosctl apply-config --nodes 172.16.101.3 --file talos/worker.yaml --insecure

# Bootstrap control plane
talosctl bootstrap --nodes 172.16.101.1

# Wait for nodes
talosctl --nodes 172.16.101.1 health
```

#### Step 3: Install Core Components (15 min)

```bash
# Get kubeconfig
talosctl kubeconfig --nodes 172.16.101.1

# Install Cilium (CNI must be first)
helm install cilium cilium/cilium \
  --namespace kube-system \
  --values values/cilium.yaml

# Wait for Cilium
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=cilium-agent -n kube-system --timeout=300s

# Install CoreDNS
helm install coredns coredns/coredns \
  --namespace kube-system \
  --values values/coredns.yaml

# Apply Cilium L2/Gateway configs
kubectl apply -f manifests/cilium/
```

#### Step 4: Install ArgoCD (10 min)

```bash
helm install argocd argo/argo-cd \
  --namespace argocd \
  --create-namespace \
  --values values/argocd.yaml

# Wait for ArgoCD
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=argocd-server -n argocd --timeout=300s
```

#### Step 5: Apply NFS Migration PVs (10 min)

Create static PVs pointing to existing NFS paths:

```yaml
# manifests/nfs-migration/vmsingle-pv.yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: vmsingle-vmsingle-migrated
spec:
  capacity:
    storage: 20Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: nfs.csi.k8s.io
    volumeHandle: "172.16.10.19#mnt/pool/k8s#pvc-689043de-966f-4e86-99c2-3d043c515bd0##"
    volumeAttributes:
      server: 172.16.10.19
      share: /mnt/pool/k8s
      subdir: pvc-689043de-966f-4e86-99c2-3d043c515bd0
  mountOptions:
    - nfsvers=4.1
    - soft
    - timeo=100
    - retrans=2
    - noresvport
    - noatime
  storageClassName: ""
---
apiVersion: v1
kind: PersistentVolume
metadata:
  name: transmission-config-migrated
spec:
  capacity:
    storage: 1Gi
  accessModes:
    - ReadWriteOnce
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: nfs.csi.k8s.io
    volumeHandle: "172.16.10.19#mnt/pool/k8s#pvc-9b16c9f2-cd6d-4bab-8a8f-8f92808da932##"
    volumeAttributes:
      server: 172.16.10.19
      share: /mnt/pool/k8s
      subdir: pvc-9b16c9f2-cd6d-4bab-8a8f-8f92808da932
  mountOptions:
    - nfsvers=4.1
    - soft
    - timeo=100
    - retrans=2
    - noresvport
    - noatime
  storageClassName: ""
```

```bash
# Apply migration PVs BEFORE ArgoCD syncs apps
kubectl apply -f manifests/nfs-migration/

# Install NFS CSI driver
kubectl apply -f argocd/infra/csi-driver-nfs.yaml
# Wait for sync...

# Apply NFS StorageClass
kubectl apply -f manifests/csi-driver-nfs/truenas-sc.yaml
```

#### Step 6: Deploy Applications via GitOps (15 min)

```bash
# Apply meta app - everything else follows
kubectl apply -f argocd/meta/meta.yaml

# Monitor deployment
watch kubectl get applications -n argocd
```

#### Step 7: Apply Secrets (5 min)

```bash
cd secrets
for f in *.yaml.asc; do
  gpg --decrypt "$f" | kubectl apply -f -
done
```

#### Step 8: Patch PVCs to Use Migration PVs (5 min)

For workloads that need existing data:

```bash
# Delete auto-created PVCs (they're empty)
kubectl delete pvc vmsingle-vmsingle -n monitoring
kubectl delete pvc transmission-config -n transmission-system

# Create PVCs bound to migration PVs
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: vmsingle-vmsingle
  namespace: monitoring
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 20Gi
  volumeName: vmsingle-vmsingle-migrated
  storageClassName: ""
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: transmission-config
  namespace: transmission-system
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
  volumeName: transmission-config-migrated
  storageClassName: ""
EOF

# Restart affected pods
kubectl delete pod -l app=vmsingle -n monitoring
kubectl delete pod -l app=transmission -n transmission-system
```

### Phase 3: Verification (15 min)

```bash
# Check all pods running
kubectl get pods --all-namespaces | grep -v Running

# Check PVCs bound
kubectl get pvc --all-namespaces

# Check external DNS records
dig argocd.home.lex.la
dig transmission.home.lex.la

# Check internal DNS (UniFi)
# Verify records in UniFi controller

# Check Gateway/HTTPRoutes
kubectl get gateway,httproute --all-namespaces

# Verify services accessible
curl -k https://argocd.home.lex.la
curl -k https://transmission.home.lex.la
```

## Rollback Plan

If migration fails, K3s can be restored:

```bash
# Reinstall Ubuntu on nodes (or keep dual-boot)

# Restore K3s via ansible
cd ansible
ansible-playbook k3s.orchestration.site

# Restore etcd from pre-migration snapshot
ssh k8s-cp-01 "sudo k3s server --cluster-reset \
  --cluster-reset-restore-path=/var/lib/rancher/k3s/server/db/snapshots/pre-talos-migration"
```

## Post-Migration Cleanup

After confirming everything works (wait 1 week):

```bash
# Remove migration manifests
rm -rf manifests/nfs-migration/

# Clean up old NFS directories (optional, after full verification)
# ssh truenas "rm -rf /mnt/pool/k8s/pvc-OLD-*"

# Update CLAUDE.md with Talos-specific instructions
```

## Talos-Specific Notes

### No SSH Access

Talos has no SSH. Use `talosctl` for all operations:

```bash
# Shell equivalent
talosctl --nodes NODE dmesg
talosctl --nodes NODE logs kubelet
talosctl --nodes NODE read /etc/os-release

# Reboot
talosctl --nodes NODE reboot

# Upgrade
talosctl --nodes NODE upgrade --image ghcr.io/siderolabs/installer:vX.Y.Z
```

### Immutable Filesystem

- No package manager
- No manual file edits
- All config via machine config YAML

### Updates Required

After migration, update:

1. `ansible/` — remove K3s playbooks or archive
2. `CLAUDE.md` — update kubectl context and node management instructions
3. `values/cilium.yaml` — verify Talos-specific settings (may need k8sServiceHost adjustment)

## Reference: Current NFS Paths

```
/mnt/pool/k8s/
├── pvc-4413fb39-bb9e-4a01-9374-26f8c4f850ed/  → etcd-backup (can recreate)
├── pvc-689043de-966f-4e86-99c2-3d043c515bd0/  → vmsingle-vmsingle (metrics history)
├── pvc-9b16c9f2-cd6d-4bab-8a8f-8f92808da932/  → transmission-config (important!)
└── downloads/                                  → transmission-downloads (static PV, no change)
```
