// Not shipped in the bundle: a node smoke test for the pure logic (math, sampling, mock scene).
// Run: npm run smoke (the catalog cross-check needs public/mock/effects.json -- npm run mock-data)
import { existsSync, readFileSync } from 'node:fs'
import * as THREE from 'three'
import { AX_CUTSCENE, AY_EFF_CUTSCENE, applyEngineBasis, engineProjection, hFov, quantiseRoll, screenFromHFov, vFov } from './src/lib/camera'
import { sample, setKey } from './src/lib/channel'
import { timecode } from './src/lib/format'
import { mockScene } from './src/mock'

let fails = 0
const deg = (r: number) => (r * 180) / Math.PI
function check(name: string, got: unknown, want: unknown, tol = 0) {
  const ok = typeof got === 'number' && typeof want === 'number'
    ? Math.abs(got - want) <= tol
    : JSON.stringify(got) === JSON.stringify(want)
  if (!ok) { fails++; console.log(`FAIL ${name}: got ${got}, want ${want}`) }
  else console.log(`ok   ${name} = ${typeof got === 'number' ? (got as number).toFixed(5) : got}`)
}

// cutscene regime (ax=1.0, ay_eff=1.70819) at screen=2 -> hFOV 53.13, vFOV 32.63
check('ax', AX_CUTSCENE, 1.0)
check('ay_eff', AY_EFF_CUTSCENE, 1.70819, 1e-5)
check('ay_eff from ratio 1.0', 1.121 * (1 + 1 / 3) * (512 / 448), 1.70819, 1e-4)
check('hFOV @ screen=2', deg(hFov(2)), 53.13, 0.01)
check('vFOV @ screen=2', deg(vFov(2)), 32.63, 0.01)
check('gameplay hFOV @ screen=2 (ax .75)', deg(hFov(2, 0.75)), 67.38, 0.01)
check('screen round-trip from hFOV', screenFromHFov(hFov(2.717)), 2.717, 1e-9)
check('median shipped screen (2.717) -> hFOV', deg(hFov(2.717)), 40.4126, 1e-3)
check('roll quantum (deg)', deg((2 * Math.PI) / 4096), 0.087890625, 1e-9)
check('roll quantise 0.5 rad -> 325 steps', quantiseRoll(0.5), 325 * ((2 * Math.PI) / 4096), 1e-12)

// engine basis: looking down +Z, x = (0,-1,0) x z = (-1,0,0); three basis = (x, -y, -z)
const o = new THREE.Object3D()
applyEngineBasis(o, {
  pos: new THREE.Vector3(0, 0, 0), trg: new THREE.Vector3(0, 0, 100), screen: 2, roll: 0,
})
const fwd = new THREE.Vector3(0, 0, -1).applyQuaternion(o.quaternion)
check('three forward == engine +Z', [+fwd.x.toFixed(4), +fwd.y.toFixed(4), +fwd.z.toFixed(4)], [0, 0, 1])
const right = new THREE.Vector3(1, 0, 0).applyQuaternion(o.quaternion)
check('screen-right == world -X', [+right.x.toFixed(4), +right.y.toFixed(4), +right.z.toFixed(4)], [-1, 0, 0])
const upv = new THREE.Vector3(0, 1, 0).applyQuaternion(o.quaternion)
check('screen-up == world +Y', [+upv.x.toFixed(4), +upv.y.toFixed(4), +upv.z.toFixed(4)], [0, 1, 0])

// a point at the exact frustum edge must land on ndc.x = 1
const P = engineProjection(2.717, 20, 600000)
const d = 5000
const edge = new THREE.Vector4(d / (AX_CUTSCENE * 2.717), 0, -d, 1).applyMatrix4(P)
check('right frustum edge -> ndc.x', edge.x / edge.w, 1, 1e-6)
const edgeY = new THREE.Vector4(0, d / (AY_EFF_CUTSCENE * 2.717), -d, 1).applyMatrix4(P)
check('top frustum edge -> ndc.y', edgeY.y / edgeY.w, 1, 1e-6)

// channel sampling
const s = mockScene()
check('mock frames', s.frames, 2180)
check('mock fps', s.fps, 60)
check('mock objects', s.objects.length, 7)
check('mock effects', s.effects.length, 7)
check('camera baked samples', s.camera.posX.baked!.length, 2180)
check('audio ends before the demo does', s.audioFrames < s.frames, true)
check('timecode(2179)', timecode(2179), '00:36:19')

