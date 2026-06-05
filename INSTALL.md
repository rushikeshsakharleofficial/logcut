# logcut installation

## Build from source

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

Debian/Ubuntu:

```bash
make deb
sudo dpkg -i dist/logcut_1.0.0_amd64.deb
```

RHEL/Rocky/CentOS/Fedora:

```bash
sudo dnf install rpm-build
make rpm
sudo rpm -ivh dist/logcut-1.0.0-1*.rpm
```

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
