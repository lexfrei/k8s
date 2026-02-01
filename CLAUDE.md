# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Git Workflow Override

**IMPORTANT**: This repository allows direct commits and pushes to master branch.
- Direct push to master is ALLOWED and PREFERRED
- Feature branches are optional
- Pull requests are optional
- This overrides global CLAUDE.md Git workflow rules

## Kubectl Context

**CRITICAL**: ALWAYS use the `homelab` kubectl context when working with this cluster.

```bash
# All kubectl commands MUST use --context homelab
kubectl --context homelab get pods
kubectl --context homelab apply --filename manifest.yaml

# Or set context for session
kubectl config use-context homelab
```

- Context name: `homelab`
- API server: https://172.16.101.101:6443 (VIP)
- Credentials: admin via client certificate

**Never run kubectl commands without specifying context** — this prevents accidental operations on wrong clusters.

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
  - `coredns.yaml` - CoreDNS configuration with custom cluster domain k8s.home.lex.la
  - `cilium.yaml` - Cilium CNI configuration (native routing, kube-proxy replacement, L2 announcements, Gateway API, Hubble enabled)

- **`secrets/`** - Bootstrap secrets (GPG encrypted)
  - `openbao-seal-key.yaml.asc` - OpenBao auto-unseal key (required before OpenBao starts)
  - `authelia.yaml.asc` - Authelia OIDC secrets (references OpenBao paths)
  - All other secrets migrated to OpenBao and managed via External Secrets Operator

### Core Infrastructure Stack

The cluster runs on K3s with these core components (in deployment order):

1. **DNS**: CoreDNS with custom cluster domain k8s.home.lex.la
2. **Networking**: Cilium CNI with native routing on 10.42.0.0/16
3. **Control Plane HA**: vipalived DaemonSet for control plane high availability via VRRP/keepalived (VIP: 172.16.101.101)
4. **GitOps**: ArgoCD (self-managed via the meta application)
5. **Storage**: Longhorn for distributed block storage
6. **Load Balancing**: Cilium L2 Announcements (LB IPAM) with dedicated IP pools
7. **Gateway API**: Cilium Gateway API v1.3.0 for HTTP/HTTPS routing with automatic HTTP→HTTPS redirect
8. **Certificate Management**: cert-manager with automatic Gateway API integration
9. **External DNS**: external-dns with Gateway API HTTPRoute support
10. **Monitoring**: Grafana operator, node-exporter, metrics-server
11. **Secrets Management**: OpenBao (Vault fork) with auto-unseal
12. **Secrets Sync**: External Secrets Operator (ESO) for OpenBao → K8s sync
13. **Authentication**: Authelia for SSO/OIDC (integrated with OpenBao)
14. **Workflow Automation**: Argo Workflows for Kubernetes-native workflow orchestration
15. **Event-Driven Automation**: Argo Events for event-driven workflow triggers

### Network Configuration

- **Control Plane VIP**: 172.16.101.101 (vipalived DaemonSet using keepalived/VRRP for control plane HA)
  - vipalived runs as DaemonSet on control-plane nodes with hostNetwork
  - Uses VRRP protocol (keepalived) for automatic failover
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
- **Cluster domain**: `k8s.home.lex.la` (configured in K3s and CoreDNS)
- **Hubble**: Enabled for network observability
  - Web UI: https://hubble.home.lex.la (internal Gateway)
  - CLI: `hubble --server 172.16.100.101:4245 observe`
  - Relay LoadBalancer: 172.16.100.101 (pinned via annotation)
  - **CRITICAL**: `peerService.clusterDomain: k8s.home.lex.la` required due to custom cluster domain

## Key Commands

### Cluster Deployment

Bootstrap the cluster (run on management machine after K3s installation):

```bash
# Add Helm repositories
helm repo add coredns https://coredns.github.io/helm
helm repo add cilium https://helm.cilium.io/
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update

# Install core components in order
helm install coredns coredns/coredns --namespace kube-system --values values/coredns.yaml
helm install cilium cilium/cilium --namespace kube-system --values values/cilium.yaml
helm install vipalived oci://ghcr.io/lexfrei/charts/vipalived --version 0.3.0 --namespace kube-system
helm install argocd argo/argo-cd --namespace argocd --values values/argocd.yaml --create-namespace

# Apply Cilium LB IP pools, L2 announcement policy, and Gateway
kubectl apply --filename manifests/cilium/

# Deploy meta application (deploys all other applications via GitOps)
kubectl apply --filename argocd/meta/meta.yaml

# Note: vipalived DaemonSet manages control plane VIP (172.16.101.101) via VRRP/keepalived
# Worker nodes can join using VIP: https://172.16.101.101:6443
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

### Working with Argo Workflows and Argo Events

**Argo Workflows** provides Kubernetes-native workflow orchestration. **Argo Events** enables event-driven automation.

```bash
# View workflows
argo list --namespace argo-events

# View workflow details
argo get WORKFLOW_NAME --namespace argo-events

# View workflow logs
argo logs WORKFLOW_NAME --namespace argo-events
argo logs @latest --namespace argo-events

