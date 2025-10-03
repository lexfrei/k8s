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
  - `argocd.yaml` - ArgoCD configuration including Crossplane health checks and server.insecure for Gateway TLS termination
  - `coredns.yaml` - CoreDNS configuration
  - `cilium.yaml` - Cilium CNI configuration (tunnel mode VXLAN, kube-proxy replacement, L2 announcements, Gateway API, Hubble disabled)
  - `kube-vip.yaml` - kube-vip configuration for control plane HA with VIP

- **`secrets/`** - Encrypted secrets (likely using SOPS or similar)
  - Contains CloudFlare credentials, Grafana config

### Core Infrastructure Stack

The cluster runs on K3s with these core components (in deployment order):

1. **DNS**: CoreDNS with custom configuration
2. **Networking**: Cilium CNI with native routing on 10.42.0.0/16
3. **Control Plane HA**: kube-vip for control plane high availability (VIP: 172.16.101.101)
4. **GitOps**: ArgoCD (self-managed via the meta application)
5. **Storage**: Longhorn for distributed block storage
6. **Load Balancing**: Cilium L2 Announcements (LB IPAM) with dedicated IP pools
7. **Gateway API**: Cilium Gateway API v1.3.0 for HTTP/HTTPS routing with automatic HTTP→HTTPS redirect
8. **Certificate Management**: cert-manager with automatic Gateway API integration
9. **External DNS**: external-dns with Gateway API HTTPRoute support
10. **Monitoring**: Grafana operator, node-exporter, metrics-server

### Network Configuration

- **Control Plane VIP**: 172.16.101.101 (kube-vip in ARP mode for control plane HA)
  - Cilium requires explicit k8sServiceHost configuration (cannot use kubernetes.default.svc due to kube-proxy replacement)
  - All nodes and worker join operations use this VIP for API access
- **Pod network**: 10.42.0.0/16 (Cilium tunnel mode with VXLAN)
- **Cilium kube-proxy replacement** for service load balancing and NodePort
- **Cilium L2 Announcements** for LoadBalancer IP allocation:
  - Public Gateway pool: 172.16.100.251
  - Internal Gateway pool: 172.16.100.250
  - Transmission pool: 172.16.100.252
  - Minecraft pool: 172.16.100.253
  - Default pool: 172.16.100.101-110
- **Cilium Gateway API** for HTTP/HTTPS routing with **security-hardened dual Gateway setup**:
  - **Public Gateway** (cilium-gateway): 172.16.100.251
    - Port-forwarded from public IP 217.78.182.161
    - Uses **explicit hostnames only** (no wildcards) to prevent Host header manipulation attacks
    - Configured hostnames: eta.lex.la, job.lex.la, map.lex.la, aleksei.sviridk.in
    - Cloudflare proxy enabled (orange cloud) for DDoS protection
    - Automatic HTTP→HTTPS redirect (301) via dedicated HTTPRoute
  - **Internal Gateway** (cilium-gateway-internal): 172.16.100.250
    - **NOT port-forwarded** - accessible only from local network
    - Uses **explicit hostnames only**: argocd.home.lex.la, transmission.home.lex.la, longhorn.k8s.home.lex.la
    - Cloudflare DNS-only mode (grey cloud) - no proxy
    - Automatic HTTP→HTTPS redirect (301) via dedicated HTTPRoute
  - TLS certificates automatically managed by cert-manager via Gateway API integration
  - Supports wildcard certificates for *.lex.la, *.home.lex.la, *.k8s.home.lex.la, *.sviridk.in
- **External DNS** automatically creates DNS records from Gateway annotations
  - Public Gateway: external-dns.alpha.kubernetes.io/target: "217.78.182.161" (proxied)
  - Internal Gateway: external-dns.alpha.kubernetes.io/target: "172.16.100.250" (DNS-only)
- **Cluster domain**: `k8s.home.example.com` (configured in K3s)
- **Hubble**: Disabled to reduce resource consumption

## Key Commands

### Cluster Deployment

Bootstrap the cluster (run on management machine after K3s installation):

