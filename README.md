# logcut

`logcut` is an emergency log compaction and rotation tool for Linux servers where a single active log file has consumed most of the disk and the application cannot be restarted.

It streams old log data from the active file, writes that data into a rotated output file, and then frees the matching blocks from the original file using Linux hole punching through Go syscalls. In gzip mode, chunks are appended directly into one final `.gz` archive, so there is no separate merge step.

## Documentation

- [Manual](MANUAL.md)
- [Installation guide](INSTALL.md)
- [Architecture](docs/architecture.md)
- [Emergency example](examples/emergency.md)
- Unix man page: `man logcut`

## Why logcut exists

Normal rotation can fail during disk emergencies:

- `cp` and `split` need duplicate space.
- Compressing a full file at once needs output space before freeing the original.
- `mv` does not help if the application keeps writing to the old inode.
- Application restart or log reopen may not be allowed.

`logcut` is designed for this exact case: preserve old log data, keep the application writing to the same file, and recover disk space gradually.

## Design principles

- Runtime operations are implemented with Go APIs and Go syscalls.
- The application does not shell out to `fallocate`, `cp`, `mv`, `split`, or `gzip`.
- Runtime code is split into small packages under `internal/`.
- `cmd/logcut` contains only the binary entry point.
- Package building uses the `nFPM` Go module directly through `go run`.
- If `go.mod` is missing, the Go dev tool can create it directly using Go file operations.
- The Makefile is only a thin wrapper around the Go-based developer tool.

## Key features

- No application restart required.
- Keeps the same active log file path and inode.
- Processes old data chunk by chunk.
- Uses only a safe percentage of current free disk space for each chunk; default is 20%.
- Automatically recalculates chunk size while running.
- Optional gzip output using one final archive file.
- Cuts only on newline boundaries where possible.
- Frees old blocks using punch-hole after safe archive write.
- Uses state and lock files for safer resume behavior.
- Shows progress summaries with percentage, speed, elapsed time, ETA, and remaining bytes.
- Supports `-v` verbose mode for per-step and per-chunk logs.
- Supports dry-run planning.
- Installs a Unix manual page at `/usr/share/man/man8/logcut.8`.
- Supports `logcut --version`.

## Basic usage

Plain rotated output:

```bash
sudo logcut file1.log file1.rotated.log
```

Gzip emergency mode:

```bash
sudo logcut -g file1.log file1.rotated.log.gz
```

Recommended emergency usage:

```bash
sudo logcut -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
```

Verbose emergency usage:

```bash
sudo logcut -v -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
```

Dry run first:

```bash
sudo logcut --dry-run -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
```

Show installed version:

```bash
logcut --version
```

After installation, read the manual:

```bash
man logcut
```

## Runtime output

Default output shows the plan and periodic progress summaries:

```text
[2026-06-06 10:00:00] progress: starting total=80.00G already_done=0B remaining=80.00G
[2026-06-06 10:00:05] progress: 12.50% done=10.00G remaining=70.00G speed=200.00M/s elapsed=50s eta=5m50s
```

Use `-v` when you need detailed logs for each internal step:

```text
[2026-06-06 10:00:01] verbose: chunk=1 status=read offset=0B target_chunk=512.00M
[2026-06-06 10:00:02] verbose: chunk=1 status=archive raw=512.00M
[2026-06-06 10:00:03] verbose: chunk=1 status=punch offset=0B length=512.00M
[2026-06-06 10:00:03] verbose: chunk=1 status=done raw=512.00M archived=64.00M punched=512.00M ratio=12.50% chunk_time=2s free_before=1.00G free_after=1.44G recovered=+448.00M next_chunk=512.00M
```

Use `--progress-interval` to control summary frequency:

```bash
sudo logcut --progress-interval 10s -g -k 10G app.log app.rotated.log.gz
```

Use `--quiet` to suppress progress/log output.

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

## Auto chunk calculation

By default, `logcut` uses only 20% of currently available free disk space as the working budget for the next chunk. The other 80% is treated as a protected buffer for application log growth, system logs, metadata updates, and sudden write spikes.

The working budget is recalculated during the run. If free space improves, chunks can become larger. If free space drops, chunks become smaller or the tool stops safely.

Override the percentage with `-p`:

```bash
sudo logcut -g -k 10G -p 15 /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
```

## Important filesystem requirement

`logcut` requires Linux hole punching support. It is expected to work on common Linux filesystems such as XFS and ext4 when mounted normally.

Check filesystem type:

```bash
df -T /var/log/app
```

Check actual disk usage after compaction:

```bash
du -h /var/log/app/debug.log
ls -lh /var/log/app/debug.log
```

`ls` shows apparent size. `du` shows real allocated disk usage.

## Versioning

The source of truth is `VERSION.txt`.

On normal pushes to `main`, GitHub Actions runs `cmd/versionbump` and increments the patch version automatically. The bump updates:

- `VERSION.txt`
- `internal/version/version.go`, used by `logcut --version`
- `nfpm.yaml`, used by `.deb` and `.rpm`
- `man/logcut.8`, used by `man logcut`
- `cmd/devtool/main.go`, used by `make`, `make deb`, and `make rpm`

The package workflow then builds packages from the bumped version.

## Options

```text
-g                         write gzip rotated archive
-k <size>                  keep latest part in active log, default: 10% of source size
-p <percent>               use only this % of current free space as working budget, default: 20
-v                         verbose logs with per-step and per-chunk details
--progress-interval <dur>  progress summary interval, default: 5s
--quiet                    suppress progress/log output
--dry-run                  print plan only, do not modify files
--force                    allow risky plain output on low disk
--version                  print logcut version
```

## Repository layout

```text
cmd/logcut/main.go                binary entry point
cmd/devtool/main.go               Go-based build/install/package helper
cmd/versionbump/main.go           auto version bump helper
internal/cli/                     CLI parsing and validation
internal/compact/                 compaction engine
internal/disk/                    statfs and punch-hole helpers
internal/human/                   size parsing and formatting
internal/lock/                    lock handling
internal/progress/                progress and verbose output reporter
internal/state/                   resume state file handling
internal/version/                 runtime version support
man/logcut.8                      Unix manual page for man logcut
configure                         creates config.mk for make install
Makefile                          thin wrapper around cmd/devtool
VERSION.txt                       source of truth for auto patch version
nfpm.yaml                         package metadata for deb/rpm generation
.github/workflows/auto-version-bump.yml  auto patch bump workflow
.github/workflows/build-packages.yml      CI package build workflow
.github/workflows/package-commit.yml      package commit workflow
docs/architecture.md              architecture details
examples/emergency.md             emergency usage example
```

## Safety notes

Use `-g` for low-disk emergencies. Plain output can require too much space because rotated data is not compressed.

Always test with `--dry-run` first.

This tool modifies sparse allocation of the source file. Old data is preserved in the rotated output file after each successful chunk, then the matching range is freed from the source file.

## License

MIT License. See [LICENSE](LICENSE).
