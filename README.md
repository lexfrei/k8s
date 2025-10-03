# Kubernetes Cluster

A Kubernetes cluster configuration designed for ARM64 systems (like Raspberry Pi) or any compatible hardware. This repository contains all the necessary manifests, configuration values, and tools to deploy a fully functional Kubernetes cluster with essential services.

## Features

- **Control Plane HA**: kube-vip for control plane high availability with VIP
- **Networking**: Cilium CNI with native routing and kube-proxy replacement
- **Load Balancing**: Cilium L2 Announcements (LB IPAM) for bare metal load balancing
- **Gateway API**: Cilium Gateway API v1.3.0 for HTTP/HTTPS routing with automatic TLS and HTTP→HTTPS redirect
- **Storage**: Longhorn for distributed storage
- **GitOps**: ArgoCD for declarative, Git-based application deployment
- **Monitoring**: Node exporter and Grafana for monitoring
- **Observability**: Hubble for network visibility and troubleshooting
- **Applications**: Various workloads including PaperMC, Transmission, etc.

## Prerequisites

### Node Configuration

1. Exclude specific IPs from your DHCP pool for Cilium L2 LB (see `manifests/cilium/*-pool.yaml`)
2. Configure public IP (217.78.182.161) for Gateway in external-dns
3. Update all DNS references in the repo (search for the `lex.la` domain)
4. Gateway API will automatically create DNS records via external-dns
5. For Raspberry Pi or similar ARM devices:
   - Add `cgroup_enable=cpuset cgroup_enable=memory cgroup_memory=1` to `/boot/cmdline.txt`
   - Set `Storage=volatile` in `/etc/systemd/journald.conf` to prevent SD card wear
6. System preparation:
   - Disable firewall: `systemctl disable --now firewalld`
   - Disable swap: `swapoff -a` and comment out swap in `/etc/fstab`
   - Set unique hostname: `hostnamectl hostname node01`
   - Expand root partition if needed: `growpart /dev/sda 3` and `resize2fs /dev/sda3`
7. Reboot the system

### On Management Machine

1. Install [Helm](https://helm.sh/docs/intro/install/)
2. Configure kubectl to access your cluster

## Cluster Installation

### Install K3s

#### On First Master Node

```shell
# Create K3s config
sudo mkdir -p /etc/rancher/k3s
cat <<EOF | sudo tee /etc/rancher/k3s/config.yaml
tls-san:
  - 172.16.101.101
  - 172.16.113.46
EOF

# Install K3s
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest INSTALL_K3S_EXEC="--disable traefik,local-storage,servicelb,metrics-server,coredns,kube-proxy --cluster-domain k8s.home.example.com --disable-network-policy --flannel-backend=none --cluster-init" sh -

# Copy content to ~/.kube/config on your management machine (update server address to kube-vip VIP: 172.16.101.101)
cat /etc/rancher/k3s/k3s.yaml

# Copy token for other nodes
cat /var/lib/rancher/k3s/server/node-token
```

#### On Additional Master Nodes

```shell
# Create K3s config with TLS SANs
sudo mkdir -p /etc/rancher/k3s
cat <<EOF | sudo tee /etc/rancher/k3s/config.yaml
tls-san:
  - 172.16.101.101
  - 172.16.113.46
EOF

curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_TOKEN=TOKEN-FROM-MASTER INSTALL_K3S_EXEC="server --server https://172.16.101.101:6443 --disable traefik,local-storage,servicelb,metrics-server,kube-proxy --cluster-domain k8s.home.example.com --disable-network-policy --flannel-backend=none" sh -
```

#### On Worker Nodes

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_URL='https://172.16.101.101:6443' K3S_TOKEN=TOKEN-FROM-MASTER INSTALL_K3S_EXEC="--disable kube-proxy" sh -
```

### Deploy Core Components

```shell
# Add helm repositories
helm repo add coredns https://coredns.github.io/helm
helm repo add cilium https://helm.cilium.io/
helm repo add argo https://argoproj.github.io/argo-helm
helm repo add kube-vip https://kube-vip.github.io/helm-charts
helm repo update

# Install components in order
helm install coredns coredns/coredns --namespace kube-system --values values/coredns.yaml
helm install cilium cilium/cilium --namespace kube-system --values values/cilium.yaml
helm install kube-vip kube-vip/kube-vip --namespace kube-system --values values/kube-vip.yaml
helm install argocd argo/argo-cd --namespace argocd --values values/argocd.yaml --create-namespace

# Apply Cilium LB IP pools and L2 announcement policy
kubectl apply --filename manifests/cilium/

# Wait for kube-vip to be ready and VIP to be assigned
kubectl wait --namespace kube-system --for=condition=ready pod --selector app.kubernetes.io/name=kube-vip --timeout=60s

# Verify VIP is assigned (should show 172.16.101.101)
kubectl get pods --namespace kube-system --selector app.kubernetes.io/name=kube-vip

# Now you can join worker nodes using the VIP address (https://172.16.101.101:6443)

# Deploy meta application (deploys all other applications via GitOps)
kubectl apply --filename argocd/meta/meta.yaml
```

## Accessing Dashboards

### Kubernetes Dashboard

```shell
# Create admin user and role
kubectl apply -f manifests/kubernetes-dashboard/account.yaml

# Get authentication token
kubectl -n kubernetes-dashboard describe secret $(kubectl -n kubernetes-dashboard get secret | grep admin-user | awk '{print $1}')
```

### ArgoCD

Access via HTTPRoute at https://argocd.lex.la

### Longhorn

Access via HTTPRoute at https://longhorn.k8s.home.lex.la

### Hubble UI

Access via port-forward or HTTPRoute for network observability and troubleshooting

## Network Architecture

This cluster uses:
- **kube-vip** for control plane HA with virtual IP (172.16.101.101) using ARP mode
- **Cilium CNI** for pod networking with native routing (10.42.0.0/16)
- **Cilium kube-proxy replacement** for service load balancing and NodePort
- **Cilium L2 Announcements** for LoadBalancer IP allocation with dedicated pools:
  - Gateway pool: 172.16.100.251
  - Transmission pool: 172.16.100.252
  - Minecraft pool: 172.16.100.253
  - Default pool: 172.16.100.101-110
- **Cilium Gateway API** v1.3.0 for HTTP/HTTPS routing with automatic TLS and HTTP→HTTPS redirect
- **cert-manager** for automatic certificate management via Gateway API integration
- **external-dns** for automatic DNS record creation from HTTPRoute resources
- **CoreDNS** for internal DNS resolution
- **Hubble** for network visibility and monitoring

## External Access

- Cloudflare can be configured as a reverse proxy for external access
- Tor hidden services can be set up for additional access methods

## Maintenance

- System upgrades managed via system-upgrade-controller
- Application updates managed via ArgoCD
- Storage managed by Longhorn

For detailed documentation on each component, see the [Wiki](../k8s.wiki)