# Manual workflow submission from template
argo submit --from workflowtemplate/system-upgrade --namespace argo-events

# Check EventSource status
kubectl get eventsource --namespace argo-events

# Check Sensor status
kubectl get sensor --namespace argo-events

# Check EventBus status
kubectl get eventbus --namespace argo-events

# Web UI
# https://argo-workflows.home.lex.la
```

**Event-Driven Architecture:**
- **EventSource** (calendar, resource, webhook, GitHub, etc.) → **EventBus** (NATS JetStream) → **Sensor** → **Workflow**
- Calendar EventSource triggers workflows on schedule (e.g., weekly system upgrades)
- Resource EventSource can trigger on Kubernetes events (new nodes, pod failures, etc.)
- Sensor watches EventBus and submits WorkflowTemplates when events match

**Example Use Cases:**
- Weekly system upgrade plan application via calendar trigger
- New node provisioning automation via resource events
- Automated recovery workflows on pod failures
- GitHub webhook-triggered deployments

### Secrets Management

Secrets are managed via **OpenBao** (Vault fork) and **External Secrets Operator (ESO)**:

- **OpenBao**: Central secrets store deployed in `security` namespace
  - UI: https://openbao.home.lex.la
  - Auto-unseal via static seal with key from `secrets/openbao-seal-key.yaml.asc`
  - KV v2 secrets engine at `secret/`
  - Kubernetes auth method for ESO

- **External Secrets Operator**: Syncs secrets from OpenBao to Kubernetes
  - `ClusterSecretStore` named `openbao` connects to OpenBao
  - `ExternalSecret` resources in each namespace define what to sync
  - Secrets refresh every 1 hour

- **Bootstrap secrets** in `secrets/` (GPG encrypted):
  - `openbao-seal-key.yaml.asc` - Required BEFORE OpenBao starts
  - `authelia.yaml.asc` - Uses path references to OpenBao secrets

```bash
# Check ExternalSecrets status
kubectl get externalsecrets --all-namespaces

# Check ClusterSecretStore
kubectl get clustersecretstore openbao

# View secret in OpenBao (requires root token or appropriate policy)
kubectl exec -it openbao-0 -n security -- bao kv get secret/PATH
```

### Ansible Node Management

The `ansible/` directory contains Ansible playbooks for node management. **Always use Ansible for node operations** instead of ad-hoc SSH commands.

```bash
# IMPORTANT: All ansible commands must be run from the ansible/ directory
cd ansible/

# Upgrade all nodes with automatic reboot if required (TRUSTED - use this by default)
ansible-playbook playbooks/upgrade-nodes.yaml

# Upgrade without automatic reboot (only if you need manual control)
ansible-playbook playbooks/upgrade-nodes.yaml --extra-vars "auto_reboot=false"

# Upgrade only control plane
ansible-playbook playbooks/upgrade-nodes.yaml --limit server

# Upgrade only workers
ansible-playbook playbooks/upgrade-nodes.yaml --limit agent

# Upgrade specific node
ansible-playbook playbooks/upgrade-nodes.yaml --limit k8s-cp-01

# Ad-hoc commands (must be from ansible/ directory for inventory and become)
ansible k3s_cluster --module-name shell --args "uname -r"
ansible k3s_cluster --module-name apt --args "name=PACKAGE state=latest"
```

**Configuration:**
- Inventory: `ansible/inventory/production.yaml`
- SSH user: `ansible` (with dedicated key `~/.ssh/ansible_ed25519`)
- Become: enabled by default (passwordless sudo)
- Serial execution: nodes upgraded one at a time to maintain cluster availability

**Trust auto_reboot:**
- The `upgrade-nodes.yaml` playbook handles reboots safely
- Nodes are upgraded sequentially (serial: 1)
- Reboot only happens if `/var/run/reboot-required` exists
- Post-reboot delay ensures node is ready before proceeding
- **Use `auto_reboot=true` (default) for routine upgrades**

### K3s Cluster Management (k3s-ansible)

K3s installation and upgrades use the `k3s.orchestration` collection.

```bash
# IMPORTANT: All ansible commands must be run from the ansible/ directory
cd ansible/

# Install/upgrade K3s cluster (full deployment)
ansible-playbook k3s.orchestration.site

# Upgrade K3s only (sequential server upgrade, then agents)
ansible-playbook k3s.orchestration.upgrade

# Reset K3s cluster (DESTRUCTIVE)
ansible-playbook k3s.orchestration.reset