```bash
# Add Helm repositories
helm repo add coredns https://coredns.github.io/helm
helm repo add cilium https://helm.cilium.io/
helm repo add kube-vip https://kube-vip.github.io/helm-charts
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update

# Install core components in order
helm install coredns coredns/coredns --namespace kube-system --values values/coredns.yaml
helm install cilium cilium/cilium --namespace kube-system --values values/cilium.yaml
helm install kube-vip kube-vip/kube-vip --namespace kube-system --values values/kube-vip.yaml
helm install argocd argo/argo-cd --namespace argocd --values values/argocd.yaml --create-namespace

# Apply Cilium LB IP pools, L2 announcement policy, and Gateway
kubectl apply --filename manifests/cilium/

# Wait for kube-vip VIP to be assigned
kubectl wait --namespace kube-system --for=condition=ready pod --selector app.kubernetes.io/name=kube-vip --timeout=60s

# Now worker nodes can join using VIP: https://172.16.101.101:6443

# Deploy meta application (deploys all other applications via GitOps)
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
4. **Choose appropriate Gateway based on service accessibility**:
   - **Public services** (accessible from internet): Use `cilium-gateway`
   - **Internal services** (home network only): Use `cilium-gateway-internal`
5. Create HTTPRoute resource:
   ```yaml
   # Example for PUBLIC service
   apiVersion: gateway.networking.k8s.io/v1
   kind: HTTPRoute
   metadata:
     name: your-app
     namespace: your-namespace
     annotations: {}
   spec:
     parentRefs:
       - name: cilium-gateway
         namespace: kube-system
         sectionName: https-YOUR-HOSTNAME-lex-la  # Must match explicit listener name
     hostnames:
       - your-app.lex.la
     rules:
       - matches:
           - path:
               type: PathPrefix
               value: /
         backendRefs:
           - name: your-service
             port: 80
   ```
   ```yaml
   # Example for INTERNAL service
   apiVersion: gateway.networking.k8s.io/v1
   kind: HTTPRoute
   metadata:
     name: your-app
     namespace: your-namespace
     annotations: {}
   spec:
     parentRefs:
       - name: cilium-gateway-internal
         namespace: kube-system
         sectionName: https-YOUR-HOSTNAME-home-lex-la  # Must match explicit listener name
     hostnames:
       - your-app.home.lex.la
     rules:
       - matches:
           - path:
               type: PathPrefix
               value: /
         backendRefs:
           - name: your-service
             port: 80
   ```
6. **For PUBLIC services**: Add new explicit hostname listener to `manifests/cilium/gateway.yaml`
7. **For INTERNAL services**: Add new explicit hostname listener to `manifests/cilium/internal-gateway.yaml`
8. Add your namespace to ReferenceGrant in `manifests/cilium/reference-grant.yaml`
9. Commit and push - ArgoCD meta app will auto-deploy

### Modifying Infrastructure Components

1. Update Helm values in `values/COMPONENT.yaml`
2. Commit changes
3. ArgoCD will detect and sync changes automatically (selfHeal: true)

### Disabling Applications

Move ArgoCD Application manifest from `argocd/CATEGORY/` to `argocd-disabled/`

## Important Constraints

- K3s cluster with specific components disabled (local-storage, servicelb, metrics-server, coredns, kube-proxy, flannel, traefik)
- Custom CoreDNS and Cilium replace default K3s networking
- Designed for ARM64 architecture (Raspberry Pi)
- All changes deploy automatically via ArgoCD (selfHeal: true, prune: true)
- Domain references throughout use `lex.la` - must be updated for different domains
- Public services use direct IP access without Cloudflare Tunnel due to ISP DPI blocking
- cert-manager automatically manages certificates for Gateway API listeners
- TLS certificates issued via DNS-01 ACME challenge with Cloudflare API
- Gateway API provides modern, role-oriented routing instead of legacy Ingress
- **SECURITY**: Both Gateways use EXPLICIT hostnames only - NO wildcards allowed to prevent Host header manipulation attacks
- **SECURITY**: Internal Gateway (172.16.100.250) MUST NOT be port-forwarded - local network access only
- kube-vip MUST be deployed before worker nodes join (they need VIP for API access)
- Cilium k8sServiceHost MUST point to kube-vip VIP (cannot be empty due to kube-proxy replacement)
- ArgoCD server.insecure MUST be true when behind Gateway with TLS termination

## Renovate Configuration

Renovate bot is configured with:
- Semantic commits enabled
- Automerge enabled
- Version pinning for all dependencies
- Separate labels for major/minor/argocd/github-actions updates
- Staging periods: 5 days for major, 3 days for minor
- PR limits: 5 hourly, 10 concurrent
- Timezone: Asia/Tbilisi
