# Theophysics research control center — first working slice

This is a separate product interface over Hister. Open the control center from
Windows using a browser pointed at your private server. PostgreSQL owns document
identity, jobs, source receipts, category relationships, duplicate candidates,
classification proposals, review decisions, index tasks, audit events, and export
history. Hister retains its own database and Bleve full-text index. Original bytes
are held in the orchestration service's content-addressed document store.

This release is a single trusted research workspace. A shared bearer token is
required. It is not a multi-user or public MCP server. The browser is the desktop
control center in this slice; a packaged native Windows client is not included.

## What works

1. Create a named research job and optionally select initial categories.
2. Upload files, select a folder, or drop a directory tree in Edge/Chrome on
   Windows. Large collections upload sequentially, show progress, and support
   retrying failed files. Each file is limited to 32 MiB; supported extraction
   includes text/code, Markdown, HTML, PDF with a text layer, DOCX, and Org.
3. Import HTTP(S) URLs directly or load links from CSV, TSV, or XLSX. Spreadsheet
   cells and XLSX hyperlink targets are supported; formulas are never evaluated.
   The spreadsheet itself is preserved with a hash and source receipt. Review the loaded list, then import. The server fetches only exact public
   hostnames enabled in `TP_URL_HOSTS`, including redirect destinations. URL
   fetches have a 45-second timeout and a 32 MiB body limit. Each fetch failure
   receives a job/source receipt. This is user-directed intake, not a crawler.
4. Preserve the original bytes, SHA-256 hash, original relative path or URL,
   timestamp, and job identity. Each distinct byte sequence has one canonical
   document and file. Exact duplicates add receipts, not copies or documents.
5. Preview extracted text and classification suggestions. The category catalog
   explicitly separates subject, framework, document role, workflow, series,
   road, and canon. “Evidence” describes a role; it does not establish quality.
6. Commit selected human labels, or explicitly choose the confidence policy.
   Without AI, blank categories go to Inbox and Workbench. With AI, confidence
   ≥0.95 applies on commit; [0.80, 0.95) applies provisionally with a review flag;
   lower confidence is held. Unknown material goes to Inbox/Workbench. Canon,
   series, road, and archive never receive automatic AI decisions. The review
   queue supports explicit acceptance/rejection with audit receipts.
7. Index committed content through a durable PostgreSQL task queue. PostgreSQL
   and Hister have no shared transaction: failed index writes are visible and
   retried with capped backoff. Stable Hister URLs make retry writes idempotent.
8. Search committed documents across every desktop/NAS collection ingested into
   this workspace, filter by job/category, inspect source receipts, and export
   up to 100 selected documents as Markdown. Exports include full extracted text,
   identities, hashes, categories, and provenance, and have their own audit/hash
   record. These are text exports, not an AI-generated “Lossless Summary.”

Original desktop files remain untouched. To add an existing NAS collection,
select its mapped network folder from Windows and upload it; the server then
owns the authoritative preserved copy. Direct scanning of arbitrary NAS paths
is not enabled. No storage volume is presumed to exist.

## Storage choices and volume-one previews

The **Storage** page lets you choose specific research documents and the kind of
files you want on volume one: preserved originals, extracted document text, or
both. You can filter by research job, category, and age since import (all ages,
60 days, or 90 days). “Plan move to volume one” in the search library carries your
selected results into this page. Up to 100 documents can be selected per preview.

The planned dedicated folder defaults to `/volume1/Theophysics/library`. An
operator may change `TP_VOLUME_ONE_FOLDER` to display another planned absolute
path. This setting is **display-only**: it does not mount storage, create a NAS
folder, or enable a migration. No volume-one mount is needed to use previews.
The actual NAS folder still needs to be confirmed and connected for future
file-transfer work.

“Preview move” saves the exact selected source paths, proposed destinations,
file sizes, document identities, and expected original hashes to PostgreSQL.
It checks local source availability and that files belong to the managed store,
but does not rehash content or check destination capacity. The preview can be
reopened or downloaded as JSON. Every saved preview records `files_moved: 0` in
activity. Duplicate source receipts still point to one canonical file, which
appears once in the plan. Missing/changed-size files block the affected plan.

