"""Registration: the stage lists that make the game load the new models and textures.

Nothing belonging to the game is edited. Each region's stage folder gets two
supplementary lists beside its own, which MGSHDFix merges at load:

    update/<region>/stage/d080p07/manifest_manhattan_crash.txt
    update/<region>/stage/d080p07/bp_assets_manhattan_crash.txt

Models are registered under their name hash (mgs2.strcode.cache_id). Textures
need one more step: a model looks its textures up in a texture group (.tri), so
rebuilt copies of the two groups this stage's models use declare the new ids
alongside the stock ones, and each texture is then registered against its group.
Textures go first in the list -- a model binds its textures once, at load, so
anything it references has to be registered ahead of it or it renders grey.

See docs/asset-registration.md for the line formats.
"""
from __future__ import annotations

import pathlib
import struct

from mgs2.outputs import checked
from mgs2.strcode import cache_id

STAGE = 'd080p07'
LISTS = ('manifest_manhattan_crash.txt', 'bp_assets_manhattan_crash.txt')
REGIONS = ('eu', 'jp')          # eu/stage serves the US/EU release, jp/stage the Japanese one

# The groups the new models name in their materials: the island and city
# geometry sits in land_high, the statue in building. Every id is declared in
# both; an unused declaration costs nothing and lets a model move between them.
TRIS = (('land_high', '0094d596'), ('building', '00f6e03f'))

HDR = 32     # TRI_FILEHEADER
REC = 192    # DG_TEX, as the Master Collection lays it out (the PS2's was 160)
ALIGN = 128  # texel data start


def add_entries(src: pathlib.Path, dst: pathlib.Path, ids: list[int]):
    """A copy of the texture group `src` that also declares `ids`. A declaration
    only says the id exists -- the pixels come from the .ctxr registered against
    <group>/<id> -- so every field but the id and the flag stays zero."""
    d = src.read_bytes()
    tex_off, tex_size, clut_off, clut_size, n, comp, texel, clut = struct.unpack_from('<8I', d, 0)
    if HDR + n * REC > texel:
        raise SystemExit(f'{src}: {n} entries do not fit before the texel data')
    out = bytearray(d[:HDR + n * REC])
    for tid in ids:
        rec = bytearray(REC)
        struct.pack_into('<I', rec, 16, tid)   # tex_id
        struct.pack_into('<I', rec, 24, 8)     # flag: the pixels come from the ctxr cache
        out += rec
    moved = (len(out) + ALIGN - 1) // ALIGN * ALIGN
    out += b'\0' * (moved - len(out))
    out += d[texel:]
    struct.pack_into('<8I', out, 0, tex_off, tex_size, clut_off, clut_size,
                     n + len(ids), comp, moved, clut + moved - texel)
    checked(dst).write_bytes(out)
    print(f'  {dst.name:22} {n} -> {n + len(ids)} entries')


def write_tris(game: pathlib.Path, dist: pathlib.Path, ids: list[int]) -> list[pathlib.Path]:
    out = []
    for base, _slot in TRIS:
        dst = dist / 'assets' / 'tri' / 'us' / f'{base}_crash.tri'
        add_entries(game / 'assets' / 'tri' / 'us' / f'{base}.tri', dst, ids)
        out.append(dst)
    return out


def lines(region: str, models: list[str], ids: list[int]) -> dict[str, list[str]]:
    """The two lists for one region. Only the trailing cache field of bp_assets
    names the region."""
    s = f'stage/{STAGE}'
    manifest = [f'assets/tri/us/{base}_crash.tri,us/{s}/cache/{slot}.tri,cache/{slot}.tri'
                for base, slot in TRIS]
    manifest += [f'assets/kms/us/{m}.kms,us/{s}/cache/{cache_id(m)}.kms,cache/{cache_id(m)}.kms'
                 for m in models]
    bp = [f'textures/flatlist/{tid:08x}.ctxr,{s}/cache/{tid:08x}.ctxr,{region}/{s}/cache/{slot}/{tid:08x}.ctxr'
          for _, slot in TRIS for tid in ids]
    bp += [f'assets/kms/us/{m}.cmdl,us/{s}/cache/{cache_id(m)}.cmdl,{region}/{s}/cache/{cache_id(m)}.cmdl'
           for m in models]
    return dict(zip(LISTS, (manifest, bp)))


def write_lists(dist: pathlib.Path, models: list[str], ids: list[int]) -> list[pathlib.Path]:
    out = []
    for region in REGIONS:
        for name, ls in lines(region, models, ids).items():
            # CRLF, and nothing else: a stray CR in a name fails its symbol lookup
            p = checked(dist / region / 'stage' / STAGE / name)
            p.write_bytes('\r\n'.join(ls).encode('ascii') + b'\r\n')
            out.append(p)
    return out
