# Post-Mortem: NFS → DNS → Cilium Cascade Failure

**Date**: 2025-11-14
**Duration**: ~10 minutes (02:57 - 03:07 UTC)
**Severity**: Critical (Complete cluster network failure)
**Status**: Resolved

---

## Summary

A cascade failure caused by NFS hard mount blocking led to complete cluster network failure. The incident chain: **NFS hard mount hang → DNS resolution failure → Cilium eBPF breakdown → network collapse → SSH inaccessible**.

---

## Timeline (UTC)

| Time | Event |
| ---- | ----- |
| 02:54:55 | NFS write operations begin blocking (`nfs_write_begin` in D state) |
| 02:57:18 | NFS server `truenas.home.lex.la` stops responding |
| 02:58:40 | **DNS resolution fails** - all image pulls fail with `lookup quay.io: Try again`, `lookup ghcr.io: Try again` |
| 03:01:15 | Cilium pods fail to mount volumes - configmaps/secrets not accessible |
| 03:02:41 | **Cilium CNI completely fails** - `[PUT /endpoint/{id}][429] putEndpointIdTooManyRequests` |
| 03:02:52 | Mass pod creation failures - no network sandboxes can be created |
| ~03:05 | **Network completely down** - SSH inaccessible, cluster unresponsive |
| ~03:07 | Hardware watchdog triggers system reboot (120s timeout) |
| 19:35:04 | System recovered after reboot, services restored |

---

## Root Cause Analysis

### Primary Root Cause

**NFS hard mount configuration** caused kernel-level blocking when NFS server became unavailable.

```yaml
# BEFORE (Dangerous Configuration)
mountOptions:
  - hard          # ← CRITICAL ISSUE
  - timeo=600     # 60 seconds timeout
  - nfsvers=4.1
```

Hard mount behavior:

- Kernel retries I/O operations **indefinitely** when NFS server unavailable
- Processes enter **D state (uninterruptible sleep)** - cannot be killed
- Multiple processes pile up in D state, cascading to system freeze
- Watchdog cannot prevent D state hangs - kernel-level freeze

### Cascade Failure Chain

```text
NFS Hard Mount Hang
      ↓
Loki Pod Blocked (writes to NFS)
      ↓
DNS Resolution Fails (CoreDNS or systemd-resolved blocked?)
      ↓
Cilium Cannot Resolve External Resources
      ↓
Cilium eBPF Regeneration Fails
      ↓
Cilium API Overloaded (429 TooManyRequests)
      ↓
No Network Sandboxes Can Be Created
      ↓
Complete Network Failure
      ↓
SSH Inaccessible
      ↓
Watchdog Triggers Hard Reboot
```

### Why DNS Failed

Evidence from logs:

```log
Nov 14 02:58:40 k8s-cp-01 k3s[1224]: dial tcp: lookup quay.io: Try again
Nov 14 02:58:40 k8s-cp-01 k3s[1224]: dial tcp: lookup ghcr.io: Try again
```

**Hypothesis**: DNS resolution (systemd-resolved or CoreDNS) was blocked waiting on NFS I/O, or DNS pods themselves were affected by Cilium failure. Without DNS:

- Cilium cannot reach external services
- Image pulls fail completely
- Cluster enters degraded state

### Why Cilium Failed Without DNS

Cilium with kube-proxy replacement and eBPF relies on:

1. **eBPF map updates** for network policy and routing
2. **Endpoint creation** for new pods
3. **External connectivity** for health checks and updates

When DNS fails:

1. Cilium agent cannot resolve external endpoints
2. eBPF program regeneration may fail
3. Endpoint creation API becomes overloaded (429 errors)
4. All new pods fail to get network - complete networking breakdown

```log
Nov 14 03:02:41 plugin type="cilium-cni" failed (add): unable to create endpoint:
[PUT /endpoint/{id}][429] putEndpointIdTooManyRequests
```

---

## Evidence

### NFS Hard Mount Blocking

```log
Nov 14 02:54:55 kernel: nfs_write_begin+0xa8/0x590 [nfs]
Nov 14 02:54:55 kernel: nfs_file_write+0x1d0/0x320 [nfs]
Nov 14 02:57:18 kernel: nfs: server truenas.home.lex.la not responding, still trying
Nov 14 02:59:00 kernel: INFO: task loki:25860 blocked for more than 368 seconds.
                        State: D (uninterruptible sleep)
```

Loki process entered D state for 368+ seconds trying to write to NFS.

### DNS Resolution Failure

