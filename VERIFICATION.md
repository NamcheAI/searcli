# Local verification — 2026-10-13

Implementation pending independent review; nothing published.

Environment: Go 1.27.2 darwin/arm64. Module declares Go 1.23; CI is configured for 1.23.x (not locally executed on that version).

Executed successfully:

- gofmt -w *.go; subsequent gofmt -l was empty.
- go test ./...: ok github.com/namcheai/searcli 0.812s
- go vet ./...: no output, exit 0.
- go test -race ./...: ok github.com/namcheai/searcli 1.927s
- go test -cover ./...: ok, coverage 67.0% of statements.
- Native go build -trimpath -o bin/searcli .
- CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 and amd64 builds to dist/.
- file: Mach-O arm64 and Mach-O x86_64 respectively.
- Native version: {"version":"dev"}; help and empty account list successful.
- Native out-of-property inspect: sanitized JSON error, exit 2, no API call.
- ruby -c packaging/searcli.rb.in: Syntax OK (template not installed/audited).

11 top-level tests plus HTTP status subtests cover PKCE authorization parameters, callback state rejection, offline exchange, refresh rotation persistence and refresh retention, account isolation/removal, file mode and traversal rejection, exact accessible property validation, URL membership, persisted snapshots/diffs, unknown/problem/error exit precedence, malformed/missing API responses, 401/403/404/429/500, fail-stop partial checks, URL list bounds/deduplication, quota exhaustion and locking. No Google login was initiated. No live credentials were supplied or used.

GitHub read-only checks: gh authenticated as jodok; active admin of NamcheAI; organization allows members to create repositories. namcheai/searcli and namche/homebrew-tap could not be resolved. namche is a User account, not an organization; tap write access is not established. No tokens printed by project commands or stored in this repository.

Remaining verification: independent code/security review, live Google Desktop OAuth + real Search Console property, both macOS architecture execution (amd64 only cross-built here), CI execution, real release archive checksums and Homebrew audit/install/test. MIT license is scaffolded; owner should confirm licensing before publication. Packaging checklist in packaging/README.md. The project currently resides within its parent's Git worktree; create an isolated repository deliberately at publication time and never commit unrelated parent files.

Known tradeoffs documented in README: plaintext private local credentials; trusted storage ancestors; manual stale-lock recovery and snapshot retention; conservative rolling quota shared only within one home; no automatic retries; aliases not verified email identities; previous-attempt rather than last-success diffs; inspection data is indexed, not live; no whole-site coverage.

## Review remediation — 2026-10-13

All requested code findings addressed; release remains on HOLD pending re-review.

- Canonical lowercase account filenames, account locks, CLI identities and snapshot/check identities. Regression exercises refresh concurrently with differently cased refresh/remove/add on the actual case-insensitive macOS filesystem (probe logged true), plus shared snapshot history.
- Every CLI JSON/help write checked; failed stdout or stderr returns exit 1. Login progress now uses injected stderr as JSON.
- Out-of-property check-list input returns usage exit 2 before network access.
- Top-level error JSON preserves http_status and retry_after for properties, preliminary property validation and sitemap errors, as inspection already did.
- Bounded response reading followed by whole-document JSON parsing rejects trailing garbage, additional JSON values and oversized suffixes for both API and OAuth responses.
- Root slash normalization; conservative rejection of raw/encoded traversal and ambiguous separators/percent encoding.
- Five-minute context covers browser launcher, callback wait and exchange; launcher uses CommandContext and bounded WaitDelay. Tests inject canceled launcher and failed progress writer.
- Homebrew docs now accurately describe installation of prebuilt binaries.

Final checks: gofmt clean; go test ./... passed (1.426s); go vet ./... exit 0; go test -race ./... passed (2.620s); coverage 79.1%. Native and both macOS architecture binaries rebuilt successfully after source fixes. Native version/account-list smoke passed; invalid list smoke returned JSON and exit 2. Writer failures verified with injected failing writers in regression tests (the attempted /dev/full shell smoke was blocked by macOS before launching the binary and is NOT evidence of CLI handling). Ruby template syntax still OK.

New files: validation.go and review_test.go. Existing auth.go, api.go, main.go, README.md and packaging/README.md updated. No live Google credentials, publication, tap writes or scheduler edits. Legacy mixed-case state from unreleased builds has no automatic migration on case-sensitive filesystems; documented remove/re-add procedure. Other previously documented limitations remain.
