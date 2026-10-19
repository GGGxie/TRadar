SHELL := /bin/sh

GO ?= go
NPM ?= npm
SQLITE3 ?= sqlite3
GOTOOLCHAIN ?= go1.26.9+auto
TRADAR_DB_PATH ?= data/radar.db
BACKUP_DIR ?= backups
BACKUP_FILE ?= $(BACKUP_DIR)/tradar-$(shell date -u +%Y%m%dT%H%M%SZ).db

export GOTOOLCHAIN

.DEFAULT_GOAL := help
.PHONY: help setup build run test e2e verify backup restore clean

help:
	@echo "TRadar local commands"
	@echo "  make setup                         Download locked dependencies"
	@echo "  make build                         Build web/dist and bin/tradar"
	@echo "  make run                           Start the built local service"
	@echo "  make test                          Run Go race/vet and frontend tests"
	@echo "  make e2e                           Build and run Chromium flows"
	@echo "  make verify                        Run the release verification set"
	@echo "  make backup                        Create and check an online SQLite backup"
	@echo "  make restore RESTORE_FROM=... CONFIRM_RESTORE=yes"

setup:
	$(GO) mod download
	$(NPM) --prefix web ci

build:
	$(NPM) --prefix web run build
	@mkdir -p bin
	$(GO) build -buildvcs=false -trimpath -o bin/tradar ./cmd/tradar

run: build
	./bin/tradar

test:
	$(GO) test -race ./...
	$(GO) vet ./...
	$(NPM) --prefix web run test -- --run

e2e: build
	$(NPM) --prefix web run test:e2e

verify: test e2e
	$(NPM) --prefix web audit --offline

backup:
	@command -v "$(SQLITE3)" >/dev/null 2>&1 || { echo "sqlite3 is required for backup" >&2; exit 1; }
	@test -f "$(TRADAR_DB_PATH)" || { echo "database not found: $(TRADAR_DB_PATH)" >&2; exit 1; }
	@mkdir -p "$(dir $(BACKUP_FILE))"
	@"$(SQLITE3)" "$(TRADAR_DB_PATH)" ".timeout 5000" ".backup '$(BACKUP_FILE)'"
	@test "$$($(SQLITE3) "$(BACKUP_FILE)" 'PRAGMA quick_check;')" = "ok" || { echo "backup integrity check failed" >&2; exit 1; }
	@chmod 600 "$(BACKUP_FILE)"
	@echo "backup created: $(BACKUP_FILE)"

restore:
	@test "$(CONFIRM_RESTORE)" = "yes" || { echo "set CONFIRM_RESTORE=yes after stopping TRadar" >&2; exit 1; }
	@test -n "$(RESTORE_FROM)" || { echo "set RESTORE_FROM to a backup database" >&2; exit 1; }
	@command -v "$(SQLITE3)" >/dev/null 2>&1 || { echo "sqlite3 is required for restore" >&2; exit 1; }
	@test -f "$(RESTORE_FROM)" || { echo "backup not found: $(RESTORE_FROM)" >&2; exit 1; }
	@test ! -e "$(TRADAR_DB_PATH)-wal" && test ! -e "$(TRADAR_DB_PATH)-shm" || { echo "WAL files found; stop TRadar cleanly before restore" >&2; exit 1; }
	@test "$$($(SQLITE3) "$(RESTORE_FROM)" 'PRAGMA quick_check;')" = "ok" || { echo "source backup integrity check failed" >&2; exit 1; }
	@mkdir -p "$(dir $(TRADAR_DB_PATH))"
	@set -eu; \
		now=$$(date -u +%Y%m%dT%H%M%SZ); \
		if test -f "$(TRADAR_DB_PATH)"; then \
			cp -p "$(TRADAR_DB_PATH)" "$(TRADAR_DB_PATH).before-restore-$$now"; \
			echo "previous database saved: $(TRADAR_DB_PATH).before-restore-$$now"; \
		fi; \
		install -m 600 "$(RESTORE_FROM)" "$(TRADAR_DB_PATH).restoring"; \
		mv "$(TRADAR_DB_PATH).restoring" "$(TRADAR_DB_PATH)"
	@echo "database restored: $(TRADAR_DB_PATH)"

clean:
	rm -rf bin web/dist
