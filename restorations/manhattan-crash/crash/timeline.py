"""The teaser's clock, and the ship's course along it.

The cutscene was authored twice. The first cut ran Konami's footage to frame 2000,
then a run of new shots; the release keeps more of the stock footage (the bridge
pass), replaces the first new shot with a takeover of the stock camera and a tour of
the Statue of Liberty, and compresses the shot after it. Rather than re-time every
hand-tuned constant, everything after frame 2390 is still written in the FIRST
cut's frame numbers ("old time") and goes through T() to land in the release's
("new time"). O() is the inverse; the physics below runs in old time and the
*_n() wrappers call it from new time.

    new frames     what plays
    0    - 1445    Konami's cutscene, untouched (the ship's course bends from 1100)
    1446 - 2180    the takeover: the stock bridge camera is seized, no cut, and arcs
                   wide onto the statue and the city
    2181 - 3163    the tour of the statue; Arsenal mounts her island, tears her off
                   her pedestal and carries her away
    3164 - 3320    the jib by Castle Clinton as the bow closes (old 2545-3049, x0.72)
    3321 - 4110    landfall, the plough through the Battery, the wreck, the white-out
                   (old 3050-3839, +271)

World units: the city set is placed at -p (-25000, 1000, -500000); 4096 = one turn.
"""
import math

SHORE = -414220.0   # city near edge at -p (-25000, 1000, -500000)
FINAL = -496880.0   # bow lands exactly on w60a_amg, the beached hull
STOP = -457944.0    # wreck alignment: where the ship comes to rest
L = 31000.0         # ship length (stern rides at bow+31k)
V = 193.75          # cruise: 4.5L->3L off the shore across the 4s heli shot
B0 = SHORE + 4.5 * L + V * 960   # bow at the cut, so bow(2390) == SHORE+4.5L

EXT = 170                        # stock frames kept past the first cut's f2000 (bridge pass)
TK0, TK1 = 1446, 2181            # the takeover shot: seize the stock camera at TK0, no
                                 # cut; pan left onto Lady Liberty; the tour cut at TK1
SH = TK1 - 2390                  # net shift for every old-time constant in [2390, 2545)
CUT = 2000 + EXT                 # ship authoring begins here; stock plays untouched below
SOL = (-102100.0, 240.0, -340500.0)   # Lady Liberty putobj anchor (placed by eye, waterline)

# THE ORBIT: helicopter tour of Lady Liberty -- front high looking down at her
# face/island (toward Jersey), sweep the SEAWARD side (Arsenal stays in view), settle
# level BEHIND her: her back + the channel + the incoming ship.
ORB_LEN = 983                    # tour + mount + departure + the linger (cut 3164)
ORB_SWEEP = 959                  # the ring sweep keeps its ORIGINAL pacing (clamped)
MNT0, MNT1, MNTD = 2567, 2800, 2900   # the mount: hit, dismount start, settled
ARRIVE = 2679                    # re-solved for VSLOW=105 so the 2567 hit meets the same water
JIB_SCALE = 0.72                 # the jib runs 1.4x: same worldspace, less time
JIB_END = 3321                   # the jib's last frame is 3320: its tail is cut, not respaced
ORB0, ORB1 = TK1, TK1 + ORB_LEN
# WORLD scale: the putobj is x9 (-s 900), so she stands ~12,000 units, pedestal top
# ~6,300, island radius ~17,000 -- the tour must fly OUTSIDE the island shore
ORB_DIST = 20000.0               # just offshore; ~full-statue framing at screen 1.6
ORB_HI, ORB_LO = 14000.0, 5200.0  # start above the torch looking down -> end below her feet
ORBJ = ORB0 + 40                 # the skip-ahead: ship jumps + re-aims while off-frame
SH2 = ORB1 - 2545                # shift for the jib era start
SH3 = JIB_END - 3050             # everything past the truncated jib
END = 3840 + SH3                 # scene grows with the orbit, shrinks with the jib


def T(c):
    """old-time constant -> new time: [2390, 2545) shifts by SH, the jib era SCALES,
    the rest shifts by SH3."""
    if c >= 3050:
        return c + SH3
    if c >= 2545:
        return min(2545 + SH2 + int(round((c - 2545) * JIB_SCALE)), JIB_END)
    return c + SH if c >= 2390 else c


