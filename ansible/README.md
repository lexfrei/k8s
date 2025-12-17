# Ansible Configuration for K8s Cluster

This directory contains Ansible automation for managing the Kubernetes cluster infrastructure at the OS level.

## Directory Structure

```
ansible/
├── inventory/
│   └── production.yaml          # Cluster node inventory (k3s-ansible format)
├── group_vars/
│   └── k3s_cluster.yaml         # Cluster-wide variables
├── playbooks/
│   ├── 00-bootstrap-ansible-user.yaml  # Bootstrap ansible user (one-time)
│   ├── setup-nodes.yaml                # Node preparation (node-prep role)
│   ├── upgrade-nodes.yaml              # System package upgrades
│   └── setup-network-diagnostics.yaml  # Network debugging tools
├── roles/
│   └── node-prep/               # Node preparation role
├── ansible.cfg                  # Ansible configuration
├── requirements.yaml            # Ansible collections
└── README.md                    # This file
```

## Scope and Responsibilities

### Ansible manages (OS level):
- Node preparation (sysctl, packages, kernel params)
- User and SSH key management
- System-level configuration (watchdog, cloud-init, etc.)
- K3s installation and upgrades (via k3s-ansible collection)
- System package upgrades

### ArgoCD manages (K8s level):
- All Kubernetes resources (Deployments, Services, etc.)
- Helm releases
- Application lifecycle

## Initial Setup

### 1. Bootstrap Ansible User

This is a **one-time operation** to create the `ansible` user on all nodes:

```bash
cd ansible
ansible-playbook playbooks/00-bootstrap-ansible-user.yaml
```

**What this does:**
- Creates `ansible` user on all nodes
- Deploys SSH public key (`~/.ssh/ansible_ed25519.pub`)
- Configures passwordless sudo
- Locks user password (SSH key only authentication)

**Prerequisites:**
- SSH access to nodes as user `lex` (current bootstrap user)
- `lex` user must have sudo privileges
- SSH key `~/.ssh/ansible_ed25519` must exist

### 2. Verify Connection

After bootstrap, test connection with ansible user:

```bash
ansible all -m ping
ansible all -m shell -a "sudo whoami"
```

Expected output: `SUCCESS` and `root`

### 3. Install k3s-ansible Collection

Install the k3s-ansible collection for K3s cluster management:

```bash
cd ansible
ansible-galaxy collection install -r requirements.yaml
```

## k3s-ansible Integration

This cluster uses k3s-ansible collection for K3s lifecycle management while preserving custom configuration (Cilium CNI, vipalived VIP, disabled default components).

### Inventory Format

Inventory follows k3s-ansible convention:
- `server`: Control plane nodes
- `agent`: Worker nodes
- `k3s_cluster`: Parent group containing all nodes

### Custom Configuration

All custom K3s flags are defined in `group_vars/k3s_cluster.yaml`:
- **Disabled components**: traefik, servicelb, local-storage, metrics-server, coredns
- **Custom domain**: k8s.home.lex.la
- **CNI**: Cilium (flannel-backend=none)
- **kube-proxy**: Disabled (Cilium replacement)
- **TLS SANs**: vipalived VIP (172.16.101.101) + server IP

### Run Full Site Playbook

Run the complete k3s-ansible site playbook (safe to run, idempotent):

```bash
ansible-playbook ~/.ansible/collections/ansible_collections/k3s/orchestration/playbooks/site.yml
```

### Upgrade K3s Version

To upgrade K3s across the cluster:

1. Update version in `inventory/production.yaml` (k3s_cluster vars):
   ```yaml
   k3s_version: v1.34.2+k3s1
   ```

2. Run upgrade playbook from k3s-ansible collection:
   ```bash
   ansible-playbook ~/.ansible/collections/ansible_collections/k3s/orchestration/playbooks/upgrade.yml
   ```

### Add New Worker Node

To add a new worker node to the cluster:

1. Add node to inventory `inventory/production.yaml`:
   ```yaml
   agent:
     hosts:
       k8s-worker-02:
         ansible_host: 172.16.101.3
   ```

2. Bootstrap ansible user on new node:
   ```bash
   ansible-playbook playbooks/00-bootstrap-ansible-user.yaml --limit k8s-worker-02
   ```

3. Prepare node with node-prep role:
   ```bash
   ansible-playbook playbooks/setup-nodes.yaml --limit k8s-worker-02
   ```

4. Install K3s agent using k3s-ansible collection:
   ```bash
   ansible-playbook ~/.ansible/collections/ansible_collections/k3s/orchestration/playbooks/site.yml --limit k8s-worker-02
   ```

5. Verify node joined:
   ```bash
   kubectl get nodes
   ```

## Node Preparation Role

The `node-prep` role prepares K3s cluster nodes with required packages and system configurations.

### Features

The role configures:

1. **System Packages** - Required dependencies for K3s
2. **Sysctl Tuning** - Kernel panic behavior and filesystem optimization
3. **DNS Configuration** - systemd-resolved with fallback servers and caching
4. **Watchdog** - Hardware watchdog in heartbeat-only mode
5. **System Upgrades** - Optional apt upgrade with automatic reboot support

### Package Categories

Packages are organized by purpose in `roles/node-prep/defaults/main.yml`:

**K8s Dependencies:**
- `nfs-common` - NFS client for NFS-based storage
- `multipath-tools` - Device mapper multipath support
- `open-iscsi` - iSCSI initiator (required by Longhorn)

