# No Shell Command Policy

`logcut` must not depend on Linux shell commands for its runtime behavior.

## Runtime rule

The log compaction engine must use Go APIs and Go-supported Linux syscalls directly.

Do not call external commands such as:

- `cp`
- `mv`
- `split`
- `gzip`
- `fallocate`
- `truncate`
- `logrotate`
- `du`
- `df`

The application must not use `os/exec` for core log processing.

## Required Go-native behavior

The application should use Go directly for:

- opening files
- reading chunks
- respecting newline boundaries
- gzip compression
- fsync/sync
- disk usage checks using syscalls
- file metadata checks using stat/syscall
- hole punching using the Linux `fallocate` syscall
- state file handling
- locking

## Packaging rule

Packaging should avoid Linux-native package builder commands such as `dpkg-deb` and `rpmbuild`.

Package generation should be Go-module based. The repository uses the nFPM Go module through:

    go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

This means if the module is not present locally, Go resolves it automatically through the module system.

## Makefile note

The Makefile is only a developer convenience wrapper. The application logic itself must remain Go-native and must not shell out to system tools.

## Contribution rule

Pull requests should not add shell-based runtime shortcuts. If a Linux feature is required, implement it through Go standard library calls or direct Linux syscalls where appropriate.
