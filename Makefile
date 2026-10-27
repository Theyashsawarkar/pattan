# Build:                             make
# System-wide or packaged install:   make install [PREFIX=/usr/local] [DESTDIR=]
# Per-user, without make:            ./install.sh
PREFIX ?= /usr/local
BINDIR  = $(DESTDIR)$(PREFIX)/bin
SHARE   = $(DESTDIR)$(PREFIX)/share

.PHONY: build install uninstall check test lint dist

build:
	go build -trimpath -o bin/pattan ./cmd/pattan

install: build
	install -Dm755 bin/pattan $(BINDIR)/pattan
	install -Dm644 README.md $(SHARE)/doc/pattan/README.md
	install -Dm644 CHANGELOG.md $(SHARE)/doc/pattan/CHANGELOG.md
	install -Dm644 LICENSE $(SHARE)/licenses/pattan/LICENSE

uninstall:
	rm -f $(BINDIR)/pattan
	rm -rf $(SHARE)/doc/pattan $(SHARE)/licenses/pattan

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	shellcheck -S warning pattan.tmux install.sh scripts/pattan-run

# What CI runs.
check: lint test build
	./bin/pattan --version
	./bin/pattan --help >/dev/null

# Release binaries (CI does this on a tag).
dist:
	mkdir -p dist
	for arch in amd64 arm64; do \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -ldflags='-s -w' -o dist/pattan-linux-$$arch ./cmd/pattan; \
	done
