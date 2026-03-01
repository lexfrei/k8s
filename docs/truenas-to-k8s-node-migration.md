# TrueNAS to Kubernetes Node Migration Plan

Status: **COMPLETE** (Phases 0-7 done, Phase 8 partially done)

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

Replace TrueNAS SCALE with a standard Linux (Ubuntu 25.10) installation on the same
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
No special driver installation needed for Ubuntu 25.10. The onboard BCM5720 (`tg3` driver)
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

### Datasets (post-migration)

| Dataset | Used | Purpose |
| --- | --- | --- |
| pool/transmission | 2.54 TB | Torrent downloads (static PV, shared RWX) |
| pool/lex | 857 GB | Personal files (static PV, Samba) |
| pool/dump | 766 GB | General dump (static PV, Samba) |
| pool/papermc-data | 6.91 GB | Minecraft server (static PV) |
| pool/pvc-* | dynamic | OpenBao, Transmission config, Loki, Samba passdb |

Old datasets destroyed: pool/k8s (NFS remnants), pool/TimeMachine (fresh dynamic PVC instead).

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

### Custom Images (**BUILT**)

Two custom images, built in this repo under `images/`:

**`ghcr.io/lexfrei/samba:latest`** (`images/samba/Containerfile`):
- Alpine 3.23 + samba-server + samba-common-tools + jq (all pinned)
- Entrypoint reads `/etc/samba/users.json` (sambacc format), creates system users
  and samba passdb entries with explicit UID/GID
- passdb backend: tdbsam (persistent across restarts via PV)
- idmap backend: autorid (stable SID→UID mapping, range 10000-99999)
- smb.conf externalized to ConfigMap (not baked in image)
- Capabilities: NET_BIND_SERVICE, SETUID, SETGID (drop ALL others)

**`ghcr.io/lexfrei/avahi:latest`** (`images/avahi/Containerfile`):
- Alpine 3.23 + avahi + dbus (all pinned)
- Entrypoint starts dbus-daemon then avahi-daemon (--no-drop-root --no-rlimits)
- avahi-daemon.conf and service files from ConfigMap
- Capabilities: NET_BIND_SERVICE, NET_RAW, CHOWN, DAC_OVERRIDE, SETUID, SETGID

**User management** uses sambacc JSON format in OpenBao (`secret/samba/users`):
```json
{
  "users": {
    "all_entries": [
      {"name": "lex", "uid": 1000, "gid": 1000, "password": "..."},
      {"name": "daria", "uid": 1001, "gid": 1001, "password": "..."}
    ]
  }
}
```
Synced to k8s Secret via ExternalSecret, mounted as file. Passwords never in env vars.

**smb.conf** (in `manifests/samba/configmap.yaml`): tdbsam + autorid, fruit VFS for
macOS/TimeMachine, guest access on Dump and Transmission, SMB2 minimum protocol.

### Avahi mDNS Discovery (**IMPLEMENTED**)

Avahi runs as a sidecar container with `hostNetwork: true`, advertising:

- `_smb._tcp` -- SMB server discovery in Finder
- `_device-info._tcp` + model=TimeCapsule8,119 -- TimeCapsule icon in Finder
- `_adisk._tcp` + `adVF=0x82` -- Time Machine volume discovery

**Requirements:**
- `hostNetwork: true` on the pod (mDNS multicast needs LAN access)
- Host avahi-daemon disabled on all nodes (conflicts on port 5353).
  Handled by ansible role `node-prep/tasks/disable-avahi.yml`
- avahi-daemon.conf restricted to `allow-interfaces=eth0` (no lxc/cilium interfaces)
- Service files mounted from ConfigMap (overrides default ssh/sftp services)

### Privileges Required

Both containers drop ALL capabilities and add only what's needed:

| Container | Capabilities | Why |
| --- | --- | --- |
| Samba | NET_BIND_SERVICE | Bind port 445 |
| Samba | SETUID, SETGID | Impersonate SMB users for file access (samba core requirement) |
| Avahi | NET_BIND_SERVICE | Bind port 5353 |
| Avahi | NET_RAW | mDNS multicast |
| Avahi | CHOWN, DAC_OVERRIDE | Runtime directory creation |
| Avahi | SETUID, SETGID | dbus/avahi privilege management |

