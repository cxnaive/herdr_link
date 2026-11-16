"""English documentation uses verified release links without changing prose or layout."""
import json
from pathlib import Path
import re
import sys
import unittest
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import project_release_docs as D


def urls(text):
    return set(re.findall(r'\]\((https://github\.com/[^)]+)\)', text))


class EnglishReadmeTests(unittest.TestCase):
    def test_translated_downloads_point_to_the_same_assets(self):
        current = json.loads((ROOT / 'VERSION.json').read_text())['release']
        for record in ({'version': '0.3.0', 'tag': 'v0.3.0'}, current):
            with self.subTest(tag=record['tag']):
                chinese, english = D.download_blocks(record), D.english_download_blocks(record)
                self.assertEqual(set(chinese), set(english))
                for key, body in english.items():
                    self.assertEqual(urls(chinese[key]), urls(body))
                    self.assertNotRegex(body, r'[\u4e00-\u9fff]')
                    self.assertNotIn('codexfix', body)
                    self.assertNotIn('-debug', body)

    def test_optional_platforms_require_the_publication_receipt(self):
        record = {'version': '1.2.3', 'tag': 'v1.2.3', 'assets': {}}
        absent = '\n'.join(D.english_download_blocks(record).values())
        for filename in ('herdr-link-relay-1.2.3-windows-amd64.zip',
                         'herdr-link-relay-1.2.3-linux-arm64.tar.gz',
                         'herdr-link-desktop-1.2.3-linux-arm64.deb',
                         'herdr-link-desktop-1.2.3-macos-universal.dmg'):
            self.assertNotIn(filename, absent)
            record['assets'][filename] = {}
        present = '\n'.join(D.english_download_blocks(record).values())
        for filename in record['assets']:
            self.assertIn(filename, present)
        self.assertIn('without Developer ID signing or Apple notarization', present)
        self.assertIn('do not uninstall', present)

    def test_checked_in_blocks_and_links_match_current_release(self):
        record = json.loads((ROOT / 'VERSION.json').read_text())['release']
        english = (ROOT / 'README.en.md').read_text()
        for key, body in D.english_download_blocks(record).items():
            start, end = f'<!-- {key}:start -->', f'<!-- {key}:end -->'
            self.assertEqual(english.count(start), 1)
            self.assertEqual(english.count(end), 1)
            self.assertEqual(english.split(start, 1)[1].split(end, 1)[0].strip(), body.strip())
        for url in urls(english):
            if '/releases/download/' in url:
                tag, filename = url.split('/releases/download/', 1)[1].split('/', 1)
                self.assertEqual(tag, record['tag'])
                self.assertIn(filename, record['assets'])

    def test_local_links_and_gallery_are_valid(self):
        for filename, other in [('README.en.md', 'README.md'), ('README.md', 'README.en.md')]:
            text = (ROOT / filename).read_text()
            self.assertIn(f']({other})', text)
            gallery = text.split('<!-- pocket-preview:start -->')[1].split('<!-- pocket-preview:end -->')[0]
            self.assertLess(text.index('## Clients'), text.index('<!-- pocket-preview:start -->'))
            self.assertEqual(gallery.count('<tr>'), 1)
            self.assertEqual(gallery.count('<td '), 4)
            self.assertEqual(gallery.count('<img '), 4)
            # Fenced command examples are not Markdown links/headings.
            prose = re.sub(r'```.*?```', '', text, flags=re.S)
            headings = set()
            for heading in re.findall(r'^#{1,6} (.+)$', prose, re.M):
                slug = re.sub(r'[^\w\- ]', '', heading.lower()).replace(' ', '-')
                headings.add(slug)
            targets = re.findall(r'\]\(([^)]+)\)', prose) + re.findall(r'(?:src|href)="([^"]+)"', prose)
            for target in targets:
                parsed = urlsplit(target)
                if parsed.scheme or parsed.netloc:
                    continue
                if parsed.path:
                    self.assertTrue((ROOT / unquote(parsed.path)).is_file(), target)
                elif parsed.fragment:
                    self.assertIn(unquote(parsed.fragment), headings, target)

    def test_publisher_commits_both_readmes(self):
        text = (ROOT / '.github/workflows/publish-components.yml').read_text()
        staging = next(line for line in text.splitlines() if 'git add --' in line)
        self.assertIn('README.md', staging.split())
        self.assertIn('README.en.md', staging.split())


if __name__ == '__main__':
    unittest.main()
