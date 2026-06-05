# Emergency Example

Scenario:

- Root disk is almost full.
- A single debug log is very large.
- The application cannot be restarted.

Dry run first:

    sudo logcut --dry-run -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz

Run compaction:

    sudo logcut -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz

Check recovered space:

    df -h /
    du -h /var/log/app/debug.log
    ls -lh /var/log/app/debug.log

Read old logs from the rotated gzip archive:

    zcat /var/log/app/debug.log.rotated.gz | less
