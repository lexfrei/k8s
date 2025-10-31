# Migration from kube-vip to vipalived

This document outlines the migration process from kube-vip to vipalived (keepalived-based solution) for control plane VIP management.

## Overview

**Current setup:**
- kube-vip manages control plane VIP 172.16.101.101
- Deployed via Helm in kube-system namespace

**Target setup:**
- vipalived (keepalived) DaemonSet on control plane nodes
- VRRP-based VIP management with equal priorities
- Auto-failover without preemption

## Pre-Migration Checklist

Before starting migration, verify:

```bash
# Check current VIP assignment
ip addr show | grep 172.16.101.101

# Verify kube-vip is running
kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=kube-vip

# Check control plane nodes
kubectl get nodes --selector node-role.kubernetes.io/control-plane

# Test API access via VIP
curl --insecure https://172.16.101.101:6443/healthz

# Backup current kube-vip configuration
helm get values kube-vip --namespace kube-system > /tmp/kube-vip-backup.yaml
```

## Migration Steps

### Step 1: Deploy vipalived

Deploy vipalived alongside kube-vip for initial testing:

```bash
# Sync ArgoCD meta application (to pick up new vipalived app)
argocd app sync argocd/meta

# Deploy vipalived
argocd app sync argocd/vipalived

# Wait for vipalived pods to be ready
kubectl wait --namespace kube-system \
  --for=condition=ready pod \
  --selector app.kubernetes.io/name=vipalived \
  --timeout=120s
```

**Expected behavior:**
- vipalived and kube-vip will compete for VIP ownership
- VRRP protocol will determine the winner
- One solution will hold the VIP, the other will be in BACKUP state

### Step 2: Verify vipalived Operation

Check that vipalived is operational:

```bash
# Check vipalived pods
kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=vipalived

# Check vipalived logs (should see VRRP negotiation)
kubectl logs --namespace kube-system --selector app.kubernetes.io/name=vipalived --tail=50

# Verify VIP is still accessible
curl --insecure https://172.16.101.101:6443/healthz

# Check which node holds the VIP
kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=vipalived \
  --output custom-columns=NODE:.spec.nodeName,NAME:.metadata.name
for pod in $(kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=vipalived --output name); do
  echo "=== $pod ==="
  kubectl exec $pod --namespace kube-system -- ip addr show | grep 172.16.101.101 || echo "No VIP"
done
```

### Step 3: Remove kube-vip

Once vipalived is verified working, remove kube-vip:

```bash
# Uninstall kube-vip Helm release
helm uninstall kube-vip --namespace kube-system

# Verify kube-vip is gone
kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=kube-vip
```

**Expected behavior:**
- VIP ownership will transfer to vipalived
- Brief interruption (1-2 seconds) possible during transition
- API access should remain available via VIP

### Step 4: Final Verification

Verify migration is complete and stable:

```bash
# Check VIP is active
ip addr show | grep 172.16.101.101

# Test API access
curl --insecure https://172.16.101.101:6443/healthz
kubectl get nodes

# Check vipalived logs for stable MASTER state
kubectl logs --namespace kube-system --selector app.kubernetes.io/name=vipalived --tail=20

# Verify all control plane pods have vipalived running
kubectl get daemonset --namespace kube-system vipalived
```

### Step 5: Test Failover

Test VRRP failover by stopping vipalived on the current master:

```bash
# Find which pod has the VIP
for pod in $(kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=vipalived --output name); do
  echo "=== $pod ==="
  kubectl exec $pod --namespace kube-system -- ip addr show | grep 172.16.101.101
done

# Stop keepalived on the master pod (replace POD_NAME)
kubectl exec POD_NAME --namespace kube-system -- pkill keepalived

# Watch VIP failover (should happen within 3-5 seconds)
watch 'ip addr show | grep 172.16.101.101'

# Verify API access is still working
kubectl get nodes

# Pod will be restarted automatically by Kubernetes
kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=vipalived
```

## Rollback Plan

If migration fails, rollback to kube-vip:

```bash
# Remove vipalived
kubectl delete --filename manifests/vipalived/

# Reinstall kube-vip from backup
helm install kube-vip kube-vip/kube-vip \
  --namespace kube-system \
  --values /tmp/kube-vip-backup.yaml

# Wait for kube-vip to acquire VIP
kubectl wait --namespace kube-system \
  --for=condition=ready pod \
  --selector app.kubernetes.io/name=kube-vip \
  --timeout=120s

# Verify VIP is working
curl --insecure https://172.16.101.101:6443/healthz
```

## Post-Migration Cleanup

After successful migration:

1. Remove kube-vip Helm repository if no longer needed:
   ```bash
   helm repo remove kube-vip
   ```

2. Update `values/kube-vip.yaml` reference in main CLAUDE.md if needed

3. Remove kube-vip from bootstrap documentation

## Troubleshooting

### VIP not assigned
```bash
# Check vipalived logs
kubectl logs --namespace kube-system --selector app.kubernetes.io/name=vipalived

# Common issues:
# - Network interface not found: check eth0 exists on nodes
# - VRRP blocked: check firewall allows VRRP (protocol 112)
# - Permissions: verify NET_ADMIN capability is granted
```

### API server unreachable during migration
```bash
# Access control plane node directly
ssh control-plane-node

# Check local API server
curl --insecure https://localhost:6443/healthz

# Manually assign VIP temporarily
ip addr add 172.16.101.101/32 dev eth0

# Investigate vipalived
kubectl logs --namespace kube-system --selector app.kubernetes.io/name=vipalived --tail=100
```

### Split-brain (multiple nodes claim VIP)
```bash
# Check all nodes for VIP
for node in $(kubectl get nodes --selector node-role.kubernetes.io/control-plane --output name | cut --delimiter=/ --fields=2); do
  echo "=== $node ==="
  ssh $node "ip addr show | grep 172.16.101.101"
done

# If split-brain detected:
# 1. Stop vipalived on all nodes
kubectl delete daemonset vipalived --namespace kube-system

# 2. Manually remove VIP from all nodes
for node in $(kubectl get nodes --selector node-role.kubernetes.io/control-plane --output name | cut --delimiter=/ --fields=2); do
  ssh $node "ip addr del 172.16.101.101/32 dev eth0"
done

# 3. Redeploy vipalived
kubectl apply --filename manifests/vipalived/
```

## Configuration Tuning

### Adjust VRRP timers
Edit `manifests/vipalived/configmap.yaml`:
```yaml
advert_int 1  # Advertisement interval (seconds)
```
Lower values = faster failover, higher network overhead

### Add health checks
Add track_script to monitor API server health (optional)

### Change authentication
Update `auth_pass` in ConfigMap for production security

## References

- Keepalived documentation: https://www.keepalived.org/
- VRRP RFC 5798: https://tools.ietf.org/html/rfc5798
- Kubernetes DaemonSet: https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/
