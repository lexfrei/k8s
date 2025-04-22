# Kubernetes Cluster

A Kubernetes cluster configuration designed for ARM64 systems (like Raspberry Pi) or any compatible hardware. This repository contains all the necessary manifests, configuration values, and tools to deploy a fully functional Kubernetes cluster with essential services.

## Features

- **Networking**: Project Calico (via Tigera Operator) for networking with VXLAN encapsulation
- **Load Balancing**: MetalLB for bare metal load balancing
- **Ingress**: Traefik as the ingress controller
- **Storage**: Longhorn for distributed storage
- **GitOps**: ArgoCD for declarative, Git-based application deployment
- **Monitoring**: Node exporter and Grafana for monitoring
- **Applications**: Various workloads including HomeAssistant, PaperMC, Transmission, etc.

## Prerequisites

### Node Configuration

1. Exclude specific IPs from your DHCP pool for MetalLB (see `manifests/metallb/*.yaml`)
2. Add Traefik's IP to your DNS records
3. Update all DNS references in the repo (search for the `lex.la` domain)
4. Add a DNS wildcard record (e.g., `*.k8s.home.example.com`) pointing to your ingress IP
5. For Raspberry Pi or similar ARM devices:
   - Add `cgroup_enable=cpuset cgroup_enable=memory cgroup_memory=1` to `/boot/cmdline.txt`
   - Set `Storage=volatile` in `/etc/systemd/journald.conf` to prevent SD card wear
6. System preparation:
   - Disable firewall: `systemctl disable --now firewalld`
   - Disable swap: `swapoff -a` and comment out swap in `/etc/fstab`
   - Disable WiFi if using MetalLB: `nmcli radio all off`
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
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest INSTALL_K3S_EXEC="--disable traefik,local-storage,servicelb,metrics-server,coredns --cluster-domain k8s.home.example.com --disable-network-policy --flannel-backend=none --cluster-init" sh -

# Copy content to ~/.kube/config on your management machine (update server address)
cat /etc/rancher/k3s/k3s.yaml

# Copy token for other nodes
cat /var/lib/rancher/k3s/server/node-token
```

#### On Additional Master Nodes

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_TOKEN=TOKEN-FROM-MASTER INSTALL_K3S_EXEC="server --server https://master01:6443 --disable traefik,local-storage,servicelb,metrics-server --cluster-domain k8s.home.example.com --flannel-backend=wireguard-native" sh -
```

#### On Worker Nodes

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_URL='https://master01:6443' K3S_TOKEN=TOKEN-FROM-MASTER sh -
```

### Deploy Core Components

```shell
# Add helm repositories
helm repo add coredns https://coredns.github.io/helm
helm repo add projectcalico https://projectcalico.docs.tigera.io/charts
helm repo add argo https://argoproj.github.io/argo-helm
helm repo update

# Install components
helm install coredns coredns/coredns --namespace kube-system -f values/coredns.yaml
helm install tigera-operator projectcalico/tigera-operator --version v3.29.0 --namespace tigera-operator -f values/tigera-operator.yaml --create-namespace
helm install argocd argo/argo-cd --namespace argocd -f values/argocd.yaml --create-namespace
kubectl apply -f argocd/meta/meta.yaml
```

## Accessing Dashboards

### Kubernetes Dashboard

```shell
# Create admin user and role
kubectl apply -f manifests/kubernetes-dashboard/account.yaml

# Get authentication token
kubectl -n kubernetes-dashboard describe secret $(kubectl -n kubernetes-dashboard get secret | grep admin-user | awk '{print $1}')
```

### Traefik Dashboard

```shell
# Apply IngressRoute configuration
kubectl apply -f manifests/traefik/ingressroute.yaml
```

### ArgoCD

Access via the configured Ingress route (typically https://argocd.k8s.home.example.com)

### Longhorn

Access via the configured Ingress route (typically https://longhorn.k8s.home.example.com)

## Network Architecture

This cluster uses:
- Calico (via Tigera Operator) for pod networking with VXLAN encapsulation
- MetalLB for bare metal load balancing with dedicated IP pools
- Traefik as the ingress controller
- CoreDNS for internal DNS resolution

## External Access

- Cloudflare can be configured as a reverse proxy for external access
- Tor hidden services can be set up for additional access methods

## Maintenance

- System upgrades managed via system-upgrade-controller
- Application updates managed via ArgoCD
- Storage managed by Longhorn

For detailed documentation on each component, see the [Wiki](../k8s.wiki)