```log
Nov 14 02:58:40 k8s-cp-01 k3s[1224]: failed to pull image:
  dial tcp: lookup quay.io: Try again

Nov 14 02:58:41 k8s-cp-01 k3s[1224]: failed to pull image:
  dial tcp: lookup ghcr.io: Try again
```

All external DNS resolution failing with "Try again" error.

### Cilium CNI Failure

```log
Nov 14 03:02:41 k8s-cp-01 k3s[1231]: plugin type="cilium-cni" failed (add):
  unable to create endpoint: [PUT /endpoint/{id}][429] putEndpointIdTooManyRequests

Nov 14 03:02:52 k8s-cp-01 k3s[1231]: Failed to create sandbox for pod:
  plugin type="cilium-cni" failed (add): unable to create endpoint
```

Cilium API returned HTTP 429 (Too Many Requests) - completely overloaded.

### Mass Pod Creation Failures

```log
Nov 14 03:02:41 cert-manager/cert-manager-webhook: CreatePodSandboxError
Nov 14 03:02:52 monitoring/grafana-operator: CreatePodSandboxError
Nov 14 03:02:52 argo/argo-workflows-workflow-controller: CreatePodSandboxError
Nov 14 03:02:52 security/policy-reporter-ui: CreatePodSandboxError
```

No pods could be created - complete networking failure.

---

## Resolution

### Immediate Fix (Manual)

System automatically rebooted via hardware watchdog after 120 seconds of unresponsiveness.

### Permanent Fix (Implemented)

Changed NFS StorageClass from **hard mount** to **soft mount** in `manifests/csi-driver-nfs/truenas-sc.yaml`:

```yaml
# AFTER (Safe Configuration)
mountOptions:
  - nfsvers=4.1
  - soft          # ← FIXED: Returns error instead of hanging
  - timeo=100     # ← FIXED: 10 seconds (faster failure detection)
  - retrans=2
  - tcp
```

**Soft mount behavior**:

- Returns I/O error (EIO) after timeout
- Application crashes/restarts, but kernel remains responsive
- System continues functioning, only affected pod fails
- Node stays operational - no SSH lockout

Added **fallback DNS servers** via systemd-resolved configuration in `/etc/systemd/resolved.conf.d/fallback.conf`:

```ini
[Resolve]
DNS=172.16.0.1
FallbackDNS=8.8.8.8 1.1.1.1
```

**Fallback DNS behavior**:

- Primary DNS: 172.16.0.1 (local network DNS)
- Fallback to Google DNS (8.8.8.8) and Cloudflare DNS (1.1.1.1) if primary fails
- Prevents complete DNS resolution failure when local DNS is unavailable
- Ensures external connectivity for critical services (Cilium, image pulls)

### Additional Fixes Required

**Existing PersistentVolumes do NOT automatically inherit StorageClass changes**:

```bash
# Manual fix required for each existing PV
kubectl edit pv PV_NAME
# Update mountOptions manually

# Recreate pods to apply new mount
kubectl delete pod POD_NAME
```

---

## Impact

- **Duration**: ~10 minutes of complete cluster unavailability
- **Services affected**: ALL cluster services (complete outage)
- **Data loss**: None (etcd on local storage)
- **External impact**: Public services (eta.lex.la, job.lex.la, map.lex.la, aleksei.sviridk.in) were unavailable
- **SSH access**: Completely blocked - no remote recovery possible
- **Recovery method**: Hardware watchdog automatic reboot

---

## Lessons Learned

### What Went Well

✅ **Hardware watchdog worked correctly** - system automatically rebooted after detecting freeze
✅ **etcd on local storage** - no data loss from cluster state
✅ **Post-reboot recovery was automatic** - all services came back up cleanly

### What Went Wrong

❌ **NFS hard mount in production** - critical infrastructure dependency with blocking behavior
❌ **No NFS availability monitoring** - server unavailability went undetected
❌ **Cascade failure not anticipated** - DNS → Cilium → network collapse
❌ **SSH became inaccessible** - no remote recovery possible, required physical access mindset

### Action Items

#### Completed

- [x] Change all NFS StorageClasses to soft mount (2025-11-14)
- [x] Reduce NFS timeout from 60s to 10s for faster failure detection (2025-11-14)
- [x] Add fallback DNS servers (8.8.8.8, 1.1.1.1) via systemd-resolved (2025-11-21)
- [x] Document NFS hard mount danger in CLAUDE.md Error 14 (2025-11-14)
- [x] Created Ansible role for node preparation (2025-11-21)
- [x] Migrated Argo Workflows configs to Ansible (2025-11-21)

