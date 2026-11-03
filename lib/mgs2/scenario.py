"""Recompile a stage scenario (.gcx) from Konami's source, with an edit.

The scenario is the stage's script: which objects exist, where, and the procs a
cutscene can call. Konami's compiler is in the leaked tree; their source for every
stage is too. A restoration edits the source and recompiles.

What makes it safe is that every build compiles twice:

  1. **Unmodified.** The pristine source, compiled, must come out byte for byte
     identical to the scenario the game ships -- after the residuals below. That
     is the proof the toolchain is faithful, so the only difference in the real
     build is the edit.
  2. **Edited.** The same tree with the restoration's changes, plus the same
     residuals.

The residuals are the differences between a clean compile and the shipped file
that are not ours: symbol tables in the source dump that are a later revision
than the build that shipped (the EU voice and demo tables), uninitialised bytes in
the font block, and the text blob. `residuals.py` learns them by diffing pass 1
against the game's own file and refuses unless that reproduces it exactly.

The EU/US release reads `assets/gcx/eu/_bp/`, the Japanese release reads
`assets/gcx/us/_bp/`; the two shipped files are genuinely different builds, so
each region learns its own residuals from its own shipped file.

    from mgs2 import scenario
    out = scenario.build('scenerio_stage_d082p01', edit=my_edit, name='flag')
    # out == {'eu': Path(...), 'us': Path(...)}
"""
from __future__ import annotations

import json
import os
import pathlib
import shutil
import subprocess
import sys
from typing import Callable

from . import config, gcx2bp, gcxdiff, residuals
from .outputs import checked

REGIONS = ('eu', 'us')

# The US argument files for both regions: the dump's EU args carry `-n 1:2:3:4:5`,
# which keeps the PAL multi-language `proc name:NN` variants the Master Collection
# does not ship. It makes no difference to a stage without language variants, but
# w61a has the boss lines in six languages and comes out 3 KB too big with them.
GCLCONV_ARGS = ('@_bp_gclc_args_us.txt', '@_bp_gclc_args_shared.txt')
GCLK_ARGS = ('@_bp_gclk_args.txt',)


def shipped(stage: str, region: str) -> pathlib.Path:
    p = config.game() / 'assets' / 'gcx' / region / '_bp' / f'{stage}.gcx'
    if not p.exists():
        raise SystemExit(f'no shipped scenario at {p}')
    return p


def _link_dir(link: pathlib.Path, target: pathlib.Path):
    """A directory link that needs no admin rights: a junction on Windows."""
    if link.exists():
        return
    if os.name == 'nt':
        import _winapi
        _winapi.CreateJunction(str(target), str(link))
    else:
        link.symlink_to(target, target_is_directory=True)


def stage_tree(work: pathlib.Path) -> pathlib.Path:
    """A fresh copy of the scenario source to compile in; the sibling folders it
    includes from are linked, not copied. Returns the copied scn folder.

    A copy, not a checkout: nothing can `git checkout` it back, so every pass
    starts from a new copy rather than trying to undo the previous one.
    """
    src = config.scn_source()
    if work.exists():
        shutil.rmtree(work)
    work.mkdir(parents=True)
    shutil.copytree(src / 'scn', work / 'scn')
    for d in ('module', 'source', 'list'):
        if (src / d).exists():
            _link_dir(work / d, src / d)
    return work / 'scn'


def compile_tree(scn: pathlib.Path, stage: str, out: pathlib.Path) -> pathlib.Path:
    """gclconv -> gclk -> BGCL form. Returns the converted (pre-residual) file."""
    bin_dir = config.gcl_bin()
    env = dict(os.environ, PATH=f'{bin_dir}{os.pathsep}{bin_dir.parent.parent}'
                                f'{os.pathsep}{os.environ.get("PATH", "")}')
    tmp = scn / '_restoration'
    tmp.mkdir(exist_ok=True)
    gco, raw = tmp / f'{stage}.gco', tmp / f'{stage}.gcx'
    log = out.with_suffix('.log')

    # Paths relative to the source folder: gclk misparses an absolute Windows path
    # (it trips on the drive colon and fails reading linkvar.def).
    rel_gco, rel_raw = f'_restoration/{stage}.gco', f'_restoration/{stage}.gcx'
    steps = [([str(bin_dir / 'gclconv.exe'), *GCLCONV_ARGS, '-o', rel_gco, f'_bp/{stage}.gcl'], gco),
             ([str(bin_dir / 'gclk.exe'), *GCLK_ARGS, '-o', rel_raw, rel_gco], raw)]
    with open(checked(log), 'w', encoding='utf-8', errors='replace') as fh:
        for cmd, product in steps:
            product.unlink(missing_ok=True)
            r = subprocess.run(cmd, cwd=scn, env=env, stdout=fh, stderr=subprocess.STDOUT)
            if r.returncode != 0 or not product.exists() or product.stat().st_size == 0:
                fh.flush()
                tail = log.read_text(encoding='utf-8', errors='replace').splitlines()[-20:]
                raise SystemExit(f'{pathlib.Path(cmd[0]).name} failed (see {log}):\n  '
                                 + '\n  '.join(tail))

    checked(out).write_bytes(gcx2bp.convert(raw.read_bytes()))
    return out


def build(stage: str, edit: Callable[[pathlib.Path], None] | None, name: str,
          regions: tuple[str, ...] = REGIONS, out_dir: pathlib.Path | None = None,
          report: bool = True) -> dict[str, pathlib.Path]:
    """Build `stage` with `edit` applied to the staged source tree.

    `edit(scn_dir)` changes files in place; it should raise if its change did not
    land. Returns the finished scenario per region, ready to deploy as
    `update/assets/gcx/<region>/_bp/<stage>.gcx`.
    """
    work = config.build_dir() / 'scenario' / f'{stage}.{name}'
    out_dir = out_dir or work / 'out'

    print(f'>> {stage}: [1/2] unmodified compile -- proves the toolchain')
    scn = stage_tree(work / 'tree')
    unmod = compile_tree(scn, stage, out_dir / f'{stage}.unmodified.gcx')
    patches = {}
    for region in regions:
        patch = residuals.learn(unmod.read_bytes(), shipped(stage, region).read_bytes(), scn)
        patches[region] = patch
        checked(out_dir / f'residuals.{region}.json').write_text(
            json.dumps(patch, indent=2) + '\n')
        print(f'   {region}: reproduces the shipped scenario exactly '
              f'({len(patch["subs"])} stale symbols, {len(patch["runs"])} region values'
              f'{", font block" if patch["font"] else ""})')

    if edit is None:
        return {}

    print(f'>> {stage}: [2/2] compile with the edit')
    scn = stage_tree(work / 'tree')
    edit(scn)
    edited = compile_tree(scn, stage, out_dir / f'{stage}.{name}.raw.gcx')

    results = {}
    for region in regions:
        final = checked(out_dir / region / f'{stage}.gcx')
        final.write_bytes(residuals.apply_patch(edited.read_bytes(), patches[region]))
        results[region] = final
        if report:
            print(f'   {region}: {final}')
            gcxdiff.main(str(shipped(stage, region)), str(final))
    return results


if __name__ == '__main__':
    # `python -m mgs2.scenario <stage>`: the unmodified proof on its own
    if len(sys.argv) < 2:
        raise SystemExit('usage: python -m mgs2.scenario <scenerio_stage_xxx>')
    build(sys.argv[1], None, 'proof')
