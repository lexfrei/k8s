# Talos Terraform Configuration

Infrastructure as Code for Talos Kubernetes cluster.

## Prerequisites

- Terraform >= 1.0
- Talos nodes booted and reachable on network
- Nodes have Talos installer (via PXE or flashed SD card)

## Usage

```bash
# Initialize
terraform init

# Plan
terraform plan

# Apply (creates cluster)
terraform apply

# Get kubeconfig
terraform output -raw kubeconfig > ~/.kube/config-talos

# Get talosconfig
terraform output -raw talosconfig > ~/.talos/config
```

## What It Does

1. Generates Talos secrets (stored in TF state)
2. Creates machine configs for each node
3. Applies configs to nodes
4. Bootstraps first control plane node
5. Outputs kubeconfig and talosconfig

## Post-Apply

After Terraform creates the cluster:

```bash
export KUBECONFIG=~/.kube/config-talos

# Install Cilium
helm install cilium cilium/cilium \
  --namespace kube-system \
  --values ../values/cilium-talos.yaml

# Wait for CNI
kubectl wait --for=condition=ready pod \
  -l app.kubernetes.io/name=cilium-agent \
  -n kube-system --timeout=300s

# Apply Cilium L2/Gateway
kubectl apply -f ../../manifests/cilium/

# Install ArgoCD
helm install argocd argo/argo-cd \
  --namespace argocd --create-namespace \
  --values ../../values/argocd.yaml

# GitOps takes over
kubectl apply -f ../../argocd/meta/meta.yaml
```

## State Management

⚠️ **Terraform state contains secrets!**

Options:
1. **Local state** (default) - encrypt the `.tfstate` file
2. **Remote state** - use S3/GCS with encryption
3. **Terraform Cloud** - managed state with encryption

## Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `cluster_name` | `homelab` | Cluster name |
| `cluster_endpoint` | `https://172.16.101.100:6443` | API VIP endpoint |
| `cluster_vip` | `172.16.101.100` | Shared VIP IP |
| `nodes` | 3 CPs | Map of node configs |
| `talos_version` | `v1.9.0` | Talos version |
| `kubernetes_version` | `1.32.0` | K8s version |

## Updating Nodes

To update node configuration:

```bash
# Edit main.tf or variables
terraform plan
terraform apply
```

Talos will apply changes and reboot if needed.

## Upgrading Talos

```bash
# Update talos_version variable
terraform apply

# Or use talosctl directly
talosctl upgrade --nodes NODE --image ghcr.io/siderolabs/installer:vX.Y.Z
```

## Destroying

```bash
# This will NOT wipe nodes, just remove TF state
terraform destroy

# To fully reset nodes, use talosctl
talosctl reset --nodes NODE --graceful=false
```
