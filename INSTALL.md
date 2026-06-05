# logcut installation

## Build from source

The project is Go-module based. If dependencies such as the package builder are not already available locally, Go resolves them through modules.

```bash
go version
make
```

## Install

Default install path is `/usr/local/bin/logcut`:

```bash
sudo make install
```

Install under `/usr/bin/logcut` instead:

```bash
sudo make install PREFIX=/usr
```

## Uninstall

```bash
sudo make uninstall
```

The uninstall target removes only the binary. It intentionally keeps `/var/lib/logcut`, `/etc/logcut`, and logs for safety.

## Build packages

The `.deb` and `.rpm` packages are built through the nFPM Go module. No `dpkg-deb`, `rpmbuild`, `fpm`, `cp`, `mv`, `split`, or gzip shell commands are required for package generation.

Debian package:

```bash
make deb
```

RPM package:

```bash
make rpm
```

Both commands use:

```bash
go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
```

So if the packaging module is not already available, Go downloads and runs it through the module system.

The generated files are placed under `dist/`.

## Runtime behavior

The `logcut` application itself does not shell out to Linux commands for rotation. It uses Go file APIs, gzip APIs, filesystem stat syscalls, file locking, and Linux `fallocate` syscall directly.

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