# Reboot all nodes
ansible-playbook k3s.orchestration.reboot
```

**K3s Version:**
- Defined in `ansible/inventory/production.yaml` → `k3s_version`
- Renovate auto-updates version via GitHub releases datasource
- Collection playbooks use FQCN format: `k3s.orchestration.<playbook>`

**Collection Installation:**
```bash
cd ansible/
ansible-galaxy collection install --requirements-file requirements.yaml
```

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
- **Node access**: Only network (SSH/Talos API) and UART available — no HDMI/display access to nodes
- All changes deploy automatically via ArgoCD (selfHeal: true, prune: true)
- Domain references throughout use `lex.la` - must be updated for different domains
- Public services use direct IP access without Cloudflare Tunnel due to ISP DPI blocking
- cert-manager automatically manages certificates for Gateway API listeners
- TLS certificates issued via DNS-01 ACME challenge with Cloudflare API
- Gateway API provides modern, role-oriented routing instead of legacy Ingress
- **SECURITY**: Public Gateway uses EXPLICIT hostnames only - NO wildcards allowed to prevent Host header manipulation attacks
- **SECURITY**: Internal Gateway uses wildcard (*.home.lex.la) since it's not exposed to internet
- **SECURITY**: Internal Gateway (172.16.100.250) MUST NOT be port-forwarded - local network access only
- **Cluster domain**: k8s.home.lex.la is configured in both K3s and CoreDNS, must be consistent across all components
- **vipalived**: DaemonSet running keepalived on control-plane nodes for VIP management
  - MUST be deployed before worker nodes join (they need VIP 172.16.101.101 for API access)
  - Uses VRRP for automatic failover between control plane nodes
  - Runs with hostNetwork and requires NET_ADMIN/NET_RAW/NET_BROADCAST capabilities
- Cilium k8sServiceHost MUST point to vipalived VIP 172.16.101.101 (cannot be empty due to kube-proxy replacement)
- ArgoCD server.insecure MUST be true when behind Gateway with TLS termination
- ArgoCD HTTPRoute is managed via Helm chart values (server.httproute section), not manual manifests
- **ArgoCD dual-access architecture**:
  - **WebUI**: https://argocd.home.lex.la (via Gateway API with TLS termination)
  - **CLI/gRPC**: api.argocd.home.lex.la:443 (via dedicated LoadBalancer 172.16.100.254, plaintext)
  - Gateway uses HTTP/1.1 (breaks gRPC), LoadBalancer allows direct cmux access for CLI
- ArgoCD LoadBalancer uses dedicated IP pool (argocd-api-pool) with external-dns integration
- **Argo Workflows**: Deployed in argo-events namespace for workflow orchestration
  - WorkflowTemplates define reusable workflow specifications
  - ServiceAccount argo-workflow has ClusterRole for kubectl operations
  - Web UI at https://argo-workflows.home.lex.la (internal Gateway)
- **Argo Events**: Event-driven automation framework deployed in argo-events namespace
  - EventBus uses NATS JetStream with Longhorn persistence (1Gi)
  - EventSources define event triggers (calendar, resource, webhook, GitHub, etc.)
  - Sensors watch EventBus and submit Workflows when events match
  - Current setup: Calendar EventSource triggers weekly system-plan.yaml application
- **NFS Storage**: TrueNAS NFS server (truenas.home.lex.la) for persistent storage
  - **CRITICAL**: Uses soft mount (not hard) to prevent kernel freeze on NFS unavailability
  - Timeout: timeo=100 (10 seconds) for fast failure detection
  - Hard mount can cause D state hangs and complete node freeze (see Error 14)
- **Hubble configuration constraints**:
  - `hubble.peerService.clusterDomain` MUST match cluster domain (k8s.home.lex.la) - Relay DNS resolution fails otherwise
  - Hubble Relay IP pinning: Helm chart doesn't support service annotations, use `kubectl annotate svc hubble-relay --namespace kube-system io.cilium/lb-ipam-ips=IP` (ArgoCD won't overwrite since Helm doesn't set annotations)
- **OpenBao**: Deployed in `security` namespace with static auto-unseal
  - Seal key stored in `secrets/openbao-seal-key.yaml.asc` (must exist before OpenBao starts)
  - Uses `longhorn-remote` StorageClass (dataLocality: disabled) due to worker-02 disk issues
  - nodeAffinity excludes k8s-worker-02
- **External Secrets Operator**: Deployed in `external-secrets` namespace
  - ClusterSecretStore `openbao` authenticates via Kubernetes auth method
  - All application secrets synced from OpenBao KV v2 (`secret/` path)
- **Authelia**: OIDC provider in `security` namespace
  - Configured clients: Grafana, ArgoCD, OpenBao
  - OIDC secrets stored in OpenBao, referenced via path in authelia config
  - Uses `claims_policies` to include groups in ID token (Authelia 4.38+ feature)

## Authentication Policy

**CRITICAL: All services MUST use OIDC authentication via Authelia. Local/password authentication is FORBIDDEN.**

- **ArgoCD**: Local admin login disabled (`admin.enabled: false`), OIDC via Dex → Authelia
- **Grafana**: OIDC via Authelia
- **OpenBao**: OIDC via Authelia
- When adding new services that support authentication, ALWAYS configure OIDC with Authelia
- NEVER enable local password authentication for any service
- User groups are managed in Authelia `users_database.yml`
- RBAC policies reference Authelia groups (e.g., `admins` group → admin role)

## Renovate Configuration

Renovate bot is configured with:
- Semantic commits enabled
- Automerge enabled
- Version pinning for all dependencies
- Separate labels for major/minor/argocd/github-actions updates
- Staging periods: 5 days for major, 3 days for minor
- PR limits: 5 hourly, 10 concurrent
- Timezone: Asia/Tbilisi

