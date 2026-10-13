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

GitHub read-only checks: gh authenticated as jodok; active admin of NamcheAI; organization allows members to create repositories. namcheai/searcli and NamcheAI/homebrew-tap could not be resolved. namche is a User account, not an organization; tap write access is not established. No tokens printed by project commands or stored in this repository.

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

## Published experimental v0.1.0

Independent re-review cleared blockers; user authorized source/release/tap publication and confirmed namcheai/tap. Released source commit 09c0afaeaa1d9de766357be5da24d607e804af76. Source CI 38412623073 and tag CI 38412808585 passed on Linux/macOS; Release workflow 38412810219 passed. Public prerelease assets were downloaded and both SHA-256 values verified against checksums.txt. Native arm64 release version output v0.1.0. Homebrew install, test and strict audit passed after formula ordering/version fixes; installed at /opt/homebrew/Cellar/searcli/0.1.0. Homebrew auto-enabled developer mode during tests; restored off afterward. No other packages/taps removed.

arm64 archive SHA-256: 7d7c550e2959b128ddc16b99e229008351094c3e39b32c1e5f437cbf6d803842
amd64 archive SHA-256: 6eac641d1720aa97a8a5ae7bb5a30409f47da84bd85ab47af869c19d3a4ae0b9

Live Google OAuth/API remains unverified; no credentials or grants used. amd64 release execution, Apple signing/notarization and production readiness are not claimed. Prior entries above are historical pre-publication observations.

## Bundled public Desktop client preparation (unpublished)

Base main 31525796473fd8e138e608248c093d9e37abbdde; existing tag v0.1.0. New client.go embeds oauth_client.json and accepts an explicit custom JSON override. Current oauth_client.json is intentionally {}: exact approved Namche/google-searcli client was NOT retrieved. command -v op found no executable; op --version returned command not found. Tool catalog search found no password-manager access tool. No vault inventory, item contents, credentials or user tokens were read. Do not release as ready until authorized local retrieval supplies the approved public configuration.

Default/override full loopback OAuth tests use synthetic public clients and synthetic tokens; verify PKCE/scope, exchange client selection, refresh persistence and no sensitive CLI output. Validation tests reject malformed/web/missing fields and forbid silent fallback. Existing tests/vet/race pass: go test ./... 1.442s; go test -race ./... 2.627s; go vet exit 0. Native build and help/version smoke passed. No real Google browser launched. No commit/push/tag/release for these changes; review remains required.

Follow-up prerequisite resolved: official Homebrew cask 1password-cli installed successfully, op version 2.41.0. op whoami --format=json (stdout suppressed) failed with “no account found for filter”. Current blocker is human 1Password sign-in/desktop CLI integration, not installation. No unrelated items/vaults listed, target item not accessed, no account configuration changed. Approved client remains unbundled; oauth_client.json remains {}.

### Bundled-client review fixes

Explicit --credentials=, --credentials "" and whitespace-only paths now return usage 2 before browser/network access, distinguished via flag.Visit. Embedded configuration uses an exact token-level allowlist (installed/client_id/client_secret only), rejects duplicate keys, unknown fields, extra objects and malformed suffixes. Custom Google-download overrides retain metadata compatibility and are not embedded. Release workflow invokes go run . validate-release-client against the real embedded bytes before archives/uploads, not a mocked test.

Regression tests cover negative/positive gate paths, no value echo, unknown token/export fields, duplicate fields and empty overrides. go test ./... passed 1.547s; go vet exit 0; go test -race ./... passed 2.654s; native build passed. Actual built binary with unchanged {} placeholder returned sanitized JSON “invalid bundled public Desktop client configuration”, exit 1. No 1Password fetch attempted, no publication. Authentication and approved public-client retrieval remain blockers.

Authentication correction: requester confirms existing service-account token/integration. Presence-only checks found OP_SERVICE_ACCOUNT_TOKEN absent in both default exec and explicit host=gateway environments. Earlier whoami result was not sufficient evidence of missing host setup. Supported tool discovery exposed no protected-secret metadata surface for 1Password. Precise blocker is credential inheritance/routing into this task, not a request for new sign-in. No raw configuration or secret stores read; no token values exposed; no item retrieval attempted without inherited service authentication.

## v0.1.1 public client retrieval

Broker retrieval succeeded after user sign-in for exact curated slugs searcli-client-id and searcli-client-secret. Returned item title google-searcli and field labels client-id/client-secret verified. Correct vault oc-tenzing, account Namche. Only the two explicitly public-approved Desktop application values were written to the minimal embedded schema; no full export, broker credentials or user tokens. Earlier access-blocker notes are historical. Actual embedded release validation now succeeds. Independent review accepted all code fixes. Real Google consent/API interoperability remains untested.

## User supersession: runtime-only remediation

Public bundling permission revoked. Remote refs inspected: main/HEAD 31525796473fd8e138e608248c093d9e37abbdde, only tag v0.1.0 at 09c0afaeaa1d9de766357be5da24d607e804af76. Rejected 6f0f0ea commit not reachable by remote refs. Local unpublished commit reset to published base, local reflogs expired and unreachable Git objects pruned. Removed oauth_client.json and credential-bearing bin/searcli; cleared task retrieval stores. No remote mutations, force push, 1Password item changes or user token deletion. This is not forensic erasure of rejected GitHub uploads, logs, backups or underlying storage. Earlier bundling statements are historical and superseded.

Runtime-only implementation pending independent review; no publication permitted.

## Clean file-import release authorization

Independent review accepted the credential-file implementation. No embedded credential file remains, rejected credential commit is absent from local object lookup and not in publication history. Repository guard and synthetic import tests pass; exact-value scan was not performed because private provisioning is separately blocked by secure-transfer constraints. No further real credential retrieval was performed. Fresh isolated build cache used for clean patch build. Residual earlier caches/backups are not comprehensively erased.
