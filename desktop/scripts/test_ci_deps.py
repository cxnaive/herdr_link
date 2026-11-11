import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('install-ci-deps.sh').resolve()


class CIDependencyTests(unittest.TestCase):
    def run_installer(self, arch='amd64', failures=1, ci='true'):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            def stub(name, body):
                path = root / name
                path.write_text('#!/usr/bin/env bash\nset -eu\n' + body)
                path.chmod(0o755)
            stub('dpkg', 'echo "$FIXTURE_ARCH"\n')
            stub('sudo', 'exec "$@"\n')
            stub('timeout', 'shift 2\nexec "$@"\n')
            stub('apt-get', '''printf '%s\\n' "$*" >> "$FIXTURE_LOG"
for argument in "$@"; do
  case "$argument" in Dir::Etc::sourcelist=*) cat "${argument#*=}" >> "$FIXTURE_LOG";; esac
done
if [[ "$*" == *" update" ]]; then
  count=$(cat "$FIXTURE_COUNT" 2>/dev/null || echo 0)
  count=$((count + 1)); echo "$count" > "$FIXTURE_COUNT"
  (( count > FIXTURE_FAILURES )) || exit 100
fi
''')
            env = {**os.environ, 'GITHUB_ACTIONS': ci, 'PATH': f'{root}:{os.environ["PATH"]}',
                   'FIXTURE_ARCH': arch, 'FIXTURE_FAILURES': str(failures),
                   'FIXTURE_LOG': str(root / 'log'), 'FIXTURE_COUNT': str(root / 'count')}
            result = subprocess.run(['bash', str(SCRIPT)], env=env, capture_output=True, text=True, timeout=10)
            log = (root / 'log').read_text() if (root / 'log').exists() else ''
            return result.returncode, log, result.stderr

    def test_fallback_keeps_signatures_architecture_and_all_dependencies(self):
        for arch, archive in [('amd64', 'ubuntu'), ('arm64', 'ubuntu-ports')]:
            with self.subTest(arch=arch):
                code, log, error = self.run_installer(arch, failures=2)
                self.assertEqual(code, 0, error)
                self.assertIn(f'https://mirrors.tuna.tsinghua.edu.cn/{archive}', log)
                self.assertIn(f'arch={arch} signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg', log)
                self.assertIn('APT::Update::Error-Mode=any', log)
                self.assertNotIn('trusted=yes', log)
                self.assertNotIn('--allow-unauthenticated', log)
                self.assertIn('libgtk-3-0t64 libwebkit2gtk-4.1-0 libsecret-1-0 gnome-keyring dbus-x11 xvfb xauth xdotool imagemagick fonts-noto-cjk', log)

    def test_failures_do_not_allow_building_without_dependencies(self):
        self.assertNotEqual(self.run_installer(failures=3)[0], 0)
        self.assertNotEqual(self.run_installer(arch='invalid')[0], 0)
        code, log, _ = self.run_installer(ci='false')
        self.assertNotEqual(code, 0)
        self.assertEqual(log, '')


if __name__ == '__main__':
    unittest.main()
