# logcut

`logcut` is an emergency log compaction and rotation tool for Linux servers where a single active log file has consumed most of the disk and the application cannot be restarted.

It streams old log data from the active file, writes that data into a rotated output file, and then frees the matching blocks from the original file using Linux hole punching through Go syscalls. In gzip mode, chunks are appended directly into one final `.gz` archive, so there is no separate merge step.

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
- Supports dry-run planning.

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

Dry run first:

```bash
sudo logcut --dry-run -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz
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

## Build from source

The build flow is Go-based. The Makefile calls `go run ./cmd/devtool`.

```bash
make
sudo make install
```

Install under `/usr` instead of `/usr/local`:

```bash
sudo make install PREFIX=/usr
```

Run the Go dev tool directly:

```bash
go run ./cmd/devtool build
sudo go run ./cmd/devtool install
```

If `go.mod` is missing, create it using Go code:

```bash
go run ./cmd/devtool modulecheck
```

Uninstall:

```bash
sudo make uninstall
```

## Build packages

Packages are generated with the `nFPM` Go module. No `dpkg-deb` or `rpmbuild` command is required.

Debian package:

```bash
make deb
```

RPM package:

```bash
make rpm
```

Source tarball:

```bash
make tar
```

Checksums:

```bash
make checksums
```

Direct Go dev tool examples:

```bash
go run ./cmd/devtool deb
go run ./cmd/devtool rpm
go run ./cmd/devtool checksums
```

## Options

```text
-g              write gzip rotated archive
-k <size>       keep latest part in active log, default: 10% of source size
-p <percent>    use only this % of current free space as working budget, default: 20
--dry-run       print plan only, do not modify files
--force         allow risky plain output on low disk
```

## Repository layout

```text
logcut.go                         main runtime source
cmd/devtool/main.go               Go-based build/install/package helper
cmd/modulecheck/main.go           small Go module bootstrap helper
go.mod                            Go module file
nfpm.yaml                         package metadata for deb/rpm generation
.github/workflows/build-packages.yml  CI package build workflow
docs/architecture.md              architecture details
examples/emergency.md             emergency usage example
```

## Safety notes

Use `-g` for low-disk emergencies. Plain output can require too much space because rotated data is not compressed.

Always test with `--dry-run` first.

This tool modifies sparse allocation of the source file. Old data is preserved in the rotated output file after each successful chunk, then the matching range is freed from the source file.

## License

MIT License. See [LICENSE](LICENSE).
