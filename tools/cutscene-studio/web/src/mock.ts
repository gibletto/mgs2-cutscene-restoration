// A plausible 2,180-frame demo so the whole UI is demonstrable with no Go server running.
// Shapes match internal/scene exactly; the effect ids are real catalog ids with their real exec types.

import type { Caption, Channel, DemoEntry, EffectInstance, Marker, ObjectTrack, Scene } from './types'

const FRAMES = 2180

const baked = (fn: (f: number) => number, n = FRAMES): Channel => {
  const a = new Array<number>(n)
  for (let f = 0; f < n; f++) a[f] = fn(f)
  return { baked: a, dirty: false }
}

const konst = (v: number): Channel => baked(() => v)

// Three shots: a slow dolly along the deck, a hard cut to a low angle, a crane up.
function cameraPos(axis: 0 | 1 | 2) {
  return (f: number) => {
    if (f < 900) {
      const t = f / 900
      return [(-9000 + 14000 * t), (1750 + 260 * Math.sin(t * 5.2)), (60500 + 5200 * t)][axis]
    }
    if (f < 1520) {
      const t = (f - 900) / 620
      return [(6200 - 1800 * t), (620 + 90 * t), (69000 + 900 * Math.sin(t * 3.1))][axis]
    }
    const t = (f - 1520) / (FRAMES - 1520)
    return [(1900 + 700 * t), (900 + 6400 * t), (70600 - 3800 * t)][axis]
  }
}

function cameraTrg(axis: 0 | 1 | 2) {
  return (f: number) => {
    if (f < 900) {
      const t = f / 900
      return [(1200 + 900 * t), (1500 - 120 * t), (67800 + 400 * t)][axis]
    }
    if (f < 1520) {
      const t = (f - 900) / 620
      return [(2050 + 60 * Math.sin(t * 6.0)), (1620 + 120 * t), (67500 - 300 * t)][axis]
    }
    const t = (f - 1520) / (FRAMES - 1520)
    return [2100, (1500 + 400 * t), (67200 + 200 * t)][axis]
  }
}

const CAST: Array<{ id: number; label: string; name: number; cacheId: number; kms: number; flag: number; joints: number; at: [number, number, number]; show: number; hide: number }> = [
  { id: 1, label: 'snake',    name: 0x5c1a3f, cacheId: 0x00a114, kms: 0x00a115, flag: 1, joints: 62, at: [1800, 0, 67600], show: 0, hide: FRAMES },
  { id: 2, label: 'otacon',   name: 0x2b7701, cacheId: 0x00a220, kms: 0x00a221, flag: 1, joints: 58, at: [2600, 0, 68100], show: 0, hide: FRAMES },
  { id: 3, label: 'olga',     name: 0x77ef10, cacheId: 0x00a331, kms: 0x00a332, flag: 1, joints: 61, at: [-400, 0, 66900], show: 420, hide: 1900 },
  { id: 4, label: 'guard_a',  name: 0x11aa02, cacheId: 0x00b001, kms: 0x00b002, flag: 1, joints: 55, at: [5400, 0, 70200], show: 900, hide: FRAMES },
  { id: 5, label: 'crate_01', name: 0x3300f1, cacheId: 0,        kms: 0x00c010, flag: 0, joints: 0,  at: [3900, 0, 69000], show: 0, hide: FRAMES },
  { id: 6, label: 'deck_gun', name: 0x3300f2, cacheId: 0,        kms: 0x00c044, flag: 2, joints: 0,  at: [-2400, 200, 71500], show: 0, hide: FRAMES },
  { id: 7, label: 'gull',     name: 0x4419aa, cacheId: 0x00d900, kms: 0x00d901, flag: 1, joints: 12, at: [0, 4200, 64000], show: 120, hide: 760 },
]

function makeObject(c: (typeof CAST)[number]): ObjectTrack {
  const wob = (i: number, amp: number, hz: number) => baked((f) => c.at[i] + amp * Math.sin((f / 60) * hz + c.id))
  return {
    id: c.id,
    name: c.name,
    nameStr: c.label,
    cacheId: c.cacheId,
    kmsCacheId: c.kms,
    flag: c.flag,
    label: c.label,
    visibility: [{ frame: c.show, visible: true }, ...(c.hide < FRAMES ? [{ frame: c.hide, visible: false }] : [])],
    posX: c.joints ? wob(0, 90, 0.6) : konst(c.at[0]),
    posY: c.label === 'gull' ? baked((f) => c.at[1] + 800 * Math.sin(f / 90)) : konst(c.at[1]),
    posZ: c.joints ? wob(2, 70, 0.4) : konst(c.at[2]),
    rotX: konst(0),
    rotY: baked((f) => (c.joints ? 2048 + 40 * Math.sin(f / 120) : 1024)),
    rotZ: konst(0),
    nJoints: c.joints,
    motionOk: c.joints > 0,
  }
}

const p = (v: number): Channel => ({ keys: [{ frame: 0, value: v, interp: 'hold' }], dirty: false })

