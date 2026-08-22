# Makefile for Scrinium MCP Server

BINARY_NAME = scrinium
VERSION = 0.2.0
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

# tidy-check reports WITHOUT writing: `go mod tidy -diff` exits non-zero and
# prints the diff when go.mod/go.sum are untidy, touching neither file. The
# previous cp/tidy/mv dance restored identical CONTENT but fresh inodes and
# mtimes, which stat-dirtied git's index — `git diff-index` then saw a dirty
# tree and the release guard refused a genuinely clean tree (measured
# 2026-08-22: owner's first `make release` died with `git status --porcelain`
# empty before and after).
tidy-check:
	go mod tidy -diff

# Install binary to /usr/local/bin
install: build
	$(INSTALL) -d $(DESTDIR)$(BINDIR)
	$(INSTALL) -m 755 $(BINARY_NAME) $(DESTDIR)$(BINDIR)/$(BINARY_NAME)

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)

# Release target: verify, bump patch version, commit, tag, and push tags to trigger GoReleaser CI.
# Refuses a dirty tree FIRST: the published v0.2.0 assets carried
# vcs.modified=true ("v0.2.0+dirty" in `go version -m`) — built from a tree
# with uncommitted tracked changes, so the binaries cannot be reproduced
# from the tag. A clean-clone build of the same tag stamps
# vcs.modified=false (measured 2026-08-22). Release CI asserts the same
# invariant on the built artifacts (see .github/workflows/release.yml).
release: verify
	@# update-index --refresh first: a STAT-dirty index (fresh inode/mtime,
	@# identical content — e.g. left behind by any tool that rewrites a file
	@# in place) must never read as a dirty tree. diff-index alone trusts
	@# the stale stat cache; `git status` refreshes it, which is why the
	@# 2026-08-22 false refusal showed a clean status beside a firing guard.
	@git update-index -q --refresh; \
	git diff-index --quiet HEAD -- || { \
		echo "release: REFUSED — working tree has uncommitted tracked changes; a release must be reproducible from its tag (v0.2.0 shipped +dirty)"; \
		exit 1; \
	}
	bump2version patch
	@NEW_VERSION=$$(grep "^VERSION =" Makefile | cut -d' ' -f3); \
	git add Makefile .bumpversion.cfg; \
	git commit -m "Release v$$NEW_VERSION"; \
	git tag v$$NEW_VERSION; \
	git push origin main --tags

.PHONY: build version test verify vet format-check staticcheck govulncheck tidy-check install clean release