def O(f):
    """new time -> old time (the physics clock). Exact inverse of T per era."""
    if f >= 3050 + SH3:
        return f - SH3
    if f >= 2545 + SH2:
        return 2545 + (f - (2545 + SH2)) / JIB_SCALE
    return f - SH if f >= 2390 + SH else f

def ship_z(f):
    """id8 (bow reference) world Z per frame, from the cut to the stop. OLD timeline."""
    if f < 2390:
        return B0 - V * (f - 2000)
    if f <= 3140:
        # the chase cut drops 570 frames of timeline but the ship KEEPS its progress
        # (the jump hides in the S2->S3 cut): 4.5L off the shore as the towers shot opens
        return (SHORE + 4.5 * L) - V * (f - 2420)   # landfall (bow == SHORE) on f3140
    if f >= 3310:
        # shot-B replay: INSTANT rewind at the cut, then a steady visible slide back
        # over the same ground, settling ~f3510; at rest for the shot's long tail
        if f >= 3510:
            return STOP
        t = f - 3310
        if t <= 160:
            return STOP + 4150.0 - 25.0 * t
        u = min(1.0, (t - 160) / 40.0)
        return STOP + 150.0 * (1.0 - u) ** 2
    # plow: ONE long deceleration -- v = V*(1-(t/T)^3), never faster than the cruise,
    # braking harder as it digs in, dead stop on the switch frame (3440); scaled so the
    # hull lands exactly on its visual wreck alignment.
    T_ = 300.0
    t = f - 3140
    z = SHORE - V * (t - t**4 / (4.0 * T_**3))
    raw_total = V * T_ * 3.0 / 4.0
    return SHORE + (z - SHORE) * (STOP - SHORE) / (-raw_total)


YAWDIR = (0.25882, 0.96593)   # the final heading (rotY 2048+171 = 195deg)


def ship_xz(f):
    """from the street cut on, the ship comes in ALONG its final heading, pre-rotated,
    converging on the same stop -- the jump hides in the f3050 street cut. OLD timeline."""
    z = ship_z(f)
    if f < 3050:
        return 0.0, z
    sdist = z - STOP
    return YAWDIR[0] * sdist, STOP + YAWDIR[1] * sdist


def ship_xz_vis(f):
    """what's ON SCREEN: the deck close-up holds the ship ~80f further out (no
    landfall on the Raiden shot); it leaps forward under the f3122 street cut, which
    opens just as the bow starts to rise -- the slam lands on the beat. OLD timeline."""
    if 2390 <= f < 2545:
        # tower shot: the ship closes 1.8x faster; the clinton cut resets to normal
        return ship_xz(2390 + (f - 2390) * 1.8)
    if 2545 <= f < 2762:
        # the clinton shot PREVIEWS the post-cut trajectory: position(2761) ==
        # position(3050), so the city cut is seamless and sprays carry over
        return ship_xz(f + 289)
    return ship_xz(f)


def vnoise_g(f, seed, block):
    k, t = divmod(f, block)
    return irnd(k + seed) * (1 - t / block) + irnd(k + 1 + seed) * (t / block)


# OLD-timeline frames (the kernel clock): 3064..3129 is new-frame 3335..3400, the
# window chosen for the plough up the bank. It used to start at 3130 = new-3401, by which point the first
# flash (new-3409) was covering the rise. BEACH_STEPS quantises the climb into stages;
# 0 leaves it continuous, which is what plowing wants - steps read as a hop.
BEACH0, BEACH_LEN, BEACH_STEPS = 3064, 65, 0


