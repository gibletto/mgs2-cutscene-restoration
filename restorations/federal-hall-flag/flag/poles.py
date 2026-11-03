"""The eleven flags hanging from the street poles: where they go and what they look like.

The flag Konami shipped is a flying flag. A hanging one is the same mesh with
different vertex positions, so this writes new keyframes for it -- generated
here from the game's own flag, no new model is authored.

Pole positions were measured from the MoreFlags / 911Restore mod, which first put
flags back on those poles.
"""
import math
import pathlib
import struct

from mgs2 import cv2 as cv2lib
from mgs2.outputs import checked

# Where the cloth hangs from, in the mesh's own space. World position comes from
# the scenario's -p, because vertex coords have no room for a street address.
ANCHOR = (584.0, 25637.0, 22500.0)

POLE_RISE_DEG = 23.0      # the brackets tilt up out of the facade
POLE_SPAN_MM = 1460.0     # how much pole carries cloth
HANG_MM = 3460.0          # how far it hangs
SWAY_DEG = 5.0            # how far the hem swings
SIDE_MM = 100.0           # it hangs beside the bar, not through it

# The stage's parallel light, off the head of assets/lt2/us/w61a.lt2. MGS2 points
# normals inward and stores the light running light->surface, so a lit vertex has
# a positive dot.
LIGHT = (-0.935, -0.159, -0.318)
TARGET_LIT = 0.36         # 0 = ambient only, ~0.9 = full sun
STREET_NEAR = (0.0, 2600.0, 23600.0)
DISTANCE_FADE = 0.15      # the far end of the street is this much darker

# Two copies of the mesh. The word at KMS +0x10 is the texture group, and it is
# the only thing that differs between Konami's own two copies of this flag: the
# ending stage registers the artwork per model, the other stages hang it off the
# map group. A mesh naming a group its stage hasn't got draws nothing, silently.
HANG_KMS = 'w61a_flag_hang'          # for d082p01
HANG_KMS_MAP = 'w61a_flag_hang_map'  # for d080p08 and w61a
TEX_GROUP_OWN = 0x009d798f
TEX_GROUP_MAP = 0x003c5e81

# Three cloth shapes so eleven flags aren't one flag repeated. dip = how far the
# top edge sags between its two corners; fold/ripple = two sets of folds at
# different wavelengths running opposite ways; hem = the bottom edge's wobble;
# curl = how far the hem furls out; travel/drift = how fast each set migrates.
SHAPES = [
    dict(dip= 90.0, fold=110.0, waves=2, phase=0.0,
         ripple=70.0, waves2=5, phase2=0.9, hem=55.0, curl=170.0,
         gather=0.03, sway_phase=0.0,  travel=1, drift=-2),
    dict(dip=125.0, fold=150.0, waves=3, phase=0.7,
         ripple=95.0, waves2=7, phase2=2.4, hem=80.0, curl=240.0,
         gather=0.04, sway_phase=2.1,  travel=1, drift= 2),
    dict(dip= 70.0, fold= 80.0, waves=2, phase=2.1,
         ripple=55.0, waves2=4, phase2=1.6, hem=40.0, curl=120.0,
         gather=0.02, sway_phase=4.2,  travel=-1, drift= 3),
]

# x, y, z of the cloth's inner top corner, and which way the bracket points:
# +1 is out along +z, -1 out along -z.
POLES = [
    (15975.0,  9130.0, 29663.0,  1),
    (37348.0,  9060.0, 29692.0,  1),
    (44825.0, 11620.0, 42283.0, -1),
    (53766.0, 11620.0, 42290.0, -1),
    (62624.0, 11630.0, 42286.0, -1),
    (91390.0,  8060.0, 28595.0,  1),
    (95809.0, 14520.0, 42284.0, -1),
    (99365.0,  8060.0, 28581.0,  1),
    (107487.0, 8060.0, 28593.0,  1),
    (109920.0, 14520.0, 42283.0, -1),
    (124277.0, 14510.0, 42281.0, -1),
]
MODELS = [f'w61a_flag_hang{i}' for i in range(len(POLES))]