**Quality of Life:**
- `vim` - Text editor
- `etcd-client` - etcdctl for K3s embedded etcd management

**System Utilities:**
- `cpufrequtils` - CPU frequency scaling
- `watchdog` - Hardware watchdog daemon
- `smartmontools` - Disk health monitoring (SMART)

### Usage

**Initial node setup:**
```bash
ansible-playbook playbooks/setup-nodes.yaml
```

**Setup specific group:**
```bash
ansible-playbook playbooks/setup-nodes.yaml --limit server
ansible-playbook playbooks/setup-nodes.yaml --limit k8s-cp-01
```

**System upgrades:**
```bash
# Upgrade all nodes with automatic reboot if required (default)
ansible-playbook playbooks/upgrade-nodes.yaml

# Upgrade without automatic reboot
ansible-playbook playbooks/upgrade-nodes.yaml --extra-vars "auto_reboot=false"

# Upgrade only control plane
ansible-playbook playbooks/upgrade-nodes.yaml --limit server

# Upgrade specific node
ansible-playbook playbooks/upgrade-nodes.yaml --limit k8s-cp-01
```

**Run specific configuration tasks:**
```bash
# Only sysctl configuration
ansible-playbook playbooks/setup-nodes.yaml --tags sysctl

# Only DNS configuration
ansible-playbook playbooks/setup-nodes.yaml --tags dns

# Only watchdog configuration
ansible-playbook playbooks/setup-nodes.yaml --tags watchdog
```

### Customization

Override variables in `group_vars/k3s_cluster.yaml`:

```yaml
# Package lists
node_prep_k8s_packages:
  - nfs-common
  - open-iscsi
  - multipath-tools
  - my-custom-package

# Sysctl tuning
node_prep_vm_swappiness: 1
node_prep_fs_inotify_max_user_watches: 524288

# DNS servers
node_prep_dns_primary: "172.16.0.1"
node_prep_dns_fallback: "8.8.8.8 1.1.1.1"

# Watchdog
node_prep_watchdog_timeout: 120
node_prep_watchdog_interval: 30
```

## Network Diagnostics

The `setup-network-diagnostics.yaml` playbook sets up tools for investigating network issues, specifically the Raspberry Pi 5 network death bug.

**Usage:**
```bash
ansible-playbook playbooks/setup-network-diagnostics.yaml
```

**What it configures:**
- ethtool stats collection (every 1 minute)
- macb ethernet driver debug logging at boot
- Network link state monitoring (every 30 seconds)
- Systemd timers and scripts in `/usr/local/bin/`

This is a debugging tool, not part of standard cluster operations.

## SSH Keys

- **Private key**: `~/.ssh/ansible_ed25519` (local machine)
- **Public key**: `~/.ssh/ansible_ed25519.pub` (deployed to nodes)
- **Key type**: ed25519 (modern, secure)
- **Passphrase**: None (for automation)

## Inventory

Nodes are organized in k3s-ansible format:

- `server`: Control plane nodes (k8s-cp-01: 172.16.101.1)
- `agent`: Worker nodes (k8s-worker-01: 172.16.101.2, k8s-worker-02: 172.16.101.3)
- `k3s_cluster`: Parent group containing all nodes

## Common Operations

### Run playbook on all nodes
```bash
ansible-playbook playbooks/PLAYBOOK_NAME.yaml
```

### Run playbook on specific group
```bash
ansible-playbook playbooks/PLAYBOOK_NAME.yaml --limit server
ansible-playbook playbooks/PLAYBOOK_NAME.yaml --limit agent
```

### Ad-hoc commands
```bash
# Check uptime
ansible all -a "uptime"

# Install package
ansible all -m apt -a "name=htop state=present"

# Reboot nodes (one by one)
ansible all -a "reboot" --forks 1
```

### Check inventory
```bash
ansible-inventory --list
ansible-inventory --graph
```

## Configuration Details

### ansible.cfg highlights:
- **Default user**: `ansible` (after bootstrap)
- **Privilege escalation**: `sudo` without password
- **SSH key**: `~/.ssh/ansible_ed25519`
- **Forks**: 10 (parallel execution)
- **Fact caching**: Enabled for performance

### Security:
- Ansible user has passwordless sudo (required for automation)
- SSH key authentication only (password locked)
- Private key protected by filesystem permissions (0600)

## Development Workflow

When adding new automation:

1. **Create role** in `roles/` directory
2. **Write playbook** in `playbooks/` directory
3. **Test on single node** with `--limit`
4. **Run on all nodes** after verification
5. **Commit to git** (Ansible configs are part of GitOps)

## Troubleshooting

### Connection issues
```bash
# Verbose output
ansible all -m ping -vvv

# Test SSH directly
ssh -i ~/.ssh/ansible_ed25519 ansible@172.16.101.1
```

### Permission denied (publickey)
- Ensure `~/.ssh/ansible_ed25519` exists and has correct permissions (0600)
- Verify public key is in `/home/ansible/.ssh/authorized_keys` on nodes
- Re-run bootstrap playbook if needed

### Sudo issues
- Verify `/etc/sudoers.d/ansible` exists on nodes
- Test manually: `ssh ansible@NODE "sudo -n true"`

## Notes

- **GitOps principle**: All Ansible code lives in this git repository
- **No kubectl apply**: Ansible does NOT manage K8s resources (ArgoCD does)
- **Idempotency**: All playbooks should be idempotent (safe to run multiple times)
- **Testing**: Always test on single node before cluster-wide rollout
