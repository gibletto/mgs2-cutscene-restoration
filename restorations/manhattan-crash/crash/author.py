"""The authoring pass: the extended cutscene, written through the Cutscene Studio.

author(sc) edits the studio's scene document for p080_13_p08 (the bridge flyover
that ends the Arsenal Gear arrival) after it has been extended to timeline.END
frames. Everything is data in that document: the camera's keys, the baked tracks
of the ship and its deck props, the effect instances, and putobj messages to the
world objects the scenario creates (see world.py). The studio then bakes it.

In order, this pass
  - bends the convoy's course from frame 1100 toward the statue and drives it
    along its new track to the stop on the Battery (ship, deck dolls, wake rig);
  - mounts Liberty Island, heaves and rolls the hull, and settles it back in the
    water;
  - re-arms Konami's wake splash streams across the extension and recolours them
    to soil over land;
  - writes every shot's camera, then layers a hand-held sway and impact shake;
  - suppresses the original's closing white-out and adds the flashes and fades;
  - sends the putobj messages that move the sets between eras, tear the statue
    off her pedestal and carry her off, swap the city through its three states,
    topple the towers, sweep the trees and right the wreck chunks;
  - fires the water eruptions of the landfall from the bow's live spawners.

The return value carries three tracks the dressing pass (dress.py) lays effects
along: the falling pedestal stump, the statue's kick points and the bow's path.

Frame numbers: anything written T(old) is in the first cut's numbering (see
timeline.py); `raw=True` on a message means the frame is already in new time.
"""
import math

from mgs2.strcode import strcode

from .timeline import (
    ARRIVE, BEACH0, BEACH_LEN, CUT, DEND, END, GRZ_X, GRZ_Z, MNT0, MNT1, MNTD,
    ORB0, ORB1, ORB_DIST, ORB_HI, ORB_LO, ORB_SWEEP, ORBJ, SOL, TK0, TK1, TURN0,
    VSLOW, O, T, beach_lift_n, graze_d, graze_ry, graze_yaw, irnd, ship_xz_vis_n,
    ship_z, vnoise_g)


def keys(cam, chn, kv, interp='linear'):
    cam[chn]['keys'] = [{'frame': f, 'value': v, 'interp': interp} for f, v in kv]
    cam[chn]['dirty'] = True


