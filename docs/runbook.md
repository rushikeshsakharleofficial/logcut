# logcut Emergency Runbook

## 1. Confirm the emergency

Confirm disk usage and identify the oversized log with your normal monitoring and filesystem inspection process.

## 2. Run preflight

Always run preflight before touching a live production log:

    logcut --preflight -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz

Preflight checks source/output safety, punch-hole support, writable state and lock directories, output directory write access, free space, and existing state.

## 3. Recover only enough space first

Use stop-free-above during incidents. This stops safely after the current chunk once enough disk is recovered:

    logcut -v --log-file /var/log/logcut-run.log --stop-free-above 20G --max-runtime 30m --compress-level 1 -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz

For small RAM machines, such as 4-6 GiB desktops or VMs, keep the working budget and chunk rate lower:

    logcut -v --log-file /var/log/logcut-run.log --stop-free-above 20G --max-runtime 30m --rate-limit 25M --sleep-between-chunks 2s --compress-level 1 --verify none -p 5 -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz

## 4. Check progress

Default output shows percentage, done, remaining, speed, elapsed, and ETA.

Verbose mode adds per-chunk read, archive, and punch details.

For automation, use JSON events:

    logcut --json -g -k 10G app.log app.rotated.log.gz

## 5. Inspect resumable state

    logcut status /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
    logcut list-state

If the job completed and resume data is no longer needed, archive the active state file:

    logcut clean-state /var/log/app/debug.log /var/log/app/debug.log.rotated.gz

This renames the state file with a timestamp instead of deleting it.

## 6. Verify actual recovered space

Use allocated-space checks, not only apparent file size checks.

The active source log becomes sparse, so apparent size can still look large while real allocated usage is reduced.

## 7. Handle hangs and stuck processes

If logcut hangs during a run (process appears in D state, no progress for minutes):

**Do not SIGKILL immediately.** A D-state process cannot be killed — it will die when the syscall returns. Instead:

1. Check if the watchdog caught it. By default `--chunk-timeout` is 5 minutes. If the watchdog fires, logcut writes an emergency state file and exits with code 3:

   ```
   /var/lib/logcut/<hash>.state.emergency
   ```

2. If the watchdog did not fire and the process is stuck in D state, the syscall (usually `fallocate` punch-hole or `fsync`) will eventually return or the kernel will error. Wait for the syscall to resolve. Once the process exits or becomes killable, the kernel releases the flock automatically.

3. If the process is dead but the lock file remains (rare, kernel should auto-release on process exit), use:

   ```
   logcut force-unlock /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
   ```

   This checks whether the PID in the lock file is alive. If the process is dead, it removes the stale lock. If alive, it refuses.

4. After a hang recovery, inspect emergency state:

   ```
   cat /var/lib/logcut/<hash>.state.emergency
   ```

   This shows the chunk number, offset, and reason for the abort. Resume normally — logcut picks up from the last punched offset:

   ```
   logcut -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
   ```

5. For filesystems known to have slow block operations (NFS, CIFS, FUSE), reduce `--chunk-timeout` to match expected operation latency:

   ```
   logcut --chunk-timeout 10m -g -k 10G app.log app.rotated.log.gz
   ```

## 8. Emergency state forensics

When logcut detects a disaster (punch-hole failure, sync failure, watchdog timeout), it writes a `.emergency` file alongside the regular `.state` file:

| Field | Meaning |
|-------|---------|
| `last_punched_offset` | Safest position to resume from — data up to here is guaranteed archived and punched |
| `last_archived_offset` | Last data written to the output archive |
| `current_chunk_offset` | Where the current chunk started |
| `reason` | What triggered the abort (watchdog_timeout, punch_hole_failed, sync_failed) |

Use `last_punched_offset` as your resume baseline.

## 9. Prevent recurrence

After the incident:

- disable or reduce debug logging
- add normal log rotation rules
- add disk alerts at 70%, 80%, and 90%
- add application log-size alerts
- review retention and compression policy