Programs, your existing PostgreSQL installation, and Hister's text index remain
on NVMe and cannot be selected for these plans. Automated age-based cleanup is
off. Age filters refer to import time; last-use tracking is not implemented.
There is no filesystem execution or deletion endpoint for storage plans in this
release, so creating or downloading a preview never changes source paths,
indexing status, search results, or file contents. This is the requested storage
selection interface; actual transfers and cache eviction remain future work.

## Local development

Use the existing checkout; a task is already isolated. No worktree is necessary.
Use Go 1.26+, Node/npm for the existing Hister SPA, and a C compiler for Hister.
The control center frontend is embedded static HTML/CSS/JS and has no Node build.

Start PostgreSQL 17 on a private interface, create a `theophysics` database, and
set these environment variables in the terminal that runs the service:

```sh
export TP_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/theophysics?sslmode=disable'
export TP_ACCESS_TOKEN='YOUR_RANDOM_TOKEN_AT_LEAST_24_CHARACTERS'
export TP_DATA_DIR='/absolute/private/development/document-store'
export TP_HISTER_URL='http://127.0.0.1:4433'
# Set TP_HISTER_TOKEN when Hister is token-protected.
go run ./cmd/theophysics
```

Run Hister separately with `./hister listen`. The control center defaults to
`127.0.0.1:4444`. Set `TP_ADDRESS` for an explicit bind address. Its `/healthz`
checks PostgreSQL; it does not establish Hister readiness. Failed indexing is
visible in each job and in activity. Jobs are staged until commit. After commit,
create a new job for further imports; unselected/unsupported sources remain
preserved and can be imported into another job without duplicating canonical
files. A dedicated volume is required for the private document store; users must
not write files or symlinks into it through unrelated shares.

The CLI creates version-one `tp_*` tables in PostgreSQL under a migration lock.
Hister tables are never migrated by this service. Use a dedicated database and
back it up before future version changes. No external model is needed to use
manual classification, imports, search, or export.

Optional AI variables:

- `TP_AI_BASE_URL`: trusted OpenAI-compatible base URL ending in `/v1`.
- `TP_AI_MODEL`: selected model identifier.
- `TP_AI_KEY`: credential if that service needs one.

“Suggest with AI” sends the title, category catalog, and up to 24,000 UTF-8 bytes
of each selected document to that configured service. It validates model JSON,
known categories, confidence ranges, and protected dimensions before persisting
proposals. Excerpts are untrusted data. No model has tools or deletion access.
Confidence scores are model estimates, not calibrated probabilities or evidence
of truth. No model endpoint has been connected by default.

## Synology preparation and deployment

For the selected DS923+ (`dlowenas`, 32 GB RAM), begin with
[the read-only DSM/Windows inspection](synology/START-HERE.md). This determines
storage paths before any NAS directory or service is changed.

Do not choose paths before inspecting the actual NAS:

```sh
uname -m
df -h
mount
docker version
docker compose version
```

Confirm Container Manager support, CPU architecture, available RAM, volume
health, filesystem permissions, free space, snapshot support, and the location
of any SSD pool. Decide separately where PostgreSQL, Hister indexes, and source
artifacts belong. Use fast storage for databases/indexes if the real layout
supports it. Keep document bytes on an appropriate capacity volume. These are
operator inspection commands; this cloud task cannot inspect your NAS.

Copy `.env.example` to `.env`, generate distinct credentials, and fill the three
absolute host directories. Use URL-safe hex for `TP_DB_PASSWORD`. Ensure UID
65532 can read/write the Hister and document directories. PostgreSQL's image
initializes its own empty database directory; preserve its ownership on restore.
Do not point PostgreSQL at a populated unrelated directory.

From this directory:

```sh
docker compose config --quiet
docker compose build
docker compose up -d
docker compose ps
```

The Compose file builds Hister from this fork and builds the separate control
center. PostgreSQL and Hister have no published host ports. Only the control
center is published, on host loopback by default. Configure Synology's HTTPS
reverse proxy to forward your private research hostname to port 4444. Keep the
host name in the forwarded Host header consistent with the browser's Origin.
Restrict that hostname to your LAN/VPN. No public MCP boundary is exposed in this
slice. Do not expose PostgreSQL or the unauthenticated health endpoint publicly.
When accessing from Windows, use that private HTTPS hostname and enter the
control center token. Tokens are stored only in the browser tab's session storage.