def author(sc):
    """Edit the scene document `sc` in place; returns the side tracks."""
    tracks = {}

    # ---- ship + wake rig ride the curve (offsets measured at the original last frame)
    by_id = {o['id']: o for o in sc['objects']}
    z8 = by_id[8]['posZ']['baked'][2179]
    x8 = by_id[8]['posX']['baked'][2179]
    vz8 = (z8 - by_id[8]['posZ']['baked'][2159]) / 20.0   # stock cruise velocity/frame

    # THE SKIP-AHEAD: at ORBJ (front view, ship off-frame) Arsenal jumps most of the
    # harbor and re-aims STRAIGHT down the graze line; solved backwards so the bow sits
    # DEND short of the graze point exactly at the rear-view settle (ORB1). The old
    # position's wake gets the whole face-view to dissipate.
    _fej = TK1 + 2.5 * (ORBJ - TK1)
    _pjx, _pjz = graze_d(_fej), z8 + vz8 * (_fej - 2179)
    _dax, _daz = GRZ_X - _pjx, GRZ_Z - _pjz
    _dam = math.hypot(_dax, _daz)
    APPR_U = (_dax / _dam, _daz / _dam)
    # visible heading veers a touch RIGHT of the actual track (nose off the line)
    APPR_YAW = math.atan2(-APPR_U[0], -APPR_U[1]) - math.radians(5.0)
    APPR_RY = APPR_YAW / (2.0 * math.pi) * 4096.0

    # THE DEPARTURE ARC: from the dismount the ship turns gradually STARBOARD onto its
    # final approach, engines building once it clears the island. Integrated per frame.
    DEP = {}
    _dd = DEND + VSLOW * (ARRIVE - MNT1)
    _dxp, _dzp = GRZ_X - APPR_U[0] * _dd, GRZ_Z - APPR_U[1] * _dd
    _phi = APPR_YAW + math.radians(5.0)          # the TRACK heading (veer is visual)
    _dv = VSLOW
    for _f in range(MNT1, ORB1 + 2):
        DEP[_f] = (_dxp, _dzp, _phi)
        if _f >= MNTD:
            _dv = min(280.0, _dv + 0.85)         # engines bite after touchdown
        # the WHOLE starboard turn happens during the splash-down -- the bobbing
        # hides it; by the settle the heading is done and it runs straight
        _tw = (_f - MNT1) / float(MNTD + 20 - MNT1)
        if 0.0 <= _tw <= 1.0:
            _phi -= math.radians(0.48) * math.sin(math.pi * _tw)
        _dxp += -_dv * math.sin(_phi)
        _dzp += -_dv * math.cos(_phi)

    def ship_clock(f):
        # ARSENAL DELAYED 34f through the impact (on screen: at 2678 the hull sits
        # where it used to at 2644). Decel ramps in over 2510-2644, holds after.
        t = max(0.0, min(1.0, (f - 2510) / 134.0))
        return f - 34.0 * t * t * (3 - 2 * t)

    def course_ry(f):
        fd = int(round(ship_clock(f)))
        if fd >= MNT1 and fd in DEP:
            return (DEP[fd][2] - math.radians(5.0)) / (2.0 * math.pi) * 4096.0
        return APPR_RY if fd >= ORBJ else graze_ry(fd)

    def ship_xz_show(f):
        # what the convoy rides: stock-velocity continuation through the takeover
        # (the pan is unbroken -- any jump would be ON SCREEN), authored curve after
        if f < ORB1:
            fd = ship_clock(f)
            fi = int(round(fd))
            # THE 2816 JUMP is a SCREEN-frame world shift hidden by the wipe -- it
            # rides presentation time, not the (delayed) ship clock. (A 28-frame
            # glide was tried and looked WORSE: visible zoom-across. Instant it is;
            # the far-side lighting is corrected by the parallel-light tilt.)
            J = 65000.0 if f >= 2816 else 0.0
            if fi >= MNT1 and fi in DEP:
                return DEP[fi][0] - x8 + J, DEP[fi][1]
            if fd >= ORBJ:
                # post-jump: ALREADY UPON HER -- the jump lands deep and the ship
                # glides the visible half at VSLOW, so the swing-around is a SURPRISE
                d = DEND + VSLOW * (ARRIVE - fd)  # linear through: past ARRIVE it sails ON
                return GRZ_X - APPR_U[0] * d - x8 + J, GRZ_Z - APPR_U[1] * d
            # takeover: the graze curve; early tour: same line at 2.5x
            fe = fd if fd <= TK1 else TK1 + 2.5 * (fd - TK1)
            return graze_d(fe), z8 + vz8 * (fe - 2179)
        return ship_xz_vis_n(f)

    c15, s15 = 0.96593, 0.25882
    # pre-sample the stock bow track: the loops below overwrite in place
    x8st = list(by_id[8]['posX']['baked'])
    z8st = list(by_id[8]['posZ']['baked'])
    for oid in (5, 6, 7, 8):
        o = by_id[oid]
        off = o['posZ']['baked'][2179] - z8
        x0 = o['posX']['baked'][2179]
        dx = x0 - x8
        bk = o['posZ']['baked']
        bkx = o['posX']['baked']
        # STOCK region: drift the whole formation and pivot it about the bow --
        # Konami's own animation rides on top untouched
        for f in range(TURN0, 2180):
            th = graze_yaw(f)
            c_, s_ = math.cos(th), math.sin(th)
            rx = bkx[f] - x8st[f]
            rz = bk[f] - z8st[f]
            bkx[f] = x8st[f] + graze_d(f) + c_ * rx + s_ * rz
            bk[f] = z8st[f] + (-s_ * rx + c_ * rz)
        for f in range(2180, END):
            x, zz = ship_xz_show(f)
            if f < ORB1:
                th = course_ry(f) / 4096.0 * 2.0 * math.pi
                c_, s_ = math.cos(th), math.sin(th)
                bk[f] = zz + (-s_ * dx + c_ * off)
                bkx[f] = x8 + x + (c_ * dx + s_ * off)
            elif (T(2545) <= f < T(2762) or f >= T(3050)) and oid != 8:
                # the hull yaws about its own origin from the jump on; the deck dolls
                # must swing with it or they slide off their marks at close-up range
                bk[f] = zz + (-s15 * dx + c15 * off)
                bkx[f] = x8 + x + (c15 * dx + s15 * off)
            else:
                bk[f] = zz + off
                bkx[f] = x0 + x

    # the ship arrives PRE-ROTATED at its final heading from the street cut on -- the
    # approach line IS the resting heading run backwards, so there is no turn to make
    ry = by_id[8]['rotY']['baked']
    for f in range(TURN0, ORB1):
        ry[f] += course_ry(f)
    for oid in (5, 6, 7):
        r = by_id[oid].get('rotY')
        if r and r.get('baked'):
            for f in range(TURN0, ORB1):
                r['baked'][f] += course_ry(f)
    # THE PORT-SIDE MOUNT (2625 -> ~2878): the island rides under the port bow --
    # hull heaves up like the landfall lift but TILTED, banking starboard so the
    # raised port wing clips HER; grind, dismount at 2800, damped splash settle.
    def mount_y(f):
        f = int(round(ship_clock(f)))
        if f < MNT0 or f >= MNTD:
            return 0.0
        if f < MNT1:
            base = 2250.0 * min(1.0, (f - MNT0) / 45.0)
            rock = 500.0 * math.exp(-(f - MNT0) / 40.0) * math.sin((f - MNT0) / 6.0)
            # the scour: the hull SKIPS off the island edge -- chunky bounces,
            # each one a different height
            bnc = 650.0 * abs(math.sin((f - MNT0) / 12.0))                       * (0.55 + 0.45 * vnoise_g(f, 6262, 24)) * min(1.0, (f - MNT0) / 45.0)
            jost = 120.0 * vnoise_g(f, 5151, 7) * min(1.0, (f - MNT0) / 45.0)
            return base + rock + bnc + jost
        t = (f - MNT1) / float(MNTD - MNT1)
        # smootherstep: zero velocity AND acceleration at touchdown -- soft landing
        down = 2250.0 * (1.0 - t * t * t * (t * (t * 6.0 - 15.0) + 10.0))
        rock = 300.0 * math.exp(-(f - MNT1) / 30.0) * math.sin((f - MNT1) / 5.0) * t
        return down + rock

    def mount_roll(f):
        f = int(round(ship_clock(f)))
        # port up / bank right: top tips toward -X => rz POSITIVE (tower convention)
        if f < MNT0 or f >= MNTD:
            return 0.0
        if f < MNT1:
            return 110.0 * min(1.0, (f - MNT0) / 55.0) + 16.0 * vnoise_g(f, 6363, 9)
        t = (f - MNT1) / float(MNTD - MNT1)
        return 110.0 * (1.0 - t * t * (3 - 2 * t))

    def mount_pitch(f):
        f = int(round(ship_clock(f)))
        # splash-down bob: stern heaves and settles, damped
        if f < MNT1 or f >= MNTD + 40:
            return 0.0
        t = f - MNT1
        return 0.045 * math.exp(-t / 45.0) * math.sin(t / 7.0)   # rad, ~2.6deg peak

    def ship_bob(f):
        # the water has her again: gentle swell after the settle, dying away
        # through the linger as the dolly pushes into manhattan
        fb = int(round(ship_clock(f)))
        if fb < MNTD:
            return 0.0
        tb = fb - MNTD
        return 260.0 * math.exp(-tb / 150.0) * math.sin(tb / 16.0)

    for oid in (5, 6, 7, 8):
        o = by_id[oid]
        bky2 = o['posY']['baked']
        po = o['posZ']['baked'][2179] - z8    # z-offset from the bow (rigid frame)
        # +34: the mount runs on the DELAYED ship clock; ending the application
        # at screen MNTD chopped ~600 units off in one frame (the 2900 pop)
        for f in range(MNT0, MNTD + 34):
            bky2[f] += mount_y(f)
        for f in range(MNT1, MNTD + 74):
            bky2[f] += mount_pitch(f) * po    # the REAR physically bobs
        rzc = o.get('rotZ')
        if rzc and rzc.get('baked'):
            for f in range(MNT0, MNTD + 34):
                rzc['baked'][f] += mount_roll(f)
        rxc = o.get('rotX')
        if rxc and rxc.get('baked'):
            for f in range(MNT1, MNTD + 74):
                rxc['baked'][f] += mount_pitch(f) / (2.0 * math.pi) * 4096.0
        for f in range(MNTD + 34, ORB1):
            bky2[f] += ship_bob(f)

    for f in range(T(3050), END):
        ry[f] += 171.0
    # clinton preview: the ship POINTS ALONG ITS MOTION; the heading flips under
    # the cut itself (a cut hides anything)
    for f in range(T(2545), T(2762)):
        ry[f] += -171.0
    for oid in (5, 6, 7):
        r = by_id[oid].get('rotY')
        if r and r.get('baked'):
            for f in list(range(T(2545), T(2762))) + list(range(T(3050), END)):
                r['baked'][f] += 171.0

    # wake, the REAL mechanism (mesg_bomb3.c): the water break is SPAWNED each time a
    # 0x8000 message (cmd 13 = WAVE_SPLUSH2) reaches the spawner actor sitting on the
    # bow control point. The points only POSITION the spawners -- which is why every
    # points-only attempt stayed invisible. The message streams' last packet lands one
    # frame short of the extension threshold, so the bus just stopped at the seam.
    # Re-arm it with the last real message, firing until the switch; the spawner
    # actors' own lifetimes (f4030/4022) fade the splash during the final grind.
    stream_i = 0
    for e in sc['effects']:
        if e['effectId'] != 16809984 or str(e.get('uid', '')).startswith('teaser_'):
            continue
        pr = e.get('params') or {}
        d9 = (pr.get('d9') or {}).get('baked') or []
        src = -1
        for f in range(min(CUT, len(d9)) - 1, -1, -1):
            if d9[f] == 13:   # channels are sparse: values live only on packet frames
                src = f
                break
        if src < 0:
            continue   # not a wave-splash stream
        e['continue'] = True
        e['end'] = T(3480)
        for ch in pr.values():
            bk2 = ch.get('baked')
            if bk2 and len(bk2) > src:
                hold = bk2[src]
                ch['baked'] = bk2[:CUT] + [hold] * (T(3512) - CUT)
        # over land (landfall 3140+): pulverised debris, not sea -- recolor the
        # WAVE_SPLUSH2 bytes (d2=R d3=G d4=B), ALTERNATING dusty brown / ash grey per
        # stream so the cloud reads as mixed soil + dust, not monochrome
        col = (26, 17, 9) if (stream_i % 2 == 0) else (14, 9, 5)   # dark mud / darker mud (the tex is bright: low values or it reads white)
        stream_i += 1
        for k, dusty in (('d2', col[0]), ('d3', col[1]), ('d4', col[2])):
            cc = pr.get(k)
            if cc and cc.get('baked'):
                for f in range(T(3140), min(T(3480), len(cc['baked']))):
                    cc['baked'][f] = dusty
        d1 = pr.get('d1')
        if d1 and d1.get('baked'):
            base1 = d1['baked'][src]
            for f in range(T(3140), min(T(3480), len(d1['baked']))):
                d1['baked'][f] = base1
        # (no fade ramps: the ship is continuous across the cut, sprays carry over)
        # shot B: only TWO spray streams (one brown, one grey) -- four stacked additive
        # sprites blew out white and starved the fire/smoke actor pool
        if stream_i > 2:
            d0c = pr.get('d0')
            if d0c and d0c.get('baked'):
                for f in range(T(3310), min(T(3312), len(d0c['baked']))):
                    d0c['baked'][f] = 1
        # spawn gap before the S2->S3 ship jump: splashes decay over ~1s anchored where
        # born, so a pre-jump spawn would linger at the OLD position into the towers
        # shot. Address the messages to nobody for the whole (shortened) chase.
        d0ch = pr.get('d0')
        if d0ch and d0ch.get('baked'):
            gap = range(CUT, T(2390))
            for f in gap:
                d0ch['baked'][f] = 1
    # the wake: 4 ControlChange streams drive the hull's water-break spline, but the
    # import merges them (same packetId), so the extension emitted ONE point four times
    # and the break died at the seam. Re-split: pin each instance to one of the original
    # f2179 points (name, x, y) and ride its z with the hull.
    WAKE_PTS = [
        (13914725, -20891.0, -272.0, -101143.0),
        (13914716, 691.0, -272.0, -115546.0),
        (13914731, 21702.0, -272.0, -100892.0),
        (5683027, -11897.0, -590.0, -104355.0),
    ]
    ccs = [e for e in sc['effects'] if e['effectId'] == 17825776]
    assert len(ccs) == len(WAKE_PTS), 'wake stream count changed: %d' % len(ccs)
    for e, (name, x, y, z0) in zip(ccs, WAKE_PTS):
        off = z0 - z8
        ox = x - x8
        e['dirty'] = True   # stock-region wake points are edited: no passthrough
        pr = e.get('params') or {}
        for key, val in (('d0', name), ('d5', y)):
            c2 = pr.get(key)
            if c2 and c2.get('baked') and len(c2['baked']) >= CUT:
                for f in range(2180, min(END, len(c2['baked']))):
                    # the landfall climb belongs to the STRIKE era only: the recut
                    # dragged old-3130+ forward of the CC cut, lifting the break
                    # water while the settled hull sits back in the river
                    lift = beach_lift_n(f) if key == 'd5' and ORB1 <= f < T(3510) else 0.0
                    c2['baked'][f] = val + lift
        d4c = pr.get('d4')
        d6c = pr.get('d6')
        if d4c and d4c.get('baked') and d6c and d6c.get('baked'):
            b4, b6 = d4c['baked'], d6c['baked']
            # STOCK region: Konami's own wake animation rides the curved course
            for f in range(TURN0, min(2180, len(b4), len(b6))):
                th = graze_yaw(f)
                c_, s_ = math.cos(th), math.sin(th)
                rx = b4[f] - x8st[f]
                rz = b6[f] - z8st[f]
                b4[f] = x8st[f] + graze_d(f) + c_ * rx + s_ * rz
                b6[f] = z8st[f] + (-s_ * rx + c_ * rz)
            for f in range(2180, min(END, len(b4), len(b6))):
                if f < ORB1:
                    th = course_ry(f) / 4096.0 * 2.0 * math.pi
                    c_, s_ = math.cos(th), math.sin(th)
                    xb, zb = ship_xz_show(f)
                    b4[f] = x8 + xb + c_ * ox + s_ * off
                    b6[f] = zb + (-s_ * ox + c_ * off)
                else:
                    b4[f] = x + ship_xz_show(f)[0]
                    b6[f] = ship_xz_show(f)[1] + off

    # ---- f4160, the dead stop (visual wreck alignment, calibrated by eye). City swaps to
    # wrecked + the beached hull
    # appears (SendEffectMessage straight to the putobj charas -- the ExecProc/GCL route
    # int3s in the MC port); the live convoy and wake duck under the sea that frame
    # Raiden (id5, rai_onemesh_mt) beds down through the deck close-up: the one-joint
    # doll settles toward the deck as if bracing -- all fours, back sinking
    bkr = by_id[5]['posY']['baked']
    for f in range(T(2960), T(3122)):
        bkr[f] -= 8.0 * min(1.0, (f - T(2960)) / 140.0)

    # shot B: the city floor sits well above the waterline -- carry the hull 75% of its
    # height (9,100 * 0.75) higher so it grinds ON the streets; the blast flash hides
    # the step up at the cut
    for oid in (5, 6, 7, 8):
        bky = by_id[oid]['posY']['baked']
        # from the ramp's own start, not a fixed frame: gating it later than BEACH0 means
        # the first applied frame already carries a topped-out curve, which reads as a hop
        for f in range(T(BEACH0), T(3510)):
            bky[f] += beach_lift_n(f)

    for oid in (5, 6, 7, 8):
        bky = by_id[oid]['posY']['baked']
        for f in range(T(3510), END):
            bky[f] -= 25000.0
    for e in sc['effects']:
        if e['effectId'] != 17825776:
            continue
        ch5 = (e.get('params') or {}).get('d5')
        if not ch5 or not ch5.get('baked'):
            continue
        for f in range(T(3510), min(END, len(ch5['baked']))):
            ch5['baked'][f] -= 25000.0

    # ---- the shots (every boundary is a hard cut: adjacent keys, 1-frame jump)
    cam = sc['camera']

    # THE TAKEOVER (TK0..TK1): no cut at the whiteout point. Seize the stock camera
    # mid-shot, keep its exact position/aim/drift (slopes measured over 40 stock
    # frames so vibration averages out), then one slow pan LEFT onto Lady Liberty
    # with a gentle push-in; the S3 heli cut lands at TK1.
    def cb(ch, f):
        return cam[ch]['baked'][f]

    # the shot SOURCES the stock BRIDGE camera (1835) -- behind the span, rails in
    # the near-blur, the convoy rumble real. Starting the shot at 1542 is a CUT
    # from the crane descent onto this camera, not a seize of the crane.
    SEIZE_F = 1835
    p0 = {c: cb(c, SEIZE_F) for c in ('posX', 'posY', 'posZ', 'trgX', 'trgY', 'trgZ')}
    sl = {c: (cb(c, SEIZE_F) - cb(c, SEIZE_F - 40)) / 40.0 for c in p0}
    dx0, dy0, dz0 = (p0['trgX'] - p0['posX'], p0['trgY'] - p0['posY'], p0['trgZ'] - p0['posZ'])
    dist0 = (dx0 * dx0 + dy0 * dy0 + dz0 * dz0) ** 0.5
    yaw0 = math.atan2(dx0, -dz0)                     # 0 == looking -Z
    el0 = math.asin(dy0 / dist0)
    # aim drift (the stock shot's slow tilt), from the target slopes
    dxe = (p0['trgX'] + 40 * sl['trgX']) - (p0['posX'] + 40 * sl['posX'])
    dye = (p0['trgY'] + 40 * sl['trgY']) - (p0['posY'] + 40 * sl['posY'])
    dze = (p0['trgZ'] + 40 * sl['trgZ']) - (p0['posZ'] + 40 * sl['posZ'])
    de = (dxe * dxe + dye * dye + dze * dze) ** 0.5
    yaw_sl = (math.atan2(dxe, -dze) - yaw0) / 40.0
    el_sl = (math.asin(dye / de) - el0) / 40.0
    def smo(t):
        t = min(1.0, max(0.0, t))
        return t * t * (3 - 2 * t)

    # THE ESTABLISHING ARC: keep the seize, then a slow WIDE sweep RIGHT on
    # a big arc about a focal point between her and the city, dropping as it goes.
    # The aim slides off the focal point toward the city: she ends frame-LEFT with
    # the skyline centred. The bridge stays near -- its cables hang in the
    # near-focus blur through the whole move.
    FP = (-63500.0, 5000.0, -520000.0)          # between her (far set) and the city
    CITY_AIM = (-25000.0, 6000.0, -600000.0)
    # the ZOOM does the chasing; the camera only drifts a little right along the
    # FP-centred arc -- tangent runs ALONG the bridge line, never across it, so the
    # cables stay in the near-blur across the bottom of frame the whole shot
    _r0x, _r0z = p0['posX'] - FP[0], p0['posZ'] - FP[2]
    _rr = math.hypot(_r0x, _r0z)
    _th0 = math.atan2(_r0x, _r0z)
    tkX, tkY, tkZ = [], [], []
    tkTX, tkTY, tkTZ, tkS = [], [], [], []
    for f in list(range(TK0, TK1 - 1, 8)) + [TK1 - 1]:
        u = smo((f - TK0) / float(2210 - TK0))       # sweep+zoom together, STILL MOVING at the cut
        th = _th0 + 0.045 * u                        # a LITTLE arc: ~14k of drift
        px = FP[0] + _rr * math.sin(th)
        pz = FP[2] + _rr * math.cos(th)
        # a LITTLE drop -- enough to keep the bridge from zooming out of frame,
        # nowhere near the truss-safari of the big one
        dy_run = min(f - TK0, 80)
        py = p0['posY'] + sl['posY'] * dy_run * (1.0 - 0.5 * smo((f - TK0) / 80.0)) \
            - 1400.0 * u
        # aim: continue the stock drift, cross-fading onto the sliding focal line
        yaw_c = yaw0 + yaw_sl * (f - TK0)
        el_c = el0 + el_sl * (f - TK0)
        axp = FP[0] + (CITY_AIM[0] - FP[0]) * 0.85 * u
        ayp = FP[1] + (CITY_AIM[1] - FP[1]) * 0.85 * u
        azp = FP[2] + (CITY_AIM[2] - FP[2]) * 0.85 * u
        dxi, dyi, dzi = axp - px, ayp - py, azp - pz
        di = (dxi * dxi + dyi * dyi + dzi * dzi) ** 0.5
        yaw_i = math.atan2(dxi, -dzi)
        el_i = math.asin(dyi / di)
        yw = yaw_c * (1.0 - u) + yaw_i * u
        el = el_c * (1.0 - u) + el_i * u
        tkX.append((f, px)); tkY.append((f, py)); tkZ.append((f, pz))
        tkTX.append((f, px + dist0 * math.sin(yw) * math.cos(el)))
        tkTY.append((f, py + dist0 * math.sin(el)))
        tkTZ.append((f, pz - dist0 * math.cos(yw) * math.cos(el)))
        # the dolly zoom IS the follow: a long push chasing the receding convoy
        tkS.append((f, cb('screen', SEIZE_F) + (3.6 - cb('screen', SEIZE_F)) * smo((f - 1700) / 430.0)))
    tk_roll = cb('roll', SEIZE_F)
    # ONE MOTION: Konami's own cut at 1445 (static shot) lands DIRECTLY on the arc.
    # No crane continuation, no dive, no seam -- the three-motion clash is dead.

    # THE ORBIT: 180deg around her, +x (her face, the channel side) sweeping the
    # SEAWARD side to -x (her back, Jersey side), high->level, aim island->torso.
    orX, orY, orZ = [], [], []
    orTX, orTY, orTZ, orS = [], [], [], []

    def gust(f, seed):
        # wind: slow irregular shoves (0.75s / 1.5s blocks), not the fast sway
        return 0.72 * vnoise_g(f, seed, 45) + 0.28 * vnoise_g(f, seed + 900, 90)

    GUSTA = 520.0
    frozen = None
    dzb = None                # dolly-zoom distance baseline, set at 2900
    for f in sorted(set(list(range(ORB0, ORB1 - 1, 8)) + [ORB1 - 1, 2815, 2816, 2948, 2956])):
        t = min(1.0, (f - ORB0) / float(ORB_SWEEP - 1))
        tt = 0.13 + 0.87 * t                        # arrive mid-motion, a few degrees in
        e = tt * tt * (3 - 2 * tt)
        b = -1.22 * math.pi * e                     # past her rear to NW: onward journey + Manhattan
        gw = GUSTA * (0.7 + 0.9 * t)                # wind builds through the tour
        px = SOL[0] + ORB_DIST * math.cos(b) + gw * gust(f, 7301)
        pz = SOL[2] + ORB_DIST * math.sin(b) + gw * gust(f, 8517)
        py = SOL[1] + ORB_HI + (ORB_LO - ORB_HI) * e + 0.5 * gw * gust(f, 7911)
        # the aim lags the shove by a few frames, then recovers -- buffeted but tracking
        ax = SOL[0] + 0.35 * gw * gust(f - 7, 7301)
        ay = SOL[1] + 3000.0 + 6000.0 * e + 0.2 * gw * gust(f - 7, 7911)
        az = SOL[2] + 0.35 * gw * gust(f - 7, 8517)
        if f >= 2816:
            px += 65000.0             # the 2816 jump: camera rides along
            ax += 65000.0
        if f >= 2800:
            # the dismount pan: follow HER out (she rides the aft-port wing --
            # see the abduction's derived OX0/OZ0), no more empty frame-right
            w = smo((f - 2800) / 200.0)
            thp = course_ry(f) / 4096.0 * 2.0 * math.pi
            cp_, sp_ = math.cos(thp), math.sin(thp)
            sxp, szp = ship_xz_show(f)
            hx2 = x8 + sxp + cp_ * -7500.0 + sp_ * -19500.0
            hz2 = szp + (-sp_ * -7500.0 + cp_ * -19500.0)
            ax = ax * (1 - w) + hx2 * w
            ay = ay * (1 - w) + 4500.0 * w
            az = az * (1 - w) + hz2 * w
        if f >= 3000:
            # DOLLY-JIB into the chase: fly to a perch high behind the stern and
            # ride with the ship; aim ahead-and-high so Arsenal hugs the BOTTOM
            # of frame with the doomed city filling the top
            cw = smo((f - 3000) / 120.0)
            thc = course_ry(f) / 4096.0 * 2.0 * math.pi
            cc_, sc_ = math.cos(thc), math.sin(thc)
            sxc, szc = ship_xz_show(f)
            cpx = x8 + sxc + sc_ * 46000.0
            cpz = szc + cc_ * 46000.0
            px = px * (1 - cw) + cpx * cw
            py = py * (1 - cw) + 9000.0 * cw
            pz = pz * (1 - cw) + cpz * cw
            apx = x8 + sxc + sc_ * -40000.0
            apz = szc + cc_ * -40000.0
            ax = ax * (1 - cw) + apx * cw
            ay = ay * (1 - cw) + 16000.0 * cw
            az = az * (1 - cw) + apz * cw
        if f <= 2956:
            if f <= 2948:
                aim_a = (ax, ay, az)          # one key before the settle
            aim_b = (ax, ay, az)
            frozen = (px, py, pz)
        else:
            # THE LINGER: hold position; the aim keeps its LAST VECTOR, slowed to
            # a residual drift -- no target, just momentum and the wind
            px, py, pz = frozen
            vax = (aim_b[0] - aim_a[0]) / 8.0 * 0.35
            vay = (aim_b[1] - aim_a[1]) / 8.0 * 0.35
            vaz = (aim_b[2] - aim_a[2]) / 8.0 * 0.35
            ax = aim_b[0] + vax * (f - 2956)
            ay = aim_b[1] + vay * (f - 2956)
            az = aim_b[2] + vaz * (f - 2956)
        if f >= 2900:
            # the RISE: camera and aim climb together through the dolly zoom --
            # the framing slides up and her water-hit stays below the frame edge
            _rise = 3400.0 * smo((f - 2900) / 240.0)
            py += _rise
            ay += _rise
            # ...and a slow pan LEFT: no dead city hanging on frame-right
            _thp2 = 0.14 * smo((f - 2875) / 100.0)   # pan lands WITH the roll's build
            _cp2, _sp2 = math.cos(_thp2), math.sin(_thp2)
            _dxa, _dza = ax - px, az - pz
            ax = px + _dxa * _cp2 + _dza * _sp2
            az = pz - _dxa * _sp2 + _dza * _cp2
        orX.append((f, px)); orY.append((f, py)); orZ.append((f, pz))
        orTX.append((f, ax)); orTY.append((f, ay)); orTZ.append((f, az))
        sv = 1.6
        if f >= 2900:
            # DOLLY ZOOM: Arsenal departs the held camera -- the lens compensates
            # with distance so the hull holds its size; the water (and her final
            # drop) slides out the bottom of frame, the splash tells the story
            _shx2, _shz2 = ship_xz_show(f)
            _dd2 = math.hypot(math.hypot(x8 + _shx2 - px, _shz2 - pz), 4000.0 - py)
            if dzb is None:
                dzb = _dd2
            sv = 1.6 * min(3.2, _dd2 / dzb)
        orS.append((f, sv))


    def tk_hh(f):
        # the stock shot is BUTTER-SMOOTH (measured: zero jitter, linear drift).
        # Near-stock stillness: whisper-level sway, ramped in from literally zero,
        # zoom-compensated so the push-in does not magnify it.
        scr = cb('screen', 1835) + (3.6 - cb('screen', 1835)) * smo((f - 1700) / 430.0)
        # the stock shot was SHAKING (convoy rumbling under the deck): the seize
        # inherits it and it dies away as Arsenal pulls off; whisper-sway remains
        decay = 1.0 - smo((f - TK0) / 420.0)
        return (1.6 * decay + 0.34) * (cb('screen', 1835) / scr)

    def amp(f, lo, hi, m):   # shake ramp: 0 at lo, m at hi
        return m * (f - lo) / (hi - lo)

    # S4 shake: pos wobbles a little (parallax), the AIM wobbles a lot (that is what
    # reads as rumble on screen); ramps from 2760 to the cut as the bow closes in
    shk = list(range(T(2760), T(2900)))
    px4 = [(f, -21905 + irnd(f) * amp(f, T(2760), T(2900), 60)) for f in shk]
    py4 = [(f, 3794 + 5000 * (f - T(2660)) / 240 + irnd(f + 7001) * amp(f, T(2760), T(2900), 60)) for f in shk]
    tx4 = [(f, 270731 + irnd(f + 9001) * amp(f, T(2760), T(2900), 2200)) for f in shk]
    ty4 = [(f, -130981 + irnd(f + 11003) * amp(f, T(2760), T(2900), 2200)) for f in shk]

    # deck close-up shake (2900-3079): constant engine-rumble jitter, same irnd style --
    # motion energy also hides the frozen deck figures. Aim wobble ~90 at 4.5k range
    # reads like the Battery shot's; pos wobble small for parallax.
    dk = list(range(T(2960), T(3122)))

    # THE JIB (replaces the whip + deck shot): cut away mid-crane (2545, still
    # rising = motion blur), land LOW by the fort, then one long steady rise with the
    # aim glued to Arsenal as it closes -- tilt-down comes free. Very handheld.
    JIB0, JIB1 = T(2545), T(3050)
    jibX, jibY, jibZ = [], [], []
    jtX, jtY, jtZ = [], [], []
    for f in range(JIB0, JIB1):
        fo = O(f)
        t = (fo - 2545) / 504.0
        # the watched half (cut at 2834): full doubled rise completes inside it
        t2 = min(1.0, (fo - 2545) / 216.0)
        te = t2 * t2 * (3 - 2 * t2)
        jibX.append((f, -13400.0 + 800.0 * t))
        jibY.append((f, 3000.0 + 3600.0 * te))
        jibZ.append((f, -433800.0))
        jtX.append((f, 0.0))
        jtY.append((f, 4800.0 - 3600.0 * t2))
        jtZ.append((f, ship_z(O(f) + 289) - 33000.0))

    keys(cam, 'posX', tkX + orX + jibX + [
                       (T(3050), -25382), (T(3309), -25382), (T(3310), -12708), (T(3509), -12708), (T(3510), -44855), (END, -44855)])             # S5 far wide, craning up
    keys(cam, 'posY', tkY + orY + jibY + [(T(3050), 22120), (T(3309), 24120), (T(3310), 7700), (T(3509), 14288), (T(3510), 95346), (T(3710), 98346), (END, 98346)])
    keys(cam, 'posZ', tkZ + orZ + jibZ + [
                       (T(3050), -484608), (T(3309), -484608), (T(3310), -442447), (T(3509), -442447), (T(3510), -505841), (END, -505841)])
    keys(cam, 'trgX', tkTX + orTX + jtX + [(T(3050), 108979), (T(3309), 108979), (T(3310), 358944), (T(3509), 358944), (T(3510), 133879), (END, 133879)])
    keys(cam, 'trgY', tkTY + orTY + jtY + [(T(3050), -373387), (T(3309), -371387), (T(3310), 25686), (T(3509), -60000), (T(3510), -269655), (T(3710), -266655), (END, -266655)])
    keys(cam, 'trgZ', tkTZ + orTZ + jtZ + [
                       (T(3050), 310949), (T(3309), 310949), (T(3310), -1243743), (T(3509), -1243743), (T(3510), -342459), (END, -342459)])
    keys(cam, 'screen', tkS + orS + [
                         (T(2545), 1.9), (T(3049), 1.9), (T(3050), 1.4), (END, 1.4)])
    keys(cam, 'roll', [(TK0, tk_roll), (TK1 - 1, tk_roll), (T(2390), 0.0), (END, 0.0)])

    # ---- fades. The original ends on an engine-animated fade TO white (one packet at
    # f2000, d0->d1 over a key range) and that state is STICKY -- without a counter-fade
    # the whole extended half plays behind a white screen. So: fade IN from white at the
    # cut (the original's closer becomes our transition), and fade back OUT at the end.
    def K(frame, v):
        return {'keys': [{'frame': frame, 'value': v, 'interp': 'hold'}], 'dirty': True}

    def fade(uid, frame, a_from, a_to, span):
        frame = T(frame)
        return {
            'uid': uid, 'effectId': 16777224, 'execType': 0, 'packetId': 0,
            'start': frame, 'end': frame + 1, 'label': 'fade(force)',
            'dirty': True, 'added': True,
            'params': {
                'd0.r': K(frame, 255), 'd0.g': K(frame, 255), 'd0.b': K(frame, 255),
                'd0.a': K(frame, a_from),
                'd1.r': K(frame, 255), 'd1.g': K(frame, 255), 'd1.b': K(frame, 255),
                'd1.a': K(frame, a_to),
                'd2': K(frame, frame + span),   # key-range max: the engine ramps over d3..d2
                'd3': K(frame, frame),
            },
        }

    # the recut is a HARD cut: the original's own closing whiteout (fade packet at
    # STOCK f2000 ramping to f2180) must not fire -- with DROP_S1 its suppression is
    # what REVEALS the extra bridge footage
    for e in sc['effects']:
        if e['effectId'] == 16777224 and e.get('start') == 2000 and not str(e.get('uid', '')).startswith('teaser_'):
            e['deleted'] = True

    # orbit atmosphere: the stock harbor wind-gas (380-876) re-fired for the tour
    for e in sc['effects']:
        if e['effectId'] == 0x01000019 and not str(e.get('uid', '')).startswith('teaser_'):
            w = {'uid': 'teaser_orbit_wind', 'effectId': e['effectId'],
                 'execType': e.get('execType', 1), 'packetId': 0,
                 'start': ORB0, 'end': ORB1, 'label': 'orbit wind',
                 'dirty': True, 'added': True, 'params': {}}
            for k, ch in (e.get('params') or {}).items():
                bk2 = ch.get('baked') or []
                v = next((x for x in bk2 if x is not None), None)
                if v is None and ch.get('keys'):
                    v = ch['keys'][0]['value']
                if v is not None:
                    w['params'][k] = K(ORB0, v)
            sc['effects'].append(w)
            break

    # CAMERA SPLASH (stolen from the tanker intro p001_01_p02, instances 974/998):
    # parameterless fullscreen pair -- droplet WARP leads, visible DRIPS join ~28f
    # later, both die together. Fired as Arsenal slams back into the water.
    DRIP0 = 3030              # her splash wets the lens (pulled 1200ms early)
    for uid, eid, st, en in (('teaser_capsplash_warp', 0x01000025, DRIP0, ORB1),
                             ('teaser_capsplash_drip', 0x01000026, DRIP0 + 28, ORB1)):
        sc['effects'].append({'uid': uid, 'effectId': eid, 'execType': 1,
                              'packetId': 0, 'start': st, 'end': en,
                              'label': 'camera splash', 'dirty': True, 'added': True,
                              'params': {}})

    # blast-flash across the shot-B cut: full white f3400-3401 swallows the city swap
    # on ANY frame-pairing parity, and saturating the afterimage blur erases its ghost
    # of the pre-swap frames instead of smearing them into shot B
    sc['effects'].append(fade('teaser_flash_up', 3306, 0, 128, 4))
    sc['effects'].append(fade('teaser_flash_dn', 3312, 128, 0, 8))

    # bow-impact flash: marks the landfall (f3140) and covers the intact->breach swap
    sc['effects'].append(fade('teaser_hit_flash_up', 3136, 0, 128, 3))
    sc['effects'].append(fade('teaser_hit_flash_dn', 3141, 128, 0, 10))

    # final fade: with the shift the tail is 70 frames longer -- stretch the ramp to
    # fill it (old: span 130 ending at 3840)
    sc['effects'].append(fade('teaser_fade_out', 3710, 0, 128, END - T(3710)))

    # the switch: OBJ_ON/OBJ_OFF messages straight to the putobj charas (StrCode names
    # verified against the .gcx bytecode; command 0=off 1=on, message length 1)
    def sem(uid, frame, name, command, args=(), raw=False):
        # putobj.c commands: 0=OFF 1=ON 2=TURN(rx,ry,rz shorts, 4096ths) 3=MOVE(x,y,z) 4=PRESHADE
        # 5=KILL. args ride in d1.. and message_len = 1+len(args), same packing as boom().
        # raw=True: the frame is ABSOLUTE new-time (set swaps at era boundaries).
        frame = frame if raw else T(frame)
        zero = {'d%d' % i: K(frame, 0) for i in range(1, 9)}
        p = dict(d0=K(frame, name), **zero,
                 d9=K(frame, command), d10=K(frame, 1 + len(args)))
        for i, v in enumerate(args):
            p['d%d' % (i + 1)] = K(frame, int(v))
        return {
            'uid': uid, 'effectId': 16809984, 'execType': 0, 'packetId': 0,
            'start': frame, 'end': frame + 1, 'label': 'mesg(putobj)',
            'dirty': True, 'added': True,
            'params': p,
        }

    # THREE-STAGE progressive destruction of Manhattan (sent 2f early for putobj latency):
    #   intact w60a_7 --f3140 bow impact--> breach vw60a_7 (Castle Clinton clipped)
    #                 --f3311 shot-B blast--> full wreck vw60a_9
    # THE SET SWAP: city + her sit 100k FURTHER OUT (-z) for the
    # Verrazzano-era shots -- scale! -- and snap to canonical under the tour cut.
    # PUTOBJ LATENCY LAW: a mesg sent at frame f renders at f+1 (object applies it
    # on its next update), so every cut-snap sem goes ONE frame early -- 2180 for
    # the 2181 heli cut, ORB1-1 for the CC cut. The editor applies mesgs at their
    # authored frame, so it shows these snaps a frame early; the game is truth.

    # BRIDGE + NJ SHOVE (tour era only): Konami's Verrazzano set is 8 putobjs spawned
    # at -p 0,0,0 (placement baked in the models) -- MOVE is a clean world offset,
    # restore is MOVE-to-zero. Shoved away from her for the tour, back under the
    # Clinton cut. w10a_5 is the land masses (NJ), the rest the bridge itself.
    BRG = ['ベラザノ橋_w10a_%s' % n for n in
           ('1_hi_reso', '2_hi_reso', '3_hi_reso', '4_hi_reso', '7_hi_reso', '5', '6', '8')]
    BRG_OFF = (-80000, 0, 80000)     # -x (NJ west) and +z (bridge seaward), together
    for nm in BRG:
        c = strcode(nm)
        sc['effects'].append(sem('teaser_brg_away_%x' % c, 2180, c, 3, BRG_OFF, raw=True))
        sc['effects'].append(sem('teaser_brg_back_%x' % c, ORB1 - 1, c, 3, (0, 0, 0), raw=True))

    # MANHATTAN DEEP SET: -200k z for the linger framing, transitioned under
    # the 2816 hull-wipe; home again for the CC jib with every restore landing ON the
    # cut frames (the towers prove multi-message frames are safe in-game)
    CITY_SET = ['w60a_%s' % n for n in ('1', '2', '3', '4', '5', '6', '8', '7')] + ['w60a_7_brooklyn']
    CITY_P0 = (-25000, 1000, -500000)
    for i, nm in enumerate(CITY_SET):
        c = strcode(nm)
        sc['effects'].append(sem('teaser_deep_out_%x' % c, 2812 + i, c, 3,
                                 (CITY_P0[0], CITY_P0[1], CITY_P0[2] - 200000), raw=True))
        sc['effects'].append(sem('teaser_deep_back_%x' % c, ORB1 - 1, c, 3,
                                 CITY_P0, raw=True))
    # the fort trees don't ride the deep set: hidden with it, back for CC
    for i in range(9):
        tc = strcode('w60a_tree%d' % (i + 1))
        sc['effects'].append(sem('teaser_deep_troff_%x' % tc, 2812 + i, tc, 0, raw=True))
        sc['effects'].append(sem('teaser_deep_tron_%x' % tc, ORB1 - 1, tc, 1, raw=True))

    SOLC = 4530226   # StrCode('w60a_sol'), algorithm verified vs w60a_7/tree1
    sc['effects'].append(sem('teaser_set_city_far', 1, 7966984, 3, (-25000, 1000, -600000)))
    # Brooklyn rides every city era move like w60a_7 (it IS the city's far bank)
    BRKC = 10002196   # strcode('w60a_7_brooklyn')
    sc['effects'].append(sem('teaser_set_brk_far', 1, BRKC, 3, (-25000, 1000, -600000)))
    sc['effects'].append(sem('teaser_set_brk_near', 2180, BRKC, 3, (-25000, 1000, -500000)))
    # brooklyn's model IS the whole city land + brooklyn: the stock w60a_7
    # stands down entirely (one object = one clean era move)
    sc['effects'].append(sem('teaser_city7_off', 2, 7966984, 0, raw=True))
    sc['effects'].append(sem('teaser_set_sol_far', 1, SOLC, 3, (-102100, 240, -440500)))
    sc['effects'].append(sem('teaser_sol_rot', 3, SOLC, 2, (0, 683, 0)))   # fort aim: -341 legacy + 1024 (90 CCW, she faces the big spike)
    SOLP = 10749624   # StrCode('w60a_solp') -- the pedestal shaft, split from the island
    sc['effects'].append(sem('teaser_solp_far', 1, SOLP, 3, (-102100, 240, -440500)))
    sc['effects'].append(sem('teaser_solp_rot', 3, SOLP, 2, (0, -341, 0)))
    sc['effects'].append(sem('teaser_solp_near', 2180, SOLP, 3, (-102100, 240, -340500)))
    SOLST = 8443880   # StrCode('w60a_solst') -- the statue rides her own putobj now
    # same-OBJECT messages sharing a frame merge on reimport (one gets eaten in the
    # editor's view) -- every statue-family init event gets its own frame
    sc['effects'].append(sem('teaser_solst_far', 1, SOLST, 3, (-102100, 6312, -440500)))
    sc['effects'].append(sem('teaser_solst_rot', 3, SOLST, 2, (0, -341, 0)))
    sc['effects'].append(sem('teaser_solst_near', 2180, SOLST, 3, (-102100, 6312, -340500)))
    # THE SWAP: the split pair's stump frustum-pops at close angles, so the WHOLE
    # one-piece statue (solw) plays her until the cleave; the pair stays hidden but
    # keeps receiving its pose stream, so the crossover is pixel-exact
    SOLW = 10749631   # StrCode('w60a_solw')
    sc['effects'].append(sem('teaser_solw_far', 1, SOLW, 3, (-102100, 6312, -440500)))
    sc['effects'].append(sem('teaser_solw_rot', 3, SOLW, 2, (0, -341, 0)))
    sc['effects'].append(sem('teaser_solw_near', 2180, SOLW, 3, (-102100, 6312, -340500)))
    sc['effects'].append(sem('teaser_split_hide_st', 5, SOLST, 0))
    sc['effects'].append(sem('teaser_split_hide_kt', 1, 10749610, 0))
    # the reflection rides each original's set moves: mirrored geometry on the same
    # anchor, so it only stays under her if it makes every move she makes
    SOLR, SOLPR, SOLWR = 10749626, 8443782, 8444006
    sc['effects'].append(sem('teaser_solr_far', 1, SOLR, 3, (-102100, 240, -440500)))
    sc['effects'].append(sem('teaser_solr_rot', 3, SOLR, 2, (0, 683, 0)))
    sc['effects'].append(sem('teaser_solpr_far', 1, SOLPR, 3, (-102100, 240, -440500)))
    sc['effects'].append(sem('teaser_solpr_rot', 3, SOLPR, 2, (0, -341, 0)))
    sc['effects'].append(sem('teaser_solpr_near', 2180, SOLPR, 3, (-102100, 240, -340500)))
    sc['effects'].append(sem('teaser_solwr_far', 1, SOLWR, 3, (-102100, 6312, -440500)))
    sc['effects'].append(sem('teaser_solwr_rot', 3, SOLWR, 2, (0, -341, 0)))
    sc['effects'].append(sem('teaser_solwr_near', 2180, SOLWR, 3, (-102100, 6312, -340500)))
    # her island served its act: hidden under the Clinton cut
    sc['effects'].append(sem('teaser_sol_hide', ORB1 - 1, SOLC, 0, raw=True))
    sc['effects'].append(sem('teaser_solp_hide', ORB1 - 1, SOLP, 0, raw=True))
    sc['effects'].append(sem('teaser_solb_hide', ORB1 - 1, 10749610, 0, raw=True))
    sc['effects'].append(sem('teaser_solr_hide', ORB1 - 1, SOLR, 0, raw=True))
    sc['effects'].append(sem('teaser_solpr_hide', ORB1 - 1, SOLPR, 0, raw=True))

    # THE ABDUCTION, beat by beat:
    #  2620 cleaved below the feet: TILTS to her right about her base (origin=feet)
    #  2689 fallen right + momentum-twisted back-down (head SW, feet NE), moving with the ship
    #  2782 the big bump: she pops airborne while Arsenal slides on underneath
    #  2816 flat on her back on the hull, riding
    #  3000 the bed-roll begins: rolls about her own length, down the wing portside
    #  3136 off the port edge, falling -- the cut takes it
    F_CLEAVE, F_DOWN, F_BUMP, F_FLAT, F_ROLL0, F_OFF = 2650, 2740, 2782, 2816, 2822, 2970
    F_ROLL1 = 3010   # the roll's own clock outlives the rail: she goes over MID-motion
    HER = (-102100.0, 6312.0, -340500.0)
    DECK = 5200.0                     # the wing surface (still above her pedestal base)

    def ride_y(f):
        # measured in game: ~6950 on the mounted wing, ~3300 settled --
        # a straight line through both in terms of the mount lift
        return 3300.0 + 1.622 * mount_y(f)

    def ship_frame(f, ox, oz):
        th = course_ry(f) / 4096.0 * 2.0 * math.pi
        c_, s_ = math.cos(th), math.sin(th)
        sx, sz = ship_xz_show(f)
        return x8 + sx + c_ * ox + s_ * oz, sz + (-s_ * ox + c_ * oz)

    def inv_ship_frame(f, wx, wz):
        th = course_ry(f) / 4096.0 * 2.0 * math.pi
        c_, s_ = math.cos(th), math.sin(th)
        sx, sz = ship_xz_show(f)
        rx_, rz_ = wx - (x8 + sx), wz - sz
        return c_ * rx_ - s_ * rz_, s_ * rx_ + c_ * rz_

    def smo01(t):
        t = min(1.0, max(0.0, t))
        return t * t * (3 - 2 * t)

    def to_game_rot(rx, ry, rz):
        # the directed look = the studio's ZXY composite of these angles (Ry spins
        # her long axis first). The PORT applies the spin LAST in world frame --
        # three 'YXZ' (seen in game as a head-over-heels flip). Build the
        # directed matrix, re-extract in the game's order; both renderers then agree.
        a, b, c = (v / 4096.0 * 2.0 * math.pi for v in (rx, ry, rz))
        ca, sa = math.cos(a), math.sin(a)
        cb, sb = math.cos(b), math.sin(b)
        cc, sc_ = math.cos(c), math.sin(c)
        m11, m13 = cc * cb - sc_ * sa * sb, cc * sb + sc_ * sa * cb
        m21, m22 = sc_ * cb + cc * sa * sb, cc * ca
        m23 = sc_ * sb - cc * sa * cb
        m31, m33 = -ca * sb, ca * cb
        x = math.asin(max(-1.0, min(1.0, -m23)))
        if abs(m23) < 0.9999999:
            y, z = math.atan2(m13, m33), math.atan2(m21, m22)
        else:
            y, z = math.atan2(-m31, m11), 0.0
        r = 4096.0 / (2.0 * math.pi)
        return int(round(x * r)), int(round(y * r)), int(round(z * r))


    # THE PREAMBLE (2540-2620): the whole monument strains AWAY from the coming
    # hull -- attached, moving, BENDING -- then she tears free at the cleave and
    # whips the other way. The base keeps its wounded lean afterwards.
    PRE0 = 2540
    LEAN = (-90.0, -55.0)             # base rx, rz at full strain (away from the hit)
    PED_H = 6072.0                    # her feet above the island origin (the lever)
    _thx = LEAN[0] / 4096.0 * 2.0 * math.pi
    _thz = LEAN[1] / 4096.0 * 2.0 * math.pi
    _dxo = -PED_H * math.sin(_thz)    # pedestal-top swing at full lean
    _dzo = PED_H * math.sin(_thx)
    HER_L = (HER[0] + _dxo, HER[1], HER[2] + _dzo)
    for f in range(PRE0, F_CLEAVE, 2):
        lt = min(1.0, (f - PRE0) / float(F_CLEAVE - PRE0)) ** 2
        rxl, rzl = LEAN[0] * lt, LEAN[1] * lt
        _gr = to_game_rot(int(rxl), -341, int(rzl))
        sc['effects'].append(sem('teaser_pre_bt_%d' % f, f + 1, SOLP, 2,
                                 _gr, raw=True))
        sc['effects'].append(sem('teaser_pre_st_%d' % f, f + 1, 8443880, 2,
                                 _gr, raw=True))
        sc['effects'].append(sem('teaser_pre_sm_%d' % f, f, 8443880, 3,
                                 (int(HER[0] + _dxo * lt), int(HER[1]),
                                  int(HER[2] + _dzo * lt)), raw=True))
        sc['effects'].append(sem('teaser_pre_kt_%d' % f, f + 1, 10749610, 2,
                                 _gr, raw=True))
        sc['effects'].append(sem('teaser_pre_km_%d' % f, f, 10749610, 3,
                                 (int(HER[0] + _dxo * lt), int(HER[1]),
                                  int(HER[2] + _dzo * lt)), raw=True))
        sc['effects'].append(sem('teaser_pre_wt_%d' % f, f + 1, SOLW, 2,
                                 _gr, raw=True))
        sc['effects'].append(sem('teaser_pre_wm_%d' % f, f, SOLW, 3,
                                 (int(HER[0] + _dxo * lt), int(HER[1]),
                                  int(HER[2] + _dzo * lt)), raw=True))
    # the cleave IS the swap: whole statue out, torn pair in, same frame, same pose
    sc['effects'].append(sem('teaser_swap_w_off', F_CLEAVE, SOLW, 0, raw=True))
    sc['effects'].append(sem('teaser_swap_st_on', F_CLEAVE, SOLST, 1, raw=True))
    sc['effects'].append(sem('teaser_swap_kt_on', F_CLEAVE, 10749610, 1, raw=True))
    # her reflection goes with her: once she is off the pedestal there is nothing
    # standing over the water to cast it
    sc['effects'].append(sem('teaser_solwr_off', F_CLEAVE, 8444006, 0, raw=True))

    _dsa, _dsb = ship_xz_show(2669)
    _dsc, _dsd = ship_xz_show(2667)
    print('ARSENAL DIR @2669: ry %.3f rad' % math.atan2(_dsa - _dsc, _dsb - _dsd))

    # THE PEDESTAL FALLS: as her tear completes the wounded shaft keeps going --
    # the lean deepens, then the fall VEERS LEFT of the tilt line and it keels
    # over. The stump rides the deck down (it stands on the pedestal top).
    _thL = math.hypot(90.0, 55.0) / 4096.0 * 2.0 * math.pi   # the lean, as axis-angle
    _phL = math.atan2(-55.0, -90.0)                          # its topple-axis bearing
    _ywP = -341.0 / 4096.0 * 2.0 * math.pi
    _cyP, _syP = math.cos(_ywP), math.sin(_ywP)

    def ped_pose(th, ph, east=0.0):
        ux, uz = math.cos(ph), math.sin(ph)
        ct, st = math.cos(th), math.sin(th)
        vt = 1.0 - ct
        R = [[ct + ux * ux * vt, -uz * st, ux * uz * vt],
             [uz * st, ct, -ux * st],
             [ux * uz * vt, ux * st, ct + uz * uz * vt]]
        # a few degrees of EAST lean on top (clears the hull)
        ce, se = math.cos(east), math.sin(east)
        R = [[ce * R[0][j] + se * R[1][j] for j in range(3)],
             [-se * R[0][j] + ce * R[1][j] for j in range(3)],
             list(R[2])]
        M = [[sum(R[i][k] * [[_cyP, 0, _syP], [0, 1, 0], [-_syP, 0, _cyP]][k][j]
                  for k in range(3)) for j in range(3)] for i in range(3)]
        x = math.asin(max(-1.0, min(1.0, -M[1][2])))
        if abs(M[1][2]) < 0.9999999:
            y, z = math.atan2(M[0][2], M[2][2]), math.atan2(M[1][0], M[1][1])
        else:
            y, z = math.atan2(-M[2][0], M[0][0]), 0.0
        r = 4096.0 / (2.0 * math.pi)
        top = (M[0][1], M[1][1], M[2][1])      # where the deck's centre goes
        return (int(round(x * r)), int(round(y * r)), int(round(z * r))), top

    # the mount-lean CONTINUES, redirected ~30deg left -- ONE tilt that NEVER
    # STOPS: the rate curve has no interior zero. deg/frame knots, integrated:
    # ~30deg by 2700 (clears the graze), face ~2890, grinding to ~102 by 3012
    # the tilt falls along the SAME scouted vector as the carry:
    # top moves (-sin phi, cos phi) == (-0.510, -0.856) horizontal
    _phT = math.atan2(0.5101, -0.8560)
    _dPh = ((_phT - _phL + math.pi) % (2.0 * math.pi)) - math.pi
    _sdx, _sdz = -math.sin(_phT), math.cos(_phT)
    _knots = ((2678, 1.30), (2712, 0.25), (2800, 0.42), (2880, 0.18), (3012, 0.02))

    def _rate(f):
        for (f0, r0), (f1, r1) in zip(_knots, _knots[1:]):
            if f <= f1:
                return r0 + (r1 - r0) * (f - f0) / float(f1 - f0)
        return _knots[-1][1]

    _thAcc = {2674: _thL, 2675: _thL, 2676: _thL, 2677: _thL}
    _a = math.degrees(_thL)              # the DROP leads; the tilt follows at 2678
    for _f2 in range(2678, 3016):
        _a += _rate(_f2)
        _thAcc[_f2] = math.radians(_a)
    _stump_track = {}
    for f in range(2674, 3016):
        ph = _phL + _dPh * smo01((f - 2678) / 26.0)
        # the carry line: start->end measured in the studio viewport, 10190 units
        # along (-0.510, -0.084, -0.856), front-loaded from 2674
        _sl = 10190.0 * (1.0 - (1.0 - min(1.0, (f - 2674) / 280.0)) ** 2)
        th = _thAcc[f]
        eul, top = ped_pose(th, ph)
        sc['effects'].append(sem('teaser_ped_fall_%d' % f, f, SOLP, 2, eul, raw=True))
        sc['effects'].append(sem('teaser_ped_fall_p_%d' % f, f, SOLP, 3,
                                 (int(-102100 - 0.5101 * _sl), int(240 - 0.0840 * _sl),
                                  int(-340500 - 0.8560 * _sl)), raw=True))
        sc['effects'].append(sem('teaser_ped_fall_t_%d' % f, f, 10749610, 2, eul, raw=True))
        sc['effects'].append(sem('teaser_ped_fall_m_%d' % f, f, 10749610, 3,
                                 (int(-102100 - 0.5101 * _sl + top[0] * 6072),
                                  int(240 - 0.0840 * _sl + top[1] * 6072),
                                  int(-340500 - 0.8560 * _sl + top[2] * 6072)), raw=True))
        if f in (2690, 2707, 2724, 2741, 2758, 2775, 2792, 2809, 2826):
            _stump_track[f] = (int(-102100 - 0.5101 * _sl + top[0] * 6072),
                               int(240 - 0.0840 * _sl + top[1] * 6072),
                               int(-340500 - 0.8560 * _sl + top[2] * 6072))
    tracks['stump_track'] = _stump_track

    def tear_ease(t):
        # torn metal, not a broomstick: strain -> CATCH -> give -> catch -> release
        # smooth throughout: breaking begins at once, no hold-then-snap
        pts = ((0.0, 0.0), (0.35, 0.18), (0.7, 0.55), (1.0, 1.0))
        for (a, ga), (b, gb) in zip(pts, pts[1:]):
            if t <= b:
                u = (t - a) / (b - a)
                u = u * u * (3 - 2 * u)
                return ga + (gb - ga) * u
        return 1.0

    _gx0, _gz0 = ship_xz_show(F_CLEAVE)
    # where the fall ACTUALLY leaves her, converted into the ship's frame once --
    # she rides exactly where she landed (no teleport to an invented perch)
    _dsx1, _dsz1 = ship_xz_show(F_DOWN)
    _lx = HER_L[0] + (_dsx1 - _gx0) * 0.30
    _lz = HER_L[2] + (_dsz1 - _gz0) * 0.30
    OX0, OZ0 = inv_ship_frame(F_DOWN, _lx, _lz)
    EDGE = OX0 - 4800.0               # she rolls off HER side of the hull

    # the drop mark (-40694, 1975, -371416), measured in THIS build at f3004,
    # converted at THAT frame -- a hull spot that survives future retimes
    EXR, EZR = inv_ship_frame(3038, -40694.0, -371416.0)   # mark re-pinned: +34f ship delay
    AFT = (VSLOW * 0.8) * (F_FLAT - F_BUMP)
    # one body width PAST the mark along the roll direction: she clears the edge
    _rm2 = math.hypot(EXR - OX0, EZR - (OZ0 + AFT)) or 1.0
    EXR += (EXR - OX0) / _rm2 * 1300.0
    EZR += (EZR - (OZ0 + AFT)) / _rm2 * 1300.0

    def roll_world(f):
        # her world position on the bed-roll, in SHIP frame (world marks rot
        # whenever the course/timing moves -- the hull is the truth).
        # t^2 ACCELERATING into the rail: she reaches the edge AT SPEED, the
        # fall inherits it -- no dying ease, no hold
        t_r = (f - F_ROLL0) / float(F_OFF - F_ROLL0) if f >= F_ROLL0 else 0.0
        rt_ = min(1.0, t_r) ** 2
        ox = OX0 + (EXR - OX0) * rt_
        oz = (OZ0 + AFT) + (EZR - (OZ0 + AFT)) * rt_
        return ship_frame(f, ox, oz)
    _kicks = {}   # her world pos at the kick launches, for the spark bursts
    for f in range(F_CLEAVE, ORB1 - 1):
        if f == F_CLEAVE:
            # this frame belongs to the swap ON -- a same-frame MOVE would merge
            # over it on reimport and hide her from the editor forever
            continue
        if f < F_DOWN:
            # THE FALL: rotation does the falling (pivot = her feet); the feet
            # only get DRAGGED a little as she takes on Arsenal's momentum
            t_ = tear_ease((f - F_CLEAVE) / float(F_DOWN - F_CLEAVE))
            e_ = t_ * t_
            shud = int(16.0 * vnoise_g(f, 8181, 3)) if t_ < 0.45 else 0
            dsx, dsz = ship_xz_show(f)
            px = HER_L[0] + (dsx - _gx0) * 0.30 * e_
            pz = HER_L[2] + (dsz - _gz0) * 0.30 * e_
            # kicked at the feet: she topples TOWARD the blow, and the hoisted
            # wing arrives BENEATH her -- she rises onto it as she falls
            ep = min(1.0, t_ * 1.25) ** 3             # she DROPS into position
            py = HER[1] * (1 - ep) + ride_y(f) * ep
            # from the strained lean, THROUGH vertical, over toward the hit
            rx = int(LEAN[0] + (1024 - LEAN[0]) * smo01(t_ * 1.35)) + shud
            ry_ = -341 + int(220 * e_)                # the momentum twist, a tadge
            rz_ = int(LEAN[1] * (1 - t_) + 380 * e_)  # ...laying her onto her back
        elif f < F_BUMP:
            # across the wing, jostling with the grind
            px, pz = ship_frame(f, OX0, OZ0)
            py = ride_y(f) + 60.0 * vnoise_g(f, 7473, 5)
            rx, ry_, rz_ = 1024, -121, 380
        elif f < F_FLAT:
            # THE BUMP: airborne; the ship travels on BELOW her (she drifts aft
            # in ship frame), shedding a little momentum
            t_ = (f - F_BUMP) / float(F_FLAT - F_BUMP)
            drift = (VSLOW * 0.8) * (f - F_BUMP)
            px, pz = ship_frame(f, OX0, OZ0 + drift)
            # the slam-bounce, kick-style: ONE true gravity arc from the launch
            # line to the landing (the wing climbs under her -- uphill, so the
            # peak sits naturally past midpoint); same apex as the old sin hump
            _y0, _y1 = ride_y(F_BUMP), ride_y(F_FLAT)
            _P = ride_y(2799) + 975.0 - _y0            # apex above the launch
            _T = float(F_FLAT - F_BUMP)
            _s = math.sqrt(max(0.0, 1.0 - (_y1 - _y0) / _P))
            _v0 = (2.0 * _P / _T) * (1.0 + _s)
            _u = f - F_BUMP
            py = _y0 + _v0 * _u - (_v0 * _v0 / (4.0 * _P)) * _u * _u
            _an = max(0.0, (py - (_y0 + (_y1 - _y0) * t_)) / _P)
            rx, ry_, rz_ = 1024, -121, 380 + int(130 * _an)
        else:
            # flat on her back, sharing Arsenal's motion; from F_ROLL0 the bed-roll
            # (about her OWN length) walks her down the wing to the port rail.
            # Rail exit solved from a world mark (-92099, 2824, -413177) @ F_OFF.
            t_r = (f - F_ROLL0) / float(F_OFF - F_ROLL0) if f >= F_ROLL0 else 0.0
            rt = min(1.0, t_r) ** 2                    # accelerating into the rail
            px, pz = roll_world(f)
            py = ride_y(f) + (2350.0 - ride_y(f)) * rt + 40.0 * vnoise_g(f, 7473, 7)
            if 2856 <= f <= 2921:
                # RIDE the settling wing (+34: the wing settles on the DELAYED ship
                # clock): bob re-anchored so the surface passes 3800 at 2921
                u_ = smo01((f - 2856) / 14.0)
                _wt = ride_y(f) + (3800.0 - ride_y(2921)) + 40.0 * vnoise_g(f, 7473, 7)
                py = py * (1.0 - u_) + _wt * u_
            elif 2921 < f < 2968:
                # back down onto the dive-to-the-rail line
                u_ = smo01((f - 2921) / 47.0)
                py = 3800.0 * (1.0 - u_) + py * u_
            py += ship_bob(f)              # she stands on the swelling deck
            rx = 1024                                  # flat...
            if 2906 <= f < 2947:
                # mid-roll bump: same kick height, longer ballistic arc that
                # settles back onto the existing ride line by 2947
                _kb = 1.0 - ((f - 2926.5) / 20.5) ** 2
                py += 600.0 * _kb
                rx = 1024 + int(80 * _kb)
            if 2958 <= f < 2970:
                # her feet graze a bump short of the rail: a 600-unit kick,
                # ballistic arc back onto the line right at the edge
                _kb = 1.0 - ((f - 2964) / 6.0) ** 2
                py += 600.0 * _kb
                rx = 1024 + int(80 * _kb)  # the hit comes through the FEET
            # ONE continuous motion: the LEAN builds from 2847 (cubic, glacial --
            # her own off-balance is what grows it) and hands off exactly into the
            # master roll curve at 2891. No rocking, no push, no seam.
            t_s = (f - 2875) / float(F_ROLL1 - 2875) if f >= 2875 else 0.0
            rs = smo01(min(1.0, t_s))
            spin = 3600.0 * rs
            if f < 2891:
                _t60 = max(0.0, (f - 2847) / 44.0)
                _ts4 = (2891 - 2875) / float(F_ROLL1 - 2875)
                spin = 3600.0 * smo01(_ts4) * (_t60 ** 3)
            ry_ = -121 + int(spin)                     # ~316 deg, finishing mid-air
            rz_ = 380
            if f >= F_OFF:
                # over the edge still rolling: her real velocity at the rail (ship
                # carry + live roll motion) bleeds off through a shallow arc while
                # gravity builds gently -- one smooth accelerating drop, no hang
                fall = f - F_OFF
                _p0x, _p0z = roll_world(F_OFF)
                _pmx, _pmz = roll_world(F_OFF - 2)
                _vx, _vz = (_p0x - _pmx) / 2.0, (_p0z - _pmz) / 2.0
                fl = min(fall, 22.0)      # the arc dies at the water -- no skating
                _th = -0.02 * fl          # bend AWAY from the hull, not into it
                _c, _s = math.cos(_th), math.sin(_th)
                # near-ballistic: she KEEPS the roll's -x momentum into the water
                _carry = (1.0 - 0.978 ** fl) / 0.022
                px = _p0x + (_vx * _c - _vz * _s) * _carry
                pz = _p0z + (_vx * _s + _vz * _c) * _carry
                _py0 = ride_y(F_OFF) + (2350.0 - ride_y(F_OFF)) + ship_bob(F_OFF)
                # she goes over WITH her descent speed; gravity compounds it --
                # quad to the surface, then a steady plunge out of view
                _vy0 = 2.0 * (2350.0 - ride_y(F_OFF)) / (F_OFF - F_ROLL0)
                if fall <= 20:
                    py = _py0 + _vy0 * fall - 8.0 * fall * fall
                else:
                    py = max(_py0 + _vy0 * 20.0 - 3200.0 - 320.0 * (fall - 20), -20000.0)
                # spin momentum survives the rail: constant rate all the way down
                _ts0 = (F_OFF - 2875) / float(F_ROLL1 - 2875)
                _rs0 = _ts0 * _ts0 * (3 - 2 * _ts0)
                _rate = 3600.0 * 6.0 * _ts0 * (1.0 - _ts0) / (F_ROLL1 - 2875)
                ry_ = -121 + int(3600 * _rs0 + _rate * fall)
            if f == F_OFF + 16:
                _shx, _shz = ship_xz_show(f)
                print('SPLASH MARK (water impact): %d %d %d @ f%d | ship @ %d %d'
                      % (px, py, pz, f, x8 + _shx, _shz))
        # the CLOSE-UPS (2715-2842) want her half a body LOWER: deeper into the
        # landing at 2715, held low -- then SNAP back at 2842 (nothing is visible
        # there; a fade would read as ghosting up)
        if f < 2842:
            py -= 600.0 * smo01((f - 2703) / 24.0)
        if f in (2782, 2906, 2958):
            _kicks[f] = (int(px), int(py), int(pz))
        # MOVE+TURN together on EVERY frame: the old odd-frame stagger (a reimport
        # merge dodge) alternated the channels in-game -- move, rotate, move, rotate
        # -- plainly visible on the fast roll. The merge only eats pairs in the
        # EDITOR's reimport; the baked demo applies both.
        sc['effects'].append(sem('teaser_grab_t_%d' % f, f, 8443880, 2,
                                 to_game_rot(rx, ry_, rz_), raw=True))
        sc['effects'].append(sem('teaser_grab_m_%d' % f, f, 8443880, 3, (int(px), int(py), int(pz)), raw=True))
    # gone under the cut (putobjs persist across eras otherwise)
    sc['effects'].append(sem('teaser_grab_off', ORB1 - 1, 8443880, 0, raw=True))

    tracks['her_kicks'] = _kicks

    # bow track for the beaching dirt: blur_rig lays the trail along this.
    # World pos + unit motion dir, every 2 screen frames across the beach.
    _bt = {}
    for _f in range(2538, 2914, 2):
        _ax, _az = ship_xz_show(_f)
        _bx2, _bz2 = ship_xz_show(_f + 2)
        _dx, _dz = _bx2 - _ax, _bz2 - _az
        _dl = math.hypot(_dx, _dz) or 1.0
        _bt[_f] = (x8 + _ax, _az, _dx / _dl, _dz / _dl)
    tracks['bow_track'] = _bt

    sc['effects'].append(sem('teaser_set_city_near', 2180, 7966984, 3, (-25000, 1000, -500000)))
    sc['effects'].append(sem('teaser_set_sol_near', 2180, SOLC, 3, (-102100, 240, -340500)))

    sc['effects'].append(sem('teaser_sw_intact_off', 3138, BRKC, 0))    # the intact city (brooklyn model) off @ breach
    sc['effects'].append(sem('teaser_sw_breach_on', 3138, 7974536, 1))     # vw60a_7 (breach) on @ breach
    sc['effects'].append(sem('teaser_sw_breach_off', 3311, 7974536, 0))    # vw60a_7 (breach) off @ wreck
