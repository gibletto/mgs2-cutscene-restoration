"""Where things are on this machine.

Nothing in the repository knows where your game, your copy of the leaked source
tree or your own models live. Those paths come from `local.env` at the repo root
(copy `local.env.example`) or from environment variables of the same names, which
win over the file.

    MGS2_GAME       the Master Collection install (the folder with METAL GEAR SOLID2.exe)
    MGS2_LEAK       root of the leaked source tree: the folder holding mgs2x/ and bp/
    MGS2_GCL_BIN    folder holding gclconv.exe and gclk.exe (default: $MGS2_LEAK/bp/Bin/Tools/Release)
    MGS2_ASSETS     your own models/textures for a restoration (never committed)
    MGS2_BUILD      scratch space for builds (default: <repo>/build)
"""
from __future__ import annotations

import os
import pathlib

REPO = pathlib.Path(__file__).resolve().parents[2]
_FILE = REPO / 'local.env'


def _read_env_file() -> dict[str, str]:
    out: dict[str, str] = {}
    if not _FILE.exists():
        return out
    for line in _FILE.read_text(encoding='utf-8').splitlines():
        line = line.strip()
        if not line or line.startswith('#') or '=' not in line:
            continue
        key, _, value = line.partition('=')
        out[key.strip()] = value.strip().strip('"').strip("'")
    return out


_ENV = _read_env_file()


def get(key: str, default: str | None = None) -> str | None:
    return os.environ.get(key) or _ENV.get(key) or default


def _path(key: str, default: str | None = None, required: bool = True) -> pathlib.Path | None:
    value = get(key, default)
    if not value:
        if required:
            raise SystemExit(f'{key} is not set -- add it to {_FILE.name} '
                             f'(see local.env.example) or the environment')
        return None
    return pathlib.Path(value).expanduser()


def game() -> pathlib.Path:
    p = _path('MGS2_GAME')
    if not (p / 'METAL GEAR SOLID2.exe').exists() and not (p / 'assets').is_dir():
        raise SystemExit(f'MGS2_GAME={p} does not look like an MGS2 Master Collection install')
    return p


def leak() -> pathlib.Path:
    p = _path('MGS2_LEAK')
    if not (p / 'mgs2x' / 'scn').is_dir():
        raise SystemExit(f'MGS2_LEAK={p} has no mgs2x/scn -- point it at the root of the source tree')
    return p


def scn_source() -> pathlib.Path:
    """The scenario source tree. EUC-JP, exactly as it came out of the leak."""
    return leak() / 'mgs2x'


def gcl_bin() -> pathlib.Path:
    default = str(leak() / 'bp' / 'Bin' / 'Tools' / 'Release') if get('MGS2_LEAK') else None
    p = _path('MGS2_GCL_BIN', default)
    for exe in ('gclconv.exe', 'gclk.exe'):
        if not (p / exe).exists():
            raise SystemExit(f'{exe} not found in MGS2_GCL_BIN={p}')
    return p


def assets(required: bool = True) -> pathlib.Path | None:
    return _path('MGS2_ASSETS', required=required)


def build_dir() -> pathlib.Path:
    p = _path('MGS2_BUILD', str(REPO / 'build'))
    p.mkdir(parents=True, exist_ok=True)
    return p
