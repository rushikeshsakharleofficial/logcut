# logcut Manual

## Purpose

`logcut` is for emergency log compaction when a large active log file has filled the disk and the application cannot be restarted.

It preserves old log data in a rotated output file and frees the matching old blocks from the active source log.

## Command format

```bash
logcut [options] <source-log> <rotated-output>
logcut status <source-log> <rotated-output>
logcut list-state
logcut clean-state <source-log> <rotated-output>
```

Recommended production emergency mode:

```bash
sudo logcut -v \
  --log-file /var/log/logcut-run.log \
  --stop-free-above 20G \
  --max-runtime 30m \
  --rate-limit 100M \
  --sleep-between-chunks 500ms \
  --compress-level 1 \
  -g -k 10G \
  file1.log file1.rotated.log.gz
```

Preflight:

```bash
sudo logcut --preflight -g -k 10G file1.log file1.rotated.log.gz
```

Dry run:

```bash
sudo logcut --dry-run -g -k 10G file1.log file1.rotated.log.gz
```

## Options

### `-g`

Enable gzip output. In this mode, all chunks are appended directly into one final gzip archive. There is no separate chunk merge step.

### `-k <size>`

Keep the latest part of the active log untouched. Examples: `10G`, `512M`, `1G`.

If `-k` is not provided, `logcut` keeps the latest 10% of the source log size.

### `-p <percent>`

Use only this percentage of current free disk as the working budget for the next chunk. Default is `20`.

This means only 20% of available free space is considered usable for the next chunk operation. The remaining 80% is protected for application writes and sudden spikes.

### `-v`

Enable verbose logs.

Verbose mode prints detailed per-step and per-chunk status, including read, archive, punch-hole, chunk duration, compression ratio, free space before/after, recovered estimate, and next chunk size.

### `--preflight`

Run safety checks only. Preflight validates source/output paths, regular-file status, write access, punch-hole support, output directory, state and lock directories, free space, and existing state.

### `--stop-free-above <size>`

Stop safely after the current chunk once free space reaches the requested amount.

Example:

```bash
--stop-free-above 20G
```

This is useful during incidents where the first goal is to recover enough disk space, not necessarily process the entire old range.

### `--max-runtime <duration>`

Stop safely after the current chunk once the runtime limit is reached.

Example:

```bash
--max-runtime 30m
```

### `--rate-limit <size>`

Approximate raw processing rate limit per second.

Example:

```bash
--rate-limit 100M
```

This reduces I/O pressure on the live application by pacing chunk processing.

### `--sleep-between-chunks <duration>`

Pause after each completed safe chunk.

Examples:

```bash
--sleep-between-chunks 500ms
--sleep-between-chunks 2s
```

### `--log-file <path>`

Also write run output to a log file for audit and incident records.

### `--json`

Emit JSON events instead of normal human progress logs. Useful for automation, monitoring integrations, and future UI wrappers.

### `--compress-level <level>`

Set gzip compression level. Use `1` for fastest emergency recovery, `9` for best compression, or `-1` for Go gzip default.

### `--verify full|none`

Control final gzip verification. `full` verifies the archive after completion. `none` skips verification when time is critical.

### `--state-dir <dir>` and `--lock-dir <dir>`

Override state and lock directories.

### `--progress-interval <duration>`

Set progress summary frequency. Default is `5s`.

### `--quiet`

Suppress progress and verbose output.

### `--dry-run`

Show the plan without modifying files.

### `--force`

Allow risky plain output on low disk. Plain output is not recommended during low-disk emergencies because it does not compress the rotated output.

## Graceful stop behavior

If `logcut` receives SIGINT or SIGTERM, it records the stop request and exits only at a safe chunk boundary. It does not intentionally stop halfway through the archive, sync, state-save, or punch-hole sequence.

The saved state allows a later run to resume.

## Runtime output

Default output shows the plan and periodic progress summaries:

```text
[2026-06-06 10:00:00] progress: starting total=80.00G already_done=0B remaining=80.00G
[2026-06-06 10:00:05] progress: 12.50% done=10.00G remaining=70.00G speed=200.00M/s elapsed=50s eta=5m50s
```

Verbose mode adds detailed chunk logs.

## What happens during gzip mode

Example:

- Source log: `debug.log`
- Source size: 90 GB
- Keep latest: 10 GB
- Old range to rotate: 80 GB
- Output: `debug.log.rotated.gz`

Flow:

1. Read old data from `debug.log`.
2. Stop at a newline boundary where possible.
3. Compress the chunk with Go gzip APIs.
4. Append it directly to `debug.log.rotated.gz`.
5. Sync the archive.
6. Save state.
7. Punch-hole the same old byte range from `debug.log`.
8. Recalculate free space and next chunk size.
9. Continue until the old range is complete or a safe stop condition is reached.

## State commands

```bash
logcut status file1.log file1.rotated.log.gz
logcut list-state
logcut clean-state file1.log file1.rotated.log.gz
```

`clean-state` archives the state file with a timestamp instead of deleting it directly.

## Important file-size behavior

After compaction, the active log becomes sparse.

`ls -lh debug.log` may still show the old apparent size.

`du -h debug.log` shows the real disk usage.

Old logs should be read from the rotated output file.

## Reading old logs

For gzip output:

```bash
zcat file1.rotated.log.gz | less
```

For plain output:

```bash
less file1.rotated.log
```

## Build and package workflow

Build:

```bash
make
```

Install:

```bash
sudo make install
```

Build `.deb`:

```bash
make deb
```

Build `.rpm`:

```bash
make rpm
```

All build helper logic is implemented in Go under `cmd/devtool`.

## Module bootstrap

If `go.mod` is missing:

```bash
go run ./cmd/devtool modulecheck
```

This creates `go.mod` using Go file APIs.

## Safety checklist

Before production use:

1. Confirm the filesystem supports hole punching.
2. Run `--preflight` first.
3. Run `--dry-run` if time allows.
4. Use gzip mode for emergency cases.
5. Keep enough latest log data using `-k`.
6. Keep the default `-p 20` unless you understand the risk.
7. Use `--rate-limit` or `--sleep-between-chunks` on busy disks.
8. Verify recovered space with `du`, not only `ls`.
9. Disable or reduce debug logging after the emergency.

## Limitations

- Requires Linux filesystem support for punch-hole.
- The source log becomes sparse.
- The old beginning of the active source log should not be treated as the historical source after compaction.
- Plain output is not suitable for very low disk conditions unless forced.
