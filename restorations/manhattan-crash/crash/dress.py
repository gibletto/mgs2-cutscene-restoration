"""The dressing pass: effect packets written straight into the baked cutscene.

The authoring pass (author.py) goes through the studio's scene document; this one
works on the bake's raw packets, frame by frame, because what it adds is sheer
volume -- fires, smoke, dust, debris, water eruptions, fog wisps, depth of field,
slow motion and the controller rumble -- and because some of it thins or retimes
packets the baker emitted.

Every effect here is Konami's own, cloned from cutscenes that use it (the
aftermath scene p080_14_p08 for the smoke columns and fog pall, p045 for fire,
t13a1d for splashes and dust, p032 for debris) and placed in this stage's world.
Positions marked "placed by eye" were read off the studio viewport or the game.

The schedules below are written in the first cut's frame numbers and moved onto
the release's timeline by T() (timeline.py); the acts the first cut never had
(the takeover, the tour, the statue) are scheduled directly in release frames.

Two hard limits shape the density:
  - DM_MAX_EFFECT: a demo can birth at most 512 exec-1 effects in its whole life;
    the 512th overruns the effect list and crashes. Checked at the end (<= 500).
  - NewTs_Spark (0x600e) is not linked into this stage: a spark packet crashes it.
"""
import math
import struct

from mgs2.sdt import DEMO, EFFECT, NOSKIP, Packet, read_packets, read_sdt, write_packets, write_sdt

from .timeline import END, T, irnd

PT_EFFECT = NOSKIP | EFFECT


def eff(pid, eid, exec_type, data):
    body = struct.pack('<Ii', eid, exec_type) + b'\0' * 8 + data
    assert (16 + len(body)) % 16 == 0, len(body)
    return Packet(PT_EFFECT, pid, 0, body)


