import tempfile
import subprocess
import sys
import unittest
from pathlib import Path
from unittest.mock import patch, Mock

import ytgrab
import sync_hister


class IntegrationTests(unittest.TestCase):
    def test_markdown_converter_preserves_raw(self):
        with tempfile.TemporaryDirectory() as root:
            path = Path(ytgrab.write_markdown({'id': 'aaaaaaaaaaa', 'title': 'Example',
                'source_folder': 'Channel', 'transcript': '[00:00] First sentence. Second sentence.',
                'method': 'direct'}, root))
            before = path.read_bytes()
            result = subprocess.run([sys.executable, str(Path(ytgrab.__file__).with_name('readable_prose_converter.py')),
                '--src', root, '--in-place'], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(path.read_bytes(), before)
            self.assertTrue(list((path.parent / 'Clean MD').glob('*.md')))

    def test_same_title_distinct_ids_and_retry_path(self):
        with tempfile.TemporaryDirectory() as root:
            def video(vid):
                return {'id': vid, 'title': 'Same title', 'source_folder': 'Channel',
                        'transcript': 'Some words.', 'method': 'direct'}
            a = ytgrab.write_markdown(video('aaaaaaaaaaa'), root)
            b = ytgrab.write_markdown(video('bbbbbbbbbbb'), root)
            self.assertNotEqual(a, b)
            retry = video('aaaaaaaaaaa')
            retry['existing_path'] = a
            self.assertEqual(ytgrab.write_markdown(retry, root), a)
            self.assertEqual(len(list(Path(root).rglob('*.md'))), 2)

    def test_sync_retries_failure_skips_acknowledged_and_ignores_derived(self):
        with tempfile.TemporaryDirectory() as root:
            video = {'id': 'aaaaaaaaaaa', 'title': 'Title', 'source_folder': 'Channel',
                     'transcript': '[00:00] Test words.', 'method': 'direct'}
            path = Path(ytgrab.write_markdown(video, root))
            derived = path.parent / 'Clean MD'
            derived.mkdir()
            (derived / 'derived.md').write_text(path.read_text(encoding='utf-8'), encoding='utf-8')
            self.assertEqual(len(sync_hister.collect(root)), 1)
            with patch('sync_hister.requests.post') as post:
                post.return_value = Mock(status_code=500)
                self.assertEqual(sync_hister.sync(root, 'http://localhost:8080'), 1)
                post.return_value = Mock(status_code=201)
                self.assertEqual(sync_hister.sync(root, 'http://localhost:8080'), 0)
                self.assertEqual(sync_hister.sync(root, 'http://localhost:8080'), 0)
                self.assertEqual(post.call_count, 2)
                self.assertEqual(post.call_args.kwargs['json']['url'], 'https://www.youtube.com/watch?v=aaaaaaaaaaa')
                self.assertIn('[00:00]', post.call_args.kwargs['json']['text'])
            self.assertEqual(sync_hister.sync(root, 'http://other:8080', dry_run=True), 0)


if __name__ == '__main__':
    unittest.main()
