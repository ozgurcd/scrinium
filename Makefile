# Makefile for Scrinium MCP Server

BINARY_NAME = scrinium
VERSION = 0.1.3
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
INSTALL ?= install
LDFLAGS = -X scrinium/cmd/scrinium.version=$(VERSION)

# Build targets
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

version:
	@echo $(VERSION)

test:
	go test ./... -count=1 -timeout=120s

# Verify: build + test + lint + format check
verify: build test vet format-check staticcheck govulncheck tidy-check

vet:
	go vet ./...

format-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

staticcheck:
	staticcheck ./...

govulncheck:
	govulncheck ./...

tidy-check:
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak 2>/dev/null || true
	@go mod tidy
	@if ! diff -q go.mod go.mod.bak > /dev/null 2>&1; then \
		mv go.mod.bak go.mod; mv go.sum.bak go.sum 2>/dev/null || true; \
		echo "go.mod is not tidy — run 'go mod tidy'"; \
		exit 1; \
	fi
	@mv go.mod.bak go.mod 2>/dev/null || true; mv go.sum.bak go.sum 2>/dev/null || true

# Install binary to /usr/local/bin
install: build
	$(INSTALL) -d $(DESTDIR)$(BINDIR)
	$(INSTALL) -m 755 $(BINARY_NAME) $(DESTDIR)$(BINDIR)/$(BINARY_NAME)

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)

# Release target: verify, bump patch version, commit, tag, and push tags to trigger GoReleaser CI
release: verify
	bump2version patch
	@NEW_VERSION=$$(grep "^VERSION =" Makefile | cut -d' ' -f3); \
	git add Makefile .bumpversion.cfg; \
	git commit -m "Release v$$NEW_VERSION"; \
	git tag v$$NEW_VERSION; \
	git push origin main --tags

.PHONY: build version test verify vet format-check staticcheck govulncheck tidy-check install clean release