**No privileged mode.** hostNetwork is required for mDNS but does not grant
additional privileges beyond network namespace sharing.

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
    ├─ k8s-storage-01 (ex-TrueNAS, 172.16.101.4):
    │   ├─ ZFS pool (imported)
    │   ├─ OpenEBS ZFS LocalPV CSI driver
    │   ├─ Static PVs: pool/lex, pool/dump, pool/timemachine, pool/transmission
    │   ├─ Dynamic PVs: pool/pvc-* (etcd-backup, VMSingle, etc.)
    │   ├─ Samba pod (PVC-mounted ZFS datasets, LoadBalancer IP)
    │   ├─ Transmission pod (PVC for downloads + PVC for config)
    │   └─ node-exporter, kubelet (standard monitoring)
    │
    ├─ Migrated workloads (NFS → ZFS LocalPV):
    │   ├─ etcd-backup → zfs-localpv PVC (dynamic)
    │   ├─ VMSingle → zfs-localpv PVC (dynamic, benefits from ZFS compression)
    │   ├─ Transmission config → zfs-localpv PVC (dynamic)
    │   ├─ Transmission downloads → zfs-localpv PVC (static import of pool/transmission)
    │   └─ OpenBao → longhorn-remote PVC (unchanged)
    │
    └─ macOS ──SMB──> Samba pod (LoadBalancer IP)
```

**Zero hostPath.** All storage access goes through CSI PVCs — existing datasets are
imported via static provisioning, new volumes are created dynamically. This gives
proper k8s lifecycle management, VolumeSnapshot support, and `kubectl get pvc` visibility.

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
pool                                          /pool                       ~4540 GB
├── dump                                      /pool/dump                    823 GB
├── lex                                       /pool/lex                     920 GB
├── pvc-<uuid>                                (legacy mountpoint)          2048 GB  ← new TimeMachine (dynamic PVC)
└── transmission                              /pool/transmission           2793 GB
```

Flat, lowercase, no wrappers. `pool/k8s` deleted (NFS goes away).
`pool/TimeMachine` destroyed (old backup data discarded, fresh PVC created with 2 Ti quota).

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

# Destroy old TimeMachine data (will be recreated as fresh dynamic PVC)
zfs destroy -r pool/TimeMachine

# Lowercase remaining datasets
zfs rename pool/Dump pool/dump
zfs rename pool/Transmission pool/transmission

# Delete NFS dataset (only after all NFS workloads migrated to Longhorn!)
# zfs destroy pool/k8s  # uncomment when ready

# Set clean mountpoints (optional, defaults to /pool/<name>)
zfs set mountpoint=/pool/lex pool/lex
zfs set mountpoint=/pool/dump pool/dump
# pool/timemachine no longer exists (destroyed above, recreated as dynamic PVC)
zfs set mountpoint=/pool/transmission pool/transmission

