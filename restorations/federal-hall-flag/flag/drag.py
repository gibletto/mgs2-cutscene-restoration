"""The officer dragging the flag away in the third cutscene.

His path comes out of the cutscene itself, so the cloth follows what he does.
He takes it at frame 2747 and the camera cuts away at 3114.

    build(<game>/assets/anm/us/w61a_flag_fall1.anm, <officer path>, <out.anm>)
"""
import math
import pathlib
import struct

from mgs2.lzsh import compress, decompress
from mgs2.outputs import checked
from mgs2.strcode import strcode

N_VERTS = 117
N_FRAMES = 249
STRIDE = N_VERTS * 12

STATIC_FRAME = 3                  # Konami hides both flags on frame 0; this puts one back
SCENE_FRAME = 2540                # the camera cuts to the shot the flag is in
GRAB_FRAME = 2747                 # the officer takes it, 207 frames later
END_FRAME = 3113                  # last frame before the camera cuts at 3114

# Created on the cut into the shot, so the swap from Konami's static flag can't be
# seen. Creating it at the top of the cutscene drifts and runs out before he moves.
SHOW_FRAME = SCENE_FRAME

# How many cutscene frames one animation frame lasts -- two here, where the fall is
# one. `Act` adds the rate a second time when DM_FrameSkip is set, and p01 has
# frame-skip effects running while the fall plays where p03 has none at all, so the
# same player steps at half the speed in this cutscene.
DEMO_PER_ANIM = 2

# how the cloth answers being pulled
SLACK_PER_MM = 0.1                # how far the bundle stretches before it all moves
FLATTEN_MM = 1500.0               # travel after the slack is gone that lays a vertex flat
DEFLATE_START_MM = 1800.0         # by the end the tail has lost its shape
DEFLATE_MM = 1200.0
MIN_DRAG_MM = 10.0

# the steps outside Federal Hall, measured off the map
LANDING_Y, LANDING_Z = 3000.0, 22600.0
GROUND_Y, GROUND_Z = 2000.0, 24100.0

PLACE = (0.0, 2600.0, 23600.0)    # where the flag lies, the same spot as the static one

MODEL = 'w61a_flag_drag'          # its own registration: p01's fall needs a different animation
SOURCE = 'w61a_flag_fall1'
ACTOR = '引き摺り星条旗'               # "dragged Stars and Stripes"

EFFECT_ID = 0x01004000
NO_PARENT = -1
PUT_ANM_PLAY = 1


def registration():
    return dict(model=MODEL, kms=SOURCE, cv2=SOURCE, anm=MODEL,
                tri='w61a_flag', texture='w61a_flag')


def stair_floor(z: float) -> float:
    if z <= LANDING_Z:
        return LANDING_Y
    if z >= GROUND_Z:
        return GROUND_Y
    return LANDING_Y + (GROUND_Y - LANDING_Y) * (z - LANDING_Z) / (GROUND_Z - LANDING_Z)


def read_path(path) -> dict[int, tuple[float, float, float]]:
    out = {}
    for line in pathlib.Path(path).read_text().splitlines():
        if line.startswith('#') or not line.strip():
            continue
        f, x, y, z, *_ = line.split()
        out[int(f)] = (float(x), float(y), float(z))
    return out


DARKEN = 0.80             # the dragged cloth sits 20% down on Konami's own shading


def shade_norms(raw: bytes):
    """Darken the animation's own normals, keeping their spread.

    Leans the whole sheet rather than scaling it, which would flatten the cloth.
    """
    from .poles import LIGHT, mean_lit, shade

    n = len(raw) // 6
    unit, mag = [], []
    for i in range(n):
        v = struct.unpack_from('<3h', raw, i * 6)
        m = math.sqrt(sum(c * c for c in v)) or 1.0
        mag.append(m)
        unit.append(tuple(c / m for c in v))

    was = mean_lit(unit, LIGHT)
    out, w = shade(unit, LIGHT, was * DARKEN)
    packed = b''.join(
        struct.pack('<3h', *(int(round(max(-32768, min(32767, c * m)))) for c in v))
        for v, m in zip(out, mag))
    print(f'  shading: lit {was:.3f} -> {mean_lit(out, LIGHT):.3f} '
          f'({DARKEN:.0%} of the original), leaned {w:+.2f}')
    return packed


def rest_pose(anm_path):
    """The crumpled flag: the last frame of Konami's fall, positions and normals."""
    raw = decompress(pathlib.Path(anm_path).read_bytes())
    o = (N_FRAMES - 1) * STRIDE
    pos = [struct.unpack_from('<3h', raw, o + i * 6) for i in range(N_VERTS)]
    norms = raw[o + N_VERTS * 6: o + STRIDE]
    return pos, norms


