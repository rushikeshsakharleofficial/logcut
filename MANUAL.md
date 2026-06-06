# logcut Manual

## Purpose

`logcut` is for emergency log compaction when a large active log file has filled the disk and the application cannot be restarted.

It preserves old log data in a rotated output file and frees the matching old blocks from the active source log.

## Command format

```bash
logcut [options] <source-log> <rotated-output>
```

Plain mode:

```bash
sudo logcut file1.log file1.rotated.log
```

Gzip mode:

```bash
sudo logcut -g file1.log file1.rotated.log.gz
```

Recommended emergency mode:

```bash
sudo logcut -g -k 10G file1.log file1.rotated.log.gz
```

Verbose emergency mode:

```bash
sudo logcut -v -g -k 10G file1.log file1.rotated.log.gz
```

Dry run:

```bash
sudo logcut --dry-run -g -k 10G file1.log file1.rotated.log.gz
```

## Options

### `-g`

Enable gzip output.

In this mode, all chunks are appended directly into one final gzip archive. There is no separate chunk merge step.

### `-k <size>`

Keep the latest part of the active log untouched.

Examples:

```bash
-k 10G
-k 512M
-k 1G
```

If `-k` is not provided, `logcut` keeps the latest 10% of the source log size.

### `-p <percent>`

Use only this percentage of current free disk as the working budget for the next chunk.

Default:

```bash
-p 20
```

This means only 20% of available free space is considered usable for the next chunk operation. The remaining 80% is protected for application writes and sudden spikes.

### `-v`

Enable verbose logs.

Verbose mode prints detailed per-step and per-chunk status, including read, archive, punch-hole, chunk duration, compression ratio, free space before/after, recovered estimate, and next chunk size.

### `--progress-interval <duration>`

Set progress summary frequency. Default is `5s`.

Examples:

```bash
--progress-interval 10s
--progress-interval 30s
--progress-interval 1m
```

### `--quiet`

Suppress progress and verbose output.

### `--dry-run`

Show the plan without modifying files.

Always use dry-run before running on production logs.

### `--force`

Allow risky plain output on low disk.

Plain output is not recommended during low-disk emergencies because it does not compress the rotated output.

## Runtime output

Default output shows the plan and periodic progress summaries:

```text
[2026-06-06 10:00:00] progress: starting total=80.00G already_done=0B remaining=80.00G
[2026-06-06 10:00:05] progress: 12.50% done=10.00G remaining=70.00G speed=200.00M/s elapsed=50s eta=5m50s
```

Verbose mode adds detailed chunk logs:

```text
[2026-06-06 10:00:01] verbose: chunk=1 status=read offset=0B target_chunk=512.00M
[2026-06-06 10:00:02] verbose: chunk=1 status=archive raw=512.00M
[2026-06-06 10:00:03] verbose: chunk=1 status=punch offset=0B length=512.00M
[2026-06-06 10:00:03] verbose: chunk=1 status=done raw=512.00M archived=64.00M punched=512.00M ratio=12.50% chunk_time=2s free_before=1.00G free_after=1.44G recovered=+448.00M next_chunk=512.00M
```

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
9. Continue until the old range is complete.

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

Direct Go devtool usage:

```bash
go run ./cmd/devtool build
go run ./cmd/devtool install
go run ./cmd/devtool deb
go run ./cmd/devtool rpm
```

Package generation uses the nFPM Go module directly through Go module resolution.

## Module bootstrap

If `go.mod` is missing:

```bash
go run ./cmd/devtool modulecheck
```

This creates `go.mod` using Go file APIs.

## Safety checklist

Before production use:

1. Confirm the filesystem supports hole punching.
2. Run dry-run first.
3. Use gzip mode for emergency cases.
4. Keep enough latest log data using `-k`.
5. Keep the default `-p 20` unless you understand the risk.
6. Verify recovered space with `du`, not only `ls`.
7. Disable or reduce debug logging after the emergency.

## Limitations

- Requires Linux filesystem support for punch-hole.
- The source log becomes sparse.
- The old beginning of the active source log should not be treated as the historical source after compaction.
- Plain output is not suitable for very low disk conditions unless forced.