STAGE = 'd082p01'
REGIONS = ('eu', 'jp')
TEXTURE_OWNER = 'w61a_flag'
HALF_TURN = 2048          # the engine counts a full turn as 4096
WAVE_BASE = 52            # roughly 45 ticks a keyframe
WAVE_RAND = 3             # randomised per flag so they don't swing together
WAVE_STAGGER = 19         # ...and each pole starts from a different base


def variants():
    """One drape per pole: (name, shape, flip normals, how lit it should be)."""
    far = max(math.dist(p[:3], STREET_NEAR) for p in POLES)
    for i, (x, y, z, facing) in enumerate(POLES):
        along = math.dist((x, y, z), STREET_NEAR) / far
        yield (MODELS[i], SHAPES[i % len(SHAPES)], facing < 0,
               TARGET_LIT * (1.0 - DISTANCE_FADE * along))


# ---------------------------------------------------------------- placement

def placement(pole):
    """-p and -r for one pole. The far side of the street is a half turn round."""
    x, y, z, facing = pole
    if facing > 0:
        return (x - ANCHOR[0], y - ANCHOR[1], z - ANCHOR[2]), 0
    return (x + ANCHOR[0], y - ANCHOR[1], z + ANCHOR[2]), HALF_TURN


def gcl_lines() -> str:
    out = []
    for i, pole in enumerate(POLES):
        pos, rot = placement(pole)
        out.append(f'\tchara プット頂点アニメオブジェ st_flag{i:02d} \\\n'
                   f'\t\t-m d:HANG_FLAG_NAME{i} \\\n'
                   f'\t\t-b {WAVE_BASE + (i * 7) % WAVE_STAGGER} \\\n'
                   f'\t\t-w {WAVE_RAND} \\\n'
                   f'\t\t-r 0,{rot},0 \\\n'
                   f'\t\t-p {pos[0]:.0f},{pos[1]:.0f},{pos[2]:.0f}')
    return '\n'.join(out)


def registration():
    return [dict(model=m, kms=HANG_KMS, cv2=m,
                 tri=TEXTURE_OWNER, texture=TEXTURE_OWNER) for m in MODELS]


# ---------------------------------------------------------------- the cloth

def mesh_uvs(kms_path, unit=0):
    """The mesh's UVs, one per expanded vertex."""
    d = pathlib.Path(kms_path).read_bytes()
    o = 0x40 + 0x50 * unit
    n_packs = struct.unpack_from('<i', d, o + 4)[0]
    packs = struct.unpack_from('<Q', d, o + 0x30)[0]
    out = []
    for p in range(n_packs):
        po = packs + p * 96
        n = struct.unpack_from('<i', d, po + 4)[0]
        up = struct.unpack_from('<Q', d, po + 0x30)[0]
        out += [tuple(v / 4096 for v in struct.unpack_from('<2h', d, up + j * 4))
                for j in range(n)]
    return out


def shared_uvs(key, kms_path):
    """Carry the mesh's UVs back onto the 117 shared vertices."""
    uvs = mesh_uvs(kms_path)
    out = [None] * len(key.verts)
    for expanded, shared in enumerate(key.verts_index):
        out[shared] = uvs[expanded]
    if any(uv is None for uv in out):
        raise SystemExit('some shared vertices carry no UV')
    return out


def rotate_about(v, axis, angle):
    """Rodrigues: turn v about a unit axis."""
    c, s = math.cos(angle), math.sin(angle)
    dot = sum(v[i] * axis[i] for i in range(3))
    cross = (axis[1] * v[2] - axis[2] * v[1],
             axis[2] * v[0] - axis[0] * v[2],
             axis[0] * v[1] - axis[1] * v[0])
    return tuple(v[i] * c + cross[i] * s + axis[i] * dot * (1 - c) for i in range(3))


