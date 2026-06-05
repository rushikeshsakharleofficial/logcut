# logcut Makefile
# Open-source friendly build/install/package targets for Linux systems.
#
# Common usage:
#   make
#   sudo make install
#   sudo make uninstall
#   make deb
#   make rpm
#
# Override install paths:
#   sudo make install PREFIX=/usr
#   make DESTDIR=/tmp/pkgroot install

APP_NAME       ?= logcut
VERSION        ?= 1.0.0
RELEASE        ?= 1
ARCH           ?= $(shell uname -m)
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
PKG_DIR        ?= packaging
BIN            := $(BUILD_DIR)/$(APP_NAME)

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
	rm -rf $(BUILD_DIR) $(DIST_DIR) $(PKG_DIR)
	@echo "Cleaned build artifacts"

package: deb rpm

# Build a Debian package without requiring fpm.
deb: build
	@rm -rf $(PKG_DIR)/deb
	@mkdir -p $(PKG_DIR)/deb/DEBIAN
	@mkdir -p $(PKG_DIR)/deb$(BINDIR)
	@mkdir -p $(PKG_DIR)/deb$(SYSCONFDIR)
	@mkdir -p $(PKG_DIR)/deb$(VARLIBDIR)
	install -m 0755 $(BIN) $(PKG_DIR)/deb$(BINDIR)/$(APP_NAME)
	@printf '%s\n' \
		'Package: $(APP_NAME)' \
		'Version: $(VERSION)' \
		'Section: admin' \
		'Priority: optional' \
		'Architecture: amd64' \
		'Maintainer: logcut maintainers <maintainers@example.com>' \
		'Description: Emergency log compaction and rotation tool' \
		' Logcut safely compacts huge active log files by streaming old data,' \
		' optionally gzip-compressing it, and freeing original blocks using hole punching.' \
		> $(PKG_DIR)/deb/DEBIAN/control
	dpkg-deb --build $(PKG_DIR)/deb $(APP_NAME)_$(VERSION)_amd64.deb
	@mkdir -p $(DIST_DIR)
	@mv $(APP_NAME)_$(VERSION)_amd64.deb $(DIST_DIR)/
	@echo "Created $(DIST_DIR)/$(APP_NAME)_$(VERSION)_amd64.deb"

# Build an RPM package using rpmbuild.
rpm: build
	@command -v rpmbuild >/dev/null 2>&1 || { echo "rpmbuild not found. Install rpm-build first."; exit 1; }
	@rm -rf $(PKG_DIR)/rpm
	@mkdir -p $(PKG_DIR)/rpm/BUILD $(PKG_DIR)/rpm/RPMS $(PKG_DIR)/rpm/SOURCES $(PKG_DIR)/rpm/SPECS $(PKG_DIR)/rpm/SRPMS
	@mkdir -p $(PKG_DIR)/rpm/root$(BINDIR) $(PKG_DIR)/rpm/root$(SYSCONFDIR) $(PKG_DIR)/rpm/root$(VARLIBDIR)
	install -m 0755 $(BIN) $(PKG_DIR)/rpm/root$(BINDIR)/$(APP_NAME)
	@tar -czf $(PKG_DIR)/rpm/SOURCES/$(APP_NAME)-$(VERSION).tar.gz -C $(PKG_DIR)/rpm/root .
	@printf '%s\n' \
		'Name: $(APP_NAME)' \
		'Version: $(VERSION)' \
		'Release: $(RELEASE)%{?dist}' \
		'Summary: Emergency log compaction and rotation tool' \
		'License: MIT' \
		'BuildArch: x86_64' \
		'Source0: %{name}-%{version}.tar.gz' \
		'' \
		'%description' \
		'Logcut safely compacts huge active log files by streaming old data,' \
		'optionally gzip-compressing it, and freeing original blocks using hole punching.' \
		'' \
		'%prep' \
		'%setup -q -c -T' \
		'tar -xzf %{SOURCE0}' \
		'' \
		'%build' \
		'' \
		'%install' \
		'mkdir -p %{buildroot}' \
		'cp -a * %{buildroot}/' \
		'' \
		'%files' \
		'$(BINDIR)/$(APP_NAME)' \
		'%dir $(SYSCONFDIR)' \
		'%dir $(VARLIBDIR)' \
		'' \
		'%changelog' \
		'* Fri Jun 05 2026 logcut maintainers <maintainers@example.com> - $(VERSION)-$(RELEASE)' \
		'- Initial package' \
		> $(PKG_DIR)/rpm/SPECS/$(APP_NAME).spec
	rpmbuild --define "_topdir $(CURDIR)/$(PKG_DIR)/rpm" -bb $(PKG_DIR)/rpm/SPECS/$(APP_NAME).spec
	@mkdir -p $(DIST_DIR)
	@cp $(PKG_DIR)/rpm/RPMS/x86_64/*.rpm $(DIST_DIR)/
	@echo "Created RPM under $(DIST_DIR)/"

tar: clean
	@mkdir -p $(DIST_DIR)/$(APP_NAME)-$(VERSION)
	@cp $(SRC) Makefile $(DIST_DIR)/$(APP_NAME)-$(VERSION)/
	@if [ -f README.md ]; then cp README.md $(DIST_DIR)/$(APP_NAME)-$(VERSION)/; fi
	@if [ -f LICENSE ]; then cp LICENSE $(DIST_DIR)/$(APP_NAME)-$(VERSION)/; fi
	@tar -czf $(DIST_DIR)/$(APP_NAME)-$(VERSION).tar.gz -C $(DIST_DIR) $(APP_NAME)-$(VERSION)
	@echo "Created $(DIST_DIR)/$(APP_NAME)-$(VERSION).tar.gz"

dist: tar deb

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
	@echo "  make deb          Build .deb package"
	@echo "  make rpm          Build .rpm package, requires rpmbuild"
	@echo "  make tar          Build source tarball"
	@echo "  make checksums    Create SHA256SUMS in dist/"
	@echo ""
	@echo "Variables:"
	@echo "  PREFIX=/usr       Install under /usr instead of /usr/local"
	@echo "  DESTDIR=/tmp/pkg  Stage install into a package root"
	@echo "  VERSION=1.0.0     Set package version"
