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
  - `argocd.yaml` - ArgoCD configuration including Crossplane health checks, server.insecure for Gateway TLS termination, and HTTPRoute configuration
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
    - Uses **wildcard listener** (*.home.lex.la) for simplified internal routing
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

**IMPORTANT**: Always use ArgoCD CLI for operations, not kubectl commands directly.

```bash
# Login to ArgoCD
argocd login api.argocd.home.lex.la:443 --username admin --plaintext

# Check all ArgoCD applications
argocd app list

# View application details
argocd app get APP_NAME

# Sync a specific application (after GitOps changes)
# 1. Make changes in git repository
# 2. Commit and push
# 3. Sync meta app first (it manages all other apps)
argocd app sync argocd/meta
# 4. Sync target application
argocd app sync argocd/APP_NAME

# Force hard refresh (reconcile from git)
argocd app sync argocd/APP_NAME --prune --force
```

**GitOps Workflow for Updates:**
1. Make changes in git repository (manifests, values, argocd definitions)
2. Commit and push to master
3. **CRITICAL**: When changing Application definitions (argocd/CATEGORY/*.yaml):
   - First sync meta app: `argocd app sync argocd/meta` or `kubectl annotate application meta --namespace argocd argocd.argoproj.io/refresh=normal --overwrite`
   - Meta app will update Application CRDs in cluster
   - Then sync target app: `argocd app sync argocd/TARGET_APP`
4. When changing only manifests/values (NOT Application definitions):
   - Sync target application directly: `argocd app sync argocd/TARGET_APP`
5. Verify: `argocd app get argocd/TARGET_APP`

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
         sectionName: https-home-lex-la  # Wildcard listener for *.home.lex.la
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
7. **For INTERNAL services**: Use existing wildcard listener `https-home-lex-la` (no Gateway changes needed)
8. Add your namespace to ReferenceGrant in `manifests/cilium/reference-grant.yaml`
9. Commit and push - ArgoCD meta app will auto-deploy

### Modifying Infrastructure Components

1. Update Helm values in `values/COMPONENT.yaml`
2. For components with HTTPRoute support (e.g., ArgoCD), use built-in Helm chart configuration instead of manual manifests
3. Commit changes
4. ArgoCD will detect and sync changes automatically (selfHeal: true)

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
- **SECURITY**: Public Gateway uses EXPLICIT hostnames only - NO wildcards allowed to prevent Host header manipulation attacks
- **SECURITY**: Internal Gateway uses wildcard (*.home.lex.la) since it's not exposed to internet
- **SECURITY**: Internal Gateway (172.16.100.250) MUST NOT be port-forwarded - local network access only
- kube-vip MUST be deployed before worker nodes join (they need VIP for API access)
- Cilium k8sServiceHost MUST point to kube-vip VIP (cannot be empty due to kube-proxy replacement)
- ArgoCD server.insecure MUST be true when behind Gateway with TLS termination
- ArgoCD HTTPRoute is managed via Helm chart values (server.httproute section), not manual manifests
- **ArgoCD dual-access architecture**:
  - **WebUI**: https://argocd.home.lex.la (via Gateway API with TLS termination)
  - **CLI/gRPC**: api.argocd.home.lex.la:443 (via dedicated LoadBalancer 172.16.100.254, plaintext)
  - Gateway uses HTTP/1.1 (breaks gRPC), LoadBalancer allows direct cmux access for CLI
- ArgoCD LoadBalancer uses dedicated IP pool (argocd-api-pool) with external-dns integration

## Renovate Configuration

Renovate bot is configured with:
- Semantic commits enabled
- Automerge enabled
- Version pinning for all dependencies
- Separate labels for major/minor/argocd/github-actions updates
- Staging periods: 5 days for major, 3 days for minor
- PR limits: 5 hourly, 10 concurrent
- Timezone: Asia/Tbilisi

## Lessons Learned: Common Mistakes to Avoid

### Critical Workflow Errors

1. **NEVER edit files while in plan mode**
   - Plan mode is ONLY for discussion and planning
   - Execute mode is for actual file modifications
   - Violation causes workflow confusion and errors

2. **ALWAYS verify commits before pushing**
   - Use `git show HEAD` or `git diff --cached` before push
   - Check for unintended content (comments, debug code, wrong language)
   - One verification step prevents hours of history cleanup

3. **Git history rewriting strategy**
   - For multiple commits with errors: `git reset --hard GOOD_COMMIT` → recreate → `git push --force-with-lease`
   - AVOID interactive rebase for signed commits (requires GPG PIN for each commit)
   - Force push creates orphaned commits on GitHub (accessible ~90 days, contact support for immediate removal)

### Code Quality Errors

4. **Language consistency**
   - ALL code, comments, commit messages MUST be in English
   - NO Russian (or other non-English) text in any files
   - Validate with: `grep -r "Russian-specific-chars" .` before commit

5. **Renovate comment rules**
   - Renovate comments (`# renovate: datasource=...`) ONLY for git sources
   - NOT needed for Helm charts (Renovate tracks ArgoCD Applications automatically)
   - Only use when source is `path:` in git repository

6. **Version verification**
   - ALWAYS verify versions with `helm search repo CHART_NAME` before use
   - NEVER guess or assume chart versions
   - Use latest stable version unless specific version required

7. **YAML cleanliness**
   - Remove empty objects: `annotations: {}`, `labels: {}`
   - Remove metadata added by kubectl/ArgoCD: `resourceVersion`, `uid`, `creationTimestamp`
   - Use `yq` or manual editing to clean exported resources

### ArgoCD-Specific Patterns

8. **Exporting from ArgoCD cluster**
   - When applications already exist in cluster, export with `kubectl get application NAME -o yaml`
   - Clean exported manifests before committing to git
   - Verify exported configs match desired state

9. **Values organization**
   - `values/` directory ONLY for bootstrap Helm charts
   - ArgoCD Applications MUST use inline `valuesObject` in Application manifest
   - This ensures GitOps single source of truth in ArgoCD Application definitions

### Pre-Commit Checklist

Before every commit, verify:
- [ ] No non-English text in code or comments
- [ ] Versions verified with `helm search repo` or official sources
- [ ] Renovate comments only on git sources, not Helm charts
- [ ] No empty YAML objects (`annotations: {}`, `labels: {}`)
- [ ] No metadata pollution (`uid`, `resourceVersion`, etc.)
- [ ] Tested with `kubectl apply --dry-run=client`
- [ ] Plan mode exited if was in planning phase

### GitHub Force Push Consequences

When using `git push --force-with-lease`:
- Remote branch is rewritten immediately
- Old commits become orphaned (unreachable from any branch)
- Orphaned commits remain accessible by direct hash URL for ~90 days
- For sensitive data removal, contact GitHub Support immediately
- Local cleanup: `git reflog expire --expire=now --all && git gc --prune=now --aggressive`
