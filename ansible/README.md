# Ansible Configuration for K8s Cluster

This directory contains Ansible automation for managing the Kubernetes cluster infrastructure at the OS level.

## Directory Structure

```
ansible/
├── inventory/
│   └── production.yaml          # Cluster node inventory
├── playbooks/
│   └── 00-bootstrap-ansible-user.yaml  # Bootstrap ansible user
├── roles/                       # Ansible roles (to be implemented)
│   ├── node-prep/              # Node preparation tasks
│   ├── k3s-bootstrap/          # K3s installation
│   └── system-hardening/       # System hardening
├── ansible.cfg                 # Ansible configuration
└── README.md                   # This file
```

## Scope and Responsibilities

### Ansible manages (OS level):
- ✅ Node preparation (sysctl, packages, kernel params)
- ✅ User and SSH key management
- ✅ System-level configuration (watchdog, cloud-init, etc.)
- ✅ K3s installation and bootstrap
- ✅ Secrets distribution (SOPS keys, certificates)

### ArgoCD manages (K8s level):
- ✅ All Kubernetes resources (Deployments, Services, etc.)
- ✅ Helm releases
- ✅ Application lifecycle

### system-upgrade-controller manages:
- ✅ K3s version upgrades
- ✅ OS package updates

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
- SSH key `~/.ssh/ansible_ed25519` must exist (already generated)

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

**What this provides:**
- Official K3s installation and upgrade roles
- Automated cluster token management
- HA cluster support
- Sequential server upgrades (prevents etcd quorum loss)

## k3s-ansible Integration

This cluster uses k3s-ansible collection for K3s lifecycle management while preserving custom configuration (Cilium CNI, vipalived VIP, disabled default components).

### Inventory Format

Inventory follows k3s-ansible convention:
- `server`: Control plane nodes (was: control_plane)
- `agent`: Worker nodes (was: workers)
- `k3s_cluster`: Parent group containing all nodes

### Custom Configuration

All custom K3s flags are defined in `group_vars/k3s_cluster.yaml`:
- **Disabled components**: traefik, servicelb, local-storage, metrics-server, coredns
- **Custom domain**: k8s.home.lex.la
- **CNI**: Cilium (flannel-backend=none)
- **kube-proxy**: Disabled (Cilium replacement)
- **TLS SANs**: vipalived VIP (172.16.101.101) + server IP

### Upgrade K3s Version

To upgrade K3s across the cluster:

1. Update version in `group_vars/k3s_cluster.yaml`:
   ```yaml
   k3s_version: v1.32.0+k3s1
   ```

2. Run upgrade playbook:
   ```bash
   ansible-playbook playbooks/k3s-upgrade.yaml
   ```

**What happens:**
- Servers upgrade sequentially (one at a time, prevents etcd issues)
- Agents upgrade in parallel
- Custom configuration preserved
- Services automatically restarted with new version

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

3. Install K3s agent and join cluster:
   ```bash
   ansible-playbook playbooks/k3s-add-agent.yaml --limit k8s-worker-02
   ```

4. Verify node joined:
   ```bash
   kubectl get nodes
   ```

### Important Notes

- **Cluster token** is configured in `group_vars/k3s_cluster.yaml`
- **Do NOT run** `k3s-ansible/playbooks/site.yml` directly - it may conflict with existing setup
- **Use wrapper playbooks** in `playbooks/` directory - they handle custom configuration
- **Custom components** (Cilium, vipalived, ArgoCD) are managed outside k3s-ansible

## Node Preparation Role

The `node-prep` role prepares K3s cluster nodes with required packages and system configurations. Converted from Argo Workflows for better maintainability and idempotency.

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

### Sysctl Configuration

**Kernel Panic Settings** (`/etc/sysctl.d/99-k8s-panic.conf`):
- `kernel.panic=10` - Reboot after 10 seconds on panic
- `kernel.panic_on_oops=1` - Panic on kernel oops
- `vm.panic_on_oom=0` - Do not panic on OOM
- `kernel.hung_task_panic=0` - Do not panic on hung tasks

**Filesystem Optimization** (`/etc/sysctl.d/99-k8s-filesystem.conf`):
- `vm.swappiness=1` - Minimal swap usage
- `vm.dirty_ratio=10` - Force writeback at 10% dirty pages
- `fs.inotify.max_user_watches=524288` - inotify watches for K8s
- `fs.file-max=2097152` - Maximum open files

### DNS Configuration

**Fallback Servers** (`/etc/systemd/resolved.conf.d/fallback.conf`):
- Primary DNS: 172.16.0.1
- Fallback DNS: 8.8.8.8, 1.1.1.1

**Caching and Timeouts** (`/etc/systemd/resolved.conf.d/timeouts.conf`):
- DNS caching enabled
- Stale cache retention: 3600 seconds
- mDNS and LLMNR disabled
- DNS-over-TLS disabled

### Watchdog Configuration

Hardware watchdog configured in **heartbeat-only mode** (`/etc/watchdog.conf`):
- Device: /dev/watchdog
- Timeout: 120 seconds
- Interval: 30 seconds
- No load monitoring (prevents false-positive reboots)

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

# System upgrades (disabled by default in setup-nodes.yaml)
node_prep_system_upgrade_enabled: false
node_prep_reboot_if_required: true  # Auto-reboot enabled by default
```

### Important Notes

- **Watchdog** runs in heartbeat-only mode (no load monitoring) to prevent false-positive reboots
- **System upgrades** are disabled by default in setup-nodes.yaml, use upgrade-nodes.yaml playbook
- **Automatic reboot** is enabled by default, disable with `--extra-vars "auto_reboot=false"`
- **Nodes are upgraded sequentially** (serial: 1) to maintain cluster availability
- **Sysctl changes** are applied immediately and persisted to `/etc/sysctl.d/`
- **DNS changes** require systemd-resolved restart (handled by role)

### Conversion from Argo Workflows

This role replaces the following Argo Workflow templates:
- `workflow-template-new-node-setup.yaml` - Initial node setup
- `workflow-template-dns-configuration.yaml` - DNS configuration
- `workflow-template-node-upgrade.yaml` - System upgrades

All configurations are now managed via Ansible for better:
- **Idempotency** - Safe to run multiple times
- **Maintainability** - YAML-based configuration instead of bash scripts
- **Visibility** - Clear variable definitions in defaults/main.yml
- **Testability** - Can test on single node before cluster-wide rollout

## SSH Keys

- **Private key**: `~/.ssh/ansible_ed25519` (local machine)
- **Public key**: `~/.ssh/ansible_ed25519.pub` (deployed to nodes)
- **Key type**: ed25519 (modern, secure)
- **Passphrase**: None (for automation)

## Inventory

Nodes are organized in k3s-ansible format:

- `server`: Control plane nodes (k8s-cp-01: 172.16.101.1)
- `agent`: Worker nodes (k8s-worker-01: 172.16.101.2)
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

## Future Plans

- [ ] Create node-prep role (replace system-upgrade plans)
- [ ] Create k3s-bootstrap role (automate K3s installation)
- [ ] Create system-hardening role (security configurations)
- [ ] Add backup/restore playbooks
- [ ] Add node addition playbook
- [ ] Integrate with SOPS for secret management

## Notes

- **GitOps principle**: All Ansible code lives in this git repository
- **No kubectl apply**: Ansible does NOT manage K8s resources (ArgoCD does)
- **Idempotency**: All playbooks should be idempotent (safe to run multiple times)
- **Testing**: Always test on single node before cluster-wide rollout