# the SIDE destruction view wears the HOLES wreck; the plain wreck only
    # takes over at the overhead cut (both ride the same T() era map)
    HOLES = 2416758   # strcode('vw60a_9_holes')
    sc['effects'].append(sem('teaser_sw_holes_on', 3311, HOLES, 1))
    sc['effects'].append(sem('teaser_sw_holes_off', 3509, HOLES, 0))
    sc['effects'].append(sem('teaser_sw_wreck_on', 3509, 14038570, 1))     # vw60a_9_crash (our wreck) on @ overhead
    # THE WRECK CHUNKS far2/far3: knocked outward by Arsenal's ram, tilting
    # BACK onto their rests through the closing shots. Tilt axes derived from
    # the look-at Arsenal(-25866,-512921) -> each chunk (small-angle eulers:
    # (rx,rz) = theta*(-vz, vx)); far3 additionally starts displaced TOWARD
    # Arsenal and slides home as it rights itself.
    # THE WRECK CHUNKS: far2 tilts back in place; far3's splits a/b/c ride in
    # displaced toward Arsenal and right themselves, staggered b -> c -> a.
    # NOTE: no raw= on any sem here -- OLD-time frames through T(), the same
    # era map as teaser_sw_wreck_on, so the chunks are structurally locked to
    # the wreck world (ON with the swap, dead at the overhead cut).
    _BK = 0
    FAR2 = 1568653   # strcode('vw60a_9_far2')
    _F2T = (-48, -31)
    sc['effects'].append(sem('teaser_far2_on', 3311 - _BK, FAR2, 1))
    sc['effects'].append(sem('teaser_far2_off', 3509 - _BK, FAR2, 0))
    _SPLITS = (
        # (chunk code, skin code, offset(x,z), move-start OLD-frame)
        # travel placed by eye: NW diagonal off the hull's flank; each skin
        # mirrors its solid twin exactly. Stagger is on MOTION, not visibility.
        # (chunk, skin, offset, move-start, shake seed, shake amp)
        # a: half the journey, later start -- it barely settles. Shakes share
        # one harmonic (same vnoise smoothing) with per-chunk seeds/amps:
        # buildings scoured across the earth, each grinding its own way
        (12469409, 10994391, (2563.9, 2382.5), 3321, 8101, 120.0),   # b leads
        (12469410, 10994393, (641.0, 595.6), 3401, 8171, 110.0),    # c: 25% travel, late
        (12469408, 10994389, (769.2, 714.8), 3366, 8241, 65.0),     # a: 30% travel, tight
    )
    for _sc_, _sk_, _so_, _s0_, _sd_, _amp_ in _SPLITS:
        for _cc_ in (_sc_, _sk_):
            sc['effects'].append(sem('teaser_f3s_on_%d' % _cc_, 3311, _cc_, 1))
            sc['effects'].append(sem('teaser_f3s_off_%d' % _cc_, 3509, _cc_, 0))
        for _nf in range(3313, 3512, 2):
            _nu = max(0.0, min(1.0, (_nf - _s0_) / (3510.0 - _s0_)))
            _ne = _nu * _nu * (3.0 - 2.0 * _nu)
            # HOLD displaced from ON (all visible in their offsets when the
            # flash clears); the SHAKE alone waits for each chunk's cue --
            # gating the offset too made them teleport out at their cues
            _hold = 1.0 - _ne
            _shk = _hold if _nf >= _s0_ else 0.0
            _px_ = int(-25000.0 + _so_[0] * _hold + _amp_ * _shk * vnoise_g(_nf, _sd_, 7))
            _py_ = int(1000.0 + 0.3 * _amp_ * _shk * vnoise_g(_nf, _sd_ + 1, 7))
            if _sc_ == 12469408:
                # building a: TIGHT shaker -- fast vertical hammer (the camera
                # bucks against this noise, aquake) + fast compact x/z rattle
                _py_ += int(90.0 * _shk * vnoise_g(_nf, _sd_ + 5, 3))
                _px_ += int(45.0 * _shk * vnoise_g(_nf, _sd_ + 6, 3))
            if _sc_ == 12469409 and _nf >= _s0_:
                # building b: the settle bounce -- an up-down wave (violently
                # shoved, tripping over itself), lessening as the slide stops
                _bw_ = math.sin((_nf - _s0_) * math.pi / 11.0)
                _bi_ = 0.75 + 0.25 * vnoise_g(_nf, _sd_ + 8, 9)
                _bt_ = (0.30 + 0.70 * _hold) * min(1.0, (3511 - _nf) / 30.0)
                _py_ += int(150.0 * _bw_ * _bi_ * _bt_)
            _pz_ = int(-500000.0 + _so_[1] * _hold + _amp_ * _shk * vnoise_g(_nf, _sd_ + 2, 7))
            sc['effects'].append(sem('teaser_f3s_m_%d_%d' % (_sc_, _nf), _nf, _sc_, 3,
                                     (_px_, _py_, _pz_)))
            sc['effects'].append(sem('teaser_f3s_ms_%d_%d' % (_sk_, _nf), _nf, _sk_, 3,
                                     (_px_, _py_, _pz_)))
    # the topple-able stand-in, a PUT-OBJECT like the states above (a demo doll cannot draw out
    # here -- see the id-5 note below). Created hidden by teaser_smash_set in the world build;
    # w60a_smash's strcode is its cache id, 0x7fabdc.
    # ---- THE TOPPLE: the bow shoves the tower over. Base-centred model + OBJ_TURN/OBJ_MOVE
    # hard-sets every 2nd frame: tilt about X (top swings -Z, the bow's travel direction),
    # ease-in-quad so it hangs then goes; sink into the rubble as it falls. Ends just before
    # the wreck swap hides it.
    def lightstyle(tag, code, frame, style, rate=6, end_state=0):
        """Quake lightstyle -> putobj ON/OFF stream. 'a'=dark..'z'=bright, 'm'=normal,
        ticking every `rate` frames (6 = Quake's 10Hz). Binary here: < 'g' is OFF, else ON.
        Emits only on state CHANGES so the packet count stays lean. end_state 0=dark 1=lit."""
        cur = None
        f = frame
        for ch in style:
            want = 0 if ch < 'g' else 1
            if want != cur:
                sc['effects'].append(sem('ls_%s_%d' % (tag, f), f, code, want))
                cur = want
            f += rate
        if cur != end_state:
            sc['effects'].append(sem('ls_%s_end' % tag, f, code, end_state))

    # THE FIVE FALLS. Each tower: (tag, strcode, base -p, T0, T1, D, R) where D = total
    # displacement (putobj-frame units, x10 world) and R = final TURN in 4096ths, both ramped
    # ease-in-quad. Screen terms from the pulled-back breach cam: right = +X, toward camera = -Z.
    # Falls before the f3138 city swap start against the intact original (reads as straining).
    TOWERS = [
        # shoreward pair leads (bow reaches them first)
        ('smash2', 16088001, (-47795.0, -4200.0, -401575.0), 3100, 3257,
         (0, -1400, 0),     (-620, 0, 0)),     # tilt toward the plow line, sink
        ('smash5', 16088004, (-71985.0, -5950.0, -407295.0), 3245, 3298,
         (0, -1800, 0),     (-820, 0, 0)),     # the little one: crumples as the flank arrives --
                                               # deeper sink + past-vertical tilt so the fall
                                               # reads clearly before the side-shot cut
        # tower 1 on the flash beat
        ('smash',  8367068,  (-51425.0, -4200.0, -404605.0), 3146, 3305,
         (0, -1400, 0),     (-620, 0, 0)),
        # tower 1's WINDOW LIGHT layer: separate putobj sharing the exact footing + ramp so it
        # stays registered with the body; the Quake lightstyle drives THIS, not the body
        ('smash_lw', 15714764, (-51425.0, -4200.0, -404605.0), 3146, 3305,
         (0, -1400, 0),     (-620, 0, 0)),
        # aftershocks: the bow shoulders these aside to its left (screen right)
        ('smash3', 16088002, (-55865.0, -9950.0, -409545.0), 3166, 3280,
         (6000, -2500, -4000), (0, 0, 150)),   # BULLDOZED: shoved +X/-Z, dropped, slight trip
        ('smash4', 16088003, (-67050.0, -10820.0, -411855.0), 3166, 3280,
         (5000, -2000, 0),  (0, 0, -400)),     # shoved right AND toppling that way (+X: rz NEGATIVE, right-handed)
        # tower 6: legs punched out -- a hard vertical drop with a lean back TOWARD the
        # Arsenal (+Z, into its killer), a little after 1 and 2
        # tower 7: the subtle one -- no tilt at all, a slow straight settle into the
        # ground starting ~30f before the side-view cut, still sinking as the shot ends
        ('smash7', 16088006, (-43860.0, -4200.0, -403920.0), 3255, 3308,
         (0, -2600, 0),     (140, 0, -60), 'smooth', 'late'),
        ('smash7_lw', 15008948, (-43860.0, -4200.0, -403920.0), 3255, 3308,
         (0, -2600, 0),     (140, 0, -60), 'smooth', 'late'),   # its window layer, same ramp  # heavy settle; in the dying
                                               # frames a slight lean toward the water (+Z)
                                               # and a touch screen-right (rz-)
        ('smash6', 16088005, (-54800.0, -16450.0, -420395.0), 3225, 3308,
         (0, -5000, 0),     (-150, 0, 260)),   # legs punched: drops, leaning 45deg left of the
                                               # Arsenal's forward vector (toward camera + screen-left)
    ]
    # tower 1's WINDOWS fall under a Quake lightstyle (the light layer is its own putobj
    # riding the same ramp): 10Hz, ends DARK as the tower settles. The body stays solid.
    lightstyle('smash_lw', 15714764, 3146, 'mmamammmmammamamaaamammma', rate=6, end_state=0)
    # tower 7's windows run Quake's torch flicker from just after the flash, guttering all
    # through the scene, dying dark right as its settle completes
    lightstyle('smash7_lw', 15008948, 3160, 'mmnmmommommnonmmonqnmmo', rate=6, end_state=0)

    # THE TREE (w60a_tree1): jostled during the CLINTON SHOT (2545-2762) -- the fort's trees
    # are first in line as the prow looms in over the embankment. Physically the bow is still
    # offshore; dramatically it's on top of them, and the shot cuts before the lie shows.
    # A shove-aside, not a yeet: small hop, pushed along the heading, keeling ~46deg, smack ease.
    _TP = (-44930.0, -3780.0, -383120.0)
    _DIR = (-0.2588, -0.9659)
    _HOP, _PUSH, _TDUR = 800.0, 3200.0, 58
    for _f in range(2740, 2740 + _TDUR + 1, 2):
        _t = (_f - 2740) / float(_TDUR)
        _e = 1.0 - (1.0 - _t) * (1.0 - _t)              # smack: fast, then settling
        _y = _TP[1] + _HOP * math.sin(math.pi * min(1.0, _t * 1.15))
        _x = _TP[0] + _DIR[0] * _PUSH * _e
        _z = _TP[2] + _DIR[1] * _PUSH * _e
        sc['effects'].append(sem('teaser_tree_turn_%d' % _f, _f, 9583077, 2,
                                 (int(-520 * _e), 0, int(140 * _e))))
        sc['effects'].append(sem('teaser_tree_move_%d' % _f, _f, 9583077, 3,
                                 (int(_x), int(_y), int(_z))))

    # THE GROVE (tree2-6, sharing tree1's model): scattered through the embankment zone;
    # each is picked up as the bow line reaches it -- carried along the heading, lifted on
    # the wake, keeling as it goes. Debris riding the land bow-wave.
    # (name, code, px, pz, hit, ground_y): tree7-9 sit on b5's higher terrace, arsenal side --
    # their ride masks building 5's crumple (3245-3298) as they join the wake
    _GROVE = [('w60a_tree1r', 9583077, -45758, -386211, 3066, -3780.0, None, None, None),
              ('w60a_tree2', 9583078, -54400, -385689, 3080, -3780.0, None, None, None),
              ('w60a_tree3', 9583079, -60863, -387788, 3091, -3780.0, None, None, None),
              ('w60a_tree4', 9583080, -54699, -393373, 3121, -3780.0, None, None, None),
              ('w60a_tree5', 9583081, -61671, -395439, 3132, -3780.0, None, None, None),
              ('w60a_tree6', 9583082, -60034, -400536, 3159, -3780.0, None, None, None),
              ('w60a_tree7', 9583083, -63957, -403904, 3215, -3448.0, -0.562, -0.827, 12245.0),
              ('w60a_tree8', 9583084, -64857, -405004, 3221, -3448.0, -0.562, -0.827, 12245.0),
              ('w60a_tree9', 9583085, -63157, -403104, 3227, -3448.0, -0.562, -0.827, 12245.0)]
    _GDIR = (-0.2588, -0.9659)
    for _gnm, _gcode, _gpx, _gpz, _ghf, _gby, _gdx, _gdz, _gdist in _GROVE:
        # HEAVE: as the bow rises onto the shore (~3126, with b2 collapsing), the ground
        # bulges under the not-yet-hit trees nearest the hull -- they lift with Arsenal
        if 3128 < _ghf < 3180:
            for _f in range(3126, _ghf - 1, 2):
                _h = 650.0 * min(1.0, (_f - 3126) / 22.0)
                sc['effects'].append(sem('teaser_%s_heave_%d' % (_gnm, _f), _f, _gcode, 3,
                                         (_gpx, int(_gby + _h), _gpz)))
        for _f in range(_ghf, 3306, 2):
            _tf = _f - _ghf
            _t1 = min(1.0, _tf / 60.0)
            _e = 1.0 - (1.0 - _t1) * (1.0 - _t1)          # smack phase
            if _gdist is None:
                _d = 5500.0 * _e + 45.0 * max(0, _tf - 60)
                _ddx, _ddz = _GDIR
            else:
                _tw = min(1.0, _tf / float(3305 - _ghf))
                _d = _gdist * (1.0 - (1.0 - _tw) * (1.0 - _tw))
                _ddx, _ddz = _gdx, _gdz
            _y = _gby + 900.0 * math.sin(math.pi * min(1.0, _t1 * 1.2)) * (1.0 if _tf < 60 else 0.0)                  + 260.0 * abs(math.sin(_tf / 9.0)) * (0.0 if _tf < 60 else 1.0)
            if _ghf < 3180:
                # riding ON the hull as it beaches: take the hull's own ramp, or they sink
                # into it whenever the climb is retimed
                _y += 3413.0 * min(1.0, max(0, _f - BEACH0) / float(BEACH_LEN))
            _rx = -420 * _e - min(180, 2 * max(0, _tf - 60))
            sc['effects'].append(sem('teaser_%s_turn_%d' % (_gnm, _f), _f, _gcode, 2,
                                     (int(_rx), 0, int((90 if _gpx % 2 else -140) * _e))))
            sc['effects'].append(sem('teaser_%s_move_%d' % (_gnm, _f), _f, _gcode, 3,
                                     (int(_gpx + _ddx * _d), int(_y),
                                      int(_gpz + _ddz * _d))))

    for _tw in TOWERS:
        _tag, _code, _P, T0, T1, _D, _R = _tw[:7]
        _ease = _tw[7] if len(_tw) > 7 else 'quad'
        _rease = _tw[8] if len(_tw) > 8 else _ease
        on_f = 3098 if T0 <= 3110 else 3138    # early fallers must exist before the swap
        sc['effects'].append(sem('teaser_%s_on' % _tag, on_f, _code, 1))
        sc['effects'].append(sem('teaser_%s_off' % _tag, 3311, _code, 0))
        for _f in range(T0, T1 + 1, 2):
            _t = (_f - T0) / float(T1 - T0)
            # quad = hangs-then-goes (topples); smooth = eases in AND decelerates to rest
            # (weight); late = cubic-in, all the motion crowds into the dying frames
            def _ez(name, t):
                if name == 'smooth': return t * t * (3 - 2 * t)
                if name == 'late':   return t * t * t
                return t * t
            _e = _ez(_ease, _t)
            _er = _ez(_rease, _t)
            sc['effects'].append(sem('teaser_%s_turn_%d' % (_tag, _f), _f, _code, 2,
                                     (int(round(_R[0] * _er)), int(round(_R[1] * _er)),
                                      int(round(_R[2] * _er)))))
            sc['effects'].append(sem('teaser_%s_move_%d' % (_tag, _f), _f, _code, 3,
                                     (int(_P[0] + _D[0] * _e), int(_P[1] + _D[1] * _e),
                                      int(_P[2] + _D[2] * _e))))
    # (the old id-5 Raiden hijack is GONE: demo dolls never render in the extended region -- the
    #  stand-in is the w60a_smash PUT-OBJECT above, and Raiden keeps his own model + motion.)
    sc['effects'].append(sem('teaser_sw_hull_on', 3509, 12197025, 1))      # arsenal on 1f early: lands under the overhead cut (putobj latency law)

    # ---- landfall destruction (3140-3399): the two LIVE MesgBomb3 spawner actors sit
    # on bow control points that ride the hull -- every message spawns one effect there
    # THAT frame (mesg_bomb3.c). cmds: 7 SPHERE_SPLUSH, 1 FLYING_SMK, 2 BOMB_GAS,
    # 3 CRUSH_FORCE, 0 BOMB_EFT. All spawned functions are direct-linked, not launchers.
    ACTORS = (5683027, 13914731)

    def boom(uid, frame, name, cmd, args):
        frame = T(frame)
        zero = {'d%d' % i: K(frame, 0) for i in range(1, 9)}
        inst = {
            'uid': uid, 'effectId': 16809984, 'execType': 0, 'packetId': 0,
            'start': frame, 'end': frame + 1, 'label': 'mesg(bomb3)',
            'dirty': True, 'added': True,
            'params': dict(d0=K(frame, name), **zero,
                           d9=K(frame, cmd), d10=K(frame, 1 + len(args))),
        }
        for i, v in enumerate(args):
            inst['params']['d%d' % (i + 1)] = K(frame, v)
        return inst

    # settle-to-sideview spray: extra WAVE_SPLUSH2 per frame per live spawner from the
    # fort hover until the side-view cut -- the bow churns ~2.5x harder
    # (custom spray boosts removed: the hull spray is the STOCK intro streams only)

    nfx = 0
    for f in (3140, 3150, 3160):                    # the bow hits: water eruptions
        for who in ACTORS:
            sc['effects'].append(boom('tsr_sp%d' % nfx, f, who, 7, [1500, 20, 100])); nfx += 1
    # VISIBLE chaos only: this stage renders water, fog and screen flashes -- the
    # smoke/gas/bomb spawn classes have no resident textures here (proven: only the
    # splashes ever showed). Debris-bursts + strike flashes + fog pall instead.
    for f in range(3150, 3309, 22):                 # eruptions as blocks are torn
        r = 1400 + (f * 7) % 1200
        sc['effects'].append(boom('tsr_eb%d' % nfx, f, ACTORS[nfx % 2], 7, [r, 24, 110])); nfx += 1
    for f in (3350, 3430, 3510):                    # settling bursts on the glide
        sc['effects'].append(boom('tsr_st%d' % nfx, f, ACTORS[nfx % 2], 7, [900, 12, 80])); nfx += 1
    for f in (3592, 3597):                          # foam around the resting hull
        sc['effects'].append(boom('tsr_fo%d' % nfx, f, ACTORS[nfx % 2], 7, [2200, 16, 90])); nfx += 1
    for f, amp2 in ((3125, 50), (3220, 58)):   # strike flashes; #3 cut -- it double-pumped the shot-B transition
        sc['effects'].append(fade('tsr_flu%d' % nfx, f, 0, amp2, 2)); nfx += 1
        sc['effects'].append(fade('tsr_fld%d' % nfx, f + 2, amp2, 0, 5)); nfx += 1

    # ---- handheld: Konami's demos carry a subtle scncamvib-style sway; our authored
    # shots are dead rails. Two layers of smooth value-noise, aim wobble scaled by
    # target DISTANCE so every shot sways the same ~0.1 degree regardless of framing.
    def interp_keys(ks, f):
        lo = hi = None
        for k in ks:
            if k['frame'] <= f:
                lo = k
            if k['frame'] >= f and hi is None:
                hi = k
        if lo is None:
            return hi['value']
        if hi is None or hi['frame'] == lo['frame']:
            return lo['value']
        t = (f - lo['frame']) / (hi['frame'] - lo['frame'])
        return lo['value'] + t * (hi['value'] - lo['value'])

    def vnoise(f, seed, block):
        k, t = divmod(f, block)
        return irnd(k + seed) * (1 - t / block) + irnd(k + 1 + seed) * (t / block)

    def sway(f, seed):
        return 0.72 * vnoise(f, seed, 22) + 0.28 * vnoise(f, seed + 5000, 6)

    def impact(f, seed, scale):
        # the MOUNT scour: the tour camera takes every bounce as the wing grinds her island
        if MNT0 <= f < MNTD + 40:
            env = 2.6 * min(1.0, (f - MNT0) / 10.0)
            if f >= MNT1:
                # splash-down slam, decaying through the settle
                env = 3.0 * max(0.0, 1.0 - (f - MNT1) / float(MNTD + 40 - MNT1))
            return scale * env * vnoise(f, seed, 3)
        # bridge-passage rattle at 1.5x+: slams full as the hull mounts the island,
        # holds through the grind, decays toward the calm side-view cut. Coarse blocks
        # survive the afterimage blur.
        if not (T(3124) <= f < T(3310)):
            return 0.0
        # swells as the bow rears out of the water (just before the 3138 flash), peaks
        # ~35% harder through the beaching, settles to the floor by ~3288 -- right as
        # building 5 finishes its crumple
        env = 3.2 * min(1.0, (f - T(3124)) / 16.0)
        if f >= T(3230):
            env *= max(0.09, 1.0 - (f - T(3230)) / 58.0)
        return scale * env * vnoise(f, seed, 3)

    def hhamp(f):
        # where the sway belongs: heavy on the approach/impact shots, calm-ish on the
        # second-to-last glide (shot B), normal elsewhere
        if TK0 <= f < TK1:
            return tk_hh(f)
        if TK1 <= f < ORB1:
            t_ = (f - TK1) / float(ORB1 - TK1)
            return 1.8 + 2.2 * t_ * t_       # intensifying as the leviathan closes in
        if T(3310) <= f < T(3510):
            return 2.2                       # the rise over the grind: shaken, not calm
        if T(2545) <= f < T(3050):
            return 3.6
        return 1.6 if f < T(2760) else 1.0

    def aquake(f):
        # building a's fast grind-bounce, OPPOSED: same noise stream (seed 8246,
        # smoothing 3, era-shifted), negated -- the frame bucks against the
        # building so the eye loses the details and feels the chaos
        A0, A1 = T(3391), T(3510)
        if not (A0 <= f < A1):
            return 0.0
        env = min(1.0, (f - A0) / 20.0) * min(1.0, (A1 - f) / 20.0)
        return -55.0 * env * vnoise_g(f - 271, 8246, 3)

    # HH0 must cover the dive: this pass REBUILDS pos/trg keys over [HH0, HH1) and
    # DISCARDS anything earlier -- keys below HH0 silently vanish
    HH0, HH1 = TK0, END
    base = {}
    for chn in ('posX', 'posY', 'posZ', 'trgX', 'trgY', 'trgZ'):
        base[chn] = [interp_keys(sorted(cam[chn]['keys'], key=lambda k: k['frame']), f)
                     for f in range(HH0, HH1)]
    ANG = 0.0019   # ~0.11 deg peak aim sway
    for i, chn in enumerate(('posX', 'posY', 'posZ')):
        vals = []
        for f in range(HH0, HH1):
            vals.append({'frame': f, 'value': base[chn][f - HH0] + 20.0 * hhamp(f) * sway(f, 977 + i * 6113)
                         + impact(f, 555 + i * 77, 260.0)
                         + (aquake(f) if chn == 'posY' else 0.0),
                         'interp': 'linear'})
        cam[chn]['keys'] = vals
        cam[chn]['dirty'] = True
    for i, chn in enumerate(('trgX', 'trgY', 'trgZ')):
        vals = []
        for f in range(HH0, HH1):
            j = f - HH0
            dx = base['trgX'][j] - base['posX'][j]
            dy = base['trgY'][j] - base['posY'][j]
            dz = base['trgZ'][j] - base['posZ'][j]
            dist = (dx * dx + dy * dy + dz * dz) ** 0.5
            vals.append({'frame': f, 'value': base[chn][j] + dist * ANG * hhamp(f) * sway(f, 40427 + i * 7211)
                         + impact(f, 90210 + i * 331, dist * 0.014),
                         'interp': 'linear'})
        cam[chn]['keys'] = vals
        cam[chn]['dirty'] = True

    # the graze rewrites STOCK-region motion; without this the baker passes
    # Konami's frames through byte-exact and the ship SNAPS onto the curve at 2180
    sc['motionDirty'] = True

    return tracks
