#!/usr/bin/env python3
"""Build the Federal Hall flag restoration into <build>/federal-hall-flag/update.

    python build.py                 everything
    python build.py --no-poles      leave out the street flags
                    --no-fall       leave out the falling flag
                    --no-drag       leave out the dragged flag
                    --no-slowmo     one flag actor, no per-shot wave rates
                    --flag-base 11  ticks between wave keyframes (11 = the PS2 rate)

Inputs are read from your game install and your copy of the source tree (see
local.env). Nothing is written to either: the result is an `update/` folder to
copy into the game, which `deploy.py` does.
"""
import argparse
import pathlib
import shutil
import sys

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parents[1] / 'lib'))
sys.path.insert(0, str(HERE))

from mgs2 import config, demo_edit, scenario, studio          # noqa: E402
from mgs2.outputs import checked                              # noqa: E402
from flag import drag, fall, gcl_edit, poles, register, timing  # noqa: E402

# The cut lives in two files: the ending demos have their own copy, the Federal
# Hall cutscenes and the Solidus fight share w61a_common.h. Different stages
# compile each, so each is rebuilt (and proved byte-exact) separately. a61a shares
# the header too but nothing needs it.
STAGES = ('scenerio_stage_d082p01', 'scenerio_stage_d080p08', 'scenerio_stage_w61a')
ENDING = 'scenerio_stage_d082p01'

# the ending's three cutscenes; the regions ship different files, so each is
# edited from its own copy
DEMO_RISE, DEMO_FALLEN, DEMO_DRAG = 'p082_01_p01', 'p082_02_p02', 'p082_04_p03'
DEMO_REGIONS = ('us', 'jp')
OFFICER = 3                      # the officer's demo object id in p082_04_p03


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--no-poles', dest='poles', action='store_false')
    ap.add_argument('--no-fall', dest='fall', action='store_false')
    ap.add_argument('--no-drag', dest='drag', action='store_false')
    ap.add_argument('--no-slowmo', dest='slowmo', action='store_false')
    ap.add_argument('--flag-base', type=int, default=gcl_edit.DEFAULT_BASE)
    ap.add_argument('--stages', nargs='+', default=list(STAGES))
    args = ap.parse_args()

    game = config.game()
    out = config.build_dir() / 'federal-hall-flag'
    dist = out / 'update'
    if dist.exists():
        shutil.rmtree(dist)

    # ---- the scenarios
    edit = gcl_edit.make_edit(args.flag_base, args.slowmo, args.poles)
    built = {}
    for stage in args.stages:
        built[stage] = scenario.build(stage, edit, 'flag', out_dir=out / 'scenario' / stage)
        for region, path in built[stage].items():
            shutil.copyfile(path, checked(dist / 'assets' / 'gcx' / region / '_bp' / f'{stage}.gcx'))
        print()
    ending_gcx = built[ENDING]['eu'] if ENDING in built else None

    specials = {'fall': fall.packet_body, 'drag': drag.packet_body}

    def demo(name, calls):
        for region in DEMO_REGIONS:
            print(f'== {name} ({region})')
            demo_edit.add_calls(game / region / 'demo2' / '_bp' / f'{name}.sdt',
                                dist / region / 'demo2' / '_bp' / f'{name}.sdt',
                                calls, scenario=ending_gcx, specials=specials)
        print()

    # ---- the first ending cutscene: the flag up, at the right rate, then the fall
    rise = timing.calls() if args.slowmo else [(timing.SHOW_FRAME, gcl_edit.PROC_FLAG_ON)]
    demo(DEMO_RISE, rise + (fall.calls() if args.fall else []))

    # ---- the second: Solidus lying under the flag. Hidden on frame 0 here too.
    demo(DEMO_FALLEN, [(3, gcl_edit.PROC_STATIC_ON)])

    # ---- Konami's own falling animation, re-cut to start where the cutscene needs it
    anm_src = game / 'assets' / 'anm' / 'us' / 'w61a_flag_fall1.anm'
    if args.fall:
        print('== the fall')
        fall.build(anm_src, dist / 'assets' / 'anm' / 'us' / 'w61a_flag_fall1.anm')
        print()

    # ---- the officer dragging it away, following his path out of the cutscene itself
    if args.drag:
        print('== the drag')
        path_txt = checked(out / 'officer_path.txt')
        path_txt.write_text(studio.objdump(game / 'us' / 'demo2' / '_bp' / f'{DEMO_DRAG}.sdt',
                                           OFFICER, drag.GRAB_FRAME, drag.END_FRAME, 1))
        drag.build(anm_src, path_txt, dist / 'assets' / 'anm' / 'us' / 'w61a_flag_drag.anm')
        print()
        demo(DEMO_DRAG, drag.calls())

    # ---- the eleven street flags: the only part that registers new assets
    if args.poles:
        print('== street pole flags')
        poles.build_all(game / 'assets' / 'cv2' / 'us' / 'w61a_flag.cv2',
                        game / 'assets' / 'kms' / 'us' / 'w61a_flag_stage_d082p01.kms',
                        dist / 'assets' / 'cv2' / 'us', dist / 'assets' / 'kms' / 'us')
        print()

    print('== asset registration')
    for rel in register.write_all(dist, args.poles, args.drag):
        print(f'  {rel}')
    print()

    files = sorted(p.relative_to(dist).as_posix() for p in dist.rglob('*') if p.is_file())
    (out / 'FILES.txt').write_text(''.join(f'update\\{f.replace("/", chr(92))}\n' for f in files))
    shutil.copyfile(HERE / 'package' / 'README.txt', out / 'README.txt')
    print(f'== {dist}')
    for f in files:
        print(f'  update/{f:55} {(dist / f).stat().st_size:10} bytes')


if __name__ == '__main__':
    main()
