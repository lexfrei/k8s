# TrueNAS to Kubernetes Node Migration Plan

Status: **DRAFT / RESEARCH COMPLETE**

## CRITICAL: Irreplaceable Data Warning

**`pool/Lex/lex` (920 GB) contains the personal photo archive and other irreplaceable
data that currently has NO BACKUP.** This dataset is the single most important thing
on the entire server. Every step of this migration must be validated against the
principle: "can I recover pool/Lex/lex if this step fails?"

**Non-negotiable prerequisites before ANY destructive action:**

1. Create ZFS snapshot: `zfs snapshot -r pool/Lex@pre-migration`
2. Verify snapshot: `zfs list -t snapshot -r pool/Lex`
3. Ideally: `zfs send pool/Lex/lex@pre-migration > /external-drive/lex-backup.zfs`
   to an external USB drive (the 32 GB DataTraveler is too small -- need a 1+ TB drive)
4. Consider setting up off-site backup (Backblaze B2, rsync to another machine) BEFORE
   starting migration -- this data has been at risk of a single RAIDZ1 failure for years

**RAIDZ1 is NOT a backup.** It survives 1 disk failure. It does not survive:
controller failure, accidental `zfs destroy`, firmware bug, fire, theft, ransomware,
or 2 simultaneous disk failures (4x same-model WD Red, same age = correlated failure risk).

## Goal

Replace TrueNAS SCALE with a standard Linux (Ubuntu 24.04) installation on the same
hardware, add the server as a 4th node in the k8s cluster, and run SMB/TimeMachine
as containerized workloads. Eliminate NFS dependency that previously caused a
cluster-wide cascade failure (see `postmortems/2025-11-14-nfs-dns-cilium-cascade-failure.md`).

## Current State

### Hardware

| | |
| --- | --- |
| Model | HP ProLiant ML310e Gen8 v2 |
| CPU | 8 cores (Xeon E3-1200 v3 family) |
| RAM | 32 GB |
| Boot | Kingston SA400S37 240 GB SSD (sdf, Intel AHCI ata-6) |
| USB | DataTraveler 3.0 32 GB (sdb, USB 3.0 xHCI) |
| Pool disks | 4x WDC WD40EFRX 4 TB (sda, sdc, sdd, sde) |
| Network | Realtek RTL8125 2.5GbE PCIe (ens1, active at 172.16.10.19/16) + Broadcom BCM5720 2x 1GbE onboard (eno1/eno2, unused) |
| iLO | 172.16.10.121 (IPMI exporter + syslog already monitored) |

### Storage Controllers

Two independent storage controllers — HDD and SSD are on **different buses**:

| Controller | PCI Address | Driver | Connected Devices |
| --- | --- | --- | --- |
| LSI SAS2008 (PCIe HBA) | 0000:04:00.0 | mpt3sas | 4x WD Red HDD (sda, sdc, sdd, sde) via SAS phys 4-7 |
| Intel C220 AHCI | 0000:00:1f.2 | ahci | Kingston SSD (sdf) on ata-6 (ODD port) |
| Intel xHCI USB 3.0 | 0000:00:14.0 | xhci_hcd | DataTraveler USB flash (sdb) on usb-0:8 |

**Key detail:** The SSD is connected to the Intel AHCI controller on ata-6, which is
the ODD (Optical Disk Drive) port. The BIOS cannot boot directly from this port,
hence the USB chainload boot mechanism (see below).

### Network Controllers

| Interface | Chip | PCI Address | Speed | State |
| --- | --- | --- | --- | --- |
| ens1 (enp13s0) | Realtek RTL8125 | 0000:0d:00.0 | 2.5 Gbps | UP (active) |
| eno1 (enp3s0f0) | Broadcom BCM5720 | 0000:03:00.0 | 1 Gbps | DOWN (unused) |
| eno2 (enp3s0f1) | Broadcom BCM5720 | 0000:03:00.1 | 1 Gbps | DOWN (unused) |

Broadcom BCM5720 is the dual-port 1GbE onboard NIC (standard for ML310e Gen8 v2).
Realtek RTL8125 is a PCIe add-in 2.5GbE card — all traffic currently runs through it.