def drag_frame(rest, disp) -> list[tuple[float, float, float]]:
    """The cloth after the officer has moved `disp` from where he picked it up."""
    dist = math.hypot(disp[0], disp[2])
    if dist < MIN_DRAG_MM:
        return [tuple(float(c) for c in v) for v in rest]

    dx, dz = disp[0] / dist, disp[2] / dist
    along = [v[0] * dx + v[2] * dz for v in rest]
    nearest, farthest = max(along), min(along)
    spread = max(nearest - farthest, 1.0)

    out = []
    for v, a in zip(rest, along):
        slack = (nearest - a) * SLACK_PER_MM
        pull = (dist - slack) / dist if dist > slack else 0.0

        # the end in his hand stays bunched; the tail behind it lays out and, late on, sags
        rear = (nearest - a) / spread
        flat = max(0.0, min((dist - slack) / FLATTEN_MM, 1.0)) * rear
        late = max(0.0, min((dist - DEFLATE_START_MM) / DEFLATE_MM, 1.0)) * rear
        sag = flat + (1.0 - flat) * late

        rest_z = PLACE[2] + v[2]
        now_z = rest_z + pull * disp[2]
        height = (PLACE[1] + v[1] - stair_floor(rest_z)) * (1.0 - sag)

        out.append((v[0] + pull * disp[0],
                    stair_floor(now_z) + height - PLACE[1],
                    v[2] + pull * disp[2]))
    return out


def build(anm_path, path_file, out_anm):
    rest, norms = rest_pose(anm_path)
    norms = shade_norms(norms)
    walk = read_path(path_file)
    if GRAB_FRAME not in walk:
        raise SystemExit(f'the officer path does not cover frame {GRAB_FRAME}')
    start = walk[GRAB_FRAME]

    def encode(pose):
        return b''.join(struct.pack('<3h', *(int(round(max(-32768, min(32767, c)))) for c in p))
                        for p in pose) + norms

    # Before he takes it the displacement is zero, so `drag_frame` hands back the rest
    # pose: the opening frames are the flag lying there, and the cloth only starts
    # moving when he does.
    frames = []

    for f in range(SHOW_FRAME, END_FRAME + 1, DEMO_PER_ANIM):
        # he walks towards it first; nothing moves until he picks it up
        here = start if f <= GRAB_FRAME else walk.get(f, start)
        disp = tuple(here[i] - start[i] for i in range(3))
        frames.append(encode(drag_frame(rest, disp)))

    raw = b''.join(frames)
    packed = compress(raw)
    checked(out_anm).write_bytes(packed)
    if decompress(packed) != raw:
        raise SystemExit('the packed animation does not read back -- refusing to ship it')

    last = walk.get(END_FRAME, start)
    moved = math.dist(last, start)
    print(f'{pathlib.Path(out_anm).name}: {len(frames)} frames, {len(packed)} bytes')
    still = (GRAB_FRAME - SHOW_FRAME) // DEMO_PER_ANIM
    print(f'  created at {SHOW_FRAME} ({still} frames lying still), '
          f'dragged from {GRAB_FRAME} to {END_FRAME}; '
          f"Konami's static flag covers {STATIC_FRAME}..{SHOW_FRAME - 1}")
    print(f'  the officer moves {moved:.0f} mm, sampled every {DEMO_PER_ANIM} cutscene frames')

    # when each part of it actually gets going, which is what "starts late" is about
    span = max(v[0] for v in rest) - min(v[0] for v in rest)
    along = max(abs(rest[i][2] - rest[j][2]) for i in (0,) for j in range(len(rest)))
    for label, need in (('the end in his hand', 0.0),
                        ('the middle', SLACK_PER_MM * along * 0.5),
                        ('the far tail', SLACK_PER_MM * along)):
        moved_by = None
        for f in range(GRAB_FRAME, END_FRAME + 1):
            here = walk.get(f, start)
            if math.hypot(here[0] - start[0], here[2] - start[2]) >= max(need, MIN_DRAG_MM):
                moved_by = f
                break
        print(f'    {label:20} starts at frame {moved_by} '
              f'({moved_by - GRAB_FRAME} after he takes it)')
    print(f'  cloth height taken from the steps: {LANDING_Y:.0f} mm at z<={LANDING_Z:.0f} '
          f'down to {GROUND_Y:.0f} mm at z>={GROUND_Z:.0f}')
    return len(frames)


def packet_body() -> bytes:
    data = bytearray(128)
    struct.pack_into('<i', data, 0x00, strcode(ACTOR))
    struct.pack_into('<i', data, 0x04, strcode(MODEL, 'ascii'))
    struct.pack_into('<4i', data, 0x10, NO_PARENT, 0, 0, 0)
    struct.pack_into('<4f', data, 0x20, *PLACE, 0.0)
    struct.pack_into('<4h', data, 0x30, 0, 0, 0, 0)
    struct.pack_into('<4f', data, 0x40, 1.0, 1.0, 1.0, 0.0)
    struct.pack_into('<4f', data, 0x50, 0.0, 0.0, 0.0, 0.0)
    struct.pack_into('<4f', data, 0x60, 0.0, 0.0, 0.0, 0.0)
    struct.pack_into('<i', data, 0x70, PUT_ANM_PLAY)
    return struct.pack('<4i', EFFECT_ID, 1, 0, 0) + struct.pack('<4i', 2000, 0, 0, 0) + bytes(data)


def calls():
    """Konami's fallen flag holds the ground; ours takes over on the cut into the shot.

    The demo hides both flags on frame 0 -- that hide is the deletion -- and never
    turns either back on, so putting the static one back is a call of its own.
    """
    return [(STATIC_FRAME, '静止星条旗オン'),
            (SHOW_FRAME, '静止星条旗オフ'), (SHOW_FRAME, '@drag')]