#### Pending

- [ ] Migrate critical workloads away from NFS to Longhorn/local storage
  - Priority: Loki, monitoring stack, databases
- [ ] Add NFS server availability monitoring and alerting
- [ ] Test NFS failure scenarios in staging
- [ ] Add pre-reboot SSH notification mechanism
- [ ] Consider serial console access for future incidents
- [ ] Audit all PersistentVolumes for hard mount configuration
- [ ] Implement graceful degradation for DNS failures
- [ ] Add Cilium health metrics and alerting

---

## Related Documentation

- **CLAUDE.md Error 14**: NFS Hard Mount Causing Node Freeze
- **values/cilium.yaml**: Cilium kube-proxy replacement configuration
- **manifests/csi-driver-nfs/truenas-sc.yaml**: NFS StorageClass configuration
- **Kubernetes kube-proxy replacement**: <https://docs.cilium.io/en/stable/network/kubernetes/kubeproxy-free/>

---

## Prevention

### Infrastructure

1. **Storage strategy**:
   - Critical services → Longhorn (distributed) or local storage
   - Bulk data → NFS with soft mount
   - Never use hard mount in production

2. **Network resilience**:
   - Monitor Cilium health metrics
   - Alert on DNS resolution failures
   - Test failure scenarios regularly

3. **Recovery mechanisms**:
   - Ensure hardware watchdog is enabled
   - Consider IPMI/serial console access
   - Document out-of-band recovery procedures

### Monitoring

1. **NFS metrics**:
   - Server availability
   - Mount point responsiveness
   - I/O wait times

2. **DNS health**:
   - Resolution success rate
   - Query latency
   - CoreDNS/systemd-resolved status

3. **Cilium metrics**:
   - Endpoint creation rate
   - API response codes (watch for 429)
   - eBPF program regeneration failures

---

## Appendices

### A. Technical Deep Dive: Why Hard Mount is Dangerous

**Hard mount** is designed for NFS reliability but is catastrophic in cloud-native environments:

```text
NFS Hard Mount Semantics:
- Retry forever (infinite timeout)
- Block calling process (D state)
- Cannot be interrupted by signals (even SIGKILL)
- Multiple processes cascade into D state
- Eventually freezes entire system
```

**D state (Uninterruptible Sleep)**:

- Process waiting for kernel I/O operation
- Cannot be killed or interrupted
- Shows as `D` in `ps` output
- Only way out: complete I/O or reboot

**Why it's worse in Kubernetes**:

- Many pods write to same NFS mount
- NFS unavailability affects multiple workloads simultaneously
- Cascades through dependencies (DNS → Cilium → all networking)
- Control plane can become affected

### B. Cilium kube-proxy Replacement Architecture

Cilium replaces kube-proxy with eBPF programs that:

- Intercept network packets at kernel level
- Implement service load balancing
- Enforce network policies
- Handle DNS responses

**Critical Cilium Dependencies**:

1. **DNS resolution** - for external health checks and service discovery
2. **Kubernetes API** - for watching service/endpoint changes
3. **eBPF subsystem** - for datapath programming

When DNS fails:

- Cilium agent cannot resolve external endpoints
- Health checks fail
- eBPF program regeneration may be blocked
- Endpoint creation API overloads (429 errors)
- New pods cannot get network interfaces

This is why **DNS failure → complete network collapse** in Cilium-based clusters.

### C. systemd-resolved vs CoreDNS

Our cluster uses **systemd-resolved** on nodes for host-level DNS, while **CoreDNS** provides cluster DNS for pods.

**Incident hypothesis**:

- systemd-resolved on control plane node may have been blocked by NFS I/O
- OR CoreDNS pods were affected by Cilium failure
- Both scenarios lead to DNS resolution failure
- Further investigation needed to determine exact failure point

### D. Hardware Watchdog Configuration

Current watchdog configuration (heartbeat-only mode):

```ini
# /etc/watchdog.conf
watchdog-device = /dev/watchdog
watchdog-timeout = 120    # Seconds before reboot
interval = 30             # Heartbeat check interval
```

**Why it worked**:

- Kernel freeze prevented watchdog heartbeats
- After 120 seconds without heartbeat, hardware reset triggered
- System rebooted and recovered

**Why it couldn't prevent the issue**:

- Watchdog detects hangs but cannot prevent D state
- D state is kernel-level - no userspace recovery possible
- Only solution is hardware reset

---

**Post-mortem compiled**: 2025-11-21
**Author**: Claude (via user request)
**Review status**: Initial draft