function mockEffects(): EffectInstance[] {
  // 0x01000006 DM_ChangeAmbient and 0x01000007 DM_ChangeParallel are exec 0: per-frame channels.
  const ambient: EffectInstance = {
    uid: 'fx-ambient', effectId: 0x01000006, execType: 0, packetId: 0x10,
    start: 0, end: FRAMES, label: 'ambient', dirty: false,
    params: {
      'd0.r': baked((f) => 40 + 18 * Math.sin(f / 400)),
      'd0.g': baked((f) => 46 + 14 * Math.sin(f / 380)),
      'd0.b': baked((f) => 62 + 10 * Math.sin(f / 300)),
      'd0.a': konst(128),
    },
  }
  const parallel: EffectInstance = {
    uid: 'fx-parallel', effectId: 0x01000007, execType: 0, packetId: 0x11,
    start: 0, end: FRAMES, label: 'key light', dirty: false,
    params: {
      'd0.r': konst(150), 'd0.g': konst(140), 'd0.b': konst(120), 'd0.a': konst(128),
      'd1.x': baked((f) => Math.sin(f / 600) * 0.5 - 0.3),
      'd1.y': konst(-0.82),
      'd1.z': baked((f) => Math.cos(f / 600) * 0.5),
    },
  }
  // 0x0100000f NewLensFlare is exec 1: born once, life baked into the packet preamble.
  const flare: EffectInstance = {
    uid: 'fx-flare', effectId: 0x0100000f, execType: 1, packetId: 0x20,
    start: 210, end: 210 + 612, label: 'sun flare', dirty: false,
    params: { d0: p(0x00fe5a24), 'd1.x': p(-10014.33), 'd1.y': p(-8209.23), 'd1.z': p(70861.54) },
  }
  const fade: EffectInstance = {
    uid: 'fx-fade', effectId: 0x01000018, execType: 0, packetId: 0x12,
    start: 0, end: 90, label: 'fade in', dirty: false,
    params: { d0: baked((f) => Math.max(0, 255 - f * 3), 90) },
  }
  const blur: EffectInstance = {
    uid: 'fx-blur', effectId: 0x01000015, execType: 1, packetId: 0x21,
    start: 940, end: 1180, label: 'concentrate blur', dirty: false,
    params: { d0: p(240), 'd1.x': p(2050), 'd1.y': p(1600), 'd1.z': p(67500) },
  }
  const smoke: EffectInstance = {
    uid: 'fx-smoke', effectId: 0x0100d003, execType: 1, packetId: 0x22,
    start: 1560, end: 1560 + 300, label: 'missile shower', dirty: false,
    params: { 'd0.x': p(1800), 'd0.y': p(3200), 'd0.z': p(69800), d1: p(1500), d2: p(0.7), d3: p(1.0) },
  }
  const ctrl: EffectInstance = {
    uid: 'fx-ctrl', effectId: 0x010ffff0, execType: 0, packetId: 0x30,
    start: 940, end: 1180, label: 'blur focus point', dirty: false,
    params: {
      d0: p(0x2201), d1: konst(0), d2: konst(0), d3: konst(0),
      d4: baked((f) => 2050 + 300 * Math.sin(f / 40), 1180),
      d5: konst(1600), d6: konst(67500),
    },
  }
  return [ambient, parallel, fade, flare, blur, ctrl, smoke]
}

const CAPTIONS: Caption[] = [
  { start: 180, end: 420, lang: 1, name: 0x5c1a3f, text: 'Otacon, I am in position.|The tanker is dead ahead.' },
  { start: 460, end: 720, lang: 1, name: 0x2b7701, text: 'Copy that. Watch the deck --|the Marines are already aboard.' },
  { start: 980, end: 1260, lang: 1, name: 0x77ef10, text: 'You are late.' },
  { start: 1320, end: 1620, lang: 1, name: 0x5c1a3f, text: 'Traffic.' },
  { start: 1700, end: 2050, lang: 1, name: 0x2b7701, text: 'Snake -- something just launched|off the starboard rail!' },
]

const MARKERS: Marker[] = [
  { frame: 0, label: 'shot 1 -- deck dolly', color: '#3d7fbf' },
  { frame: 900, label: 'shot 2 -- low angle CUT', color: '#bf6a3d' },
  { frame: 1520, label: 'shot 3 -- crane up', color: '#54a06b' },
  { frame: 1980, label: 'audio ends', color: '#b04a5a' },
]

export function mockScene(): Scene {
  return {
    sourcePath: 'us/demo2/_bp/p028_01_p01.sdt',
    frames: FRAMES,
    fps: 60,
    camera: {
      posX: baked(cameraPos(0)), posY: baked(cameraPos(1)), posZ: baked(cameraPos(2)),
      trgX: baked(cameraTrg(0)), trgY: baked(cameraTrg(1)), trgZ: baked(cameraTrg(2)),
      screen: baked((f) => (f < 900 ? 2.717 : f < 1520 ? 3.709 : 2.159 + 1.2 * ((f - 1520) / 660))),
      roll: baked((f) => (f < 1520 ? 0 : 0.04 * Math.sin((f - 1520) / 200))),
    },
    objects: CAST.map(makeObject),
    effects: mockEffects(),
    captions: CAPTIONS,
    markers: MARKERS,
    hasAudio: true,
    audioFrames: 1980,   // the demo legally outruns its audio -- the clock falls back to vsync
  }
}

export const mockDemos = (): DemoEntry[] => [
  { name: 'p028_01_p01', path: 'us/demo2/_bp/p028_01_p01.sdt', frames: 2180, durationSec: 36.33, hasAudio: true, sizeBytes: 4_182_016 },
  { name: 'p001_03_p02', path: 'us/demo/_bp/p001_03_p02.sdt', frames: 1470, durationSec: 24.5, hasAudio: true, sizeBytes: 2_614_784 },
  { name: 'p080_05_p03', path: 'us/demo2/_bp/p080_05_p03.sdt', frames: 3320, durationSec: 55.33, hasAudio: false, sizeBytes: 1_998_336 },
]
