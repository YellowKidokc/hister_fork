# defuddle-bridge

Standalone [defuddle](https://github.com/kepano/defuddle) wrapper used by the
Nogon `Defuddle` extractor. Speaks a simple JSON protocol over stdin/stdout
so the Go side can shell out to it the same way the `ytdlp` extractor shells
out to `yt-dlp`.

## Protocol

Input: one JSON object on **stdin** (never argv, so page content does not
leak through process listings and is not bound by argument size limits):

```json
{"url": "https://example.com/article", "html": "<!doctype html>..."}
```

- `html` (required): full page HTML as served/captured
- `url` (optional): page URL, used for relative link resolution and extractor
  matching

Output on success (exit 0): one JSON object on stdout:

```json
{
  "title": "…",
  "text": "block-aware plain text of the main content",
  "content_html": "HTML of the main content",
  "word_count": 123,
  "language": "en",
  "domain": "example.com",
  "favicon": "https://example.com/favicon.ico",
  "metadata": {
    "author": "…",
    "published": "…",
    "site": "…",
    "description": "…",
    "image": "…"
  }
}
```

On failure: a diagnostic on stderr, no stdout payload, non-zero exit.
Input is capped at 5 MiB.

## Enable in Nogon

The Docker image includes the bridge. Enable Defuddle under the extractor
settings, or add this to the Nogon configuration:

```yaml
extractors:
  defuddle:
    enable: true
```

The bridge parses saved HTML, including comment sections where available.
Nogon sanitizes the extracted HTML before rendering a preview.

## Build

```bash
npm install
npm run build   # → dist/defuddle-bridge
```

With [bun](https://bun.sh) available this produces a standalone binary. The
esbuild fallback produces a single-file Node.js script (requires node >= 20)
with the same protocol.

## Test

```bash
npm test
```

Runs protocol-level smoke tests as a child process: success shape, clutter
removal, and failure semantics.
