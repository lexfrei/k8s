# Network Death Investigation - Raspberry Pi 5 Cluster

**Date of incident:** 2025-12-08
**Affected nodes:** k8s-cp-01, k8s-worker-01 (both Raspberry Pi 5 Model B Rev 1.0)
**OS:** Ubuntu 25.10 (Questing Quokka)
**Kernel:** 6.17.0-1004-raspi

**CRITICAL**: Problem affects BOTH nodes with identical symptoms (duck typing match).

## Timeline (Precise)

| Time (UTC) | Event |
|------------|-------|
| 05:09:56 | 1 RCU stall (5 hours BEFORE network death) |
| 10:20:03.988 | Last normal kernel log (apparmor audit) |
| 10:20:12.548 | **NETWORK DIED** - first k3s `context deadline exceeded` |
| 10:20:42.668 | `http2: client connection lost` (multiple watches fail) |
| 10:20:51.188 | `read tcp 172.16.101.2->172.16.101.1:6443: i/o timeout` |
| 10:21:34 | `rpc_check_timeout: 28 callbacks suppressed` |
| 13:00:04 | First NFS timeout logged (~3 hours after network death) |
| 13:17:21 MSK | Last kubelet heartbeat |
| 13:56:56 | RCU stall detected (3.5 hours AFTER network death) |
| ~14:01 | Last log entry, node completely hung |

**CRITICAL FINDING:**
- Network died between **10:20:03** and **10:20:12** (9 seconds window)
- **ZERO kernel messages** about eth0/macb/RP1/IRQ/DMA/PCIe during network death
- RCU stall came **3.5 hours AFTER** network death - consequence, not cause
- Network driver (macb) did NOT report any error

## Key Findings

### 1. No Error Logs at Network Death
- No `Link is Down` message from macb driver
- No PCIe errors
- No IRQ errors
- No DMA errors
- No thermal/voltage warnings
- Network simply stopped working silently between 10:20:03 and 10:21:29

### 2. rp1-pio Firmware - NOT the Cause

**Before EEPROM update** (k8s-worker-01):

```text
rp1-pio 1f00178000.pio: failed to contact RP1 firmware
rp1-pio 1f00178000.pio: probe with driver rp1-pio failed with error -2
```

**After EEPROM update to 2025-02-12** (k8s-worker-01):

```text
rp1-pio 1f00178000.pio: Created instance as pio0
```

**IMPORTANT**: rp1-pio is NOT related to network death:

- k8s-cp-01 had rp1-pio working from the start, but still experienced network death
- Both nodes have identical network death symptoms regardless of rp1-pio status
- rp1-pio is for Programmable I/O (GPIO, SPI, etc.), not ethernet

