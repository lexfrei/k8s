# K8S cluster

## Not stable yet!

## What is it

Kubernetes cluster ready for use on RPis or any other arm64 systems

### Included

* kubernetes-dashboard
* longhorn
* Metallb
* traefic
* node-exporter
* victoria-metrics
* ...

## Pre-requirement

1. Exclude some ip's from your dhcp-pool. Put them to metallb config
2. Add Traefik's IP to your DNS
3. Change all DNSs in the repo. You can find it with `lex.la` substring
4. Install Ubuntu 20.04 to your system

On your host:

1. [Helm](https://helm.sh/docs/intro/install/)
2. [Helm diff plugin](https://github.com/databus23/helm-diff#install)
3. [helfmile](https://github.com/roboll/helmfile)

## Install k3s

On all hosts add `cgroup_enable=cpuset cgroup_enable=memory cgroup_memory=1` to `/boot/firmware/cmdline.txt`

On master:

```shell
curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL=latest sh -s - --disable traefik,local-storage,servicelb,metrics-server --cluster-domain k8s.home.lex.la
# copy content to ~/.kube/config and change address
cat /etc/rancher/k3s/k3s.yaml
# copy token for slave
cat /var/lib/rancher/k3s/server/node-token
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

### Longhrn
Already enabled
