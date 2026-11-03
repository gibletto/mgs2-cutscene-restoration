"""The flag falling onto Solidus, played by the game's own animation player.

Konami's animation starts at the top of the pole; the cutscene needs it starting
about three metres up and taking the time it was tuned for, so it is re-cut and
re-packed. Nothing can seek a stream, which is why it has to be rewritten.

    build(<game>/assets/anm/us/w61a_flag_fall1.anm, <out.anm>)
"""
import math
import pathlib
import struct

from mgs2.lzsh import compress, decompress
from mgs2.outputs import checked
from mgs2.strcode import strcode

N_VERTS = 117
N_FRAMES = 249
STRIDE = N_VERTS * 12             # short[3] positions then short[3] normals

CHOP_FRAME = 5066                 # Raiden cuts the pole
LAND_FRAME = 5205                 # the flag is down, 139 frames after the cut
FALL_START = 137                  # animation frame the flag is ~3 m up
TOUCHDOWN = 162                   # animation frame it reaches the ground

# How many cutscene frames one animation frame lasts. One: `Act` adds the rate twice
# while a cutscene is running (`ratio += rate * DM_FrameSkip`, then `ratio += rate`),
# which cancels the player's half-speed default. Tested both ways -- at two the fall
# is visibly twice too quick.
DEMO_PER_ANIM = 1
SETTLE_FRAMES = 240               # cutscene frames of cloth settling after it lands

# The model the stage already registers, and its own animation file, replaced in the
# overlay. The runtime version created exactly this model id; a fresh registration is
# a difference with nothing to gain, and it did not work.
MODEL = 'w61a_flag_fall1'
ACTOR = '落下星条旗'                 # "falling Stars and Stripes"
PLACE = (0.0, 2600.0, 23600.0)    # where the fallen flag sits, as the static one does
# The runtime version passed zeroes here and drew fine.
BOUND_U = (0.0, 0.0, 0.0)
BOUND_L = (0.0, 0.0, 0.0)

EFFECT_ID = 0x01004000
NO_PARENT = -1                    # d2.vx: what the shipped packets use for "not attached"
PUT_ANM_PLAY = 1


def frames(raw: bytes):
    if len(raw) != STRIDE * N_FRAMES:
        raise SystemExit(f'expected {STRIDE * N_FRAMES} bytes of animation, got {len(raw)}')
    return [raw[f * STRIDE:(f + 1) * STRIDE] for f in range(N_FRAMES)]


def blend(a: bytes, b: bytes, t: float) -> bytes:
    """One frame between two, interpolated componentwise."""
    if t <= 0:
        return a
    if t >= 1:
        return b
    n = len(a) // 2
    va = struct.unpack(f'<{n}h', a)
    vb = struct.unpack(f'<{n}h', b)
    return struct.pack(f'<{n}h', *(int(round(x + (y - x) * t)) for x, y in zip(va, vb)))


def retime(anim, first: float, last: float, count: int, start_rate: float | None = None):
    """Resample animation frames `first`..`last` into `count` frames.

    `start_rate` carries the incoming speed into the first frame and eases off,
    so the settle doesn't snap after the stretched fall.
    """
    span = last - first
    a = 1.0
    if start_rate is not None and span:
        a = max(0.0, min(1.0, start_rate * count / span))

    out = []
    for i in range(count):
        t = i / max(1, count - 1)
        at = first + span * (a * t + (1.0 - a) * t * t)
        lo = min(N_FRAMES - 1, int(math.floor(at)))
        out.append(blend(anim[lo], anim[min(N_FRAMES - 1, lo + 1)], at - lo))
    return out


def build(src_anm, out_anm):
    anim = frames(decompress(pathlib.Path(src_anm).read_bytes()))

    fall_frames = (LAND_FRAME - CHOP_FRAME) // DEMO_PER_ANIM
    settle_frames = SETTLE_FRAMES // DEMO_PER_ANIM
    cut = retime(anim, FALL_START, TOUCHDOWN, fall_frames) \
        + retime(anim, TOUCHDOWN, N_FRAMES - 1, settle_frames)[1:]

    raw = b''.join(cut)
    packed = compress(raw)
    checked(out_anm).write_bytes(packed)

    if decompress(packed) != raw:
        raise SystemExit('the packed animation does not read back -- refusing to ship it')

    def lowest(frame):
        return min(struct.unpack_from('<3h', frame, i * 6)[1] for i in range(N_VERTS))

    print(f'{pathlib.Path(out_anm).name}: {len(cut)} frames, {len(packed)} bytes')
    print(f'  animation {FALL_START}..{TOUCHDOWN} stretched over {fall_frames} frames '
          f'= {fall_frames * DEMO_PER_ANIM} demo frames: the cut at {CHOP_FRAME}, '
          f'down at {CHOP_FRAME + fall_frames * DEMO_PER_ANIM}')
    print(f'  then {settle_frames} frames of settling, ending at '
          f'{CHOP_FRAME + len(cut) * DEMO_PER_ANIM}, after which it holds')
    print(f'  the settle picks up at the speed the fall arrives at '
          f'({(TOUCHDOWN - FALL_START) / max(1, fall_frames):.2f} animation frames each) '
          f'and eases off')
    print(f'  starts {lowest(cut[0]) + PLACE[1]:.0f} mm up, '
          f'ends {lowest(cut[-1]) + PLACE[1]:.0f} mm up')
    return len(cut)


def packet_body() -> bytes:
    """The effect packet that creates it, laid out as `4000_NewPutSTanimeObjectCall.c`.

    exec_type 1, so a [life, 0, 0, 0] preamble comes before the struct. Life is
    how long the effect lives; the animation outlasts the demo either way.
    """

    data = bytearray(128)                                     # roundup16(sizeof(Data))
    struct.pack_into('<i', data, 0x00, strcode(ACTOR))   # d0  name
    struct.pack_into('<i', data, 0x04, strcode(MODEL, 'ascii'))           # d1  model
    # rot and scale were passed as null pointers by the runtime version; zeroed
    # rotation and unit scale are the same thing to GetResourcesCall
    struct.pack_into('<4i', data, 0x10, NO_PARENT, 0, 0, 0)               # d2  link
    struct.pack_into('<4f', data, 0x20, *PLACE, 0.0)                      # d5  position
    struct.pack_into('<4h', data, 0x30, 0, 0, 0, 0)                       # d6  rotation
    struct.pack_into('<4f', data, 0x40, 1.0, 1.0, 1.0, 0.0)               # d7  scale
    struct.pack_into('<4f', data, 0x50, *BOUND_U, 0.0)                    # d8  bound upper
    struct.pack_into('<4f', data, 0x60, *BOUND_L, 0.0)                    # d9  bound lower
    struct.pack_into('<i', data, 0x70, PUT_ANM_PLAY)                      # d10 mode

    life = 2000
    return struct.pack('<4i', EFFECT_ID, 1, 0, 0) + struct.pack('<4i', life, 0, 0, 0) + bytes(data)


def calls():
    """At the cut: put the flying flag away, and start the fall."""
    return [(CHOP_FRAME, '旗オフ'), (CHOP_FRAME, '@fall')]