**Migration note:** The RTL8125 uses the `r8169` kernel driver (in-tree since Linux 5.9).
No special driver installation needed for Ubuntu 24.04. The onboard BCM5720 (`tg3` driver)
can serve as a fallback if the Realtek card has issues. RPi nodes use 1 Gbps, so
the 2.5 Gbps link provides headroom for storage-heavy workloads on this node.

### Boot Chain

The server uses Legacy BIOS (not EFI). BIOS cannot boot from the SSD on ata-6
(ODD port), so a USB flash drive acts as a boot relay:

```text
BIOS
  → USB flash (sdb) MBR boot
    → GRUB from sdb1 (ext4, full i386-pc GRUB with ZFS module)
      → grub.cfg chainloads hd1:
          set root=(hd1)
          chainloader +1
        → SSD (sdf) MBR
          → SSD bootloader loads TrueNAS kernel from boot-pool (sdf3, ZFS)
```

**USB GRUB config** (`sdb1:/boot/grub/grub.cfg`, 135 bytes):

```text
set timeout=5
set default=0

menuentry "TrueNAS Boot" {
    insmod part_msdos
    insmod chain
    set root=(hd1)
    chainloader +1
}
```

**SSD partition layout** (GPT):

| Partition | Size | Type | Purpose |
| --- | --- | --- | --- |
| sdf1 | 1 MB | BIOS boot | GRUB stage 1.5 (for GPT disks) |
| sdf2 | 512 MB | EFI (vfat) | EFI System Partition (unused in Legacy BIOS mode) |
| sdf3 | 223 GB | ZFS (boot-pool) | TrueNAS OS: kernel, initrd, root filesystem |

**Migration implications:**

