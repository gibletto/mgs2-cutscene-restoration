#!/usr/bin/env python3
"""Fill your asset folder from the released mod.

The release on Nexus Mods (Metal Gear Solid 2 Master Collection, mod 135, "Arsenal Gear
Crash") carries every model and texture this build needs. This copies them out of its zip
into the layout ASSETS.md describes:

    python from_release.py <the downloaded .zip>

They go to <MGS2_ASSETS>/manhattan-crash/ (see local.env). The four stock models are not
copied, because the build takes those from your install. Nothing else is read from the
zip: the scenario, the cutscene, the stage lists and the texture groups are all rebuilt.
"""
import argparse
import pathlib
import sys
import zipfile

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parents[1] / 'lib'))
sys.path.insert(0, str(HERE))

from mgs2 import config            # noqa: E402
from mgs2.outputs import checked   # noqa: E402
from crash import models           # noqa: E402

NAME = 'manhattan-crash'
KMS = 'update/assets/kms/us/{}.kms'
CMDL = 'update/assets/kms/us/_win/{}.cmdl'
TEXTURES = 'update/textures/flatlist/ovr_stm/_win/'


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('zip', type=pathlib.Path, help='the release zip, as downloaded')
    args = ap.parse_args()

    dst = config.assets() / NAME
    with zipfile.ZipFile(args.zip) as z:
        names = set(z.namelist())

        def take(member: str, out: pathlib.Path):
            checked(out).write_bytes(z.read(member))

        missing = []
        mine = [m for m, source, _ in models.MODELS if source != 'game']
        for m in mine:
            for member, out in ((KMS.format(m), dst / 'models' / f'{m}.kms'),
                                (CMDL.format(m), dst / 'models' / f'{m}.cmdl')):
                if member in names:
                    take(member, out)
                else:
                    missing.append(member)

        textures = sorted(n for n in names
                          if n.startswith(TEXTURES) and n.endswith('.ctxr') and '/' not in n[len(TEXTURES):])
        for n in textures:
            stem = n[len(TEXTURES):-len('.ctxr')]
            int(stem, 16)                      # texture files are named by their hex id
            take(n, dst / 'textures' / f'{stem}.ctxr')

    if missing:
        raise SystemExit('not in this zip -- is it the right release?\n  ' + '\n  '.join(missing))
    print(f'{dst}: {len(mine)} models, {len(textures)} textures')


if __name__ == '__main__':
    main()
