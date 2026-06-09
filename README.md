<div align="center">

# logcut

Emergency log compaction for Linux — free disk space without restarting the application.

[![License](https://img.shields.io/github/license/rushikeshsakharleofficial/logcut?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/logcut/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/rushikeshsakharleofficial/logcut?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/logcut/releases)
[![Build](https://img.shields.io/github/actions/workflow/status/rushikeshsakharleofficial/logcut/go-ci.yml?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/logcut/actions)
[![Stars](https://img.shields.io/github/stars/rushikeshsakharleofficial/logcut?style=for-the-badge)](https://github.com/rushikeshsakharleofficial/logcut/stargazers)

</div>

## What is this?

`logcut` is a CLI for Linux servers where a single active log file has consumed most of the disk and the application cannot be restarted. It streams old data from the beginning of the active log into a rotated output file, then frees the matching disk blocks using Linux hole-punching — all while the application keeps writing to the same file path and inode. In gzip mode, chunks are appended directly into one final `.gz` archive with no separate merge step.

## Quick Start

**Install from source:**

```bash
./configure
make
sudo make install
```

**Or install under `/usr`:**

```bash
./configure --prefix=/usr
make
sudo make install
```

**Build `.deb` / `.rpm` packages:**

```bash
make deb
make rpm
```

**Preflight check first:**

```bash
sudo logcut --preflight -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
```

**Run (recommended production usage):**

```bash
sudo logcut -v \
  --log-file /var/log/logcut-run.log \
  --stop-free-above 20G \
  --max-runtime 30m \
  -g -k 10G \
  /var/log/app/debug.log \
  /var/log/app/debug.log.rotated.gz
```

See [INSTALL.md](INSTALL.md) for full build options.

## Key features

- No application restart required.
- Preserves the active log file path and inode.
- Processes old data chunk by chunk, resuming safely on interruption.
- Auto-throttle enabled by default — reads system load, memory, and disk free space directly from Go (no external tools like `iostat` or `vmstat`).
- Automatically adjusts rate limit, sleep between chunks, max chunk size, and gzip level based on system pressure. Small-memory machines get gentler defaults.
- Uses only a safe percentage of current free disk space per chunk (default 20%).
- Optional gzip output written to one final archive.
- Cuts only on newline boundaries.
- Frees old blocks via punch-hole after safe archive write and sync; drops page cache for the punched range immediately after.
- Runs at idle IO scheduling class — other processes keep full disk priority.
- Parallel gzip compression across all available CPUs.
- Watchdog detects hung chunks and writes emergency state before aborting.
- State and lock files for safe resume behavior.
- SIGINT/SIGTERM support — stops cleanly before the next chunk.
- `--preflight` safety check mode.
- `--stop-free-above` and `--max-runtime` for controlled incident stops.
- `--rate-limit` and `--sleep-between-chunks` override auto mode when needed.
- `--chunk-timeout` watchdog abort with emergency state on hang.
- `status`, `list-state`, `clean-state`, and `force-unlock` subcommands.
- Progress summaries with percentage, speed, elapsed time, ETA, and remaining bytes.
- `--json` event output for automation pipelines.
- `--log-file` for audit logs.

## How it works

Given an active log at 90 GB with `-k 10G`:

1. `logcut` identifies the cutoff — keep the last 10 GB, rotate the first 80 GB.
2. It reads a safe chunk from the start of `debug.log`, compresses it in parallel across all CPUs (if `-g`), and appends it to `debug.log.rotated.gz`.
3. It syncs the output, punches a hole over the same byte range in `debug.log` to free those disk blocks, then drops the page cache for the freed range.
4. It repeats until the cutoff is reached, recalculating chunk size and throttle on each iteration.

At the end, `debug.log` keeps the latest data and the same inode. `debug.log.rotated.gz` holds the older rotated portion. `ls -lh` on `debug.log` may still show a large apparent size — check real usage with `du -h`.

## Auto-throttle behavior

Auto mode is on by default and runs entirely within Go — no shell commands, no external tools.

Between chunks, logcut evaluates:

| Signal | Source |
|--------|--------|
| Disk free space | `statfs` via Go |
| System load | `/proc/loadavg` |
| Available memory | `/proc/meminfo` |
| Total installed RAM | `/proc/meminfo` |

It then adjusts rate limit, sleep between chunks, max chunk size, and gzip compression level. Pass `--rate-limit` or `--sleep-between-chunks` to override.

## System impact during emergencies

logcut is designed to run as a background operation without degrading the machine:

| Mechanism | Effect |
|-----------|--------|
| `ioprio_set(IDLE)` | Disk IO deferred to idle slots — app writes always take priority |
| `fadvise(DONTNEED)` | Page cache for each punched range is released immediately, reducing memory pressure |
| Parallel gzip | Compression uses all CPUs, minimising the wall-clock time logcut holds IO |
| Rate limit + sleep | Auto-throttle keeps sustained IO within safe bounds |

## Runtime output

```text
[2026-06-06 10:00:00] auto-throttle: pressure=high load1=3.20 cpus=2 mem_avail=14.0% free=1.50G rate=25.00M/s sleep=2s max_chunk=64.00M reason=high disk, load, or memory pressure
[2026-06-06 10:00:05] progress: 12.50% done=10.00G remaining=70.00G speed=25.00M/s elapsed=6m40s eta=46m40s
```

Use `-v` for per-step and per-chunk logs. Use `--quiet` to suppress all output. Use `--progress-interval` to control summary frequency.

## Options

```text
-g                           write gzip rotated archive
-k <size>                    keep latest part in active log, default: 10% of source size
-p <percent>                 use only this % of current free space as working budget, default: 20
-v                           verbose logs with per-step and per-chunk details
--preflight                  run safety checks only
--stop-free-above <size>     stop safely once free space is above size
--max-runtime <duration>     stop safely once runtime is reached
--chunk-timeout <duration>   max per-chunk time before watchdog abort, default: 5m
--rate-limit <size>          maximum raw read/archive rate per second
--sleep-between-chunks <d>   sleep after each chunk
--log-file <path>            also write run output to file
--json                       emit JSON events
--compress-level <level>     gzip level: 0 auto, -1 default, or 1..9
--verify <mode>              gzip verification: full or none
--state-dir <dir>            state directory
--lock-dir <dir>             lock directory
--progress-interval <dur>    progress summary interval, default: 5s
--quiet                      suppress progress/log output
--dry-run                    print plan only, do not modify files
--force                      allow risky plain output on low disk
--version                    print logcut version
```

**Subcommands:**

```text
logcut status <source> <output>           show resume state
logcut list-state                         list all state files
logcut clean-state <source> <output>      remove state file
logcut force-unlock <source> <output>     remove stale lock if process is dead
```

## Safety notes

Use `-g` for low-disk emergencies. Plain (uncompressed) output can require as much space as the data being rotated, which defeats the purpose.

Always run `--preflight` first.

`logcut` requires a Linux filesystem with hole-punch support (XFS, ext4 with default mount options). See [Filesystem support](docs/filesystem-support.md).

If `logcut` is interrupted mid-chunk (watchdog timeout, OOM, power loss), it writes an emergency state file. Resume with the same command — logcut picks up from the last safe checkpoint.

## Project Structure

```
build/              compiled output
cmd/
  devtool/          development utilities
  logcut/           binary entry point
  versionbump/      version management tool
docs/               architecture, filesystem support, runbook
examples/           emergency usage examples
internal/
  adaptive/         auto-throttle: load, memory, disk evaluation
  cli/              command-line parsing and subcommands
  compact/          core engine: chunk read/write/punch loop
  control/          signal handling and stop coordination
  disk/             hole-punch and filesystem stat wrappers
  emergency/        emergency state write on fatal errors
  event/            JSON event emission
  human/            human-readable size parsing and formatting
  job/              job identity and lock path helpers
  lock/             flock-based process lock with PID tracking
  preflight/        safety checks before modifying files
  progress/         progress reporting
  state/            resume state persistence
man/                Unix man page (logcut.8)
test/               integration tests
configure           build configuration script
go.mod
Makefile
nfpm.yaml           package build spec
```

## Documentation

| Resource | Description |
|----------|-------------|
| [MANUAL.md](MANUAL.md) | Full manual with all flags and behaviors |
| [INSTALL.md](INSTALL.md) | Build, configure, and package installation |
| [Architecture](docs/architecture.md) | Internal design and component overview |
| [Emergency runbook](docs/runbook.md) | Step-by-step recovery guide for disk emergencies |
| [Filesystem support](docs/filesystem-support.md) | Supported filesystems and hole-punch requirements |
| [Emergency example](examples/emergency.md) | Worked example of an emergency compaction run |
| Man page | `man logcut` after install |

## Contributing

1. Fork the repo and create a branch.
2. Run `go test ./...` before submitting.
3. Open a pull request — CI runs build and tests automatically.

<a href="https://github.com/rushikeshsakharleofficial/logcut/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=rushikeshsakharleofficial/logcut" />
</a>

## License

MIT License. See [LICENSE](LICENSE).

---

<div align="center">

[![Star History Chart](https://api.star-history.com/svg?repos=rushikeshsakharleofficial/logcut&type=Date)](https://star-history.com/#rushikeshsakharleofficial/logcut&Date)

</div>
