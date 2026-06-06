# logcut installation and manual

## Requirements

- Linux server
- Go 1.22 or newer
- Filesystem with hole-punch support, usually XFS or ext4
- Root or sufficient permission to read/write the target log and punch holes

## Configure before build

`logcut` supports a standard configure step before `make`.

Default configuration:

```bash
./configure
make
sudo make install
```

Install under `/usr` instead of `/usr/local`:

```bash
./configure --prefix=/usr
make
sudo make install
```

Install the binary under `/usr/sbin`:

```bash
./configure --prefix=/usr --bindir=/usr/sbin
make
sudo make install
```

Supported configure options:

```text
--prefix=PATH        install prefix, default: /usr/local
--bindir=PATH        binary install directory, default: PREFIX/bin
--sysconfdir=PATH    config directory, default: /etc/logcut
--varlibdir=PATH     state directory, default: /var/lib/logcut
--logdir=PATH        log directory, default: /var/log
--lockdir=PATH       lock directory, default: /var/lock
--go=PATH            Go command, default: go
--version=VERSION    build/package version override
--src=PATH           Go package source, default: .
```

The configure script writes `config.mk`, which is loaded by the Makefile.

## Build from source

The project is Go-module based. If required Go modules are not already available locally, Go resolves them through the module system.

```bash
go version
make
```

The Makefile is only a wrapper. The actual build logic is implemented in Go under `cmd/devtool`.

Equivalent direct Go command:

```bash
go run ./cmd/devtool build
```

## Create go.mod if missing

If someone downloads only the source files without `go.mod`, create it using Go code:

```bash
go run ./cmd/devtool modulecheck
```

This does not call `go mod init`; it writes the module file directly using Go file APIs.

## Install

Default install path is `/usr/local/bin/logcut`:

```bash
sudo make install
```

Direct Go devtool install:

```bash
sudo go run ./cmd/devtool install
```

Install under `/usr/bin/logcut` instead:

```bash
./configure --prefix=/usr
sudo make install
```

The install process creates:

- the configured `logcut` binary path
- `/usr/share/man/man8/logcut.8`
- `/usr/share/doc/logcut`
- the configured config directory
- the configured state directory
- the configured log directory
- the configured lock directory

After install, open the manual with:

```bash
man logcut
```

## Uninstall

```bash
sudo make uninstall
```

Direct Go devtool uninstall:

```bash
sudo go run ./cmd/devtool uninstall
```

The uninstall target removes the binary and man page. It intentionally keeps state, config, and logs for safety.

## Build packages

The `.deb` and `.rpm` packages are built through the nFPM Go module. No `dpkg-deb`, `rpmbuild`, or `fpm` package command is required.

Debian package:

```bash
make deb
```

RPM package:

```bash
make rpm
```

Direct Go devtool commands:

```bash
go run ./cmd/devtool deb
go run ./cmd/devtool rpm
```

Internally this uses the Go module:

```bash
go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
```

So if the package builder is not already available, Go downloads and runs it through the module system.

The generated files are placed under `dist/`.

## Build source tarball and checksums

```bash
make tar
make checksums
```

Direct Go devtool commands:

```bash
go run ./cmd/devtool tar
go run ./cmd/devtool checksums
```

The tarball and checksum generation are implemented with Go archive and crypto APIs.

## Runtime behavior

The `logcut` application itself does not shell out to Linux commands for rotation. It uses:

- Go file APIs
- Go gzip APIs
- Go buffered readers
- Go state-file handling
- Linux statfs/stat syscalls
- Linux file locking syscall
- Linux `fallocate` syscall for punch-hole

It does not call external runtime commands like `fallocate`, `cp`, `mv`, `split`, or `gzip`.

## Usage

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
sudo logcut --dry-run -g -k 10G file1.log file1.rotated.log.gz
sudo logcut -g -k 10G file1.log file1.rotated.log.gz
```

## Options

```text
-g              write gzip rotated archive
-k <size>       keep latest part in active log, for example 10G or 512M
-p <percent>    use only this percent of current free disk as working budget, default 20
--dry-run       print plan only
--force         allow risky plain output on low disk
--version       print logcut version
```

## Disk-safety rule

By default, only 20% of currently free disk is used as the chunk working budget. The remaining 80% is treated as protected space for application writes and spikes.

Example: if free space is 1 GB, logcut plans the next operation using only around 200 MB as the top-level budget, then applies another internal safety margin before choosing the raw chunk size.