def beach_lift(f):
    """the hull rides UP onto the shore, OVERSHOOTS and rocks as the bow slams down,
    then jostles over the rubble in proportion to its grind speed. OLD timeline."""
    if f < BEACH0:
        return 0.0
    u = min(1.0, (f - BEACH0) / float(BEACH_LEN))
    if BEACH_STEPS:
        u = math.ceil(u * BEACH_STEPS) / BEACH_STEPS
    base = 3413.0 * u
    rock = 0.0
    if f >= BEACH0 + BEACH_LEN - 10:
        t = f - (BEACH0 + BEACH_LEN - 10.0)
        rock = 620.0 * math.exp(-t / 55.0) * math.sin(t / 8.0)
    v = ship_z(f) - ship_z(f + 1)
    jostle = 130.0 * min(1.0, max(0.0, v) / 25.0) * vnoise_g(f, 4242, 7)
    # the ascent itself grinds: speed alone leaves the climb glassy, and it bites harder
    # the further the hull is up the bank
    grind = 0.0
    if BEACH0 <= f < BEACH0 + BEACH_LEN:
        r = (f - BEACH0) / float(BEACH_LEN)
        grind = (250.0 * (0.35 + 0.65 * r) * vnoise_g(f, 9317, 5)
                 + 90.0 * vnoise_g(f, 5501, 3))
    return base + rock + jostle + grind


# ---- new-time wrappers: authoring loops run in the NEW timeline; the kernels above
# stay calibrated in the old one
def ship_z_n(f):
    return ship_z(O(f))


def ship_xz_n(f):
    return ship_xz(O(f))


def ship_xz_vis_n(f):
    return ship_xz_vis(O(f))


def beach_lift_n(f):
    return beach_lift(O(f))


# ---- THE GRAZE (TURN0..TK1): bend Arsenal's course toward Lady Liberty so that by the
# cut it reads as heading just right of her. Cubic-in lateral drift (zero slope at the
# start: invisible under the bridge, perspective does the work), hull yaw derived from
# the ACTUAL course velocity so the turn is coherent and no effect switches direction.
TURN0 = 1100                      # earlier start = gentler everywhere
VSTK = 115.38                     # stock cruise speed (measured, units/frame)
GRZ_MARGIN = 7800.0               # side-swipe clearance: bow line misses her by this

# S-curve on the HEADING: turn rate ramps in, peaks mid-curve, settles to ZERO by the
# cut -- the ship ends on a straight diagonal at its final heading (no end lurch).
# The final heading is solved so that straight line passes just right of her in view.
_GCAM = (-18964.0, 12437.0)       # the stock camera (defines "right of her" on screen)
_ZCUT = -86291.0 - VSTK * (TK1 - 2179)
_vdx, _vdz = -102100.0 - _GCAM[0], -340500.0 - _GCAM[1]
_vm = math.hypot(_vdx, _vdz)
GRZ_X = -102100.0 + (-_vdz / _vm) * GRZ_MARGIN   # graze point = her + view-right margin
GRZ_Z = -340500.0 + (_vdx / _vm) * GRZ_MARGIN
GRZ_TH = 0.3
for _ in range(6):                # fixed point: end-heading <-> end-position
    _xend = -0.5 * VSTK * (TK1 - TURN0) * math.tan(GRZ_TH)
    GRZ_TH = math.atan2(_xend - GRZ_X, _ZCUT - GRZ_Z)
GRZ_TH *= 0.5                     # the full solve points too hard on screen: split the difference


VORB = 2.5 * VSTK                 # tour-era ship speed (pre-jump stretch)
VSLOW = 105.0                     # the glide at near-cruise: momentum never dies
DEND = 18000.0                    # bow this short of the graze point at the settle


def graze_yaw(f):
    """course angle in rad, left positive (ry convention verified: +171 == 15deg left)"""
    t = min(1.0, max(0.0, (f - TURN0) / float(TK1 - TURN0)))
    return GRZ_TH * t * t * (3.0 - 2.0 * t)


def graze_d(f):
    # integral of the heading curve: x(t) = -V*span*tan(TH)*(t^3 - t^4/2); past the
    # curve the ship runs STRAIGHT on the settled heading (the orbit watches it come)
    if f > TK1:
        return graze_d(TK1) - VSTK * math.tan(GRZ_TH) * (f - TK1)
    t = min(1.0, max(0.0, (f - TURN0) / float(TK1 - TURN0)))
    return -VSTK * (TK1 - TURN0) * math.tan(GRZ_TH) * (t ** 3 - 0.5 * t ** 4)


def graze_ry(f):
    return graze_yaw(f) / (2.0 * math.pi) * 4096.0


def irnd(f):
    """deterministic [-1,1) per frame -- scncamvib-style shake"""
    h = (f * 1103515245 + 12345) & 0x7FFFFFFF
    return ((h >> 16) & 0xFFFF) / 32767.5 - 1.0