def drape(uv_list, cycle, shape):
    """Vertex positions for one keyframe. `cycle` is 0..1 round the loop.

    u runs across the cloth (1 on the pole), v along it. Hanging swaps them: the
    hoist edge lies along the pole and the fly end points at the ground.
    """
    rise = math.radians(POLE_RISE_DEG)
    pole = (0.0, math.sin(rise), math.cos(rise))
    down = (0.0, -1.0, 0.0)
    side = (1.0, 0.0, 0.0)

    turn = 2.0 * math.pi * cycle
    sway_rad = math.radians(SWAY_DEG) * math.sin(turn + shape['sway_phase'])
    travel = shape['travel'] * turn
    drift = shape['drift'] * turn

    out = []
    for u, v in uv_list:
        s = 1.0 - v                       # 0 at the outer end, 1 at the wall
        t = 1.0 - u                       # 0 on the pole, 1 at the hem
        drop = t * HANG_MM
        pinned = math.sin(math.pi * s)    # 0 at both corners, so it stays hung

        dip = shape['dip'] * pinned * (1.0 + 0.12 * math.sin(turn + 1.1))
        fold = shape['fold'] * pinned * (0.15 + 0.85 * t) \
            * math.sin(shape['waves'] * math.pi * s + shape['phase'] + travel)
        ripple = shape['ripple'] * pinned * t * t \
            * math.sin(shape['waves2'] * math.pi * s + shape['phase2'] + drift)
        hem = shape['hem'] * t ** 3 * math.sin(3.3 * math.pi * s + 0.6 - drift * 0.5)
        curl = shape['curl'] * t * t
        gather = 1.0 - shape['gather'] * pinned * t

        local = tuple(side[i] * (SIDE_MM + fold + ripple + curl)
                      + down[i] * (drop + dip + hem) for i in range(3))
        swung = rotate_about(local, pole, sway_rad * (drop / HANG_MM))
        out.append(tuple(ANCHOR[i] + pole[i] * s * POLE_SPAN_MM * gather + swung[i]
                         for i in range(3)) + (0.0,))
    return out


def normals(verts, uv_list, flip=False):
    """Normals off the 13 x 9 grid. Negated, because MGS2 points them inward."""
    us = sorted({round(u, 4) for u, _ in uv_list})
    vs = sorted({round(v, 4) for _, v in uv_list})
    at = {(round(u, 4), round(v, 4)): i for i, (u, v) in enumerate(uv_list)}

    sign = 1.0 if flip else -1.0
    out = [None] * len(verts)
    for ui, u in enumerate(us):
        for vi, v in enumerate(vs):
            i = at[(u, v)]
            a = [verts[at[(us[min(ui + 1, len(us) - 1)], v)]][k]
                 - verts[at[(us[max(ui - 1, 0)], v)]][k] for k in range(3)]
            b = [verts[at[(u, vs[min(vi + 1, len(vs) - 1)])]][k]
                 - verts[at[(u, vs[max(vi - 1, 0)])]][k] for k in range(3)]
            n = (a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0])
            m = math.sqrt(sum(c * c for c in n)) or 1.0
            out[i] = tuple(sign * c / m for c in n) + (0.0,)
    return out


# ---------------------------------------------------------------- shading

def half_turn(v):
    """The far-side poles are placed 180 deg round, so their light is mirrored."""
    return (-v[0], v[1], -v[2])


def blend(norms, light, w):
    """Lean every normal towards the light (w > 0) or away from it (w < 0)."""
    out = []
    for n in norms:
        b = [(1 - abs(w)) * n[i] + w * light[i] for i in range(3)]
        m = math.sqrt(sum(c * c for c in b)) or 1.0
        out.append(tuple(c / m for c in b))
    return out


def mean_lit(norms, light):
    """How bright these normals come out, clamped the way the preshader clamps."""
    return sum(max(0.0, sum(n[i] * light[i] for i in range(3)))
               for n in norms) / max(len(norms), 1)


def shade(norms, light, target):
    """Find the one blend that puts the mean brightness on `target`.

    One weight for the whole sheet, so each normal keeps its offset and the folds
    still read. Signed, so it darkens as well as brightens.
    """
    lo, hi = -0.95, 0.95
    for _ in range(50):
        mid = (lo + hi) / 2
        if mean_lit(blend(norms, light, mid), light) < target:
            lo = mid
        else:
            hi = mid
    w = (lo + hi) / 2
    return blend(norms, light, w), w


