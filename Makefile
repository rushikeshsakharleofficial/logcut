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
SRC ?= ./cmd/logcut
DEVTOOL := SRC=$(SRC) PREFIX=$(PREFIX) BINDIR=$(BINDIR) SYSCONFDIR=$(SYSCONFDIR) VARLIBDIR=$(VARLIBDIR) LOGDIR=$(LOGDIR) LOCKDIR=$(LOCKDIR) VERSION=$(VERSION) $(GO) run ./cmd/devtool
TEST_TMP ?= .tmp/local-test
TEST_LOG_LINES ?= 200000

.PHONY: all modulecheck build install uninstall reinstall clean test test-disaster dry-run test-logfile test-small-machine package deb rpm tar dist checksums help

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

test-disaster:
	$(GO) test -v -run TestIntegrationDisasterRecovery ./internal/cli/ -timeout 60m

dry-run: build
	@echo "Example dry run command:"
	@echo "  build/logcut --dry-run -g -k 10G /var/log/app/debug.log /var/log/app/debug.log.rotated.gz"

test-logfile:
	@mkdir -p "$(TEST_TMP)"
	@awk 'BEGIN { for (i = 0; i < $(TEST_LOG_LINES); i++) printf("2026-06-06T00:00:00Z level=debug seq=%09d message=local logcut test payload payload payload payload payload payload\n", i) }' > "$(TEST_TMP)/app.log"
	@rm -f "$(TEST_TMP)/app.rotated.log.gz" "$(TEST_TMP)/logcut-run.log"
	@rm -rf "$(TEST_TMP)/state" "$(TEST_TMP)/lock"
	@mkdir -p "$(TEST_TMP)/state" "$(TEST_TMP)/lock"
	@echo "Created $(TEST_TMP)/app.log"
	@du -h "$(TEST_TMP)/app.log"

test-small-machine: build test-logfile
	build/logcut -v \
	  --log-file "$(TEST_TMP)/logcut-run.log" \
	  --max-runtime 5m \
	  --rate-limit 25M \
	  --sleep-between-chunks 2s \
	  --compress-level 1 \
	  --verify none \
	  -p 5 \
	  -g -k 1M \
	  --state-dir "$(TEST_TMP)/state" \
	  --lock-dir "$(TEST_TMP)/lock" \
	  "$(TEST_TMP)/app.log" \
	  "$(TEST_TMP)/app.rotated.log.gz"
	@echo "Run log: $(TEST_TMP)/logcut-run.log"
	@du -h "$(TEST_TMP)/app.log" "$(TEST_TMP)/app.rotated.log.gz"

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