const ch = setKey(setKey({ dirty: false }, 0, 0, 'linear'), 100, 100, 'linear')
check('linear midpoint', sample(ch, 50), 50, 1e-9)
const hold = setKey(setKey({ dirty: false }, 0, 0, 'hold'), 100, 100, 'linear')
check('hold midpoint', sample(hold, 50), 0)
const sm = setKey(setKey({ dirty: false }, 0, 0, 'smooth'), 100, 100, 'linear')
check('smooth midpoint', sample(sm, 50), 50, 1e-9)
check('smooth quarter (smoothstep)', sample(sm, 25), 15.625, 1e-6)

// keys override baked; outside the keyed span the imported bake still wins
const mixed = setKey({ baked: Array.from({ length: 10 }, (_, i) => i * 10), dirty: false }, 5, 999, 'linear')
check('key wins at its frame', sample(mixed, 5), 999)
check('baked wins outside the keyed span', sample(mixed, 9), 90)

// effect exec types come from the catalog, never invented
const flare = s.effects.find((e) => e.uid === 'fx-flare')!
check('NewLensFlare exec type', flare.execType, 1)
check('NewLensFlare life', flare.end - flare.start, 612)
const amb = s.effects.find((e) => e.uid === 'fx-ambient')!
check('DM_ChangeAmbient exec type', amb.execType, 0)
check('DM_ChangeAmbient spans the demo', amb.end - amb.start, 2180)

// catalog cross-check: exec types and param offsets from the real 245-entry file
type Def = { hexid: string; effectId: number; execType: number; dataBaseOffset: number; packetBodySize: number; dataStructSize: number; params: { name: string; byteOffset: number; byteSize: number }[] }
const catalogPath = 'public/mock/effects.json'
if (!existsSync(catalogPath)) {
  console.log(`skip catalog cross-check: ${catalogPath} not found (generate the data, then npm run mock-data)`)
} else {
  const cat = JSON.parse(readFileSync(catalogPath, 'utf8')) as Def[]
  check('catalog size', cat.length, 245)
  check('exec 0 count', cat.filter((e) => e.execType === 0).length, 23)
  check('exec 1 count', cat.filter((e) => e.execType === 1).length, 222)
  const byId = new Map(cat.map((e) => [e.effectId, e]))
  for (const e of s.effects) {
    const def = byId.get(e.effectId)
    if (!def) { fails++; console.log(`FAIL mock effect ${e.effectId.toString(16)} not in catalog`) }
    else if (def.execType !== e.execType) { fails++; console.log(`FAIL exec mismatch on ${def.hexid}`) }
  }
  console.log(`ok   all ${s.effects.length} mock effects resolve in the catalog with matching exec types`)
  // packet = 32 + (16 if exec 1) + roundup16(sizeof(Data)).
  // packetBodySize excludes the 16-byte DEMO_PACKET header, so it is that minus 16.
  const body = (e: Def) => 16 + (e.execType === 1 ? 16 : 0) + Math.ceil(e.dataStructSize / 16) * 16
  const bad = cat.filter((e) => e.packetBodySize !== body(e))
  check('packet-size formula holds across all 245', bad.length, 0)
  check('DM_ControlChange total packet size', byId.get(0x010ffff0)!.packetBodySize + 16, 64)
  check('NewLensFlare total packet size', byId.get(0x0100000f)!.packetBodySize + 16, 80)
  const holes = cat.filter((e) => e.params.some((p, i) => p.name !== `d${i}`))
  console.log(`ok   ${holes.length}/245 effects have non-contiguous d-indices -- the UI keys on p.name, never on the index`)
}

// perf: the timeline resamples every visible channel once per pixel column, every repaint.
// Worst realistic case from the corpus: a 19,978-frame demo, fully keyed, ~1,600 columns.
{
  const N = 19978
  const keys = Array.from({ length: N }, (_, i) => ({ frame: i, value: Math.sin(i / 60), interp: 'linear' as const }))
  const kch = { keys, dirty: false }
  const t0 = performance.now()
  let acc = 0
  for (let rep = 0; rep < 20; rep++) for (let x = 0; x < 1600; x++) acc += sample(kch, Math.round((x / 1600) * N))
  const ms = (performance.now() - t0) / 20
  console.log(`ok   resample 1,600 columns of a ${N}-key channel: ${ms.toFixed(4)} ms/repaint (acc ${acc.toFixed(1)})`)
  check('one channel repaint stays well inside a 16.6 ms frame', ms < 2, true)
}

console.log(fails ? `\n${fails} FAILURES` : '\nALL PASS')
process.exit(fails ? 1 : 0)
