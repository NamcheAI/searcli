# searcli

A single-binary, read-only Google Search Console CLI. No runtime dependencies or hosted service. **Reviewed release-candidate source; no binary release yet. OAuth/API behavior is mock-tested, not live-Google validated.**

## Build

Go 1.23+:

~~~sh
go build -trimpath -o bin/searcli .
bin/searcli --help
~~~

## Google setup

1. Create/select a Google Cloud project and enable the **Google Search Console API**.
2. Configure its OAuth consent screen. For an external app in Testing, add your Google accounts as test users. Testing refresh tokens can expire after seven days; production consent/verification requirements depend on your app configuration.
3. Create an OAuth client of type **Desktop app**, download its JSON, and protect that file (chmod 600).
4. On a desktop with a browser, run:

~~~sh
searcli account add --name work --credentials /private/path/desktop-client.json
searcli account add --name personal --credentials /private/path/another-client.json
searcli account list
searcli properties --account work
~~~

Choose the intended Google account in the browser. Aliases are case-insensitive ASCII names, canonicalized to lowercase for files, locks, snapshots and CLI output; Work and work are the same account. Aliases are local names, not verified email addresses. This unreleased storage format has no legacy migration: if testing an older build on a case-sensitive filesystem, stop all searcli processes and deliberately remove or migrate the old mixed-case file (for example accounts/Work.json) in your private state directory before re-adding the lowercase alias. The repaired account remove command lowercases its input and cannot remove that legacy mixed-case filename. Never overwrite a conflicting lowercase account; fresh installations are unaffected. Only https://www.googleapis.com/auth/webmasters.readonly is requested. Login uses a random IPv4 loopback port, state validation, S256 PKCE and offline refresh. No interactive login is needed for subsequent scheduled commands. The browser must run on the same machine; headless/remote authorization is not implemented. Credentials are configurable per account, not embedded in the binary.

Copy the **exact siteUrl** returned by properties, including a URL-prefix property's trailing slash, or sc-domain:example.com. Each command verifies accessible property membership; properties includes Google permissionLevel.

~~~sh
searcli sitemaps --account work --property 'sc-domain:example.com'
searcli inspect --account work --property 'sc-domain:example.com' --url 'https://example.com/'
searcli check --account work --property 'sc-domain:example.com' --urls urls.txt --max 100
searcli account remove --name personal
~~~

Sitemaps returns Google's sitemap status JSON, including available errors, warnings, download dates and content counts. Removing an account deletes local credentials only; revoke access separately in your Google account. Existing snapshots remain. Remove/re-add to reauthorize an existing alias.

## Daily checks and JSON

urls.txt is an explicit UTF-8, newline-separated URL list. Blank lines and lines beginning with # are ignored; exact duplicates are removed. The entire list is validated before inspections. Maximum 2,000 unique URLs, 8 MiB file and 64 KiB per line; --max can lower the limit. Oversized lists fail rather than silently truncate. URLs must be HTTP(S), have no user info or fragment, and belong to the selected property. A slashless origin URL is treated as the root path for membership. Dot segments (including encoded dot segments), encoded separators/backslashes, literal backslashes and encoded percent signs in paths are rejected conservatively rather than guessing browser/server normalization. No discovery or crawling occurs.

All command results are JSON except help; errors are JSON on stderr. Login progress is also a JSON object on stderr, through the same CLI output path. The five-minute authorization deadline includes browser launch and token exchange; cancellation terminates the launcher. Any stdout/stderr write failure returns operational exit 1, even after a mutation or snapshot has already succeeded. check writes a dated snapshot and latest.json under snapshots/<account-property-hash>/ and emits the snapshot plus changes. First run reports added URLs; later runs report changed, added and removed. lastCrawlTime is retained but excluded from comparisons to avoid timestamp-only noise. Diffs compare the immediately preceding attempt, including errors, not a hidden last-success baseline. Missing index status and NEUTRAL/unspecified verdicts are **unknown**, not healthy or proven broken. PASS is ok; FAIL/PARTIAL are problem. A problem is Google's inspection finding, not necessarily an actionable technical defect; exclusions/canonical choices can be intentional.

Exit codes for inspect/check (precedence 1 > 4 > 3 > 0):

| Code | Meaning |
|---|---|
| 0 | All inspected results ok, or successful administrative command |
| 1 | API/auth/storage/quota/operational failure, including URLs not attempted after a failure |
| 2 | CLI or URL-list usage error |
| 3 | One or more problem results, no errors or unknowns |
| 4 | One or more unknown results, no operational errors; problems may also exist |

