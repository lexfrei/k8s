# K8S cluster

## What is it

Kubernetes cluster ready for use on RPis or any other arm64 systems

### Included

* kubernetes-dashboard
* longhorn
* Metallb
* Traefik
* node-exporter
* victoria-metrics
* ...

## Pre-requirement

1. Exclude some ip's from your dhcp-pool. Put them to metallb config
2. Add Traefik's IP to your DNS
3. Change all DNSs in the repo. You can find it with `lex.la` substring
4. Add DNS wildcard to your DNS-server (ex.: `*.k8s.home.lex.la`)
5. Install Rocky Linux 9 as your system
6. `dnf install wireguard-tools iscsi-initiator-utils nfs-utils`
7. Add `cgroup_enable=cpuset cgroup_enable=memory cgroup_memory=1` to `/boot/cmdline.txt`
8. Set `Storage=volatile` in `/etc/systemd/journald.conf` to prevent filling up your SD card
9. Run `systemctl disable --now firewalld` to disable firewall
10. Run `swapoff -a` to disable swap
11. Run `nmcli radio all off` to disable wifi (you can't use it with MetalLB)
12. Set hostname with `hostnamectl hostname node01`
13. Resize root partition with `growpart /dev/sda 3` and `resize2fs /dev/sda3`
14. Reboot

On your host:

1. [Helm](https://helm.sh/docs/intro/install/)
2. [Helm diff plugin](https://github.com/databus23/helm-diff#install)
3. [helmfile](https://github.com/roboll/helmfile)

## Install k3s

On 1st master:

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest INSTALL_K3S_EXEC="--disable traefik,local-storage,servicelb,metrics-server,coredns --cluster-domain k8s.home.lex.la --disable-network-policy --flannel-backend=none --cluster-init" sh -
# copy content to ~/.kube/config and change address
cat /etc/rancher/k3s/k3s.yaml
# copy token for slave
cat /var/lib/rancher/k3s/server/node-token
```

On else master nodes:

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_TOKEN=TOKEN-FROM-MASTER INSTALL_K3S_EXEC="server --server https://master01:6443 --disable traefik,local-storage,servicelb,metrics-server --cluster-domain k8s.home.lex.la --flannel-backend=wireguard-native" sh -
```

On slave:

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_URL='https://master01:6443' K3S_TOKEN={{TOKEN-FROM-MASTER}} sh -
```

## Install all charts

```shell
helmfile apply
```

## Dashboards

### Kubernetes

Enabled, but you need a token to enter

```shell
# Add account and role
kubectl apply -f charts/kubernetes-dashboard/account.yaml
# Extract token
kubectl -n kubernetes-dashboard describe secret $(kubectl -n kubernetes-dashboard get secret | grep admin-user | awk '{print $1}')
```

### Traefik

```shell
# Add ingrees route
kubectl apply -f charts/traefik-dashboard/ingressroute.yaml
```

### Longhorn

Already enabled
