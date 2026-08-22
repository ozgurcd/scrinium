# Makefile for Scrinium MCP Server

BINARY_NAME = scrinium
VERSION = 0.4.0
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

# Release level: make release BUMP=minor cuts a minor release; the default
# stays patch. Validated BEFORE anything runs (check-bump is the first
# prerequisite): a typo like BUMP=mnor refuses by name and never reaches
# verify or bump2version. Do NOT pre-bump by hand and then run release —
# release bumps again (double bump); and do NOT hand-run the release steps —
# that bypasses the verify + porcelain guards that exist because two
# releases already shipped +dirty.
BUMP ?= patch

check-bump:
	@case "$(BUMP)" in \
		major|minor|patch) ;; \
		*) echo "release: REFUSED — BUMP must be major, minor, or patch (got \"$(BUMP)\")"; exit 1 ;; \
	esac

# Release target: verify, bump $(BUMP) version, commit, tag, and push tags to trigger GoReleaser CI.
# Refuses an unclean tree FIRST. History of the +dirty releases, corrected:
# v0.2.0 AND v0.2.1 both published binaries stamping vcs.modified=true.
# The cause was NOT a stray local dirty build — the release pipeline
# stamped +dirty BY CONSTRUCTION: goreleaser writes dist/ into the tree,
# dist/ was not gitignored, and Go's stamper counts untracked files
# (measured 2026-08-22: fresh clone + goreleaser build → +dirty; with
# dist/ gitignored → vcs.modified=false; a single untracked file alone
# flips the stamp). dist/ is now ignored and every build carries a
# provenance post hook inside .goreleaser.yaml that fails the release
# BEFORE publication.
release: check-bump verify
	@# The guard measures what Go's build stamper measures: `git status
	@# --porcelain` — tracked changes AND untracked files both stamp
	@# vcs.modified=true into the binaries (measured: an untracked file
	@# alone flips the stamp). diff-index was wrong twice over: it missed
	@# untracked files entirely, and its stale stat cache once refused a
	@# genuinely clean tree. update-index --refresh is kept so stat-dirt
	@# (fresh inode, identical content) never reads as dirty.
	@git update-index -q --refresh; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "release: REFUSED — working tree is not clean (tracked changes or untracked files); Go stamps vcs.modified=true for either, and a release must be reproducible from its tag (v0.2.0 and v0.2.1 both shipped +dirty)"; \
		git status --porcelain; \
		exit 1; \
	fi
	bump2version $(BUMP)
	@# Rebuild AFTER the bump so ./$(BINARY_NAME) and the Makefile agree:
	@# verify's build runs PRE-bump by design (a failing verify after the
	@# bump would leave a dirty tree the porcelain guard then refuses), so
	@# its scratch binary carries the PREVIOUS version — the v0.4.0 cut
	@# printed a 0.3.1 ldflags line for exactly this reason. The local
	@# stamp is asserted here; the PUBLISHED stamp is asserted by the
	@# goreleaser per-build post hook.
	@NEW_VERSION=$$(grep "^VERSION =" Makefile | cut -d' ' -f3); \
	$(MAKE) --no-print-directory build; \
	if ! ./$(BINARY_NAME) version | grep -qx "scrinium $$NEW_VERSION"; then \
		echo "release: REFUSED — rebuilt binary prints '$$(./$(BINARY_NAME) version)', want 'scrinium $$NEW_VERSION'"; \
		exit 1; \
	fi; \
	git add Makefile .bumpversion.cfg; \
	git commit -m "Release v$$NEW_VERSION"; \
	git tag v$$NEW_VERSION; \
	git push origin main --tags

.PHONY: build version test verify vet format-check staticcheck govulncheck tidy-check install clean release check-bump
