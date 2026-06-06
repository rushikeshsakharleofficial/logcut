# logcut

`logcut` is an emergency log compaction and rotation tool for Linux servers where a single active log file has consumed most of the disk and the application cannot be restarted.

It streams old log data from the active file, writes that data into a rotated output file, and then frees the matching blocks from the original file using Linux hole punching through Go syscalls. In gzip mode, chunks are appended directly into one final `.gz` archive, so there is no separate merge step.

## Documentation

- [Manual](MANUAL.md)
- [Installation guide](INSTALL.md)
- [Architecture](docs/architecture.md)
- [Emergency runbook](docs/runbook.md)
- [Filesystem support](docs/filesystem-support.md)
- [Emergency example](examples/emergency.md)
- Unix man page: `man logcut`

## Key features

- No application restart required.
- Keeps the same active log file path and inode.
- Processes old data chunk by chunk.
- Auto-throttle is enabled by default.
- Inbuilt adaptive control reads system load, memory availability, and disk free space directly from Go.
- Automatically adjusts rate limit, sleep between chunks, max chunk size, and gzip level.
- Uses only a safe percentage of current free disk space for each chunk; default is 20%.
- Automatically recalculates chunk size while running.
- Optional gzip output using one final archive file.
- Cuts only on newline boundaries where possible.
- Frees old blocks using punch-hole after safe archive write.
- Uses state and lock files for safer resume behavior.
- Handles SIGINT/SIGTERM by stopping safely before the next chunk.
- Supports `--preflight` safety checks before modifying logs.
- Supports `--stop-free-above` and `--max-runtime` for safe incident stops.
- Manual `--rate-limit` and `--sleep-between-chunks` still override auto mode when needed.
- Supports `--log-file` for audit logs.
- Supports `--json` event output for automation.
- Supports `--compress-level` and `--verify full|none`.
- Supports `status`, `list-state`, and safe `clean-state` state management.
- Shows progress summaries with percentage, speed, elapsed time, ETA, and remaining bytes.
- Supports `-v` verbose mode for per-step and per-chunk logs.
- Installs a Unix manual page at `/usr/share/man/man8/logcut.8`.
- Supports `logcut --version`.

## Basic usage

Preflight first:

```bash
sudo logcut --preflight -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
```

Recommended production emergency usage with default auto-throttle:

```bash
sudo logcut -v \
  --log-file /var/log/logcut-run.log \
  --stop-free-above 20G \
  --max-runtime 30m \
  -g -k 10G \
  /var/log/app/debug.log \
  /var/log/app/debug.log.rotated.gz
```

Manual override example when you want a fixed limit:

```bash
sudo logcut -v \
  --rate-limit 25M \
  --sleep-between-chunks 2s \
  -g -k 10G \
  /var/log/app/debug.log \
  /var/log/app/debug.log.rotated.gz
```

JSON automation output:

```bash
sudo logcut --json -g -k 10G app.log app.rotated.log.gz
```

State inspection:

```bash
sudo logcut status /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
sudo logcut list-state
sudo logcut clean-state /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
```

Show installed version:

```bash
logcut --version
```

## Auto-throttle behavior

Auto mode is enabled by default. It is fully inbuilt and does not call external tools such as `iostat`, `vmstat`, `ionice`, or shell commands.

On each run, and again between chunks, logcut evaluates:

- current filesystem free space
- system load from Linux proc data
- available memory from Linux proc data

It then adjusts:

- raw processing rate limit
- sleep between chunks
- maximum chunk size
- gzip compression level

Manual flags still win. If you pass `--rate-limit` or `--sleep-between-chunks`, those values override the auto-selected values.

## Runtime output

Default output shows the plan and periodic progress summaries:

```text
[2026-06-06 10:00:00] auto-throttle: pressure=high load1=3.20 cpus=2 mem_avail=14.0% free=1.50G rate=25.00M/s sleep=2s max_chunk=64.00M reason=high disk, load, or memory pressure
[2026-06-06 10:00:05] progress: 12.50% done=10.00G remaining=70.00G speed=25.00M/s elapsed=6m40s eta=46m40s
```

Use `-v` when you need detailed logs for each internal step.

Use `--progress-interval` to control summary frequency:

```bash
sudo logcut --progress-interval 10s -g -k 10G app.log app.rotated.log.gz
```

Use `--quiet` to suppress progress/log output.

## Graceful stop behavior

If `logcut` receives SIGINT or SIGTERM, it does not interrupt a chunk in the middle of archive/write/punch state transitions. It records the signal request, finishes the current safe chunk boundary, saves state, and exits with a stop reason. The next run can resume from the saved state.

## Configure and build

```bash
./configure
make
sudo make install
```

Install under `/usr`:

```bash
./configure --prefix=/usr
make
sudo make install
```

Build packages:

```bash
make deb
make rpm
```

## How it works

Example:

- Active log: `debug.log`, size 90 GB
- Keep latest active data: 10 GB
- Rotate old data: first 80 GB
- Output: `debug.log.rotated.gz`

`logcut` reads old data from the beginning of `debug.log`, compresses a safe chunk when `-g` is enabled, appends it to `debug.log.rotated.gz`, syncs the output, then punches a hole in the same byte range of the original file. This frees disk blocks while the application continues writing to `debug.log`.

At the end:

- `debug.log` remains the active file and keeps latest data.
- `debug.log.rotated.gz` contains the older rotated logs.
- `debug.log` may still show a large apparent size with `ls -lh`, but real disk usage should be checked with `du -h`.

## Important filesystem requirement

`logcut` requires Linux hole punching support. It is expected to work on common Linux filesystems such as XFS and ext4 when mounted normally.

See [Filesystem support](docs/filesystem-support.md).

## Options

```text
-g                         write gzip rotated archive
-k <size>                  keep latest part in active log, default: 10% of source size
-p <percent>               use only this % of current free space as working budget, default: 20
-v                         verbose logs with per-step and per-chunk details
--preflight                run safety checks only
--stop-free-above <size>   stop safely once free space is above size
--max-runtime <duration>   stop safely once runtime is reached
--rate-limit <size>        manual override for raw processing rate limit per second
--sleep-between-chunks <d> manual override for pause after every safe chunk
--log-file <path>          also write run output to file
--json                     emit JSON events
--compress-level <level>   gzip level: -1 or 1..9
--verify <mode>            gzip verification: full or none
--state-dir <dir>          state directory
--lock-dir <dir>           lock directory
--progress-interval <dur>  progress summary interval, default: 5s
--quiet                    suppress progress/log output
--dry-run                  print plan only, do not modify files
--force                    allow risky plain output on low disk
--version                  print logcut version
```

## Safety notes

Use `-g` for low-disk emergencies. Plain output can require too much space because rotated data is not compressed.

Always run `--preflight` first.

This tool modifies sparse allocation of the source file. Old data is preserved in the rotated output file after each successful chunk, then the matching range is freed from the source file.

## License

MIT License. See [LICENSE](LICENSE).
