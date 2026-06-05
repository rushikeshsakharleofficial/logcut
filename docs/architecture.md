# logcut Architecture

## Goal

`logcut` is built for emergency log rotation on single-disk Linux systems where:

- The root filesystem is almost full.
- One active log file consumed most of the disk.
- The application cannot be restarted or asked to reopen logs.
- The old logs must be preserved.

## Runtime architecture

The runtime application is written in Go and uses Go APIs/syscalls directly.

It does not shell out to external commands for the compaction operation. In particular, it does not call external `fallocate`, `cp`, `mv`, `split`, or `gzip` commands.

Core runtime dependencies:

- `os` and `io` for file handling
- `bufio` for line-safe chunk reads
- `compress/gzip` for gzip archive writing
- `syscall.Statfs` for free-space checks
- `syscall.Flock` for lock handling
- `syscall.SYS_FALLOCATE` for punch-hole

## Core idea

Instead of copying or moving the full log, `logcut` processes old data in small chunks:

1. Read a safe old chunk from the active log.
2. Stop at a newline boundary where possible.
3. Write the chunk to the rotated output.
4. In gzip mode, append it directly as a gzip member to one final `.gz` file.
5. Sync and verify the output operation.
6. Punch a hole in the same byte range of the original log.
7. Repeat until the selected old range is complete.

The active log file path remains unchanged, so the application can continue writing.

## Auto chunk calculation

The tool never calculates the chunk size from total free disk directly. By default, it uses only 20% of the currently available free disk as the working budget.

The remaining 80% is treated as protected buffer for:

- Continued application writes
- System logs
- Metadata updates
- Temporary filesystem activity
- Unexpected spikes

Chunk size is recalculated during the run as free space changes.

## Single archive design

Gzip mode creates one final rotated archive from the beginning:

- No temporary `part0001.gz`, `part0002.gz`, or merge step is required.
- Each processed chunk is appended as another gzip member to the same output file.
- Standard gzip readers can read concatenated gzip members in order.

Final result:

- `app.log` remains the active sparse log file.
- `app.log.rotated.gz` contains the old rotated data.

The active log may still show its original apparent size with `ls -lh`. Use `du -h` to check real allocated space.

## Safety model

For each chunk:

1. Archive first.
2. Sync archive.
3. Update state.
4. Punch hole.
5. Update state again.

The tool must never punch before the output is safely written.

## State and locking

`logcut` stores state under `/var/lib/logcut` and lock files under `/var/lock`.

The state tracks:

- source path
- output path
- source inode and device
- original size
- cutoff offset
- last archived offset
- last punched offset
- gzip mode

This allows safer resume behavior after interruption.

## Build and packaging architecture

The build support is also Go-centered.

- `cmd/devtool` handles build, install, package, tarball, checksums, and module bootstrap.
- The Makefile is a thin wrapper around `go run ./cmd/devtool`.
- `.deb` and `.rpm` packages are generated with the `nFPM` Go module through `go run`.
- If `go.mod` is missing, `cmd/devtool modulecheck` can create it using Go file APIs.

## Limitations

- Requires filesystem support for hole punching.
- Plain output is risky during low-disk emergencies because it may not reduce space enough.
- The source log becomes sparse after compaction.
- Historical reads should use the rotated output, not the old beginning of the active log.