For cloud Docker builds with TLS inspection, pass the platform-provided public
CA with `docker build --secret id=proxy_ca,src="$CODEX_PROXY_CERT" -f
Dockerfile ...` as appropriate. Do not disable signature/checksum/TLS checks.
The deployment uses `hister.Dockerfile` for this first slice: Hister search and
previews without the later optional media-crawler tools. Both Dockerfiles
accept the same optional `proxy_ca` build secret.
NAS deployment and restoration must be verified on the actual NAS before
calling it production-ready.

## Backup and recovery

A complete recovery set contains a PostgreSQL dump, the **entire** document store
(`objects/` and `text/`), and Hister's data directory. Indexes are derivative but
keeping their consistent snapshot shortens recovery. Preserve the same container
mount paths so canonical source references stay valid. Keep credentials separately
in a secure store. Exports are not database backups.

For a consistent maintenance backup, stop intake/index writers first:

```sh
# Run in this directory. A backup path must already have been selected on the NAS.
docker compose stop control-center hister
# Pipe directly into a file in a private backup location chosen by the operator.
docker compose exec -T postgres pg_dump -U theophysics -d theophysics -Fc > /CHOSEN_BACKUP/research.dump
# Snapshot or copy TP_DOCUMENT_DIR and TP_HISTER_DIR with metadata preserved.
# Resume only after those snapshots/copies finish.
docker compose start hister control-center
```

Keep the services stopped if the dump/copies fail; diagnose before resuming.
Restore into a separate empty test database and separate directories first. Use
`pg_restore --exit-on-error --no-owner` with matching DB ownership. Verify hashes,
source downloads, document/receipt counts, category relationships, a Hister
search, pending index retries, and Markdown export before switching traffic.
Automated NAS backup schedules and a tested NAS restore are still future work.
No backup job or deployment has been installed on your NAS by this change.

## Tests

```sh
go test ./internal/research ./cmd/theophysics
# The command above skips integration cases unless their services are provided.
TP_TEST_DATABASE_URL='postgres://.../test_database?sslmode=disable' \
TP_TEST_HISTER_URL='http://127.0.0.1:4433' \
go test -race -count=1 -v ./internal/research
```

Use a disposable PostgreSQL test database whose user can create schemas. Each
integration test creates and removes its own schema and source store. The real
Hister integration adds a unique test document to the selected Hister instance;
use a disposable index. It covers intake, commit, indexing, search, source hash
verification, Markdown content, and export provenance. PostgreSQL and Hister
must already be running. Browser tests live in `tests/theophysics/`; see their
README for the running-service test command.

## Deliberate limits and next slices

The full architecture remains the goal. This change does not ship a packaged
native Windows application, automated NAS filesystem scanning, resumable single
file uploads, a general crawl farm, public/private MCP endpoints, search-provider
federation, behavioral reranking, Mattermost, Obsidian projections, canonization,
or archive/restore/permanent deletion. No AI classification can delete a file.
The UI has no destructive endpoints or misleading deletion buttons.

Probable duplicate detection is advisory: normalized text hashes and bounded
fingerprint comparisons using the first 256 KiB against the most recent 1,000 extracted documents. It
never merges candidates or claims semantic equivalence. Exact SHA-256 identity
is global and unbounded. OCR, image/audio/video transcription, encrypted files,
and executable document content are not processed. Extraction errors retain
source bytes for review.

Search uses Hister's full-text results, then applies PostgreSQL metadata filters.
Each Hister page contains at most 100 candidates; filtered pages can be empty.
“Next results” follows the Hister page key. Empty-query browsing is PostgreSQL
metadata browsing over indexed documents, not a replacement text index. There
are no personal-relevance, evidence-quality, or validity scores hidden in labels.
Activity shows the latest 200 events; all events remain in PostgreSQL. It is an
application audit trail, not an immutable tamper-proof external ledger.

AI assistance disclosure: this first implementation, tests, and documentation
were generated with OpenAI Codex at the repository owner's request. No upstream
issue, comment, or pull request has been submitted.