def dress(src, dst, tracks):
    """Write `src` (a bake) to `dst` with the dressing added. `tracks` are the
    side tracks the authoring pass returned."""
    chunks, tail = read_sdt(src)
    fi = 0
    pid = 0x00B10000
    touched = 0
    fog_tmpl = None
    focus_tmpl = None
    # far-focus DOF: (frame, life, distance). The crane/chase softens NYC beyond 120k;
    # the pre-whip window softens Arsenal beyond 90k and DIES ON THE WHIP FRAME -- the
    # zoom doubles as a rack-focus snap.
    FOCUS_AT = {2000: (389, 90000), 2390: (154, 260000)}   # haze the skyline; towers shot obscures past the hull
    nearfocus_tmpl = None
    # near-focus resolve: fort (136k out) sits inside the near-blur until the aim
    # settles on it -- the actor dies at 2870 and Castle Clinton snaps clear
    NEARFOCUS_AT = {}
    rumble_tmpl = None
    # controller rumble. THE PATTERN TRUTH (decoded from the .var archives):
    # the stage set is SIX envelopes 16575451-456, and the old heavy/mid/low
    # labels were wrong -- 454 is a gentle 52->64 build (weaker than 455!).
    #   451 = 192 impact ringing down (the real heavy)   453 = 128->160 swell-crash
    #   452 = 60->80 ramp                                455 = flat 72 plateau
    #   454 = 52->64 soft build                          456 = 64->32 natural fade
    # d1 semantics (vibrate.c NewPadVibration2EX): +1 = STRETCH the envelope over
    # the (d3,d2) window -- real ramps/tapers, no pulse trains; -1 = play native.
    # Schedule lives AFTER the remap in final screen frames; spans overlap 4f.
    RUMBLE_AT = {}
    _V451, _V452, _V453, _V454, _V455, _V456 = (16575451, 16575452, 16575453,
                                                16575454, 16575455, 16575456)
    _VSTAB, _VPUNCH = 2848731, 9703213   # resident.var: 255/3f stab, 255/20f punch
    # fog wisps: opening crane, dolly cloud-dive, city shots
    # coverage gaps at 2430-2570 (crane pan-up) and 2880-3080 (drift+deck) filled:
    # the wisps now run unbroken from the NYC cut to the fade
    FOG_AT = {2010: 250, 2140: 250, 2270: 160, 2400: 180, 2570: 150, 2600: 150, 2630: 150,
              2660: 150, 2690: 150, 2720: 140, 2750: 130, 2880: 150, 2960: 130,
              3080: 330, 3130: 200,
              3170: 220, 3200: 300, 3230: 200, 3270: 180, 3300: 310, 3400: 210,
              3500: 200, 3600: 250, 3700: 130}
    # ---- THE CATASTROPHE (donors: p045 fire/smoke, t13a1d splash/dust) ----
    # deterministic scatter; heights varied to hedge unknown terrain
    FIRE_AT, SMOKE_AT, SPLASH_AT, DUST_AT, ERUPT_AT = {}, {}, {}, {}, {}
    FOGSET_AT = {}     # NewFogSet_Demo 0x0000: (R,G,B,A, near, far, apply_end, apply_start)
    CSMOKE_AT = {}     # NewCommonSmoke 0x7101: (x,y,z, radius, r,g,b,a, life, dist_type)
    DEBRIS_AT = {}     # NewDebris_Cm_Demo 0x6007: (bound0, bound1, rot_euler, pow, num, objcode, (r,g,b), scale, flags, life)
    _nm = [0xB10000]

    def add(d, f, item):
        d.setdefault(f, []).append(item)

    # TWO WORLDS: before f3311 the INTACT city stands -- effects there must sit on the
    # open waterfront (proven-visible strip). the spots placed by eye are WRECKED terrain,
    # valid only after the swap: they birth at 3315 under the flash and burn to the end.

    # IMPACT (intact world, 3136-3162): strung along the strike shoreline
    # its twin, slightly later, off Arsenal's port side (screen-right in the street shot)
    add(ERUPT_AT, 3142, (-2800.0, 300.0, -415000.0, 200, 2200.0, 24.0))

    # the BIG water explosion from the earlier build, verbatim (same LCG -> same spots):
    # it lands square on the first strike flash
    for k in range(4):
        add(ERUPT_AT, 3126 + k * 3,
            (8500.0 + 3800.0 * irnd(k + 500), 300.0, -412500.0 - 2100.0 * k,
             200, 1700.0 + 500.0 * abs(irnd(k + 510)), 22.0))

    STRIKE = [(11316.0, 1500.0, -414500.0), (9500.0, 1500.0, -417200.0),
              (23624.0, 6880.0, -452403.0), (7400.0, 1400.0, -415600.0)]   # 3rd moved east/deep (placed by eye)
    ERUPT_PTS = [(11316.0, -414500.0), (9500.0, -417200.0),
                 (12600.0, -419500.0), (7400.0, -415600.0)]   # eruptions stay at the waterline
    for k, (x, z) in enumerate(ERUPT_PTS):
        add(ERUPT_AT, 3136 + k * 3, (x, 500.0, z, 190, 1900.0, 24.0))
    # impact plumes: shifted back along the approach vector (+YAWDIR toward the water edge) AND delayed
    # so the beaching hull leaves them in its WAKE. NEAR-to-camera plumes wait 60f (hull is well past
    # them by then), FAR plumes 30f -- staggered so they light up progressively down the wake.
    _wake = (2588.0, 9659.0)   # +YAWDIR(0.25882,0.96593) * 10000, back toward the water behind the bow
    _cam = (-23139.0, -471328.0)  # breach-cam XZ (posX/posZ hold over f3050-3309)
    for k, (x, y, z) in enumerate(STRIKE):
        _d = ((x + _wake[0] - _cam[0]) ** 2 + (z + _wake[1] - _cam[1]) ** 2) ** 0.5
        _delay = 30 if _d < 65000.0 else 60    # nearer to camera fires SOONER; far plumes wait longer
        add(SMOKE_AT, 3138 + k * 4 + _delay, (x + _wake[0] + 700.0 * irnd(k + 60), y, z + _wake[1] + 700.0 * irnd(k + 70),
                                     190, 160.0, 3.2, 1300.0, 4400.0, 108, 96 if k % 2 else 64))
    for k in range(2):
        add(SPLASH_AT, 3139 + k * 6, (10200.0 - 1800.0 * k, 300.0, -413800.0 - 1500.0 * k, 170, 5.0, -16))
    # CASTLE CLINTON BREACH: brick/wall debris flung OUT of the fort TOWARD THE CAMERA on the
    # bow impact (f3140). objcode = w04a1_brk_wall_hahen (0xc8e2ba, registered in the d080p07
    # manifest). rot euler aims the force (mat row-2 = +Z axis) up-and-back toward the breach cam
    # at ~(-23139,16200,-471328); impact box sits on the Castle Clinton wreck (~-13000,2500,-435000).
    # Castle Clinton LOCAL rubble kick: chunks hop UP-and-slightly-out from the fort and fall a short
    # way (~200u), NOT a camera-ward projectile. Modest count. Box sits ON the fort (un-shifted).
    # [PARKED for the NEXT, closer building: the dramatic at-camera burst -- rot (-0.348,-2.869,0),
    #  pow 4000, scale 3, dense stream + big w04b1_brk (0xb9a472) slabs. Reads great up close.]
    # TINY, subtle rubble scatter that stays LOW at the crash site (was lofting into frame like moths).
    # Low pow + mild up-forward tilt = small hop that settles; small scale; few chunks; short life.
    _dbox0, _dbox1 = (-13800.0, 2000.0, -435800.0), (-12200.0, 2600.0, -434200.0)   # low, on the fort
    _drot = (-0.5, 0.0, 0.0)   # slight up + forward (away from cam), NOT straight up
    for _df in (3140, 3144, 3148):
        add(DEBRIS_AT, _df, (_dbox0, _dbox1, _drot, 150.0, 30, 0xc8e2ba, (140, 110, 90), 0.8, 0, 55))
    # breach dust rising with the rubble
    for _sf, _al in ((3140, 96), (3146, 72), (3152, 56)):
        add(SMOKE_AT, _sf, (-13000.0, 2600.0, -435000.0, 260, 110.0, 2.2, 1000.0, 4200.0, _al, 96))
    # (shore-edge fires removed -- they sat on the NYC model's first-hit lip)
    # dust on the waterfront as the hull grinds ashore
    for k in range(3):
        add(DUST_AT, 3160 + k * 20, (9000.0 + 1800.0 * k, 1400.0, -417000.0 - 2600.0 * k, 140))

    # THE SWAP (3315+, wrecked world): every position placed by eye, popping in under
    # the blast flash, living for the settle + overhead
    # LANDFALL cover (placed by eye at -10127, 3282, -432231): sphere water burst + a
    # single light-grey spray plume (softest sprite, fast rise -> reads as kicked-up
    # water/dust, not burning smoke)
    add(ERUPT_AT, 3153, (-10127.0, 3282.0, -432231.0, 210, 2500.0, 26.0))
    add(SMOKE_AT, 3153, (-10127.0, 3282.0, -432231.0, 300, 130.0, 2.6, 900.0, 3800.0, 88, 32))

    # the big established plume for the side shot's opening frame (placed by eye)
    add(SMOKE_AT, 3311, (-13583.0, 8142.0, -490129.0, 526, 140.0, 2.6, 1500.0, 5200.0, 56, 96))

    add(SMOKE_AT, 3313, (-10177.0, 3633.0, -425772.0, 522, 110.0, 2.2, 1000.0, 3800.0, 48, 96))
    add(SMOKE_AT, 3319, (-10177.0, 3933.0, -425772.0, 516, 90.0, 1.8, 700.0, 3000.0, 40, 64))
    add(SMOKE_AT, 3315, (428.0, 3287.0, -435368.0, 520, 100.0, 2.0, 900.0, 3400.0, 48, 96))
    add(SMOKE_AT, 3317, (5270.0, 3764.0, -437368.0, 518, 110.0, 2.2, 900.0, 3400.0, 48, 64))
    _nm[0] += 1
    add(FIRE_AT, 3311, (_nm[0], 5270.0, 3764.0, -437368.0, 524))
    _nm[0] += 1
    add(FIRE_AT, 3315, (_nm[0], -10177.0, 3633.0, -425772.0, 520))
    add(SMOKE_AT, 3321, (6450.0, 14305.0, -445715.0, 514, 130.0, 2.4, 1400.0, 5000.0, 56, 96))
    # reveal-ring fires
    for k in range(4):
        _nm[0] += 1
        side = -1.0 if k % 2 else 1.0
        add(FIRE_AT, 3313 + k * 2,
            (_nm[0], side * (15500.0 + 2800.0 * (k // 2)), 2600.0 + 900.0 * (k % 2),
             -443000.0 - 4600.0 * (k // 2), 522 - k * 2))
    # camera-aware fire CLUSTERS (3 stacked flames each = a real burn on screen):
    # west shore for the street shot, midground for the side view
    CLUSTERS = [
        (3160, -11500.0, 2600.0, -433000.0, 680),   # on already-plowed ground, by the
        (3168, -14000.0, 2400.0, -436000.0, 672),   # castle-clinton smoke -- burning WAKE
        (3316,   4500.0, 2400.0, -441500.0, 519),   # side-view midground
        (3320,   7500.0, 2800.0, -447500.0, 515),
    ]
    for (bf, cx, cy, cz, clife) in CLUSTERS:
        for j, (ox, oy, oz) in enumerate([(0.0, 0.0, 0.0), (450.0, 260.0, 300.0), (-380.0, 130.0, -420.0)]):
            _nm[0] += 1
            add(FIRE_AT, bf + j, (_nm[0], cx + ox, cy + oy, cz + oz, clife - j))

    # late shot-B break-out (placed by eye at -1123, 8393, -443866): the mid-tower
    # fire. Originally one static 610-life fire -- now a TRACKED EMITTER: chained short fires at
    # the same tower fraction (hf 0.42) riding the tilt arc, so the flame follows the lean, with
    # smoke + debris co-spawned at the moving point to cover the clipping. After the wreck swap
    # hides the tower, one long fire burns on at the fallen resting point. (_arc defined below,
    # before the write loop runs.)
    BRK = (-1123.0, 8393.0, -443866.0)
    _BRK_HF = 0.42
    add(ERUPT_AT, 3232, (BRK[0], BRK[1] - 1500.0, BRK[2], 200, 1600.0, 22.0))

    # ---- TOPPLE RIDE-ALONG: fire/smoke/debris pinned to the falling tower ---------------------
    # ANCHOR = the BRK break-out fire below (placed by eye) (spawn -1123,8393,-443866; reads ~-855,
    # 8822,-442831 in flight): it sits ~44% up the tower, which pins the effect-frame tower as
    # base (-855, 1100, -442831), height 17,750 (the effect frame IS x10 -- the old low scouted
    # fires were on the FORT, not tower fractions). Mirrors the OBJ_TURN ramp: ease-in-quad tilt
    # to TILT/4096 of a turn over T0..T1, top swinging -Z, sinking SINK.
    _SP = (-855.0, 1100.0, -442831.0)
    _T0, _T1, _TILT, _SINK, _H = 3146, 3305, 620, 1400.0, 17750.0
    # tower 2's effect anchor = its putobj base + the b1-measured putobj->effect translation
    # (50570, 5300, -38226); both frames are x10 so one correspondence closes the mapping.
    _SP2 = (2775.0, 1100.0, -439801.0)
    _T0_2, _T1_2, _H2 = 3100, 3257, 20000.0

    def _arcT(f, hf, SP, T0, T1, H):
        t = min(1.0, max(0.0, (f - T0) / float(T1 - T0)))
        e = t * t
        th = (_TILT * e) * (2.0 * math.pi / 4096.0)
        h = H * hf
        return (SP[0],
                SP[1] - _SINK * e + h * math.cos(th),
                SP[2] - h * math.sin(th))           # top swings -Z, same sign as the TURN ramp

    def _arc(f, hf):
        return _arcT(f, hf, _SP, _T0, _T1, _H)

    # ---- tower 2 dressing (leads by a beat: the bow hits it first) ----
    for _f in range(3104, 3253, 12):
        for _hf in (0.35, 0.8):
            _nm[0] += 1
            x, y, z = _arcT(_f, _hf, _SP2, _T0_2, _T1_2, _H2)
            x += 160.0 * irnd(_f + 21)
            y += 220.0 * irnd(_f + 29)
            z += 160.0 * irnd(_f + 37)
            add(FIRE_AT, _f, (_nm[0], x, y, z, 40))
    for _f in range(3108, 3253, 20):
        x, y, z = _arcT(_f, 0.6, _SP2, _T0_2, _T1_2, _H2)
        add(SMOKE_AT, _f, (x, y + 400.0, z, 180, 110.0, 2.2, 700.0, 3400.0, 88, 32))
    for _f in range(3102, 3253, 14):
        add(DUST_AT, _f, (_SP2[0], _SP2[1] + 300.0, _SP2[2], 60))
    # (tower 2's debris kit REMOVED pending the dressing rework: it rode the translation-derived
    #  anchor, which is off -- its pre-flash kick-off burst read as debris from nowhere)

    # fires climbing the facade, re-spawned short so they ride the lean
    for _f in range(3150, 3301, 12):
        for _hf in (0.35, 0.8):
            _nm[0] += 1
            x, y, z = _arc(_f, _hf)
            add(FIRE_AT, _f, (_nm[0], x, y, z, 58))
    # smoke: soft columns off the burn points + grey billows shedding off the top
    for _f in range(3152, 3301, 20):
        x, y, z = _arc(_f, 0.6)
        add(SMOKE_AT, _f, (x, y + 400.0, z, 90, 110.0, 2.2, 700.0, 3400.0, 88, 32))
    for _f in range(3170, 3301, 26):
        x, y, z = _arc(_f, 0.95)
        add(CSMOKE_AT, _f, (x, y, z, 900.0, 70, 68, 66, 96, 80, 1))
    # ground grind: dust boiling at the base the whole way down
    for _f in range(3148, 3301, 14):
        add(DUST_AT, _f, (_SP[0], _SP[1] + 300.0, _SP[2], 60))
    # the tracked BRK emitter: chained fires at hf 0.42 riding the lean, smoke + debris at the
    # same moving point to cover the clipping; then one long burn at the resting point after
    # the wreck swap hides the tower.
    # flame: dense overlapped short spawns + jitter = a smear along the arc, not stamps
    # (life 34 / spacing 6 -> ~5 concurrent flames strung down the recent path)
    for _f in range(3230, 3305, 6):
        _nm[0] += 1
        x, y, z = _arc(_f, _BRK_HF)
        x += 160.0 * irnd(_f)
        y += 220.0 * irnd(_f + 7)
        z += 160.0 * irnd(_f + 13)
        add(FIRE_AT, _f, (_nm[0], x, y, z, 34))
    # trail smoke: long-lived, left hanging where the flame has been (the smear's wake)
    for _f in range(3230, 3305, 12):
        x, y, z = _arc(_f, _BRK_HF)
        add(SMOKE_AT, _f + 3, (x, y + 300.0, z, 200, 120.0, 2.4, 700.0, 3600.0, 84, 32))
    for _f in range(3238, 3301, 22):
        x, y, z = _arc(_f, _BRK_HF)
        add(DEBRIS_AT, _f, ((x - 2200.0, y - 1200.0, z - 500.0), (x + 2200.0, y + 500.0, z + 500.0),
                            (-0.348, -2.869, 0.0), 500.0, 20, 0xc8e2ba, (140, 110, 90), 1.1, 0, 55))
    _nm[0] += 1
    xr, yr, zr = _arc(3305, _BRK_HF)
    # the tower burn RIDES building c: burns at the fall spot
    # until the wreck swap, then re-lights on the DISPLACED chunk and slides
    # home with it -- chained births stepping c's own smoothstep
    add(FIRE_AT, 3306, (_nm[0], xr, yr, zr, 276))       # to the swap
    _C0, _C1 = 3672, 3781                               # c's ride, screen time

    def _cpos(f):
        u = max(0.0, min(1.0, (f - _C0) / float(_C1 - _C0)))
        e = u * u * (3.0 - 2.0 * u)
        return xr + 641.0 * (1.0 - e), zr + 595.6 * (1.0 - e)

    for _ff in list(range(3584, 3776, 24)) + [3776]:
        _fx, _fz = _cpos(_ff)
        add(FIRE_AT, _ff, (_nm[0], _fx, yr, _fz, 530 if _ff == 3776 else 30))
    add(SMOKE_AT, 3310, (xr + 641.0, yr + 300.0, zr + 595.6, 520, 120.0, 2.4, 800.0, 3800.0, 84, 32))
    add(SMOKE_AT, 3700, (_cpos(3700)[0], yr + 300.0, _cpos(3700)[1], 90, 120.0, 2.4, 800.0, 3800.0, 84, 32))
    # DEBRIS: kick-off at the base, shedding stream mid-fall, then the parked "dramatic
    # at-camera burst" (pow 4000, scale 3, big w04b1_brk slabs) where the top slams down.
    # All bursts aim (-0.348,-2.869,0): the tilt direction (-Z) leaning toward the camera.
    _dr = (-0.348, -2.869, 0.0)
    add(DEBRIS_AT, 3148, ((_SP[0] - 4000.0, _SP[1], _SP[2] - 800.0),
                          (_SP[0] + 4000.0, _SP[1] + 2500.0, _SP[2] + 800.0),
                          _dr, 600.0, 45, 0xc8e2ba, (140, 110, 90), 1.2, 0, 70))
    for _f in range(3180, 3281, 24):
        x, y, z = _arc(_f, 0.75)
        add(DEBRIS_AT, _f, ((x - 2500.0, y - 1500.0, z - 600.0), (x + 2500.0, y + 500.0, z + 600.0),
                            _dr, 350.0, 22, 0xc8e2ba, (140, 110, 90), 1.0, 0, 60))
    xs, ys, zs = _arc(3300, 0.9)
    add(DEBRIS_AT, 3296, ((xs - 5000.0, ys - 2000.0, zs - 1500.0), (xs + 5000.0, ys + 1000.0, zs + 1500.0),
                          (-0.348, -2.869, 0.0), 4000.0, 60, 0xb9a472, (150, 120, 95), 3.0, 0, 110))
    add(DUST_AT, 3298, (xs, ys - 1000.0, zs, 90))
    add(DUST_AT, 3302, (xs + 1500.0, ys - 1200.0, zs + 900.0, 84))

    # KONAMI AFTERMATH KIT (cloned from p080_14_p08, the scene that follows ours):
    # fog pall + mega smoke columns + rubble billows, from the side-view cut on
    add(FOGSET_AT, 4, (35, 40, 41, 128, -350000.0, 600000.0, 8, 4))            # ONE constant haze, whole first half (20% denser)
    add(FOGSET_AT, 3311, (120, 130, 116, 128, -30000.0, 200000.0, 3315, 3311))  # pall at half strength
    add(FOGSET_AT, 3508, (120, 130, 116, 128, -50000.0, 500000.0, 3512, 3508))  # eased way out for the overhead
    # mega columns (s0 20k -> s1 30k, alpha 4, sprite 160): rising off the two freshly
    # smashed towers -- building 3's ruin and building 6's ruin (putobj sites mapped to the
    # effect frame; bases at street level, their deep footings translate subterranean)
    add(SMOKE_AT, 3312, (-5295.0, 1300.0, -447771.0, 523, 20.0, 1.0, 20000.0, 30000.0, 4, 160))
    add(SMOKE_AT, 3316, (-4230.0, 1500.0, -458621.0, 519, 25.0, 1.0, 10000.0, 20000.0, 8, 160))
    add(SMOKE_AT, 3314, (-16954.0, 2256.0, -450868.0, 521, 22.0, 1.0, 5000.0, 9000.0, 10, 160))   # b5: smaller, same style (placed by eye)
    # and a denser visible column rising right off b5's grave, born as it's crushed so it's
    # already climbing through the transition (medium-column family: tighter, more present)
    add(SMOKE_AT, 3260, (-16954.0, 2400.0, -450868.0, 575, 60.0, 1.2, 700.0, 7000.0, 26, 80))
    # medium columns (600 -> 5000, alpha 16, sprite 80)
    add(SMOKE_AT, 3318, (8000.0, 2200.0, -452000.0, 517, 50.0, 1.0, 600.0, 5000.0, 16, 80))
    add(SMOKE_AT, 3322, (2000.0, 2600.0, -458000.0, 513, 50.0, 1.0, 600.0, 5000.0, 16, 80))
    # LAYERING QUADS (p14's trick): giant slow sprite-160 smokes hanging MID-AIR over
    # the crater -- the overhead looks THROUGH them, so fires sit "into" the haze
    add(SMOKE_AT, 3324, (5000.0, 12000.0, -455000.0, 511, 20.0, 1.0, 18000.0, 28000.0, 4, 160))
    add(SMOKE_AT, 3326, (-8000.0, 9000.0, -465000.0, 509, 15.0, 1.0, 15000.0, 25000.0, 4, 160))
    add(SMOKE_AT, 3328, (12000.0, 15000.0, -472000.0, 507, 25.0, 1.0, 20000.0, 30000.0, 4, 160))
    add(SMOKE_AT, 3330, (-2000.0, 10000.0, -448000.0, 505, 15.0, 1.0, 15000.0, 25000.0, 4, 160))
    add(SMOKE_AT, 3332, (-25000.0, 11000.0, -430000.0, 503, 15.0, 1.0, 18000.0, 35000.0, 4, 160))
    add(SMOKE_AT, 3334, (30000.0, 13000.0, -455000.0, 501, 20.0, 1.0, 18000.0, 35000.0, 4, 160))
    add(SMOKE_AT, 3336, (-20000.0, 12000.0, -490000.0, 499, 15.0, 1.0, 18000.0, 35000.0, 4, 160))
    add(SMOKE_AT, 3338, (20000.0, 10000.0, -500000.0, 497, 20.0, 1.0, 18000.0, 35000.0, 4, 160))

    # TREE JOSTLE dust (clinton shot, 2740-2762 on screen): sandy puffs kicked out under the
    # shoved trees -- the little tremor before CHAOS. Position = tree putobj through the frame
    # translation; if it lands off, place it by eye.
    add(DUST_AT, 2742, (5640.0, 600.0, -421346.0, 55))
    add(DUST_AT, 2750, (6100.0, 650.0, -420900.0, 50))
    add(DUST_AT, 2757, (5200.0, 600.0, -421800.0, 45))
    add(CSMOKE_AT, 2746, (5640.0, 800.0, -421346.0, 350.0, 96, 88, 70, 30, 60, 2))   # sandy billow

    # THE LAND BOW-WAVE: dusty clouds forming across the bow as it plows the embankment
    # zone -- the breakwater effect, in dirt. Line anchored on a point placed by eye
    # (-6988,3293,-425657), marching with the bow, puffs thrown to alternating sides.
    _PW=(-6988.0,-425657.0); _PDIR=(-0.2588,-0.9659); _PPERP=(0.9659,-0.2588)
    add(DUST_AT, 3085, (-6988.0, 3293.0, -425657.0, 70))            # the big scouted one
    add(CSMOKE_AT, 3087, (-6988.0, 3400.0, -425657.0, 500.0, 96, 88, 70, 36, 80, 2))
    _LEAD = 1500.0   # the wave breaks just AHEAD of the prow and gets overrun
    for _f in range(3072, 3173, 8):
        _s = (_f - 3089) * 185.0 + _LEAD
        _lat = (1800.0 + 3400.0 * abs(irnd(_f))) * (1 if (_f // 8) % 2 else -1)
        _px_ = _PW[0] + _PDIR[0] * _s + _PPERP[0] * _lat
        _pz_ = _PW[1] + _PDIR[1] * _s + _PPERP[1] * _lat
        add(DUST_AT, _f, (_px_, 3000.0 + 500.0 * irnd(_f + 3), _pz_, 55))
        if _f % 24 == 6:
            add(CSMOKE_AT, _f + 2, (_px_, 3300.0, _pz_, 420.0, 96, 88, 70, 28, 70, 2))

    # building 5: fort-style LOCAL rubble kick (same recipe as the Castle Clinton one --
    # low pow, modest count, chunks hop up-and-out and settle) as it's crushed
    _b5 = (-16954.0, 2256.0, -450868.0)   # placed by eye
    for _df in (3250, 3264, 3278):
        add(DEBRIS_AT, _df, ((_b5[0] - 800.0, _b5[1], _b5[2] - 400.0),
                             (_b5[0] + 800.0, _b5[1] + 600.0, _b5[2] + 400.0),
                             (-0.5, 0.0, 0.0), 150.0, 30, 0xc8e2ba, (140, 110, 90), 0.8, 0, 55))

    # BREACH-SHOT LAYERING QUADS: the p14 mid-air giants, brought forward into this shot to
    # haze the hull's intersections with towers 3 and 6 as they fall (born on the falls,
    # living to the swap)
    add(SMOKE_AT, 3160, (-9500.0, 3800.0, -450800.0, 150, 20.0, 1.0, 12000.0, 20000.0, 6, 160))
    add(SMOKE_AT, 3175, (-6607.0, 9515.0, -460154.0, 135, 15.0, 1.0, 15000.0, 24000.0, 5, 160))   # placed by eye
    add(SMOKE_AT, 3195, (-6800.0, 4500.0, -451500.0, 115, 18.0, 1.0, 10000.0, 16000.0, 8, 160))
    add(SMOKE_AT, 3215, (-3000.0, 6500.0, -455000.0, 95, 15.0, 1.0, 12000.0, 20000.0, 6, 160))

    # building 5's demise (the little one, flattened 3245-3298): dirty small smoke boiling
    # out of it as it goes under, one lingering billow after the swap
    add(CSMOKE_AT, 3252, (-16954.0, 2206.0, -450868.0, 450.0, 70, 66, 50, 40, 90, 2))
    add(CSMOKE_AT, 3268, (-16589.0, 2356.0, -450547.0, 550.0, 64, 60, 46, 32, 80, 2))
    add(CSMOKE_AT, 3284, (-17239.0, 2256.0, -451147.0, 500.0, 60, 58, 44, 28, 70, 2))
    add(CSMOKE_AT, 3315, (-16954.0, 2456.0, -450868.0, 700.0, 64, 64, 48, 20, 500, 2))

    # rubble billows along the furrow (dark khaki, distortion type 2)
    add(CSMOKE_AT, 3315, (1500.0, 2400.0, -447000.0, 800.0, 64, 64, 48, 16, 520, 2))
    add(CSMOKE_AT, 3319, (7000.0, 2600.0, -453000.0, 600.0, 64, 64, 48, 32, 516, 2))
    add(CSMOKE_AT, 3323, (-3500.0, 2200.0, -449500.0, 1000.0, 64, 64, 48, 8, 512, 2))

    # FINALE rooftops (placed by eye, straddling the aerial cut)
    for k, (x, y, z) in enumerate([(29237.0, 9332.0, -486399.0), (-5056.0, 9453.0, -500529.0)]):
        _nm[0] += 1
        add(FIRE_AT, 3323 + k * 2, (_nm[0], x, y, z, 512 - k * 2))
        add(SMOKE_AT, 3327 + k * 2, (x, y + 400.0, z, 508 - k * 2, 100.0, 2.0, 800.0, 3200.0, 44, 96 if k else 64))

    # ---- RETIME all schedules to the recut ----
    def _remap(d):
        out = {}
        for k, v in d.items():
            if k < 2545:
                continue
            out[T(k)] = v
        return out
    FOG_AT = _remap(FOG_AT); FOCUS_AT = _remap(FOCUS_AT); NEARFOCUS_AT = _remap(NEARFOCUS_AT)
    FIRE_AT = _remap(FIRE_AT); SMOKE_AT = _remap(SMOKE_AT); SPLASH_AT = _remap(SPLASH_AT)
    DUST_AT = _remap(DUST_AT); ERUPT_AT = _remap(ERUPT_AT);
    FOGSET_AT = _remap(FOGSET_AT); CSMOKE_AT = _remap(CSMOKE_AT); DEBRIS_AT = _remap(DEBRIS_AT)
    RUMBLE_AT = {T(k): (T(v[0]) if v[0] >= 3050 else v[0], v[1])
                 for k, v in RUMBLE_AT.items() if k >= 3050}

    # the opening acts, regenerated:
    for _wf in range(1450, 3150, 130):
        if 2600 <= _wf <= 2900:
            continue                          # the carnage haze owns this window
        FOG_AT.setdefault(_wf, 250)           # wisps: the pan + the whole tour
    # the DISMOUNT: city-size swirls building from 2760 to a THICK camera cover
    # right at 2815 -- the river-crossing world jump hides inside the mass
    FOG_AT[2792] = 160
    FOG_AT[2806] = 160
    FOG_AT[2840] = 280

    # THE BEACHING (2540-2910): dirt torn up at the bow where the wake used to
    # break -- Konami-wake style, overlapping bursts marching with the hull,
    # alternating bow shoulders. Track exported by author_teaser (world pos +
    # motion dir, so the 34f delay and the 2816 world-jump come along free).
    _bt = tracks['bow_track']
    _bk = 0
    for _fs, (_bx, _bz, _bdx, _bdz) in sorted((int(k), v) for k, v in _bt.items()):
        if not (2540 <= _fs <= 2910):
            continue
        _bk += 1
        _px, _pz = _bx + _bdx * 26000.0, _bz + _bdz * 26000.0   # the bulbous bow
        _nx, _nz = -_bdz, _bdx
        _cap = (2814 - _fs) if _fs < 2816 else 999   # nothing outlives the jump
        # one dust shoulder per step, alternating: dense enough to read as a
        # churned trail, thin enough not to stack white
        _side = -1.0 if (_bk & 1) else 1.0
        _sp = _side * (1400.0 + 900.0 * abs(irnd(_bk + 900)))
        _dxw, _dzw = _px + _nx * _sp, _pz + _nz * _sp
        # the dust runs 40 frames AHEAD of the hull: same spatial trail (positions
        # from the unshifted track), earlier start -- chosen on screen.
        # EVERY 4TH FRAME, longer-lived: DM_MAX_EFFECT is 512 LIFETIME BIRTHS for
        # the whole demo (crash-proven at f3616) -- density comes from overlap
        _ef = _fs - 40
        _dxe = _dxw - 65000.0 if (_fs >= 2816 and _ef < 2816) else _dxw
        _ecap = (2814 - _ef) if _ef < 2816 else 999
        _dlife = min(40 + int(12 * abs(irnd(_bk + 910))), _ecap)
        # statue-side (_side<0) line cut: it read as a second wave born from nothing
        if _dlife >= 6 and _bk % 5 in (0, 2) and _side > 0:
            add(DUST_AT, _ef, (_dxe, 700.0, _dzw, _dlife))
        # tiny debris clouds CARVED off either side of the bow (the proven
        # hahen chunks, earth-tinted, small and tight)
        if _bk % 10 == 0:
            _ds = -1.0 if (_bk // 10) & 1 else 1.0
            _dbx, _dbz = _px + _nx * _ds * 1600.0, _pz + _nz * _ds * 1600.0
            _dlf = min(36, _cap)
            if _dlf >= 10:
                add(DEBRIS_AT, _fs, ((_dbx - 500.0, 300.0, _dbz - 500.0),
                                     (_dbx + 500.0, 900.0, _dbz + 500.0),
                                     (-0.5, 0.0, 0.0), 260.0, 7, 0xc8e2ba,
                                     (115, 98, 74), 0.3, 0, _dlf))
                # the carve throws its own small smoke (sparks would crash: 0x600e is
                # not linked in this stage)
                add(CSMOKE_AT, _fs + 2, (_dbx, 800.0, _dbz, 700.0,
                                         96, 80, 60, 56, min(40, _cap), 0))
        # basic smoke rolling up behind the carve
        if _bk % 8 == 0:
            _clife = min(60 + int(20 * abs(irnd(_bk + 920))), _cap)
            if _clife >= 10:
                add(CSMOKE_AT, _fs, (_px + _nx * _sp * 0.5, 1100.0, _pz + _nz * _sp * 0.5,
                                     1000.0 + 500.0 * abs(irnd(_bk + 930)),
                                     96, 80, 60, 64, _clife, 0))

    # THE BREAK POINT: sparks lead the tear debris by 20 frames -- the strain
    # sputtering before the metal lets go. Pulled toward the camera so the
    # statue doesn't occlude them, and bigger than the beach grind.
    # the split's smoke: the soft WHITE PUFFS (the chimney-smoke swap looked
    # comic), IN the tear, breathing with the debris at 2681/2685
    _TB2 = (-101739.0, 7181.0, -341685.0)
    add(CSMOKE_AT, 2679, (_TB2[0], _TB2[1], _TB2[2], 580.0, 66, 63, 58, 46, 76, 0))
    add(CSMOKE_AT, 2684, (_TB2[0] + 300.0, _TB2[1] - 250.0, _TB2[2], 660.0, 62, 60, 55, 42, 86, 0))
    # a tiny white breath trailing the torn leg on the plinth as it falls
    # (positions exported by the author from the stump's own path)
    _stk = tracks['stump_track']
    for _sf2, (_sx2, _sy2, _sz2) in sorted((int(k), v) for k, v in _stk.items()):
        add(CSMOKE_AT, _sf2, (_sx2, _sy2 + 250.0, _sz2, 210.0, 66, 63, 58, 26, 44, 0))

    # BLACK PLUMES: the big aerial-coverage smoke FOLLOWS the pedestal's drag --
    # each plume births where the shaft actually is at that frame (the author's
    # carry curve), so the carnage trails it all the way out
    _PV = (-0.5101, -0.0840, -0.8560)
    for _pf in (2678, 2698, 2718, 2740, 2764, 2790, 2818):
        _pu = min(1.0, (_pf - 2674) / 280.0)
        _psl = 10190.0 * (1.0 - (1.0 - _pu) ** 2)
        add(SMOKE_AT, _pf, (-102100.0 + _PV[0] * _psl + 600.0 * irnd(_pf),
                            700.0, -340500.0 + _PV[2] * _psl + 600.0 * irnd(_pf + 7),
                            360, 100.0, 2.0, 800.0, 3200.0, 44, 96))
    # NO carnage fog veil: the stage paints the SKY with the fog colour, and the
    # night sky is darker than ANY veil -- every version brightened it (the
    # f2656 dimmer switch). The smoke masses alone carry the carnage mood.
    # DAWN LADDER: lighting progresses ONLY at temporal cuts. Step 1 fires AT
    # the Castle Clinton cut (instant, no ramp -- a cut hides a step); step 2
    # is the existing wreck pall at 3311 (120,130,116), brighter still.
    add(FOGSET_AT, 3164, (55, 58, 58, 128, -120000.0, 420000.0, 3164, 3164))
    # ZONE HAZE (placed by eye): smoke already hanging at the cleave, building
    # down the vector from the wound toward the shore, bigger as it goes
    _ZA = (-104528.0, 2730.0, -340645.0)
    _ZB = (-108383.0, 300.0, -351705.0)
    for _zi, _zf in enumerate((2620, 2650, 2685, 2720)):
        _zu = (_zi + 1) / 4.0
        _zx = _ZA[0] + (_ZB[0] - _ZA[0]) * _zu + 500.0 * irnd(_zi + 990)
        _zy = _ZA[1] + (_ZB[1] - _ZA[1]) * _zu + 300.0
        _zz = _ZA[2] + (_ZB[2] - _ZA[2]) * _zu + 500.0 * irnd(_zi + 995)
        add(CSMOKE_AT, _zf, (_zx, _zy, _zz, 1200.0 + 500.0 * _zu,
                             54, 53, 51, 46, 140 + int(60 * _zu), 0))

    # THE DAWN: NewLensFlare_Demo chained firings, one live at a time, the sun
    # climbing out of the harbor NE of the statue (behind the Brooklyn Bridge).
    # A hint at the takeover; full flare by the ride to Manhattan. Ends clean
    # before the Castle Clinton cut.
    # (the NewDemoSun/lens-flare dawn is GONE: below-horizon at most camera
    # angles, and taming it needed scene-specific ASI hacks)

    # (the golden-light/warm-fog ride pass is GONE: the stage's own sunrise
    # sky carries the colour)
    # BUT the stock stream only drives the key light to f2180; the state then
    # freezes and the river-jump shifts what the hull picks up locally. HOLD
    # the stock parallel (the heli-shot light) explicitly to the CC cut --
    # exec-0 state sets, zero births.
    # NO light overrides: the "wipe darkening" was never the sun -- it was the
    # bright carnage veil (above) dropping at 2858. Lighting stays stock.
    FOCUS_AT[1446] = (735, 90000)             # skyline haze: ON at the bridge->chase cut, dies at the heli cut
    FOCUS_AT[3164] = (156, 120000)            # CC: soften NYC beyond the fort
    # the Verrazzano zoom: the bridge's bars/cables blur close to the lens while the
    # push holds HER sharp on the horizon
    NEARFOCUS_AT[1870] = (311, 50000)
    # the linger: DOF in front of the deep-set Manhattan -- the skyline goes soft
    # behind the departing ship
    FOCUS_AT[2956] = (208, 150000)
    # ---- THE RUMBLE SCORE (a beat sheet, in screen frames) ----
    # Every shape is one of Konami's own envelopes, so the game's vibration archives are
    # never touched. A pattern shorter than its span is covered by re-arming, not by
    # authoring a new one. Act 1 builds -> plateaus -> fades; the finale is punch +
    # ring-down at each flash. ACT 2 (2465-3009) is signed off: do not retune it.
    _VBUILD, _VPLAT, _VFADE1 = _V454, _V455, _V456
    _VRING, _VRING2, _VFFADE = _V451, _V451, _V456
    RUMBLE_AT[874] = (1122, _VBUILD, -1)      # bridge pass: 52->64 build (248f native)
    RUMBLE_AT[1118] = (1166, _VPLAT, -1)      # ...held to the cruise plateau
    # cruise plateau 72, re-armed every ~45f: something around the 1446 handover
    # kills live vib actors (mechanism unfound -- no stop-all exists in vibrate.c)
    # and one re-arm only shrank the gap, so overlapping births outflank it
    for _rr in (1162, 1207, 1252, 1297, 1342, 1387, 1432, 1447, 1477, 1522, 1552):
        RUMBLE_AT[_rr] = (min(_rr + 66, 1584), _VPLAT, -1)
    RUMBLE_AT[1580] = (1855, _VFADE1, -1)     # dying: real 72->2 fade, out by 1855
    # 1855-2465 silent. act 2: ramp into the slam, simmer to a grind
    RUMBLE_AT[2465] = (2536, _V454, 1)        # ramp from nothing: 52->64
    RUMBLE_AT[2532] = (2591, _V452, 1)        # 60->80 rising into the hit
    RUMBLE_AT[2591] = (2614, _VPUNCH, -1)     # THE HARD HIT: 255 punch, native decay
    RUMBLE_AT[2592] = (2640, _V453, 1)        # swell-crash body under it (stage-set fallback)
    RUMBLE_AT[2636] = (2900, _V455, 1)        # simmer down to the 72 grind
    RUMBLE_AT[2681] = (2685, 11464525, -1)    # the tear: slightly-strong kick as the shin debris flies
    RUMBLE_AT[2900] = (3009, _V456, 1)        # taper: 64->32 envelope, out by 3009
    RUMBLE_AT[2958] = (2962, _VSTAB, -1)      # her final kick: tiny 255/3f stab
    RUMBLE_AT[2959] = (2962, _V451, 1)        # (192 blip under it as fallback)
    # 3009-3164 silent. act 3: CC build -> 3309 hit swelling into flash #1, then
    # the statue recipe at each flash: 255 punch + hard ring-down to subtle
    RUMBLE_AT[3164] = (3240, _V454, 1)        # CC shot: ramp from nothing
    RUMBLE_AT[3236] = (3294, _V452, 1)        # 60->80 rising
    RUMBLE_AT[3294] = (3317, _VPUNCH, -1)     # the CC hit: the statue-hit stab, 15f earlier
    RUMBLE_AT[3313] = (3396, _V455, 1)        # shake held under it, through to flash #1
    RUMBLE_AT[3396] = (3418, _VPUNCH, -1)     # THE BIG HIT: building 1 + the flash
    RUMBLE_AT[3397] = (3475, _VRING, -1)      # 192->48 ring-down, hard then subtle
    RUMBLE_AT[3471] = (3491, _V455, 1)        # grind bridge
    RUMBLE_AT[3491] = (3513, _VPUNCH, -1)     # flash #2 punch
    RUMBLE_AT[3492] = (3560, _VRING2, -1)     # its ring-down
    RUMBLE_AT[3556] = (3581, _V455, 1)        # shoulder into the side cut
    RUMBLE_AT[3577] = (3981, _VFFADE, -1)     # one true fade to zero, DEAD at the whiteout

    # HER SPLASH: the sea takes her -- the big explosive water fires at the impact
    # mark the bake prints (F_OFF + the gravity frames), and the cut takes it
    # THE TEAR BURST (2681): shards blast out of her shins in ARSENAL'S travel
    # direction (measured ry -2.811 at that frame) -- the hull rips them along
    _TB = (-101739.0, 7181.0, -341685.0)
    add(DEBRIS_AT, 2681, ((_TB[0] - 700.0, _TB[1] - 500.0, _TB[2] - 700.0),
                          (_TB[0] + 700.0, _TB[1] + 500.0, _TB[2] + 700.0),
                          (-0.35, -2.811, 0.0), 550.0, 30, 0xc8e2ba, (120, 148, 134), 0.7, 0, 65))
    add(DEBRIS_AT, 2685, ((_TB[0] - 500.0, _TB[1] - 400.0, _TB[2] - 500.0),
                          (_TB[0] + 500.0, _TB[1] + 400.0, _TB[2] + 500.0),
                          (-0.2, -2.811, 0.0), 320.0, 18, 0xc8e2ba, (120, 148, 134), 0.55, 0, 55))

    _IMP = (-47491.0, 300.0, -359316.0)
    add(ERUPT_AT, 2984, (_IMP[0], _IMP[1], _IMP[2], 200, 2600.0, 26.0))
    add(SPLASH_AT, 2986, (_IMP[0], _IMP[1], _IMP[2] - 2500.0, 170, 5.0, -16))
    add(ERUPT_AT, 2989, (_IMP[0] + 2200.0, _IMP[1], _IMP[2] + 1600.0, 200, 1800.0, 22.0))
    add(ERUPT_AT, 2993, (_IMP[0] - 2400.0, _IMP[1], _IMP[2] - 1900.0, 200, 1500.0, 20.0))
    for c in chunks:
        if c.ctype != DEMO:
            continue
        if fi == 380 and fog_tmpl is None:
            pkts, _ = read_packets(c.payload)
            for q in pkts:
                if q.kind == 6 and len(q.body) >= 20 and struct.unpack_from('<I', q.body, 0)[0] == 0x01000019:
                    fog_tmpl = bytes(q.body)
        if fi == 874 and rumble_tmpl is None:
            pkts, _ = read_packets(c.payload)
            for q in pkts:
                if q.kind == 6 and len(q.body) >= 44 and struct.unpack_from('<I', q.body, 0)[0] == 0x01009000:
                    rumble_tmpl = bytes(q.body)
        if fi >= 2186:
            # THE MEASURED KONAMI SPRAY RHYTHM (from the original's own packets):
            #   bow deep  0x56B753: every 2f, scale 1  (the steady break)
            #   bow ctr   0xD4525C: every 6f, scale 2
            #   flank L   0xD45265: every 10f, scale 2
            #   flank R   0xD4526B: every 10f (phase +8), scale 2
            # ~0.87 msgs/frame total. The baker emits every frame; thin to this.
            KEEP_ON = {0x56B753: (2, 0), 0xD4525C: (6, 0), 0xD45265: (10, 0), 0xD4526B: (10, 8)}
            pkts, ptail = read_packets(c.payload)
            seen = set()
            keep = []
            changed = False
            for q in pkts:
                if q.kind == 6 and len(q.body) >= 60 and struct.unpack_from('<I', q.body, 0)[0] == 0x01008000:
                    dd = struct.unpack_from('<16i', q.body, 0)
                    if dd[13] == 13:
                        nm = dd[4]
                        per = KEEP_ON.get(nm)
                        if per is None or (fi % per[0]) != per[1] or nm in seen:
                            changed = True
                            continue
                        seen.add(nm)
                keep.append(q)
            if changed:
                c.payload = write_packets(keep, ptail)
                touched += 1
        if fi == 2565:
            # the ABDUCTION under the same dream-lens as the city: slow-mo crossfade
            # from the mount slam all the way to the Castle Clinton cut
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            d = struct.pack('<4i', 599, 0, 0, 0)
            d += struct.pack('<4i', 3164, 2565, 60, 2) + bytes(16)
            pkts.append(eff(pid, 0x01000013, 1, d)); pid += 16
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        if fi == 3451:   # T(3180): the lift-ramp completes here in the recut
            # NewCrossFadeEffectCustom (0x13): full speed while the bow rises; SLOW
            # MOTION from the moment Arsenal is at full mounting height (lift ramp
            # completes f3180) through the grind, crossfading the side-view cut
            # (3310) before the effect ends. Template from p080_11_p06:
            # data = [life,0,0,0, frame_end, frame_start, rate=60, skip=2, 0...]
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            d = struct.pack('<4i', 140, 0, 0, 0)
            d += struct.pack('<4i', 3591, 3451, 60, 2) + bytes(16)
            pkts.append(eff(pid, 0x01000013, 1, d)); pid += 16
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        if fi in FIRE_AT or fi in SMOKE_AT or fi in SPLASH_AT or fi in DUST_AT or fi in ERUPT_AT or fi in FOGSET_AT or fi in CSMOKE_AT or fi in DEBRIS_AT:
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            for (nm, x, y, z, life) in FIRE_AT.get(fi, []):
                d = struct.pack('<i', life) + bytes(12) + struct.pack('<i', nm) + bytes(12)
                d += struct.pack('<3f', x, y, z) + bytes(4)
                pkts.append(eff(pid, 0x01006018, 1, d)); pid += 16
            for (x, y, z, life, spd, acc, s0, s1, al, sp) in SMOKE_AT.get(fi, []):
                d = struct.pack('<i', life) + bytes(12)
                d += struct.pack('<3f', x, y, z) + bytes(4)
                d += struct.pack('<2i', 64666, 65451)
                d += struct.pack('<4f', spd, acc, s0, s1)
                d += struct.pack('<2i', al, sp)
                pkts.append(eff(pid, 0x01007109, 1, d)); pid += 16
            for (x, y, z, life, size, alpha) in SPLASH_AT.get(fi, []):
                d = struct.pack('<i', life) + bytes(12)
                d += struct.pack('<3f', x, y, z) + bytes(4)
                d += struct.pack('<3f', 0.0, 0.0, 0.0) + bytes(4)
                d += struct.pack('<f', size) + struct.pack('<i', alpha) + bytes(8)
                pkts.append(eff(pid, 0x01000107, 1, d)); pid += 16
            for (x, y, z, life) in DUST_AT.get(fi, []):
                d = struct.pack('<i', life) + bytes(12)
                d += struct.pack('<3f', x, y, z) + bytes(4)
                d += struct.pack('<2i', 0, 65067) + bytes(8)
                pkts.append(eff(pid, 0x0100600b, 1, d)); pid += 16
            for (x, y, z, life, radius, strength) in ERUPT_AT.get(fi, []):
                d = struct.pack('<i', life) + bytes(12)
                d += struct.pack('<3f', x, y, z) + bytes(4)
                d += struct.pack('<2f', radius, strength) + bytes(8)
                pkts.append(eff(pid, 0x01000106, 1, d)); pid += 16
            for (r, g, bl, al, near, far, ae, a0) in FOGSET_AT.get(fi, []):
                d = struct.pack('<4i', r, g, bl, al)
                d += struct.pack('<2f', near, far) + struct.pack('<2i', ae, a0)
                pkts.append(eff(pid, 0x01000000, 0, d)); pid += 16
            for (x, y, z, rad, cr, cg, cb, ca, life, dt) in CSMOKE_AT.get(fi, []):
                d = struct.pack('<i', life) + bytes(12)
                d += struct.pack('<3f', x, y, z) + bytes(4)
                d += struct.pack('<f', rad) + bytes(12)
                d += struct.pack('<4i', cr, cg, cb, ca)
                d += struct.pack('<3i', fi + life, fi, dt) + bytes(4)
                pkts.append(eff(pid, 0x01007101, 1, d)); pid += 16
            for (b0, b1, rot, pw, num, obj, col, scale, flags, life) in DEBRIS_AT.get(fi, []):
                d = struct.pack('<i', life) + bytes(12)                                # data 0-15  life+reserved
                d += struct.pack('<3f', *b0) + bytes(4)                                # 16-31  bound0
                d += struct.pack('<3f', *b1) + bytes(4)                                # 32-47  bound1
                d += struct.pack('<3f', *rot) + bytes(4)                               # 48-63  rot euler -> force (+Z axis)
                d += struct.pack('<f', pw) + struct.pack('<2i', num, obj) + bytes(4)   # 64-79  pow, num, objcode, pad
                d += struct.pack('<3i', *col) + bytes(4)                               # 80-95  color rgb + pad
                d += struct.pack('<f', scale) + struct.pack('<i', flags) + bytes(8)    # 96-111 scale, flags, pad
                pkts.append(eff(pid, 0x01006007, 1, d)); pid += 16
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        if fi in (874, 1166, 1442):
            # stock rumble packets: native envelopes end short of their spans
            # (the felt on/off holes). Stripped; the score owns act 1 now.
            pkts, ptail = read_packets(c.payload)
            kept = [q for q in pkts if not (q.kind == 6 and len(q.body) >= 44
                    and struct.unpack_from('<I', q.body, 0)[0] == 0x01009000)]
            if len(kept) != len(pkts):
                c.payload = write_packets(kept, ptail)
                touched += 1
        if fi in RUMBLE_AT and rumble_tmpl is not None:
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            end, vib, mode = RUMBLE_AT[fi]
            bb = bytearray(rumble_tmpl)
            struct.pack_into('<i', bb, 16, end - fi + 2)   # life
            struct.pack_into('<i', bb, 32, vib)            # d0: vibration pattern file
            struct.pack_into('<i', bb, 36, mode)           # d1: +1 stretch-to-window, -1 native
            struct.pack_into('<i', bb, 40, end)            # d2: absolute end frame
            struct.pack_into('<i', bb, 44, fi)             # d3: start frame
            pkts.append(Packet(PT_EFFECT, pid, 0, bytes(bb))); pid += 16
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        if fi in NEARFOCUS_AT and nearfocus_tmpl is not None:
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            life, dist = NEARFOCUS_AT[fi]
            bb = bytearray(nearfocus_tmpl)
            struct.pack_into('<i', bb, 16, life)
            struct.pack_into('<i', bb, 40, dist)
            pkts.append(Packet(PT_EFFECT, pid, 0, bytes(bb))); pid += 16
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        if fi in FOCUS_AT and focus_tmpl is not None:
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            life, dist = FOCUS_AT[fi]
            bb = bytearray(focus_tmpl)
            struct.pack_into('<i', bb, 16, life)
            struct.pack_into('<i', bb, 40, dist)
            pkts.append(Packet(PT_EFFECT, pid, 0, bytes(bb))); pid += 16
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        if fi in FOG_AT and fog_tmpl is not None:
            # Konami's own camera-fore fog wisps (proven params from the bridge flyover),
            # staggered thick through the city shots
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            bb = bytearray(fog_tmpl)
            struct.pack_into('<i', bb, 16, FOG_AT[fi])
            pkts.append(Packet(PT_EFFECT, pid, 0, bytes(bb))); pid += 16
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        if fi == 0:
            pkts, ptail = read_packets(c.payload)
            term = pkts.pop()
            assert term.kind in (0, 1)
            if fi == 0:
                # Konami convention: the scene's NewBlur_Demo life == demo length (the
                # original says 2180, its old length). Retarget the ORIGINAL packet's
                # life to OUR length -- one actor, whole demo, no overlap seam.
                for q in pkts:
                    if q.kind == 6 and len(q.body) >= 20 and struct.unpack_from('<I', q.body, 0)[0] == 0x01000011:
                        bb = bytearray(q.body)
                        struct.pack_into('<i', bb, 16, END + 10)
                        q.body = bytes(bb)
                        print('frame0 blur life -> %d' % (END + 10))
                # the original far-focus actor (dist 50k) dies AT our cut; per-shot
                # clones with tuned distances take over from there
                for q in pkts:
                    if q.kind == 6 and len(q.body) >= 44 and struct.unpack_from('<I', q.body, 0)[0] == 0x01000001:
                        focus_tmpl = bytes(q.body)
                for q in pkts:
                    if q.kind == 6 and len(q.body) >= 44 and struct.unpack_from('<I', q.body, 0)[0] == 0x01000002:
                        nearfocus_tmpl = bytes(q.body)
                        bb = bytearray(q.body)
                        struct.pack_into('<i', bb, 16, 2000)
                        q.body = bytes(bb)
                        print('frame0 far-focus life -> 2000')
            pkts.append(term)
            c.payload = write_packets(pkts, ptail)
            touched += 1
        fi += 1
    n = write_sdt(dst, chunks, tail)
    print('  dressed %d frames, wrote %d bytes' % (touched, n))
    # THE 512 LAW: DM_MAX_EFFECT lifetime births for the WHOLE file (stock + ours)
    _ch2, _ = read_sdt(dst)
    _bt2 = 0
    for _c2 in _ch2:
        try:
            _pk2, _ = read_packets(_c2.payload)
        except Exception:
            continue
        for _p2 in _pk2:
            if len(_p2.body) >= 8:
                _e2, _x2 = struct.unpack_from('<Ii', _p2.body, 0)
                if (_e2 >> 24) == 1 and _x2 == 1:
                    _bt2 += 1
    print('exec-1 births: %d / 512 (eft_list overrun crashes at the 512th)' % _bt2)
    assert _bt2 <= 500, 'EFFECT BUDGET BLOWN: %d births > 500 -- thin the schedules' % _bt2