Changes do not alone determine the exit code. Inspect current results as well as changes. A check stops API requests after the first operational error and marks remaining URLs error/not attempted, then persists the partial snapshot. API errors contain HTTP status and (when supplied) Retry-After, never the response body. No automatic API retries: on 429 honor Retry-After externally; do not immediately retry the entire batch. Requests have a 30-second timeout; Ctrl-C cancels in-flight work.

## Quotas and limitations

Google URL Inspection limits are **2,000 requests/property/day and 600/property/minute**. searcli reserves each attempt in a persisted per-property rolling-24-hour ledger, shared across aliases in the same SEARCLI_HOME, and spaces attempts by at least 110 ms (below 600/minute). Failed attempts count conservatively. Concurrent operations on a locked quota ledger fail safely rather than oversubscribe. Google quotas also include project-level limits and usage from other clients; this local ledger cannot see other machines/tools. Do not use multiple homes to evade quotas. A full 2,000-URL daily run may need scheduling slightly more than 24 hours apart or a smaller list because the rolling budget is stricter than Google's reset window.

**Inspection reports Google's indexed version, not a live URL test.** Data can be stale. The API does not provide whole-site Page Indexing/coverage export, live crawl, crawling or indexing submission. Sitemap counts are not a complete coverage audit. This tool never claims an uninspected page is healthy.

## Scheduling examples (not installed automatically)

Authorize manually first. Use absolute binary, home and list paths. Keep output private because URLs can be sensitive. For example, an owner-managed cron entry:

~~~cron
15 7 * * * umask 077; SEARCLI_HOME=/home/me/.config/searcli /home/me/bin/searcli check --account work --property sc-domain:example.com --urls /home/me/urls.txt --max 500 > /home/me/searcli-latest.json 2> /home/me/searcli-error.json
~~~

On macOS, default storage differs from Linux; set SEARCLI_HOME consistently for manual and scheduled runs. A launchd job can invoke the same absolute command via ProgramArguments and StartCalendarInterval; supply SEARCLI_HOME via EnvironmentVariables and set private StandardOutPath/StandardErrorPath. This repository does not install or edit a scheduler. For alerting, capture $? immediately and map the documented exit codes; cron alone does not interpret findings.

## Security and state

Default state: Go os.UserConfigDir()/searcli (macOS ~/Library/Application Support/searcli; Linux typically ~/.config/searcli). SEARCLI_HOME overrides it. Account filenames accept only 1–64 letters, digits, _ and -. Tokens and per-account Desktop credentials are local plaintext, not encrypted/keychain-backed: directories are 0700, atomic replacement files 0600, flushed before rename. Refresh rotation is saved before use. Do not sync state to a shared directory, check it into Git, or give it to untrusted users. Use a private, trusted parent directory; filesystem permissions cannot defend against a compromised same-user process, root, or hostile ancestor symlinks. Backup only with encryption. Credentials/refresh/access tokens are never emitted by CLI commands; account list shows aliases only. OAuth authorization URLs are passed directly to the system browser, not printed; OS process inspection by the same user is outside this boundary.

Exclusive on-disk operation locks protect refresh/check/quota updates. After a hard crash, verify no searcli process is active before removing stale files inside SEARCLI_HOME/locks. Snapshots and quota files contain URLs/timestamps, no tokens. Snapshot retention is manual; do not delete the quota ledger to bypass limits. Google account revocation is separate from local removal.

## Development and release

~~~sh
gofmt -w *.go
go test ./...
go test -race ./...
go vet ./...
~~~

Tests use httptest, no live credentials. Live consent/API smoke testing still requires an authorized Google Desktop client and accessible property. CI tests on Linux/macOS. Tag-triggered release builds macOS arm64/amd64 archives and computes real SHA-256 checksums; no release exists merely because this scaffold exists. See packaging/README.md. Source publication has passed independent review; binary releases still require live validation and release approval.

References: [Inspection API](https://developers.google.com/webmaster-tools/v1/urlInspection.index/inspect), [quotas](https://developers.google.com/webmaster-tools/limits), [OAuth desktop apps](https://developers.google.com/identity/protocols/oauth2/native-app), [sitemaps](https://developers.google.com/webmaster-tools/v1/sitemaps/list).
