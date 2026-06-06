# logcut Makefile
#
# Thin wrapper around the Go-based developer tool.
# The actual build, install, packaging, tar, checksum, and module bootstrap logic
# is implemented in cmd/devtool using Go APIs and Go modules.
#
# Optional configuration:
#   ./configure --prefix=/usr
#   make
#   sudo make install

-include config.mk

GO ?= go
SRC ?= .
DEVTOOL := SRC=$(SRC) PREFIX=$(PREFIX) BINDIR=$(BINDIR) SYSCONFDIR=$(SYSCONFDIR) VARLIBDIR=$(VARLIBDIR) LOGDIR=$(LOGDIR) LOCKDIR=$(LOCKDIR) VERSION=$(VERSION) $(GO) run ./cmd/devtool

.PHONY: all modulecheck build install uninstall reinstall clean test dry-run package deb rpm tar dist checksums help

all: build

modulecheck:
	$(DEVTOOL) modulecheck

build:
	$(DEVTOOL) build

install:
	$(DEVTOOL) install

uninstall:
	$(DEVTOOL) uninstall

reinstall: uninstall install

test:
	$(GO) test ./...

dry-run: build
	@echo "Example dry run command:"
	@echo "  build/logcut --dry-run -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz"

clean:
	$(DEVTOOL) clean

package: deb rpm

deb:
	$(DEVTOOL) deb

rpm:
	$(DEVTOOL) rpm

tar:
	$(DEVTOOL) tar

dist:
	$(DEVTOOL) dist

checksums:
	$(DEVTOOL) checksums

help:
	$(DEVTOOL) help