# ---------------------------------------------------------------- build

def build(src, dst, shape, flip, lit, kms):
    """Re-key the flying flag's cv2 into one hanging drape."""
    c = cv2lib.read(src)
    uv = shared_uvs(c.keys[0], kms)
    light = half_turn(LIGHT) if flip else LIGHT
    weight = None

    for i, key in enumerate(c.keys):
        key.verts = drape(uv, i / len(c.keys), shape)   # one full loop over the keys
        if key.norms:
            fresh = normals(key.verts, uv, flip)
            if weight is None:
                _, weight = shade(fresh, light, lit)
            fresh = [n + (0.0,) for n in blend(fresh, light, weight)]
            key.norms = [fresh[j % len(fresh)] for j in range(len(key.norms))]

    cv2lib.check_for_vanime(c)
    size = cv2lib.write(dst, c)
    got = mean_lit([n[:3] for n in c.keys[0].norms[:len(uv)]], light)
    print(f'  {pathlib.Path(dst).name}: {size} B  '
          f'shape dip {shape["dip"]:.0f}/fold {shape["fold"]:.0f}  '
          f'normals {"flipped" if flip else "as authored"}  lit {got:.3f}')
    return (tuple(min(v[i] for k in c.keys for v in k.verts) for i in range(3)),
            tuple(max(v[i] for k in c.keys for v in k.verts) for i in range(3)))


def patch_mesh(src_kms, dst_kms, lo, hi, group, margin=0.06):
    """Copy the mesh with a box that fits the drape, and the right texture group.

    put_vanime.c copies the KMS box straight into the object's culling bounds, and
    Konami's box describes the flying flag -- the drape hangs 1.7 m below it, so
    the flags popped in and out at the edge of frame.
    """
    d = bytearray(pathlib.Path(src_kms).read_bytes())
    pad = [(hi[i] - lo[i]) * margin for i in range(3)]
    struct.pack_into('<3f', d, 0x1C, *(lo[i] - pad[i] for i in range(3)))
    struct.pack_into('<3f', d, 0x28, *(hi[i] + pad[i] for i in range(3)))
    struct.pack_into('<I', d, 0x10, group)
    checked(dst_kms).write_bytes(bytes(d))
    print(f'  {pathlib.Path(dst_kms).name}: box fitted, texture group {group:#08x}')


def build_all(flying_cv2, flag_kms, cv2_dir, kms_dir):
    """Every drape's keyframes into cv2_dir, both mesh copies into kms_dir.

    flying_cv2: the game's assets/cv2/us/w61a_flag.cv2
    flag_kms:   the game's assets/kms/us/w61a_flag_stage_d082p01.kms (and its _win/.cmdl)
    """
    cv2_dir, kms_dir = pathlib.Path(cv2_dir), pathlib.Path(kms_dir)
    print(f'  {len(MODELS)} drapes, lit {TARGET_LIT:.2f} fading {DISTANCE_FADE:.0%} '
          f'down the street:')
    boxes = [build(flying_cv2, cv2_dir / f'{n}.cv2', sh, fl, lit, flag_kms)
             for n, sh, fl, lit in variants()]
    lo = tuple(min(b[0][i] for b in boxes) for i in range(3))
    hi = tuple(max(b[1][i] for b in boxes) for i in range(3))

    patch_mesh(flag_kms, kms_dir / f'{HANG_KMS}.kms', lo, hi, TEX_GROUP_OWN)
    patch_mesh(flag_kms, kms_dir / f'{HANG_KMS_MAP}.kms', lo, hi, TEX_GROUP_MAP)

    # the .cmdl is the D3D half of the mesh and is unchanged, but registration
    # names one basename for both files, so each copy needs one
    src_kms = pathlib.Path(flag_kms)
    cmdl = (src_kms.parent / '_win' / (src_kms.stem + '.cmdl')).read_bytes()
    for name in (HANG_KMS, HANG_KMS_MAP):
        checked(kms_dir / '_win' / f'{name}.cmdl').write_bytes(cmdl)
