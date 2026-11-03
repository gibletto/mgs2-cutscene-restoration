"""Helpers that run the Cutscene Studio (tools/cutscene-studio).

The studio is a Go program; the first call builds it into the build folder, so
the only requirement is a Go toolchain on PATH.

Two ways in:
  - its command-line tools (objdump, ...), run once per call;
  - its server, which a build starts privately for as long as it needs it
    (`with serve(work) as studio:`) and drives over HTTP: open a scene, extend
    it, post an edited scene document back, bake. A bake lands in the server's
    out/ folder, which serve() points inside the build -- nothing a build does
    through the studio writes into the game.
"""
from __future__ import annotations

import contextlib
import json
import os
import pathlib
import shutil
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

from . import config

STUDIO = config.REPO / 'tools' / 'cutscene-studio'


def _binary(name: str) -> pathlib.Path:
    exe = config.build_dir() / 'bin' / (name + ('.exe' if os.name == 'nt' else ''))
    if not exe.exists():
        if not shutil.which('go'):
            raise SystemExit(f'building {name} needs Go on PATH (https://go.dev/dl/)')
        exe.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(['go', 'build', '-o', str(exe), f'./cmd/{name}'], cwd=STUDIO, check=True)
    return exe


def objdump(sdt_path, obj_id: int, first: int, last: int, stride: int = 1) -> str:
    """Per-frame world position (and Y rotation) of one demo object, as text:
    `frame x y z ry` per line, after a `#` header."""
    r = subprocess.run([str(_binary('objdump')), str(sdt_path), str(obj_id), str(first),
                        str(last), str(stride)], capture_output=True, text=True, check=True)
    return r.stdout


def mgs2cs() -> pathlib.Path:
    """The studio server/CLI binary."""
    return _binary('mgs2cs')


class Studio:
    """A client for a running studio server."""

    def __init__(self, url: str):
        self.url = url.rstrip('/')

    def _call(self, path: str, data=None):
        try:
            if data is None:
                return json.load(urllib.request.urlopen(self.url + path))
            r = urllib.request.Request(self.url + path, data=json.dumps(data).encode(),
                                       headers={'content-type': 'application/json'})
            return json.load(urllib.request.urlopen(r))
        except urllib.error.HTTPError as e:
            raise SystemExit(f'studio: HTTP {e.code} on {path}: {e.read()[:300]!r}')

    def scene(self, sdt) -> dict:
        """Open (or re-read) a cutscene; returns the scene document."""
        return self._call('/api/scene?path=' + urllib.parse.quote(str(sdt)))

    def extend(self, sdt, frames: int) -> dict:
        return self._call('/api/extend', {'path': str(sdt), 'frames': frames})

    def post(self, scene: dict) -> dict:
        """Replace the open scene with an edited document."""
        return self._call('/api/scene', scene)

    def bake(self, sdt) -> pathlib.Path:
        """Bake the open scene; returns the baked file. Refuses a fatal finding."""
        b = self._call('/api/bake', {'path': str(sdt)})
        fatal = [f for f in (b.get('findings') or []) if f['severity'] == 'fatal']
        print(f'  bake: {b["bytesWritten"]} bytes, {len(fatal)} fatal')
        for f in fatal[:5]:
            print('    FATAL', f['rule'], f['msg'][:80])
        if not b['ok'] or fatal:
            raise SystemExit('the bake was refused')
        return pathlib.Path(b['outPath'])


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(('127.0.0.1', 0))
        return s.getsockname()[1]


@contextlib.contextmanager
def serve(work_dir):
    """Run a private studio server for the length of a `with` block.

    The server's working directory is `work_dir`, so its bakes land in
    `work_dir/out`. It reads the game from MGS2_GAME and its effect catalog from
    the studio's data folder (see tools/cutscene-studio/README.md).
    """
    work = pathlib.Path(work_dir)
    work.mkdir(parents=True, exist_ok=True)
    port = _free_port()
    env = dict(os.environ, MGS2_DIR=str(config.game()))
    env.setdefault('MGS2CS_DATA_DIR', str(STUDIO / 'data'))
    log = open(work / 'studio.log', 'w')
    proc = subprocess.Popen([str(mgs2cs()), 'serve', str(port)], cwd=work, env=env,
                            stdout=log, stderr=subprocess.STDOUT)
    url = f'http://127.0.0.1:{port}'
    try:
        for _ in range(100):
            if proc.poll() is not None:
                raise SystemExit(f'the studio server exited; see {work / "studio.log"}')
            try:
                urllib.request.urlopen(url + '/api/config', timeout=1)
                break
            except OSError:
                time.sleep(0.1)
        else:
            raise SystemExit('the studio server did not start')
        yield Studio(url)
    finally:
        proc.terminate()
        proc.wait()
        log.close()
