"""Copy a built `update/` folder into the game, reversibly.

Everything a restoration adds lives under the game's `update/` folder, which the
Ultimate ASI Loader (shipped with MGSHDFix) redirects file opens into. Nothing
belonging to the game is modified.

A deploy remembers what it installed (`.deployed` beside the build), so the next
deploy removes files that the build no longer produces -- switching a feature off
and redeploying takes it back out of the game. A file in `update/` that is not
ours is kept once as `<name>.pre-<tag>` before being replaced.
"""
from __future__ import annotations

import pathlib
import shutil

from . import config
from .outputs import checked


def deploy(dist: pathlib.Path, tag: str):
    game = config.game()
    update = game / 'update'
    manifest = dist.parent / '.deployed'
    if not dist.is_dir():
        raise SystemExit(f'nothing built at {dist} -- run build.py first')

    previous = set(manifest.read_text().split('\n')) - {''} if manifest.exists() else set()
    current = sorted(p.relative_to(dist).as_posix() for p in dist.rglob('*') if p.is_file())

    for rel in sorted(previous - set(current)):
        dst = update / rel
        if dst.exists():
            dst.unlink()
            print(f'  remove   update/{rel}')

    for rel in current:
        dst = checked(update / rel, allow_update=True)
        if not dst.exists():
            print(f'  add      update/{rel}')
        elif rel in previous or dst.with_name(dst.name + f'.pre-{tag}').exists():
            print(f'  update   update/{rel}')
        else:
            shutil.copyfile(dst, dst.with_name(dst.name + f'.pre-{tag}'))
            print(f'  replace  update/{rel}  (original kept as .pre-{tag})')
        shutil.copyfile(dist / rel, dst)

    manifest.write_text('\n'.join(current) + '\n')
    print(f'\ndeployed to {update}')
