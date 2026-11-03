#!/usr/bin/env python3
"""Build the Manhattan crash restoration into <build>/manhattan-crash/update.

    python build.py                  everything
    python build.py --stock-audio    the game's score (t13a1d) instead of the soundtrack
    python build.py --world-only     just the scenario
    python build.py --demo-only      just the cutscene

Inputs are read from your game install, the source tree and your asset folder
(see local.env and ASSETS.md); the soundtrack is in audio/. Nothing is written to any of them: the result is
an `update/` folder to copy into the game, which `deploy.py` does.

The cutscene is built in three passes over Konami's own p080_13_p08:
  1  author  (crash/author.py)  through a private Cutscene Studio server: extend
                                 it, write the new shots, bake
  2  dress   (crash/dress.py)   effects written into the bake's packets
  3  audio   (crash/audio.py)   the soundtrack encoded and grafted into the PCM channel
"""
import argparse
import json
import pathlib
import shutil
import sys

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parents[1] / 'lib'))
sys.path.insert(0, str(HERE))

from mgs2 import config, scenario, studio                 # noqa: E402
from mgs2.outputs import checked                          # noqa: E402
from crash import audio, models, register, timeline, world  # noqa: E402
from crash.author import author                           # noqa: E402
from crash.dress import dress                             # noqa: E402

NAME = 'manhattan-crash'
DEMO = 'p080_13_p08'               # the bridge flyover at the end of the arrival
DONOR = ('us', 'demo', '_bp', 't13a1d.sdt')   # the tanker intro: its score, its PCM framing
DEMO_REGIONS = ('us', 'jp')        # the same file serves both


def assets_dir() -> pathlib.Path:
    d = config.assets() / NAME
    if not d.is_dir():
        raise SystemExit(f'no assets at {d} -- see ASSETS.md')
    return d


def build_world(out: pathlib.Path, dist: pathlib.Path):
    built = scenario.build(world.STAGE, world.edit, 'manhattan_crash', out_dir=out / 'scenario')
    for region, path in built.items():
        shutil.copyfile(path, checked(dist / 'assets' / 'gcx' / region / '_bp' / f'{world.STAGE}.gcx'))


SOUNDTRACK = HERE / 'audio' / 'arsenal-crash.wav'   # the released mix, part of this repo


def soundtrack(stock: bool, work: pathlib.Path) -> bytes | None:
    """The packed stream. The repo's own mix unless your asset folder overrides it with
    soundtrack.wav (prepared and encoded here), soundtrack.wma (encoded) or
    soundtrack.amwx (packed)."""
    if stock:
        return None
    a = config.assets(required=False)
    a = a / NAME if a else None
    donor = config.game().joinpath(*DONOR)
    if a and (a / 'soundtrack.amwx').exists():
        return (a / 'soundtrack.amwx').read_bytes()
    if a and (a / 'soundtrack.wma').exists():
        wma = a / 'soundtrack.wma'
    else:
        wav = a / 'soundtrack.wav' if a and (a / 'soundtrack.wav').exists() else SOUNDTRACK
        print(f'  soundtrack: {wav.name}')
        audio.prepare_wav(wav, work / 'soundtrack.prepared.wav')
        wma = work / 'soundtrack.wma'
        audio.encode_wma(work / 'soundtrack.prepared.wav', wma)
    stream = audio.pack_stream(audio.wma_packets(wma), donor)
    checked(work / 'soundtrack.amwx').write_bytes(stream)
    return stream


def build_demo(out: pathlib.Path, dist: pathlib.Path, stock_audio: bool):
    game = config.game()
    work = out / 'demo'
    if work.exists():
        shutil.rmtree(work)
    src = checked(work / f'{DEMO}.sdt')
    shutil.copyfile(game / 'us' / 'demo2' / '_bp' / f'{DEMO}.sdt', src)
    stream = soundtrack(stock_audio, work)

    print('== [1/3] author')
    with studio.serve(work / 'studio') as s:
        s.scene(src)
        print('  extend:', s.extend(src, timeline.END))
        sc = s.scene(src)
        tracks = author(sc)
        print('  posted, dirty =', s.post(sc)['dirty'])
        baked = s.bake(src)
    checked(work / 'tracks.json').write_text(json.dumps(tracks, indent=1))

    print('== [2/3] dress')
    dressed = work / f'{DEMO}.dressed.sdt'
    dress(baked, dressed, tracks)

    print('== [3/3] audio')
    final = work / f'{DEMO}.final.sdt'
    audio.graft(dressed, game.joinpath(*DONOR), final, stream)
    for region in DEMO_REGIONS:
        shutil.copyfile(final, checked(dist / region / 'demo2' / '_bp' / f'{DEMO}.sdt'))


def build_assets(dist: pathlib.Path):
    game = config.game()
    a = assets_dir()
    kms, win = dist / 'assets' / 'kms' / 'us', dist / 'assets' / 'kms' / 'us' / '_win'
    missing = []
    for m, source, _ in models.MODELS:
        if source == 'game':
            pair = (game / 'assets' / 'kms' / 'us' / f'{m}.kms',
                    game / 'assets' / 'kms' / 'us' / '_win' / f'{m}.cmdl')
        else:
            pair = (a / 'models' / f'{m}.kms', a / 'models' / f'{m}.cmdl')
        for p in pair:
            if not p.exists():
                missing.append(p)
        if not missing:
            shutil.copyfile(pair[0], checked(kms / f'{m}.kms'))
            shutil.copyfile(pair[1], checked(win / f'{m}.cmdl'))
    if missing:
        raise SystemExit('missing models:\n  ' + '\n  '.join(map(str, missing)))

    ids = sorted(int(p.stem, 16) & 0xFFFFFF for p in (a / 'textures').glob('*.ctxr'))
    for tid in ids:
        shutil.copyfile(a / 'textures' / f'{tid:08x}.ctxr',
                        checked(dist / 'textures' / 'flatlist' / 'ovr_stm' / '_win' / f'{tid:08x}.ctxr'))
    print(f'  {len(models.MODELS)} models, {len(ids)} textures')
    register.write_tris(game, dist, ids)
    register.write_lists(dist, [m for m, _, _ in models.MODELS], ids)


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--stock-audio', action='store_true')
    only = ap.add_mutually_exclusive_group()
    only.add_argument('--world-only', action='store_true')
    only.add_argument('--demo-only', action='store_true')
    args = ap.parse_args()

    out = config.build_dir() / NAME
    dist = out / 'update'
    if dist.exists():
        shutil.rmtree(dist)

    if not args.demo_only:
        print('== the world')
        build_world(out, dist)
        print('== models, textures, registration')
        build_assets(dist)
    if not args.world_only:
        build_demo(out, dist, args.stock_audio)

    files = sorted(p.relative_to(dist).as_posix() for p in dist.rglob('*') if p.is_file())
    (out / 'FILES.txt').write_text(''.join(f'update\\{f.replace("/", chr(92))}\n' for f in files))
    shutil.copyfile(HERE / 'package' / 'README.txt', out / 'README.txt')
    print(f'== {dist}: {len(files)} files, '
          f'{sum((dist / f).stat().st_size for f in files) / 1e6:.1f} MB')


if __name__ == '__main__':
    main()