### 3. RCU Stalls
- Last 24 hours before incident: 8 RCU stalls
- CPU governor already set to `performance` (workaround for bug #2133877)
- RCU stalls are symptom, not cause

### 4. Related Bugs/Issues

- **Launchpad:** https://bugs.launchpad.net/ubuntu/+source/linux-raspi/+bug/2133877
- **Cilium:** https://github.com/cilium/cilium/issues/43198

Symptoms match:
- Raspberry Pi 5 + Ubuntu 25.10 + kernel 6.17
- Network dies without logging
- PHY link stays up (no "Link is Down")
- Only power cycle recovers
- RCU stalls correlate but don't cause

Workaround (`performance` governor) did NOT prevent network death.

## Hardware Info

```text
Model: Raspberry Pi 5 Model B Rev 1.0
Revision: 0x00d04170 (same on both nodes)
Ethernet: Cadence GEM (macb) via RP1 PCIe southbridge
PHY: Broadcom BCM54213PE
RP1 chip_id: 0x20001927
CNI: Cilium (tunnel mode VXLAN, kube-proxy replacement, Gateway API)
```

## Confirmed Affected Firmware Versions

**Problem reproduced on BOTH nodes with different EEPROM versions:**

| Node | EEPROM Version | RP1 Firmware | Problem Observed |
|------|----------------|--------------|------------------|
| k8s-cp-01 | 2025-05-08T14:13:17 | eb39cfd516f8c90628aa9d91f52370aade5d0a55 | Yes |
| k8s-worker-01 | 2024-09-23T13:02:56 | (not reported in logs) | Yes |

**Conclusion:** EEPROM/firmware version does NOT prevent the issue.

**After update (2025-12-08):**
- k8s-worker-01 updated to EEPROM 2025-02-12
- Awaiting observation period to confirm if issue persists

## Comparison: k8s-cp-01 vs k8s-worker-01

| Parameter | k8s-cp-01 | k8s-worker-01 |
|-----------|-----------|---------------|
| Hardware revision | 0x00d04170 | 0x00d04170 |
| rp1-pio (current) | ✅ Created instance as pio0 | ✅ Created instance as pio0 |
| EEPROM version | 2025-05-08 | 2025-02-12 |
| RP1 firmware hash | eb39cfd516f8c90628aa9d91f52370aade5d0a55 | eb39cfd516f8c90628aa9d91f52370aade5d0a55 |
| Network death observed | ✅ Yes - identical symptoms | ✅ Yes - identical symptoms |
| RCU stalls | Present | Present |

**Both nodes experience the SAME network death problem with identical symptoms.**

## Kernel Logs Before Death

**Last normal activity (10:15 - 10:20 UTC):**

- Only apparmor audit messages (container starts every 10 seconds)
- No errors, no warnings
- Last message: `10:20:03.988843` apparmor audit

**At 10:20:12.548 (network death):**

- First k3s error: `context deadline exceeded`
- **NO kernel messages** - network died silently

**After death (10:20:42+):**

- `http2: client connection lost` (dozens of watches fail simultaneously)
- `read tcp 172.16.101.2->172.16.101.1:6443: i/o timeout`

**Key observation:**

- eth0/macb driver logged NOTHING during or after network death
- Last macb kernel message was at **Dec 02** (boot time)
- No "Link is Down", no errors, no warnings - complete silence

## Hypotheses

1. **macb/GEM driver bug** - silent failure without logging (most likely)
2. **RP1 ethernet controller issue** - RP1 manages ethernet, but fails silently
3. **PCIe communication issue** - RP1 connected via PCIe, potential for silent failures
4. **Kernel 6.17 regression** - bug #2133877 points to kernel issues
5. **Cilium-related?** - 2 out of 2 confirmed reports are Cilium users. May be coincidence, or Cilium's eBPF/VXLAN workload may stress the driver in ways that trigger the bug more frequently

**Ruled out:**

- ~~rp1-pio failure~~ - both nodes have same problem regardless of rp1-pio status
- ~~Hardware defect specific to one board~~ - problem on both boards
- ~~EEPROM/firmware version~~ - problem on multiple firmware versions

## Checked and Ruled Out

- ❌ PCIe errors in logs - none found
- ❌ IRQ errors - none found
- ❌ DMA errors - none found
- ❌ Thermal throttling - no warnings in logs
- ❌ Undervoltage - no warnings in logs
- ❌ Link down events - none logged (PHY stayed "up")

## Next Steps

1. Check dmesg on CP after reboot for rp1-pio comparison
2. Monitor with `ethtool -S eth0` for packet errors over time
3. Consider kernel downgrade test (6.11 series)
4. Hardware swap test between CP and worker roles
5. File detailed bug report with this data

## Raw Data

### Exact network death sequence

```text
Dec 08 10:20:03.988843 kernel: audit: apparmor="AUDIT" (last normal log)
Dec 08 10:20:11.500917 systemd: cri-containerd scope deactivated successfully
Dec 08 10:20:12.548244 k3s: "Unhandled Error" - timed out waiting for the condition
Dec 08 10:20:13.042914 k3s: "Failed to update lease" - context deadline exceeded
Dec 08 10:20:42.668863 k3s: http2: client connection lost (multiple simultaneous)
Dec 08 10:20:51.188763 k3s: read tcp 172.16.101.2:40812->172.16.101.1:6443: i/o timeout
```

### RCU stalls in this incident

```text
Dec 08 05:09:56: 1 stall (5 hours BEFORE network death)
Dec 08 13:56:56: 1 stall (3.5 hours AFTER network death)
```

**NOTE**: No RCU stalls during network death (10:20). RCU stall at 13:56 is consequence of hung I/O.

### Pod network also broken (not just eth0)

```text
Dec 08 10:20:42 k3s: dial tcp 10.42.3.112:10250: connect: no route to host
Dec 08 10:20:45 k3s: dial tcp 10.42.3.112:10250: connect: no route to host
```

**Critical finding:** 10.42.x.x is pod network (Cilium). "no route to host" means:

- Routing broken even for LOCAL pod IPs
- Not just eth0, entire datapath (Cilium eBPF/routing) affected
- localhost (127.0.0.1) still works - requests arrive but can't be forwarded
- vmagent cannot scrape even local pods during network death

This suggests the problem is deeper than just physical interface:

- Kernel routing tables corrupted
- Cilium eBPF datapath broken
- Or RP1/PCIe issue affecting all network subsystems

### ethtool stats (post-reboot, all zeros)

```text
tx_carrier_sense_errors: 0
rx_frame_check_sequence_errors: 0
rx_symbol_errors: 0
rx_alignment_errors: 0
rx_resource_errors: 0
```
