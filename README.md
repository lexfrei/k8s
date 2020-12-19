# K8S cluster

## Not stable yet!

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
5. Install Ubuntu 20.04 to your system
6. `sudo apt install wireguard`

On your host:

1. [Helm](https://helm.sh/docs/intro/install/)
2. [Helm diff plugin](https://github.com/databus23/helm-diff#install)
3. [helmfile](https://github.com/roboll/helmfile)

## Install k3s

On all hosts add `cgroup_enable=cpuset cgroup_enable=memory cgroup_memory=1` to `/boot/firmware/cmdline.txt`

On 1st master:

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest INSTALL_K3S_EXEC="--disable traefik,local-storage,servicelb --cluster-domain k8s.home.lex.la --flannel-backend=wireguard --cluster-init" sh -
# copy content to ~/.kube/config and change address
cat /etc/rancher/k3s/k3s.yaml
# copy token for slave
cat /var/lib/rancher/k3s/server/node-token
```

On else master nodes:

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_TOKEN=TOKEN-FROM-MASTER INSTALL_K3S_EXEC="server --server https://master01:6443 --disable traefik,local-storage,servicelb --cluster-domain k8s.home.lex.la --flannel-backend=wireguard" sh -
```

On slave:

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest K3S_URL=https://master01:6443 K3S_TOKEN=TOKEN-FROM-MASTER sh -
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

## Update k8s

Use [system-upgrade-controller](https://github.com/rancher/system-upgrade-controller/)

```shell
# Only once, install upgrader
kubectl apply -f charts/system-upgrade/system-upgrade.yaml
# Apply update plan
kubectl apply -f charts/system-upgrade/k3s-plans.yaml
```