# Verify
zfs list -o name,mountpoint,used,available
```

### Volume Mapping After Restructuring

Each ZFS dataset becomes a static PV via OpenEBS ZFS LocalPV (see "Importing existing
datasets" section above). Pods mount PVCs, not hostPath.

| Dataset | PV Name | PVC | Consumer(s) | Access Mode |
| --- | --- | --- | --- | --- |
| pool/lex | pool-lex | samba-lex | Samba | RWO |
| pool/dump | pool-dump | samba-dump | Samba | RWO |
| pool/timemachine | *(dynamic)* | samba-timemachine | Samba | RWO |
| pool/transmission | pool-transmission | transmission-data | Samba + Transmission | **RWX** (shared) |

**pool/transmission is shared** between Samba (read-only SMB access) and Transmission
(write downloads). Requires `shared: "yes"` in the ZFSVolume CR and `ReadWriteMany`
access mode on the PV. Both pods run on k8s-storage-01.

**pool/timemachine is NOT imported** — old backup data is discarded. A fresh dynamic
PVC (2 Ti quota) is created instead. TimeMachine will start a new full backup.

In the Samba container, PVCs are mounted as `/data/*`:

```yaml
volumeMounts:
  - name: timemachine
    mountPath: /data/timemachine
  - name: lex
    mountPath: /data/lex
  - name: dump
    mountPath: /data/dump
  - name: transmission
    mountPath: /data/transmission
    readOnly: true               # Samba serves Transmission read-only
volumes:
  - name: timemachine
    persistentVolumeClaim:
      claimName: samba-timemachine
  - name: lex
    persistentVolumeClaim:
      claimName: samba-lex
  - name: dump
    persistentVolumeClaim:
      claimName: samba-dump
  - name: transmission
    persistentVolumeClaim:
      claimName: transmission-data
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

**Importing existing datasets (static provisioning):**

ZFS LocalPV supports importing pre-existing ZFS datasets as PVs. This is how
Samba and Transmission get their existing data without hostPath:

1. Set mountpoint to legacy: `zfs set mountpoint=legacy pool/lex`
2. Create a `ZFSVolume` CR (registers the dataset with the CSI driver)
3. Create a `PersistentVolume` with `csi.volumeHandle: pool/lex`
4. Create a `PersistentVolumeClaim` bound to that PV
5. Pod mounts the PVC — CSI driver mounts the ZFS dataset

Example for `pool/lex`:

```yaml
# 1. ZFSVolume CR (registers dataset with CSI driver)
apiVersion: zfs.openebs.io/v1
kind: ZFSVolume
metadata:
  name: pool-lex
  namespace: openebs
  finalizers: []              # no finalizer = CSI won't destroy dataset on PV delete
spec:
  capacity: "1099511627776"   # 1 TB (informational, ZFS uses actual dataset size)
  fsType: zfs
  ownerNodeID: k8s-storage-01
  poolName: pool
  volumeType: DATASET
status:
  state: Ready
---
# 2. PersistentVolume
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pool-lex
spec:
  capacity:
    storage: 1Ti
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain    # CRITICAL: never delete the dataset
  storageClassName: zfs-localpv
  csi:
    driver: zfs.csi.openebs.io
    fsType: zfs
    volumeHandle: pool-lex
    volumeAttributes:
      openebs.io/poolname: pool
  nodeAffinity:
    required:
      nodeSelectorTerms:
        - matchExpressions:
            - key: kubernetes.io/hostname
              operator: In
              values: [k8s-storage-01]
---
# 3. PersistentVolumeClaim
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: samba-lex
  namespace: samba
spec:
  storageClassName: zfs-localpv
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Ti
  volumeName: pool-lex       # binds to specific PV
```

**IMPORTANT:** `persistentVolumeReclaimPolicy: Retain` and no finalizer on ZFSVolume
ensure that deleting the PVC/PV will **never** destroy the underlying ZFS dataset.

Same pattern for the 3 imported datasets: pool/lex, pool/dump, pool/transmission.
TimeMachine is a fresh dynamic PVC (old data discarded, new 2 Ti quota).

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

### Phase 1: Build Samba Container Image (**DONE**)

1. ~~Create repo `lexfrei/samba` (or add to existing charts/images repo)~~
   Images built in this repo: `images/samba/` and `images/avahi/`
2. ~~Build minimal image~~
   - `ghcr.io/lexfrei/samba:latest` -- Alpine + samba-server + jq, sambacc-style
     JSON user management with tdbsam + autorid UID/GID mapping
   - `ghcr.io/lexfrei/avahi:latest` -- Alpine + avahi + dbus, mDNS discovery sidecar
3. ~~Multi-arch build~~ ARM64 built via Colima, amd64 to be added for storage node
4. ~~Push to GHCR~~
5. ~~Test in cluster~~ PoC validated: discovery (Bonjour/Finder), login (lex + daria),
   guest shares (Dump, Transmission). hostNetwork + minimal capabilities.

**Samba architecture (validated in PoC):**
- User definitions in sambacc JSON format, stored in OpenBao (`secret/samba/users`),
  synced via ExternalSecret to k8s Secret, mounted as `/etc/samba/users.json`
- Entrypoint parses JSON with jq, creates system users + samba passdb entries
- Two users configured: lex (uid 1000) and daria (uid 1001)
- Avahi sidecar for mDNS: `_smb._tcp`, `_device-info._tcp` (TimeCapsule icon),
  `_adisk._tcp` (Time Machine discovery). Config from ConfigMap.
- Host avahi-daemon disabled on all cluster nodes (ansible role `disable-avahi`)

### Phase 2: Prepare k8s Manifests

1. Create `manifests/openebs-zfs/` with:
   - StorageClass `zfs-localpv`
   - Static PVs + ZFSVolume CRs for existing datasets (lex, dump, timemachine, transmission)
   - pool/transmission ZFSVolume with `shared: "yes"` for RWX
2. Create `argocd/infra/openebs-zfs.yaml` (Helm chart + manifests)
3. Create `manifests/samba/` with:
   - Deployment (nodeAffinity to k8s-storage-01)
   - Service (LoadBalancer, Cilium L2 IPAM)
   - ConfigMap (smb.conf)
   - ExternalSecret (SMB password from OpenBao)
   - PVCs bound to static PVs (lex, dump, timemachine, transmission)
4. Create `argocd/workloads/samba.yaml`
5. Update Transmission manifests:
   - Downloads PVC → bound to static PV `pool-transmission` (RWX shared with Samba)
   - Config PVC → dynamic `zfs-localpv` PVC
6. Prepare dynamic PVC manifests for migrated NFS workloads (etcd-backup, VMSingle)

### Phase 3: Install Ubuntu on TrueNAS Hardware (**DONE**)

**Boot chain:** The server uses Legacy BIOS and cannot boot from the SSD directly
(ata-6 ODD port). USB DataTraveler acts as boot relay with GRUB chainloading to SSD.
All disks stay connected during installation -- disk order is stable (determined by
PCI controller addresses, not cable order).

**Current USB GRUB** (`sdb1:/boot/grub/grub.cfg`):
```text
set root=(hd1)
chainloader +1
```
This positional `hd1` reference works because disk order hasn't changed. After Ubuntu
install, `hd1` still points to SSD (same controllers, same ports). UUID-based search
can be added later as a safety improvement but is not required.

**Installation steps:**

1. Boot from Ubuntu installer USB via iLO virtual media (or swap DataTraveler
   temporarily for installer USB)
2. Install Ubuntu 25.10 on SSD (sdf):
   - Root filesystem on SSD
   - **Install GRUB bootloader to SSD (`/dev/sdf`)**, NOT to USB
   - The existing USB GRUB chainloads to SSD MBR -- after install it will
     chainload Ubuntu's GRUB instead of TrueNAS bootloader
3. Restore DataTraveler if removed, reboot -- verify full boot chain:
   BIOS → USB GRUB → `chainloader +1` → SSD GRUB → Ubuntu kernel
4. Install ZFS: `apt install --assume-yes zfsutils-linux`
5. Import pool: `zpool import pool` (ZFS metadata is on disks, no data loss)
6. Verify: `zpool status`, `zfs list`
7. Configure ZFS auto-import: `zpool set cachefile=/etc/zfs/zpool.cache pool`
8. Set up basic monitoring: smartmontools, node-exporter
9. Configure network: static IP 172.16.101.4/16, gateway, DNS

**Learnings:**

- `intel_iommu=off` kernel parameter required -- without it, SATA link errors appear
  on the Intel AHCI controller (ata-6). With it: 0 errors, stable 3.0 Gbps link
- ZFS `zfs_experimental_recv` module parameter appeared but is harmless (TrueNAS
  leftover in pool metadata, does not affect operation)
- ZFS ARC configured to 8 GB via `/etc/modprobe.d/zfs.conf`, verified working

### Phase 4: Join Kubernetes Cluster (**DONE**)

1. Add node `k8s-storage-01` to ansible inventory (`ansible/inventory/production.yaml`)
   as agent (first worker node), assign new IP from cluster range
2. Run k3s agent installation via ansible: `ansible-playbook k3s.orchestration.site --limit k8s-storage-01`
3. Verify node joins: `kubectl get nodes` -- k8s-storage-01 Ready
4. Label node: `kubectl label node k8s-storage-01 node.kubernetes.io/role=storage`
5. No taint -- this is a general-purpose worker node
6. Install OpenEBS ZFS LocalPV CSI driver via ArgoCD (`argocd/infra/openebs-zfs.yaml`)
7. Configure ZFS ARC memory limit (see "ZFS ARC Memory" section)

**Learnings:**

- Ansible `--limit k8s-storage-01` safely runs only prep + agent install, server nodes untouched
- Node joined as k3s agent (v1.35.0+k3s3) with kernel 6.17.0-14-generic (x86_64)

### Phase 5: Deploy Samba + Import Existing Datasets (PARTIAL)

**Done:**

1. Set `mountpoint=legacy` on datasets (lex, dump, transmission)
2. OpenEBS ZFS LocalPV deployed (StorageClass, static PVs, ZFSVolume CRs)
3. Static PVs bound correctly (lex, dump, transmission, papermc-data)
4. Custom images built: `ghcr.io/lexfrei/samba`, `ghcr.io/lexfrei/avahi`
5. ConfigMap (`manifests/samba/configmap.yaml`) and ExternalSecret (`manifests/samba/external-secret.yaml`) created
6. PoC validated: mDNS discovery, SMB login, guest shares

**Remaining:**

- [ ] Create `manifests/samba/deployment.yaml` (hostNetwork, Avahi sidecar, volume mounts)
- [ ] Create `manifests/samba/pvcs.yaml` (samba-lex, samba-dump, samba-timemachine, samba-transmission, samba-passdb)
- [ ] Create `argocd/workloads/samba.yaml` (ArgoCD Application)
- [ ] Verify production deployment (mDNS, SMB access, TimeMachine)

**Samba architecture (planned):**
- `hostNetwork: true` -- no LoadBalancer Service needed, SMB on port 445 at node IP
- Avahi sidecar for mDNS discovery
- PVCs: samba-lex (static), samba-dump (static), samba-transmission (static, read-only),
  samba-timemachine (dynamic zfs-localpv, 2Ti), samba-passdb (dynamic, 1Gi)

### Phase 6: Migrate NFS Workloads (**DONE**)

All NFS workloads migrated:

1. **Transmission downloads**: static PV `zfs-transmission` (RWX, shared with Samba)
2. **Transmission config**: dynamic `zfs-localpv` PVC on k8s-storage-01
3. **etcd-backup**: migrated to `longhorn-remote` (runs on cp nodes, zfs-localpv not available there)
4. **VMSingle**: migrated to `zfs-localpv` on k8s-storage-01
5. **Loki**: migrated to `zfs-localpv` on k8s-storage-01
6. **PaperMC**: migrated to `zfs-localpv` static PV `zfs-papermc-data` on k8s-storage-01 (9.7GB data copied without loss)
7. **OpenBao**: migrated to `zfs-localpv` on k8s-storage-01

**Additional migrations beyond original plan:**

- Loki and VMSingle moved from longhorn to zfs-localpv (data loss accepted, fresh start)
- PaperMC moved from longhorn on k8s-cp-02 to zfs-localpv on k8s-storage-01 (data preserved via tar pipe copy)
- OpenBao moved from longhorn-remote to zfs-localpv

**Learnings:**

- StatefulSet `volumeClaimTemplates` are immutable -- must delete StatefulSet before changing storageClass
- ArgoCD sync must be disabled on BOTH meta and target app before destructive operations, re-enable ONLY meta at the end
- Static PV with `claimRef` must exist BEFORE StatefulSet creates PVC, otherwise PVC binds to dynamic provisioner
- Cilium L2 announcement `nodeSelector` must match the node where pod runs -- after moving PaperMC, the old `minecraft=true` label had to be replaced with `kubernetes.io/hostname: k8s-storage-01` in L2 policy
- `externalTrafficPolicy: Local` means traffic only reaches the node where pod is scheduled
- Busybox tar is unreliable for pipe operations (EOF errors), use alpine for data copy jobs

### Phase 7: Decommission NFS (**DONE**)

1. `csi-driver-nfs` ArgoCD application moved to `argocd-disabled/`
2. `longhorn` set as default StorageClass
3. `pool/k8s` ZFS dataset destroyed (660MB of old NFS PVC data, all already migrated)
4. TrueNAS (172.16.10.19) no longer exists -- server is now k8s-storage-01 (172.16.101.4)

### Phase 8: Cleanup (PARTIAL)

Remaining:

1. [ ] Remove graphite-exporter (node-exporter replaces it)
2. [ ] Update monitoring dashboards
3. [ ] Update blackbox-exporter probe (if hostname/IP changes)
4. [ ] Update CLAUDE.md with new architecture (remove NFS references, add storage-01)
5. [ ] Remove TrueNAS-specific alerting rules
6. [ ] Fix extractedprism chart appVersion `v` prefix (GHCR tags lack `v` prefix)

## Risks and Mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| ZFS module breaks on kernel update | Pool unavailable until rebuild | Ubuntu ships ZFS as native package (not DKMS), keep previous kernel in GRUB as fallback. Plan: migrate all nodes to 26.04 LTS when available |
| Samba container crash | SMB unavailable | k8s auto-restart, Deployment with liveness probe on port 445 |
| TimeMachine instability in container | Backup corruption | Test thoroughly before go-live, keep TrueNAS config backup for rollback |
| ZFS LocalPV CSI driver failure | Pods can't mount storage | CSI driver is a DaemonSet with auto-restart, ZFS datasets survive driver restarts |
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
- [x] **Transmission downloads**: ZFS LocalPV static PV (RWX shared with Samba)
- [x] **Samba image**: built in this repo, pushed to `ghcr.io/lexfrei/samba`
- [x] **Avahi image**: sidecar for mDNS discovery, pushed to `ghcr.io/lexfrei/avahi`
- [x] **User management**: sambacc JSON format, OpenBao → ExternalSecret → file mount
- [x] **passdb backend**: tdbsam with autorid (stable UID mapping, multi-user ready)
- [x] **Guest shares**: Dump and Transmission accessible without login
- [x] **mDNS discovery**: Avahi sidecar with hostNetwork, host avahi disabled via ansible
- [x] **Two users**: lex (uid 1000) and daria (uid 1001), passwords in OpenBao
- [x] **Boot chain**: USB GRUB chainloads to SSD, positional `hd1` (works, UUID later)
- [x] **SMART monitoring**: works directly via `smartctl /dev/sdX` (mpt3sas SAT passthrough), no special flags needed

## Open Questions

- [ ] **pool/Lex backup strategy**: what off-site backup to set up? (Backblaze B2, rsync to another machine, zfs send to remote) -- urgent regardless of migration, owner is doing this independently

## References

### SMB / TimeMachine

- [mbentley/docker-timemachine](https://github.com/mbentley/docker-timemachine) -- analyzed, trivially reproducible
- [ServerContainers/samba](https://github.com/ServerContainers/samba) -- analyzed, over-engineered for our use
- [samba-in-kubernetes/samba-operator](https://github.com/samba-in-kubernetes/samba-operator) -- CRD-based, amd64-only, v0.8 alpha, minimal maintenance
- [samba-in-kubernetes/sambacc](https://github.com/samba-in-kubernetes/sambacc) -- JSON user format borrowed for our entrypoint
- [Samba vfs_fruit docs](https://www.samba.org/samba/docs/current/man-html/vfs_fruit.8.html) -- official fruit VFS documentation

### Storage

- [OpenEBS ZFS LocalPV](https://github.com/openebs/zfs-localpv) -- chosen CSI driver
- [ZFS LocalPV: Import Existing Volume](https://github.com/openebs/zfs-localpv/blob/develop/docs/import-existing-volume.md) -- static provisioning for existing datasets
- [ZFS LocalPV: Shared Volumes](https://github.com/openebs/zfs-localpv/issues/152) -- RWX support via `shared: "yes"`
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
