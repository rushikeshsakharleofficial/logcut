# logcut Makefile
# Open-source friendly build/install/package targets for Linux systems.
#
# Runtime code uses Go APIs/syscalls directly. Packaging uses the nFPM Go module
# directly through `go run`; no dpkg-deb, rpmbuild, preinstalled nfpm binary,
# cp, mv, split, gzip shell commands are used by the logcut application.

APP_NAME       ?= logcut
VERSION        ?= 1.0.0
GO             ?= go
GOOS           ?= linux
GOARCH         ?= $(shell go env GOARCH 2>/dev/null || echo amd64)
CGO_ENABLED    ?= 0

PREFIX         ?= /usr/local
BINDIR         ?= $(PREFIX)/bin
SYSCONFDIR     ?= /etc/$(APP_NAME)
VARLIBDIR      ?= /var/lib/$(APP_NAME)
LOGDIR         ?= /var/log
LOCKDIR        ?= /var/lock

SRC            ?= $(APP_NAME).go
BUILD_DIR      ?= build
DIST_DIR       ?= dist
BIN            := $(BUILD_DIR)/$(APP_NAME)
NFPM_MODULE    ?= github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

GOFLAGS        ?= -trimpath
LDFLAGS        ?= -s -w -X main.version=$(VERSION)

.PHONY: all build clean install uninstall reinstall test dry-run package deb rpm tar dist checksums help

all: build

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) $(SRC)
	@echo "Built $(BIN)"

install: build
	install -d -m 0755 $(DESTDIR)$(BINDIR)
	install -m 0755 $(BIN) $(DESTDIR)$(BINDIR)/$(APP_NAME)
	install -d -m 0755 $(DESTDIR)$(SYSCONFDIR)
	install -d -m 0755 $(DESTDIR)$(VARLIBDIR)
	install -d -m 0755 $(DESTDIR)$(LOGDIR)
	install -d -m 0755 $(DESTDIR)$(LOCKDIR)
	@echo "Installed $(APP_NAME) to $(DESTDIR)$(BINDIR)/$(APP_NAME)"
	@echo "State directory: $(DESTDIR)$(VARLIBDIR)"
	@echo "Config directory: $(DESTDIR)$(SYSCONFDIR)"

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(APP_NAME)
	@echo "Removed $(DESTDIR)$(BINDIR)/$(APP_NAME)"
	@echo "Keeping $(DESTDIR)$(VARLIBDIR), $(DESTDIR)$(SYSCONFDIR), and logs for safety. Remove manually if needed."

reinstall: uninstall install

test:
	$(GO) test ./...

dry-run: build
	@echo "Example dry run command:"
	@echo "  $(BIN) --dry-run -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz"

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)
	@echo "Cleaned build artifacts"

package: deb rpm

deb: build
	@mkdir -p $(DIST_DIR)
	$(GO) run $(NFPM_MODULE) package --packager deb --config nfpm.yaml --target $(DIST_DIR)/$(APP_NAME)_$(VERSION)_amd64.deb
	@echo "Created $(DIST_DIR)/$(APP_NAME)_$(VERSION)_amd64.deb"

rpm: build
	@mkdir -p $(DIST_DIR)
	$(GO) run $(NFPM_MODULE) package --packager rpm --config nfpm.yaml --target $(DIST_DIR)/$(APP_NAME)-$(VERSION)-1.x86_64.rpm
	@echo "Created $(DIST_DIR)/$(APP_NAME)-$(VERSION)-1.x86_64.rpm"

tar: clean
	@mkdir -p $(DIST_DIR)/$(APP_NAME)-$(VERSION)
	@cp $(SRC) go.mod Makefile nfpm.yaml $(DIST_DIR)/$(APP_NAME)-$(VERSION)/
	@if [ -f README.md ]; then cp README.md $(DIST_DIR)/$(APP_NAME)-$(VERSION)/; fi
	@if [ -f INSTALL.md ]; then cp INSTALL.md $(DIST_DIR)/$(APP_NAME)-$(VERSION)/; fi
	@if [ -f LICENSE ]; then cp LICENSE $(DIST_DIR)/$(APP_NAME)-$(VERSION)/; fi
	@tar -czf $(DIST_DIR)/$(APP_NAME)-$(VERSION).tar.gz -C $(DIST_DIR) $(APP_NAME)-$(VERSION)
	@echo "Created $(DIST_DIR)/$(APP_NAME)-$(VERSION).tar.gz"

dist: tar deb rpm

checksums:
	@mkdir -p $(DIST_DIR)
	@cd $(DIST_DIR) && sha256sum * > SHA256SUMS
	@echo "Created $(DIST_DIR)/SHA256SUMS"

help:
	@echo "Targets:"
	@echo "  make              Build $(APP_NAME)"
	@echo "  make install      Install to $(PREFIX)/bin"
	@echo "  make uninstall    Remove installed binary only"
	@echo "  make clean        Remove build/package artifacts"
	@echo "  make deb          Build .deb package using nFPM Go module"
	@echo "  make rpm          Build .rpm package using nFPM Go module"
	@echo "  make tar          Build source tarball"
	@echo "  make checksums    Create SHA256SUMS in dist/"
	@echo ""
	@echo "Variables:"
	@echo "  PREFIX=/usr       Install under /usr instead of /usr/local"
	@echo "  DESTDIR=/tmp/pkg  Stage install into a package root"
	@echo "  VERSION=1.0.0     Set package version"
	@echo "  NFPM_MODULE=...   Override nFPM Go module path/version"
