"""Incrementally index raw timestamped transcripts through Hister/Nogon's API."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
from urllib.parse import urlsplit

import requests


def collect(root):
    records = {}
    for parent, dirs, files in os.walk(root):
        dirs[:] = [d for d in dirs if d.casefold() not in
                   {'clean md', 'channel summary', 'channel analysis', 'prompts'}
                   and not d.casefold().startswith('clean md - previous')]
        for name in sorted(files):
            if not name.endswith('.md'):
                continue
            path = Path(parent) / name
            text = path.read_text(encoding='utf-8')
            match = re.search(r'\*\*Video ID:\*\* `([A-Za-z0-9_-]{11})`', text)
            method = re.search(r'\*\*Retrieved via:\*\* (.+)', text)
            if not match or not method or method[1].strip() in {'failed', 'error', 'unknown', 'no_transcript'}:
                continue
            body = text.partition('## Transcript\n')[2].strip()
            if not body or body.startswith('*No transcript'):
                continue
            vid = match[1]
            payload = {'url': f'https://www.youtube.com/watch?v={vid}',
                       'title': text.splitlines()[0].lstrip('# ').strip(),
                       'text': text, 'label': 'youtube'}
            digest = hashlib.sha256(text.encode('utf-8')).hexdigest()
            records[vid] = (payload, digest, path)
    return records


def sync(root, base_url, token='', dry_run=False):
    parts = urlsplit(base_url)
    if parts.scheme not in {'http', 'https'} or not parts.hostname or parts.username or parts.password or parts.query or parts.fragment:
        raise ValueError('Use an http(s) server URL without embedded credentials, query or fragment')
    endpoint = base_url.rstrip('/') + '/api/add'
    state_path = Path(root) / '.hister-sync.json'
    state = json.loads(state_path.read_text(encoding='utf-8')) if state_path.exists() else {}
    saved = state.setdefault(endpoint, {})
    failures = 0
    for vid, (payload, digest, path) in collect(root).items():
        if saved.get(vid) == digest:
            continue
        print(f'{"Would index" if dry_run else "Indexing"}: {path.name}')
        if dry_run:
            continue
        try:
            response = requests.post(endpoint, json=payload,
                                     headers={'Authorization': f'Bearer {token}'} if token else {},
                                     timeout=60, allow_redirects=False)
            if not 200 <= response.status_code < 300:
                raise RuntimeError(f'HTTP {response.status_code}')
        except (requests.RequestException, RuntimeError) as exc:
            print(f'Failed {vid}: {type(exc).__name__}; will retry next run')
            failures += 1
            continue
        saved[vid] = digest
        temp = state_path.with_suffix('.json.tmp')
        temp.write_text(json.dumps(state, indent=2), encoding='utf-8')
        temp.replace(state_path)
    return failures


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--src', default=str(Path(__file__).parent / 'subtitles'))
    parser.add_argument('--server', default=os.environ.get('HISTER_URL', 'http://localhost:8080'))
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    if not Path(args.src).is_dir():
        parser.error('Transcript source directory does not exist')
    raise SystemExit(bool(sync(args.src, args.server, os.environ.get('HISTER_TOKEN', ''), args.dry_run)))
