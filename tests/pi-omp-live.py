#!/usr/bin/env python3
"""Opt-in Linux PTY check: original CLIs, synthetic extension, no models or tools."""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import termios
import time

import pyte

ROOT = Path(__file__).resolve().parents[1]
RUNTIME = Path(os.environ['PI_OMP_RUNTIME']).resolve()
WORK = Path(os.environ['PI_OMP_WORK']).resolve() / 'live'
BUN = os.environ['PI_OMP_BUN']
TEST = os.environ['PI_OMP_GO_TEST']
KEYS = {'Down': b'\x1b[B', 'Up': b'\x1b[A', 'Enter': b'\r', 'Esc': b'\x1b'}


def check(provider, kind):
    directory = WORK / f'{provider}-{kind}'
    directory.mkdir(parents=True, exist_ok=False)
    home = directory / 'home'; home.mkdir()
    cwd = directory / 'work'; cwd.mkdir()
    result = directory / 'answer.json'
    config = directory / 'config.yml'
    config.write_text('startup:\n  setupWizard: false\n  checkUpdate: false\n  showSplash: false\nupdate:\n  auto: false\n')
    env = {'PATH': os.environ['PATH'], 'HOME': str(home), 'TERM': 'xterm-256color', 'PI_OFFLINE': '1',
           'PI_CODING_AGENT_DIR': str(home / '.pi/agent'), 'FIXTURE_PROVIDER': provider,
           'FIXTURE_KIND': kind, 'FIXTURE_RESULT': str(result)}
    if provider == 'pi':
        command = ['node', str(RUNTIME / 'node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js')]
    else:
        command = [BUN, str(RUNTIME / 'node_modules/@oh-my-pi/pi-coding-agent/dist/cli.js'), '--config', str(config)]
    command += ['--no-session', '--no-context-files', '--no-extensions', '-e', str(ROOT / 'tests/fixtures/pi-omp-live.ts')]
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 55, 120, 0, 0))
    child = subprocess.Popen(command, cwd=cwd, env=env, stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
    os.close(slave)
    screen = pyte.Screen(120, 55); stream = pyte.ByteStream(screen)
    started = time.monotonic(); requested = sent = False
    raw = bytearray()
    try:
        while time.monotonic() - started < 30 and child.poll() is None:
            if not requested and time.monotonic() - started > 8:
                os.write(master, b'/fixture\r'); requested = True
            if select.select([master], [], [], .1)[0]:
                data = os.read(master, 65536); raw.extend(data); stream.feed(data)
                if b'\x1b[6n' in data: os.write(master, b'\x1b[1;1R')
            if not sent and time.monotonic() - started > 12:
                request = directory / 'request.json'; response = directory / 'response.json'
                frame = '\n'.join(screen.display)
                request.write_text(json.dumps({'Agent': provider, 'Frame': frame, 'Intent': {'Selected': [1]}}))
                subprocess.run([TEST, '-test.run=^TestPiOMPUpstreamContract$', '-test.timeout=5s'], check=True, timeout=8,
                               env={**env, 'HERDR_INTERACTION_INPUT': str(request), 'HERDR_INTERACTION_OUTPUT': str(response)}, stdout=subprocess.DEVNULL)
                translated = json.loads(response.read_text())
                if translated['classification']['Kind'] != 'question': continue
                assert not translated['validation_error'], translated
                (directory / 'screen.txt').write_text(frame)
                for step in translated['steps']:
                    assert not step['Text'], 'This live fixture chooses only synthetic options'
                    for key in step['Keys']:
                        os.write(master, KEYS[key]); time.sleep(.15)
                sent = True
            if result.exists():
                value = json.loads(result.read_text())
                if provider == 'pi': assert value == 'South', value
                elif kind == 'approval': assert value == 'deny', value
                else: assert value['results'][0]['selectedOptions'] == ['South'], value
                print(f'PASS original {provider} CLI {kind}: {json.dumps(value, ensure_ascii=False)}')
                return
        raise AssertionError(f'{provider}/{kind}: no matching native answer before deadline; see {directory}')
    finally:
        (directory / 'terminal.ansi').write_bytes(raw)
        if child.poll() is None: os.killpg(child.pid, signal.SIGTERM)
        try: child.wait(timeout=8)
        except subprocess.TimeoutExpired: os.killpg(child.pid, signal.SIGKILL); child.wait()
        os.close(master)


for provider, kind in [('pi', 'select'), ('omp', 'approval'), ('omp', 'ask')]:
    check(provider, kind)
