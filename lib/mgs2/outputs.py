"""Refuse to write into the game install or the source tree.

Both are inputs. A shifted command-line argument once wrote a rebuilt animation
over the game's own copy, with no complaint and hours of confusion after it.
Every script that writes a file goes through `checked()`, which makes that
mistake a crash instead of a silent loss.

The one deliberate exception is deploying a finished build into the game's
`update/` overlay folder, which `allow_update=True` permits.
"""
from __future__ import annotations

import pathlib

from . import config


def _inside(p: pathlib.Path, root: pathlib.Path | None) -> bool:
    if root is None:
        return False
    try:
        p.relative_to(root.resolve())
        return True
    except ValueError:
        return False


def checked(path, allow_update: bool = False) -> pathlib.Path:
    p = pathlib.Path(path).resolve()
    game = config.get('MGS2_GAME')
    leak = config.get('MGS2_LEAK')
    game_root = pathlib.Path(game) if game else None
    if _inside(p, game_root):
        if not (allow_update and _inside(p, game_root / 'update')):
            raise SystemExit(f'refusing to write into the game install: {p}\n'
                             '  outputs belong in the build folder -- check the argument order')
    if _inside(p, pathlib.Path(leak) if leak else None):
        raise SystemExit(f'refusing to write into the source tree: {p}')
    p.parent.mkdir(parents=True, exist_ok=True)
    return p
