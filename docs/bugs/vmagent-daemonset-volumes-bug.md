# VMAgent DaemonSetMode Volumes Bug

**Component:** victoria-metrics-operator v0.66.1
**Date discovered:** 2025-12-08
**Status:** Open (not reported upstream yet)

## Summary

VictoriaMetrics operator ignores user-provided `volumes` configuration in DaemonSetMode and always creates `persistent-queue-data` volume as `emptyDir`, despite documentation stating hostPath should work.

## Expected Behavior

Per [documentation](https://github.com/VictoriaMetrics/operator/blob/master/docs/resources/vmagent.md):

> Volume for the persistent-queue could be mounted with `volumes` and must have either hostPath or emptyDir.

User should be able to override `persistent-queue-data` volume with hostPath for persistent WAL storage.

## Actual Behavior

Operator always creates `persistent-queue-data` as `emptyDir` regardless of user configuration.

## Root Cause

In `internal/controller/operator/factory/vmagent/vmagent.go`, function `newPodSpec()` around line 545:

```go
// in case for sts, we have to use persistentVolumeClaimTemplate instead
if !cr.Spec.StatefulMode {
    volumes = append(volumes, corev1.Volume{
        Name: vmAgentPersistentQueueMountName,
        VolumeSource: corev1.VolumeSource{
            EmptyDir: &corev1.EmptyDirVolumeSource{},
        },
    })
}

volumes = append(volumes, cr.Spec.Volumes...)
```

The operator:
1. Creates `persistent-queue-data` with `emptyDir` FIRST (for non-StatefulMode)
2. Then appends user's volumes

User's volumes are added but do NOT override existing volume with same name.

## Attempted Workarounds

### 1. Using `volumes` field with same name
```yaml
volumes:
  - name: persistent-queue-data
    hostPath:
      path: /var/lib/vmagent-wal
```
**Result:** Volume ignored, emptyDir used.

### 2. Using `extraVolumes` field
```yaml
extraVolumes:
  - name: vmagent-wal-host
    hostPath:
      path: /var/lib/vmagent-wal
```
**Result:** Field not recognized by operator code (reads `cr.Spec.Volumes`, not `cr.Spec.ExtraVolumes`).

### 3. Combining `daemonSetMode` + `statefulMode`
```yaml
daemonSetMode: true
statefulMode: true
```
**Result:** Rejected by admission webhook: "daemonSetMode and statefulMode cannot be used in the same time"

### 4. Working workaround: separate volume name
```yaml
volumes:
  - name: vmagent-wal-host  # different name
    hostPath:
      path: /var/lib/vmagent-wal
volumeMounts:
  - name: vmagent-wal-host
    mountPath: /vmagent-wal
extraArgs:
  remoteWrite.tmpDataPath: /vmagent-wal
```
**Result:** Volume IS added, but volumeMounts NOT applied (separate bug, see below).

## Related Issues

- [Issue #450](https://github.com/VictoriaMetrics/operator/issues/450) - "allowed override vmagent volume" - Fixed for StatefulMode only (PR #452, v0.25.0)
- DaemonSetMode was not addressed in that fix

## Suggested Fix

Modify `newPodSpec()` to check if user provided a volume with name `vmAgentPersistentQueueMountName` before creating default emptyDir:

```go
// Check if user provided persistent-queue volume
userProvidedPQVolume := false
for _, v := range cr.Spec.Volumes {
    if v.Name == vmAgentPersistentQueueMountName {
        userProvidedPQVolume = true
        break
    }
}

if !cr.Spec.StatefulMode && !userProvidedPQVolume {
    volumes = append(volumes, corev1.Volume{
        Name: vmAgentPersistentQueueMountName,
        VolumeSource: corev1.VolumeSource{
            EmptyDir: &corev1.EmptyDirVolumeSource{},
        },
    })
}

volumes = append(volumes, cr.Spec.Volumes...)
```

## Impact

- Cannot use persistent hostPath storage for WAL in DaemonSetMode
- Node reboot = lost metrics queue
- Network outage recovery impossible (metrics not buffered persistently)
