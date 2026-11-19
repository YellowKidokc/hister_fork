# YouTube acquisition for Hister / Nogon

Preserves David's working Python downloader, readable Markdown pipeline and
standalone SRT conversion. No existing transcript collection or credentials
are bundled. Original source remains untouched.

## Setup and use

Windows: run `powershell -ExecutionPolicy Bypass -File SETUP.ps1` here.
Then run `SET_WEBSHARE_CREDENTIALS.bat` if proxy fallback is wanted, and
`RUN_WITH_WEBSHARE.bat` for the familiar interactive URL launcher.
The launcher requires Webshare credentials; the Python CLI also supports direct-only use.

Other systems: create a Python 3.10+ environment and install `requirements.txt`.
The original Windows virtual environment is deliberately excluded.
Optional `punctuators` enables the existing punctuation model; conversion works without it.

```powershell
$env:HISTER_URL = 'http://localhost:8080' # replace with your actual server URL
$env:HISTER_TOKEN = '<your API token>' # only if authentication is enabled
.\venv\Scripts\python.exe ytgrab.py https://www.youtube.com/@CHANNEL --sync-hister
```

Without `--sync-hister`, downloading and Markdown conversion work independently.
To retry indexing without accessing YouTube:

```powershell
.\venv\Scripts\python.exe sync_hister.py --src subtitles --dry-run
.\venv\Scripts\python.exe sync_hister.py --src subtitles
```

Use HTTPS for a remote authenticated server. Keep credentials in your environment.
Run one acquisition/sync process per library at a time.

## Repeat-channel rule

Revisiting a channel three times in a week means three incremental checks,
not three copies of its transcripts. Each check enumerates the channel again.
Saved raw video IDs are skipped, new videos and failed downloads are fetched.
Known `no_transcript` results remain skipped; delete their local placeholder
only if you intentionally want another check for newly added captions.
Shorts require `--shorts`; this does not promise every private, unavailable,
members-only or captionless video can be acquired.

- Stable identity: YouTube video ID, independent of title changes.
- New raw files: `subtitles/<channel or playlist>/<title> [<video-id>].md`.
- Readable derivatives: each source folder's `Clean MD/` directory.
- Existing files retain their paths; failures are retried at their existing path.
- Raw timestamped Markdown is indexed once per video ID using the canonical
  YouTube watch URL. Derived notes are excluded to avoid duplicate search hits.
- Sync receipts use content SHA-256 and server URL; only successful HTTP
  acknowledgements are recorded. Failed syncs retry without redownloading.
- Sync is separate from downloading: copied pre-existing transcripts can also
  be indexed. It does not delete server documents or local originals.

For readable prose recovery, including a library with no new downloads:

```powershell
.\venv\Scripts\python.exe readable_prose_converter.py --src subtitles --in-place
```

For SRT sources, use `run_srt_to_markdown.cmd` or `srt_to_markdown.py --help`.
README files and licenses are included; generated research Markdown is excluded.

## Validation

`python -m unittest test_integration -v` exercises title collisions, retry paths,
derived exclusion, failed API retries and acknowledged-content skipping with a
mock API. CLI help and a real local Markdown conversion are also checked.
Live YouTube/Webshare acquisition and a running Hister server still require a
live acceptance run. This is a CLI integration, not a new web UI.
