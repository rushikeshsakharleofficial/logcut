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

## 7. Prevent recurrence

After the incident:

- disable or reduce debug logging
- add normal log rotation rules
- add disk alerts at 70%, 80%, and 90%
- add application log-size alerts
- review retention and compression policy
