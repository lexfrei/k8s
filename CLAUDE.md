# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Purpose

This is a Kubernetes cluster configuration for ARM64 systems (Raspberry Pi compatible) managed via GitOps with ArgoCD. The repository contains all Kubernetes manifests, Helm values, and ArgoCD application definitions for a production home cluster.

## Architecture Overview

### GitOps Structure

The repository uses **ArgoCD's App-of-Apps pattern**:

- `argocd/meta/meta.yaml` - Root application that manages all other ArgoCD applications
- ArgoCD applications are organized by category in `argocd/` subdirectories:
  - `meta/` - ArgoCD self-management and projects
  - `infra/` - Infrastructure components (networking, storage, ingress)
  - `monitoring/` - Monitoring stack components
  - `workloads/` - User applications
  - `smarthome/` - Smart home related applications
  - `default/` - Default namespace applications
- Disabled/experimental applications are kept in `argocd-disabled/`

### Component Organization

- **`argocd/`** - ArgoCD Application manifests defining what to deploy
  - Each Application references either Helm charts or directories in `manifests/`
  - Applications use `automated` sync policy with `selfHeal: true` and `prune: true`
  - Organized by ArgoCD Projects (meta, infra, monitoring, workloads, smarthome, default)

- **`manifests/`** - Raw Kubernetes YAML manifests for applications
  - Each subdirectory contains manifests for a specific application
  - Referenced by ArgoCD Applications for deployment

- **`values/`** - Helm chart values files for infrastructure components
  - `argocd.yaml` - ArgoCD configuration including Crossplane health checks
  - `coredns.yaml` - CoreDNS configuration
  - `cilium.yaml` - Cilium CNI configuration (VXLAN, kube-proxy replacement, L2 announcements)

- **`secrets/`** - Encrypted secrets (likely using SOPS or similar)
  - Contains CloudFlare credentials, Grafana config

### Core Infrastructure Stack

The cluster runs on K3s with these core components (in deployment order):

1. **Networking**: Cilium CNI with VXLAN encapsulation on 10.42.0.0/16
2. **DNS**: CoreDNS with custom configuration
3. **Storage**: Longhorn for distributed block storage
4. **Load Balancing**: Cilium L2 Announcements (LB IPAM) with dedicated IP pools
5. **Ingress**: Traefik as the ingress controller
6. **Certificate Management**: cert-manager with ClusterIssuer
7. **GitOps**: ArgoCD (self-managed via the meta application)
8. **External DNS**: external-dns for Cloudflare integration
9. **Monitoring**: Grafana operator, node-exporter, metrics-server

### Network Configuration

- Pod network: 10.42.0.0/16 (Cilium VXLAN)
- Cilium kube-proxy replacement for service load balancing and NodePort
- Cilium L2 Announcements for LoadBalancer IP allocation:
  - Ingress pool: 172.16.100.251
  - Transmission pool: 172.16.100.252
  - Minecraft pool: 172.16.100.253
  - Default pool: 172.16.100.101-110
- Traefik ingress controller for HTTP/HTTPS routing
  - Public services use direct IP access: 217.78.182.161
  - Cloudflare operates in DNS-only mode (no proxy)
  - TLS certificates managed by cert-manager with ACME DNS-01 challenge
- External DNS updates Cloudflare records automatically
- Cluster domain: `k8s.home.example.com` (configured in K3s)
- Hubble for network observability and troubleshooting

## Key Commands

### Cluster Deployment

Bootstrap the cluster (run on management machine after K3s installation):

```bash
# Add Helm repositories
helm repo add coredns https://coredns.github.io/helm
helm repo add cilium https://helm.cilium.io/
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update

# Install core components
helm install coredns coredns/coredns --namespace kube-system --values values/coredns.yaml
helm install cilium cilium/cilium --namespace kube-system --values values/cilium.yaml
helm install argocd argo/argo-cd --namespace argocd --values values/argocd.yaml --create-namespace

# Apply Cilium LB IP pools and L2 announcement policy
kubectl apply --filename manifests/cilium/

# Deploy meta application (deploys all other applications)
kubectl apply --filename argocd/meta/meta.yaml
```

### Working with ArgoCD Applications

```bash
# Check all ArgoCD applications
kubectl get applications --namespace argocd

# Sync a specific application
kubectl patch application APP_NAME --namespace argocd --type merge --patch '{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"normal"}}}'

# View application details
kubectl describe application APP_NAME --namespace argocd
```

### Testing Changes

Before committing:

```bash
# Validate Kubernetes YAML syntax
kubectl apply --dry-run=client --filename manifests/APP_NAME/

# Validate ArgoCD application
kubectl apply --dry-run=client --filename argocd/CATEGORY/APP_NAME.yaml

# Check Helm values render correctly (for components using Helm)
helm template TEST_NAME CHART_NAME --values values/COMPONENT.yaml
```

### Secrets Management

Secrets in `secrets/` directory are encrypted. Pattern indicates SOPS or similar tool is used.

## Development Workflow

### Adding a New Application

1. Create manifests in `manifests/NEW_APP/`
2. Create ArgoCD Application in appropriate `argocd/CATEGORY/` directory
3. Reference the manifests directory in the Application spec
4. For public ingress resources, include these annotations:
   ```yaml
   annotations:
     traefik.ingress.kubernetes.io/router.entrypoints: websecure
     external-dns.alpha.kubernetes.io/target: "217.78.182.161"
     external-dns.alpha.kubernetes.io/cloudflare-proxied: "false"
     cert-manager.io/cluster-issuer: "cloudflare-issuer"
   ```
5. Add TLS configuration:
   ```yaml
   tls:
     - hosts:
         - your-domain.lex.la
       secretName: your-app-tls
   ```
6. Commit and push - ArgoCD meta app will auto-deploy

### Modifying Infrastructure Components

1. Update Helm values in `values/COMPONENT.yaml`
2. Commit changes
3. ArgoCD will detect and sync changes automatically (selfHeal: true)

### Disabling Applications

Move ArgoCD Application manifest from `argocd/CATEGORY/` to `argocd-disabled/`

## Important Constraints

- K3s cluster with specific components disabled (traefik, local-storage, servicelb, metrics-server, coredns, kube-proxy, flannel)
- Custom CoreDNS and Cilium replace default K3s networking
- Designed for ARM64 architecture (Raspberry Pi)
- All changes deploy automatically via ArgoCD (selfHeal: true, prune: true)
- Domain references throughout use `lex.la` - must be updated for different domains
- Public services use direct IP access without Cloudflare Tunnel due to ISP DPI blocking
- cert-manager uses DNS-01 ACME challenge with Cloudflare API for Let's Encrypt certificates

## Renovate Configuration

Renovate bot is configured with:
- Semantic commits enabled
- Automerge enabled
- Version pinning for all dependencies
- Separate labels for major/minor/argocd/github-actions updates
- Staging periods: 5 days for major, 3 days for minor
- PR limits: 5 hourly, 10 concurrent
- Timezone: Asia/Tbilisi
