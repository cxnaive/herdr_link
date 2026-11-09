#!/usr/bin/env python3
"""Start only synthetic demo data in an isolated home; never connect to a Relay."""
import argparse
import os
from pathlib import Path
import subprocess
import tempfile
import time


def smoke(app, output):
    output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='herdr-mac-smoke-') as home:
        env = {**os.environ, 'HOME': home, 'CFFIXED_USER_HOME': home}
        with (output / 'startup.log').open('w') as log:
            process = subprocess.Popen([str(app / 'Contents/MacOS/Herdr Desktop'), '--demo'], env=env, stdout=log, stderr=subprocess.STDOUT)
            try:
                for _ in range(12):
                    time.sleep(1)
                    if process.poll() is not None:
                        raise RuntimeError('App exited during demo startup; see startup.log')
                if 'Creating native window' not in (output / 'startup.log').read_text():
                    raise RuntimeError('App did not reach native window creation')
            finally:
                if process.poll() is None:
                    process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill(); process.wait()
    with (output / 'pdf.log').open('w') as log:
        subprocess.run(['go', 'run', './internal/pdfview/smoke', str(output / 'pdf.png')], check=True, timeout=60, stdout=log, stderr=subprocess.STDOUT)
    if not (output / 'pdf.png').is_file():
        raise RuntimeError('Native PDF preview produced no screenshot')
    print('Isolated app startup and native PDF smoke passed; not full manual UX qualification.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('app', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    smoke(args.app.resolve(), args.output.resolve())