- The USB flash drive is **required** for booting as long as the SSD is on ata-6
- Ubuntu installer must replicate this pattern: install GRUB on USB, root on SSD
- Alternative: install Ubuntu entirely on USB (boot + root), use SSD for something else
- The SSD has an EFI partition (sdf2) but it's unused — could be leveraged if BIOS
  supports EFI boot from ata-6 (unlikely given it's an ODD port)
- The `hd1` reference in grub.cfg is positional — if disk order changes after
  reconnecting drives, the chainloader target may shift. Use UUID-based boot instead

### ZFS Pool

| | |
| --- | --- |
| Layout | RAIDZ1 (4 disks) |
| Usable capacity | ~11.5 TB |
| Used | 4780 GB (41.6%) |
| Free | 6703 GB |
| Fragmentation | 23% |
| Status | ONLINE, healthy, 0 errors |
| Scrub | Weekly (Sunday 00:00), last scrub clean |
| Snapshots | **None configured** |
| Replication | **None configured** |

### Datasets

| Dataset | Used | Purpose |
| --- | --- | --- |
| pool/Transmission | 2793.3 GB | Torrent downloads |
| pool/Lex/lex | 919.8 GB | Personal files (SMB, per-user) |
| pool/Dump | 822.7 GB | General dump (SMB) |
| pool/TimeMachine/lex | 239.6 GB | macOS Time Machine backup |
| pool/k8s | 0.7 GB | NFS share for k8s PVCs |

### Active Services

| Service | State | Purpose |
| --- | --- | --- |
| SMB (cifs) | running, autostart | File sharing (4 shares) |
| NFS | running, autostart | k8s storage backend |
| SSH | running, autostart | Management |
| Everything else | stopped | Not used |

### SMB Shares

| Share | Path | Type | Notes |
| --- | --- | --- | --- |
| TimeMachine | /mnt/pool/TimeMachine | TIMEMACHINE_SHARE | fruit VFS, auto-snapshot, auto-dataset per user (%U) |
| Lex | /mnt/pool/Lex | PRIVATE_DATASETS | Per-user subdatasets (%U) |
| Dump | /mnt/pool/Dump | DEFAULT_SHARE | General purpose |
| Transmission | /mnt/pool/Transmission | DEFAULT_SHARE | Torrent downloads |

SMB global: AAPL extensions enabled, SMB1 disabled, multichannel enabled, NTLMv1 disabled.

### NFS Shares

| Path | k8s Consumer | StorageClass |
| --- | --- | --- |
| /mnt/pool/k8s | etcd-backup, OpenBao, VMSingle, Transmission config | truenas-nfs-csi (default) |
| /mnt/pool/Transmission | Transmission downloads PV (manual, 10Ti) | N/A (static PV) |

NFS global: NFSv4 only, 8 server threads, allow_nonroot, no Kerberos.

### Users

Only `root` and `nobody` found via API. SMB authentication uses Samba's own passdb
(likely a single user `lex`).

### Cluster Dependencies on TrueNAS

| Workload | Namespace | Storage | Size |
| --- | --- | --- | --- |
| etcd-backup | kube-system | truenas-nfs-csi PVC | 10 Gi |
| OpenBao | security | truenas-nfs-csi PVC | 5 Gi |
| VMSingle | monitoring | truenas-nfs-csi PVC | dynamic |
| Transmission config | transmission-system | truenas-nfs-csi PVC | dynamic |
| Transmission downloads | transmission-system | manual NFS PV | 10 Ti |

All NFS mounts use soft mount with 10s timeout (post Nov 14 incident fix).

## What We Gain

- **+1 powerful node**: 8 cores + 32 GB RAM (more than all 3 RPi nodes combined)
- **Longhorn replica on real disks**: 4x 4TB WD Red, far more reliable than RPi SD/NVMe
- **Eliminate NFS SPOF**: workloads run locally, no network storage dependency
- **Unified management**: everything through GitOps, no separate WebUI
- **Native monitoring**: node-exporter, kubelet metrics instead of Graphite hacks
- **ZFS pool preserved**: `zpool import` works across distros without data loss

## What We Lose

| Lost Feature | Replacement | Effort |
| --- | --- | --- |
| TrueNAS WebUI | Cockpit + 45Drives cockpit-zfs plugin | Low |
| SMB config GUI | Static smb.conf in ConfigMap | None (simpler) |
| NFS config GUI | Not needed (NFS removed) | None |
| SMART monitoring GUI | smartmontools + node-exporter textfile collector | Low |
| ZFS snapshot GUI | CLI (`zfs snapshot`) or sanoid for automation | Low |
| Disk replacement wizard | CLI (`zpool replace`) | Low |
| TimeMachine integration | Samba fruit VFS in container (see below) | Low |

Net assessment: **all losses are trivially replaceable**. Current TrueNAS has zero
snapshots and zero replication configured, so the "advanced" features are unused.

## SMB in Containers: Analysis

### Key Finding

Both popular images (mbentley/timemachine, servercontainers/samba) are thin wrappers
around standard Samba with env-var-to-smb.conf generation. No patching, no custom
modules. The entire TimeMachine "magic" is 5 lines in smb.conf using the built-in
`vfs_fruit` module.

### mbentley/timemachine

- Alpine + samba-server + avahi + dbus + s6
- ~350 lines shell entrypoint: create user, generate smb.conf, generate Avahi XML
- s6 supervises 4 processes: smbd, nmbd, dbus, avahi-daemon
- 25 environment variables for configuration
- Over-engineered for k8s (env-var templating useful for Docker Compose, not ConfigMap)

### servercontainers/samba

- Alpine + samba + avahi + runit + wsdd2 (compiled from source)
- ~250 lines shell entrypoint: same pattern, env vars to smb.conf via `sed 's/;/\n/g'`
- runit supervises 4 processes: smbd, nmbd, avahi, wsdd2
- wsdd2 for Windows Network discovery (unnecessary for macOS-only use)

### Minimal Custom Image (Recommended)

Neither image is needed. A custom image is simpler, auditable, and avoids
tracking upstream Alpine rebuilds from random maintainers.

**Dockerfile** (~5 lines):

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache samba-server samba-common-tools
COPY smb.conf /etc/samba/smb.conf
COPY entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
CMD ["smbd", "--foreground", "--no-process-group"]
```

**entrypoint.sh** (~5 lines):

```bash
#!/bin/sh
set -e
adduser -D -H -s /bin/false "${SMB_USER:-lex}" 2>/dev/null || true
echo -e "${SMB_PASSWORD}\n${SMB_PASSWORD}" | smbpasswd -a -s "${SMB_USER:-lex}"
exec "$@"
```

**smb.conf** (~35 lines):

```ini
[global]
   server role = standalone server
   security = user
   passdb backend = smbpasswd
   load printers = no
   log file = /dev/stdout

   # macOS compatibility (built-in fruit VFS)
   vfs objects = catia fruit streams_xattr
   fruit:aapl = yes
   fruit:model = TimeCapsule8,119
   fruit:metadata = stream

   # Hardening
   server min protocol = SMB2
   ntlm auth = no

[TimeMachine]
   path = /data/timemachine
   valid users = lex
   writable = yes
   fruit:time machine = yes
   fruit:time machine max size = 500G
   durable handles = yes
   kernel oplocks = no
   kernel share modes = no
   posix locking = no

[Lex]
   path = /data/lex
   valid users = lex
   writable = yes

[Dump]
   path = /data/dump
   valid users = lex
   writable = yes

[Transmission]
   path = /data/transmission
   valid users = lex
   read only = yes
```

### What About Avahi (mDNS)?

Avahi advertises 3 services over mDNS:

- `_smb._tcp` -- SMB server discovery
- `_device-info._tcp` + model -- TimeCapsule icon in Finder
- `_adisk._tcp` + `adVF=0x82` -- "this server has Time Machine volumes"

**Not needed in k8s.** Without Avahi, connect manually once:
`smb://IP/TimeMachine` in Finder, then System Preferences > Time Machine > Select Disk.
macOS remembers the target. In k8s the SMB service gets a stable LoadBalancer IP via
Cilium L2 IPAM, so the address never changes.

If auto-discovery is desired later, Avahi can be added as a sidecar or `hostNetwork`
with dbus. But it's pure convenience for a single-user setup.

### Privileges Required

| Feature | Privileged? | Capability? |
| --- | --- | --- |
| Basic SMB file sharing | No | None |
| TimeMachine (fruit VFS) | No | None |
| xattr support | No | Depends on underlying FS (ZFS/ext4 support xattr natively) |
| Samba AD DC | Yes | SYS_ADMIN |
| Avahi (if added) | No | None (but needs hostNetwork for broadcast) |

For our use case: **no privileged mode, no special capabilities**.

### Performance

Container overhead for SMB is negligible (~0.12% CPU, +5us network latency at P99).
Bottleneck will be disk I/O and 1 Gbps network, not containerization.

## Migration Architecture

```text
BEFORE (current):
  RPi cluster (3 nodes) ──NFS──> TrueNAS (standalone)
  macOS ──SMB──> TrueNAS

AFTER (target):
  k8s cluster (4 nodes, including ex-TrueNAS)
    ├─ ex-TrueNAS node:
    │   ├─ ZFS pool (imported, hostPath)
    │   ├─ Samba pod (hostPath to ZFS datasets, LoadBalancer IP)
    │   ├─ Longhorn replica (using ZFS-backed local storage)
    │   └─ node-exporter, kubelet (standard monitoring)
    │
    ├─ Workloads that used NFS:
    │   ├─ etcd-backup → Longhorn PVC (or local on ex-TrueNAS)
    │   ├─ OpenBao → Longhorn PVC (already uses longhorn-remote)
    │   ├─ VMSingle → Longhorn PVC
    │   ├─ Transmission config → Longhorn PVC
    │   └─ Transmission downloads → hostPath on ex-TrueNAS node
    │
    └─ macOS ──SMB──> Samba pod (LoadBalancer IP)
```

## ZFS Dataset Restructuring

Current dataset hierarchy is a TrueNAS GUI artifact with unnecessary nesting.
The per-user subdatasets (`Lex/lex`, `TimeMachine/lex`) were auto-created by
TrueNAS PRIVATE_DATASETS and TIMEMACHINE_SHARE types using the `%U` scheme.
After migration there is only one user, so the wrapper datasets are pointless.

### Current Layout

```text
pool                                          /mnt/pool                    4780 GB
├── Dump                                      /mnt/pool/Dump                823 GB
├── k8s                                       /mnt/pool/k8s                   1 GB
├── Lex                                       /mnt/pool/Lex                 920 GB
│   └── lex                                   /mnt/pool/Lex/lex             920 GB  ← all data here
├── TimeMachine                               /mnt/pool/TimeMachine         240 GB
│   └── lex                                   /mnt/pool/TimeMachine/lex     240 GB  ← all data here
└── Transmission                              /mnt/pool/Transmission       2793 GB
```

### Target Layout

```text
pool                                          /pool                        4780 GB
├── dump                                      /pool/dump                    823 GB
├── lex                                       /pool/lex                     920 GB
├── timemachine                               /pool/timemachine             240 GB
└── transmission                              /pool/transmission           2793 GB
```

Flat, lowercase, no wrappers. `pool/k8s` deleted (NFS goes away).

### ZFS Rename Rules

- `zfs rename` works within the same pool, can move between hierarchy levels
- Children move automatically with parent
- Cannot rename across pools (requires `zfs send | zfs receive`)
- Dataset must not be in use (stop SMB/NFS first)
- Mountpoints update automatically (or set explicitly with `zfs set mountpoint=`)

### Rename Commands

Run during Phase 3 (after Ubuntu install, after pool import, before k8s join).

**Before renaming, verify the pre-migration snapshot still exists:**

```bash
zfs list -t snapshot -r pool/Lex
# Must show pool/Lex/lex@pre-migration -- if not, DO NOT proceed
```

```bash
# Stop any services using the pool
systemctl stop smbd 2>/dev/null || true

# Collapse Lex/lex -> lex (move child up, remove empty parent)
# NOTE: snapshot travels with the dataset -- pool/Lex/lex@pre-migration
#       becomes pool/lex@pre-migration after rename
zfs rename pool/Lex/lex pool/lex
# Verify data is intact before destroying parent:
ls /pool/lex/  # must show photo archive and other files
zfs destroy pool/Lex

# Collapse TimeMachine/lex -> timemachine
zfs rename pool/TimeMachine/lex pool/timemachine
zfs destroy pool/TimeMachine

# Lowercase remaining datasets
zfs rename pool/Dump pool/dump
zfs rename pool/Transmission pool/transmission

# Delete NFS dataset (only after all NFS workloads migrated to Longhorn!)
# zfs destroy pool/k8s  # uncomment when ready

# Set clean mountpoints (optional, defaults to /pool/<name>)
zfs set mountpoint=/pool/lex pool/lex
zfs set mountpoint=/pool/dump pool/dump
zfs set mountpoint=/pool/timemachine pool/timemachine
zfs set mountpoint=/pool/transmission pool/transmission

# Verify
zfs list -o name,mountpoint,used,available
```

### Samba Paths After Restructuring

The smb.conf paths map directly to the new mountpoints:

| Share | Old path (TrueNAS) | New path (Linux) |
| --- | --- | --- |
| TimeMachine | /mnt/pool/TimeMachine/lex | /pool/timemachine |
| Lex | /mnt/pool/Lex/lex | /pool/lex |
| Dump | /mnt/pool/Dump | /pool/dump |
| Transmission | /mnt/pool/Transmission | /pool/transmission |

In the Samba container, these are mounted via hostPath and appear as `/data/*`:

```yaml
volumes:
  - name: timemachine
    hostPath:
      path: /pool/timemachine
  - name: lex
    hostPath:
      path: /pool/lex
# ...
```

## Storage Strategy: OpenEBS ZFS LocalPV

### Why Not Longhorn on ZFS?

**Longhorn is incompatible with ZFS.** Longhorn replicas use `FIEMAP` (File Extent Map)
which ZFS does not support. Replicas crash with:

```text
file extent is unsupported: operation not supported
```

Confirmed in [longhorn/longhorn#5106](https://github.com/longhorn/longhorn/issues/5106)
and [#11125](https://github.com/longhorn/longhorn/issues/11125).

**Workaround exists** (zvol + ext4 on top), but it's an anti-pattern: double CoW,
wasted capacity, Longhorn replication on top of ZFS replication = pointless overhead.

### Alternatives Considered

| Solution | Verdict | Why |
| --- | --- | --- |
| Longhorn on zvol+ext4 | No | Double CoW, capacity waste, anti-pattern |
| SeaweedFS | No | Overkill for 4 nodes (designed for 100s+ nodes) |
| democratic-csi (iSCSI) | No | Recreates NFS SPOF problem over network |
| democratic-csi (local) | Maybe | Works, but less mature than OpenEBS for local ZFS |
| NFS from the node | No | Same problems as TrueNAS (see Nov 14 postmortem) |
| local-path-provisioner | Fallback | Works but loses all ZFS features (snapshots, compression) |
| **OpenEBS ZFS LocalPV** | **Yes** | **Native ZFS, minimal overhead, CSI snapshots, resize** |

### OpenEBS ZFS LocalPV

[openebs/zfs-localpv](https://github.com/openebs/zfs-localpv) -- CSI driver that
creates PVs directly as ZFS datasets or zvols. Pure control-plane, no dataplane overhead.

**How it works:**

1. PVC requests StorageClass `zfs-localpv`
2. CSI driver runs `zfs create pool/pvc-<uuid>` on the target node
3. Pod mounts the dataset directly
4. All ZFS features available: checksums, compression, snapshots, instant clones

**StorageClass:**

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: zfs-localpv
provisioner: zfs.csi.openebs.io
parameters:
  poolname: "pool"
  fstype: "zfs"            # native ZFS dataset (not zvol)
  compression: "lz4"
  thinprovision: "yes"
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
```

**Features:**

- VolumeSnapshot API (native ZFS snapshots, instant)
- Online resize (`zfs set quota=`)
- Clones from snapshots (instant, zero copy)
- Thin provisioning (no pre-allocation)
- Compression per-volume

**Limitations:**

- Node-local only -- no replication, no HA
- Pod is pinned to k8s-storage-01 via node affinity (automatic)
- ZFS pool must exist on the node before driver starts

**Comparison with Longhorn:**

| | OpenEBS ZFS LocalPV | Longhorn |
| --- | --- | --- |
| Replication | No (node-local) | Yes (cross-node) |
| HA | No | Yes |
| Performance | Native ZFS (minimal overhead) | Significant (userspace replicas) |
| Snapshots | ZFS native (instant) | Longhorn snapshots (slower) |
| Compression | ZFS lz4/zstd | No |
| Checksums | ZFS (bitrot protection) | No |
| Complexity | Minimal | Medium |

### Dual StorageClass Architecture

After migration, the cluster has two storage backends:

```text
StorageClass         Backend              Nodes              Use case
─────────────────────────────────────────────────────────────────────────
longhorn             Longhorn replicated   RPi nodes          HA workloads (databases, etc.)
zfs-localpv          ZFS native datasets   k8s-storage-01     Heavy storage (media, metrics, backups)
```

Workloads choose the right backend via StorageClass in their PVC. Scheduler
automatically places pods on the correct node.

## ZFS ARC Memory Budget

32 GB total RAM on k8s-storage-01. Need to split between ZFS ARC cache and k8s workloads.

**Proposed split:**

| Consumer | RAM | Rationale |
| --- | --- | --- |
| ZFS ARC | 8 GB | Enough for metadata + hot data caching on a 4.8 TB used pool |
| kubelet + system | 2 GB | OS, kubelet, containerd, node-exporter |
| k8s workloads | 22 GB | Samba, Transmission, pods scheduled on this node |

**Configuration** (in `/etc/modprobe.d/zfs.conf`):

```text
options zfs zfs_arc_max=8589934592
```

This is conservative. ZFS defaults to 50% of RAM (16 GB), which would leave only
16 GB for k8s -- tight if heavy workloads land on this node. 8 GB ARC is generous
for a 4.8 TB dataset with sequential workloads (media files, backups).

Can be tuned later based on `arcstat` output without restart:

```bash
echo 8589934592 > /sys/module/zfs/parameters/zfs_arc_max
```

## Step-by-Step Migration Plan

### Phase 0: Preparation (before any changes)

**DO NOT proceed to Phase 1 until every item here is complete.**

1. **SET UP BACKUP FOR pool/Lex** -- this is the blocker for the entire migration:
   - Option A (minimum): `zfs snapshot -r pool/Lex@pre-migration` (protects against
     accidental rename/destroy, does NOT protect against disk/controller failure)
   - Option B (recommended): `zfs send` to an external USB drive (need 1+ TB)
   - Option C (ideal): set up recurring off-site backup (Backblaze B2, rsync to
     another machine, or `zfs send` to a remote ZFS system) -- this should exist
     regardless of whether migration happens
2. Verify backup integrity: mount/read the backup, spot-check files
3. Create ZFS snapshots of ALL datasets: `zfs snapshot -r pool@pre-migration`
4. Document current SMB user passwords
5. Back up TrueNAS config: System > General > Save Config
6. Note all IP addresses (172.16.10.19 for TrueNAS, 172.16.10.121 for iLO)
7. Verify all data is accounted for (datasets, SMB shares, NFS consumers)

### Phase 1: Build Samba Container Image

1. Create repo `lexfrei/samba` (or add to existing charts/images repo)
2. Build minimal image (Dockerfile above)
3. Multi-arch build (amd64 at minimum, arm64 optional)
4. Push to GHCR
5. Test locally: `docker run` with bind-mount, connect from macOS, verify TimeMachine

### Phase 2: Prepare k8s Manifests

1. Create `manifests/samba/` with:
   - Deployment (or DaemonSet with nodeAffinity to ex-TrueNAS node)
   - Service (LoadBalancer, Cilium L2 IPAM)
   - ConfigMap (smb.conf)
   - ExternalSecret (SMB password from OpenBao)
2. Create `argocd/workloads/samba.yaml`
3. Prepare Longhorn PVC manifests for migrated workloads
4. Update Transmission manifests (hostPath instead of NFS PV)
5. Remove/update CSI NFS driver configuration

### Phase 3: Install Ubuntu on TrueNAS Hardware

**Boot chain considerations:** The server uses Legacy BIOS and cannot boot from the
SSD directly (ata-6 ODD port). The USB flash drive is the boot relay. Ubuntu must
be installed with this constraint in mind.

1. **Disconnect data disks** (sda, sdc, sdd, sde on LSI SAS2008) -- leave only
   boot SSD (sdf on Intel AHCI) and USB flash (sdb on xHCI)
2. Install Ubuntu 24.04 LTS:
   - Boot from Ubuntu USB installer (temporarily remove DataTraveler, use installer USB)
   - Install root filesystem on SSD (sdf)
   - Install GRUB to the DataTraveler USB (sdb) -- the BIOS boots from USB
   - Alternative: partition SSD with BIOS boot partition + root, keep USB GRUB
     chainloader pointing to SSD (same pattern as TrueNAS)
   - **IMPORTANT:** After install, verify `grub.cfg` uses UUID-based root, not
     positional `hd1` -- disk order will change when HDD are reconnected
3. Verify boot works with only SSD + USB connected
4. Install ZFS: `apt install --assume-yes zfsutils-linux`
5. Reconnect data disks (LSI SAS2008 controller)
6. Verify disk order hasn't broken GRUB: reboot and confirm Ubuntu boots
7. Import pool: `zpool import pool` (ZFS metadata is on the disks, no data loss)
8. Verify: `zpool status`, `zfs list`
9. Configure ZFS auto-import: `zpool set cachefile=/etc/zfs/zpool.cache pool`
10. Set up basic monitoring: smartmontools, node-exporter

### Phase 4: Join Kubernetes Cluster

1. Add node `k8s-storage-01` to ansible inventory (`ansible/inventory/production.yaml`)
   as agent (first worker node), assign new IP from cluster range
2. Run k3s agent installation via ansible
3. Verify node joins: `kubectl get nodes`
4. Label node: `kubectl label node k8s-storage-01 node.kubernetes.io/role=storage`
5. No taint -- this is a general-purpose worker node
6. Install OpenEBS ZFS LocalPV CSI driver (see "Storage Strategy" section)
7. Configure ZFS ARC memory limit (see "ZFS ARC Memory" section)

### Phase 5: Deploy Samba

1. Push Samba manifests to git
2. ArgoCD syncs and deploys Samba pod on the storage node
3. Samba pod mounts ZFS datasets via hostPath
4. Verify SMB access from macOS
5. Verify TimeMachine backup works
6. Update internal-dns if needed (truenas.home.lex.la or new hostname)

### Phase 6: Migrate NFS Workloads

Migrate one at a time, verify after each:

1. **etcd-backup**: change PVC to zfs-localpv or Longhorn
2. **VMSingle**: change PVC to zfs-localpv (benefits from ZFS compression)
3. **Transmission config**: change PVC to zfs-localpv or Longhorn
4. **Transmission downloads**: hostPath on k8s-storage-01 (`/pool/transmission`)
5. **OpenBao**: already on longhorn-remote, verify no NFS dependency

### Phase 7: Decommission NFS

1. Remove truenas-nfs-csi StorageClass
2. Remove CSI NFS driver ArgoCD application
3. Remove old NFS PVs
4. Disable NFS on the node (no longer needed)
5. Remove NFS-related mount options from CLAUDE.md and docs

### Phase 8: Cleanup

1. Remove graphite-exporter (node-exporter replaces it)
2. Update monitoring dashboards
3. Update blackbox-exporter probe (if hostname/IP changes)
4. Update CLAUDE.md with new architecture
5. Remove TrueNAS-specific alerting rules

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| ZFS DKMS breaks on kernel update | Pool unavailable until rebuild | Pin kernel version, test updates in staging, keep previous kernel in GRUB |
| Samba container crash | SMB unavailable | k8s auto-restart, Deployment with liveness probe on port 445 |
| TimeMachine instability in container | Backup corruption | Test thoroughly before go-live, keep TrueNAS config backup for rollback |
| hostPath security | Pod access to host filesystem | nodeAffinity pins to storage node, RBAC restricts who can deploy hostPath |
| ZFS pool import fails | Data loss | Snapshot before migration, keep TrueNAS boot media as rollback |
| **pool/Lex data loss** | **Irreplaceable (photo archive, personal files)** | **External backup BEFORE migration, ZFS snapshot, verify after every step** |
| Correlated disk failure (4x same-model WD Red) | Pool loss, all data gone | RAIDZ1 survives only 1 disk -- set up off-site backup independently of migration |
| Node resource contention | Samba I/O affects k8s workloads | Resource limits on Samba pod, ZFS ARC limit (`zfs_arc_max`) |
| USB flash failure | Node won't boot (GRUB on USB) | Keep spare USB with GRUB installed, document reinstall procedure |
| Disk order change after reconnecting HDD | GRUB `hd1` chainloader points to wrong disk | Use UUID-based root in grub.cfg, not positional device names |

## Rollback Plan

At any point before Phase 7 (NFS decommission):

1. Stop Samba pod
2. Disconnect data disks
3. Reinstall TrueNAS SCALE on boot SSD
4. Import pool (same `zpool import pool` -- works both ways)
5. Restore TrueNAS config from backup
6. Re-enable NFS shares
7. k8s workloads reconnect via existing NFS PVs (still in git)

## Decisions Made

- [x] **Node hostname**: `k8s-storage-01`
- [x] **IP address**: 172.16.101.4 (next in k8s node range after .1/.2/.3)
- [x] **Storage strategy**: OpenEBS ZFS LocalPV (Longhorn incompatible with ZFS, see above)
- [x] **ZFS ARC memory**: 8 GB (tunable at runtime)
- [x] **Taint strategy**: no taint, first general-purpose worker node
- [x] **Transmission downloads**: hostPath (`/pool/transmission`)
- [x] **Samba image**: built in this repo, pushed to `ghcr.io/lexfrei/samba`
- [x] **SMART monitoring**: works directly via `smartctl /dev/sdX` (mpt3sas SAT passthrough), no special flags needed

## Open Questions

- [ ] **pool/Lex backup strategy**: what off-site backup to set up? (Backblaze B2, rsync to another machine, zfs send to remote) -- urgent regardless of migration, owner is doing this independently

## References

### SMB / TimeMachine

- [mbentley/docker-timemachine](https://github.com/mbentley/docker-timemachine) -- analyzed, trivially reproducible
- [ServerContainers/samba](https://github.com/ServerContainers/samba) -- analyzed, over-engineered for our use
- [samba-in-kubernetes/samba-operator](https://github.com/samba-in-kubernetes/samba-operator) -- CRD-based, overkill for 4 shares
- [Samba vfs_fruit docs](https://www.samba.org/samba/docs/current/man-html/vfs_fruit.8.html) -- official fruit VFS documentation

### Storage

- [OpenEBS ZFS LocalPV](https://github.com/openebs/zfs-localpv) -- chosen CSI driver
- [Longhorn ZFS incompatibility #5106](https://github.com/longhorn/longhorn/issues/5106) -- FIEMAP not supported
- [Longhorn ZFS incompatibility #11125](https://github.com/longhorn/longhorn/issues/11125) -- confirmed by maintainer
- [Kubernetes and Longhorn on ZFS (zvol workaround)](https://scvalex.net/posts/49/) -- anti-pattern documented
- [democratic-csi](https://github.com/democratic-csi/democratic-csi) -- considered, rejected for local use
- [Bare Metal K8s: ZFS LocalPV + Longhorn](https://vadosware.io/post/bare-metal-k8s-storage-zfs-local-pv-with-rancher) -- dual StorageClass reference

### ZFS Management

- [45Drives cockpit-zfs](https://github.com/45Drives/cockpit-zfs) -- ZFS WebUI replacement
- [Poolsman](https://www.poolsman.com/) -- alternative ZFS WebUI for Cockpit

### Incident History

- `docs/postmortems/2025-11-14-nfs-dns-cilium-cascade-failure.md` -- NFS cascade failure incident
