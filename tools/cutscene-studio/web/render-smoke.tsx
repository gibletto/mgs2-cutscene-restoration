// Not shipped: renders the panels through react-dom/server, then pushes ALL 245 catalog effects
// through the generated properties panel. The canvas/WebGL effects never fire under SSR, so this
// proves the render paths and the catalog->widget mapping, not the paint.
//
// zustand v5 serves useSyncExternalStore's SERVER snapshot from api.getInitialState(), so setState
// is invisible here -- we mutate that initial-state object directly instead.
import { existsSync, readFileSync } from 'node:fs'
import { renderToString } from 'react-dom/server'
import { EffectBrowser } from './src/components/EffectBrowser'
import { Properties } from './src/components/Properties'
import { TransportBar } from './src/components/TransportBar'
import { mockScene } from './src/mock'
import { paramChannels, useStore } from './src/store'
import type { EffectDef, EffectInstance, Scene } from './src/types'

// The catalog is generated data (not committed): npm run mock-data copies it into public/mock/.
const catalogPath = 'public/mock/effects.json'
if (!existsSync(catalogPath)) {
  console.log(`SKIP render smoke: ${catalogPath} not found (generate the data, then npm run mock-data)`)
  process.exit(0)
}
const catalog = JSON.parse(readFileSync(catalogPath, 'utf8')) as EffectDef[]
const init = useStore.getInitialState() as Record<string, unknown>
const setInit = (patch: Record<string, unknown>) => Object.assign(init, patch)

setInit({ scene: mockScene(), catalog, demos: [], loading: false, frame: 1000, selection: { kind: 'camera' } })

let fails = 0
const run = (name: string, fn: () => string) => {
  try {
    const h = fn()
    if (!h.length) throw new Error('empty output')
    return h
  } catch (e) { fails++; console.log(`FAIL ${name}: ${(e as Error).message}`); return '' }
}

const tb = run('TransportBar', () => renderToString(<TransportBar />))
console.log(`ok   TransportBar renders (${tb.length} chars); audio-outruns badge: ${tb.includes('audio ends')}`)

const eb = run('EffectBrowser', () => renderToString(<EffectBrowser />))
const rows = (eb.match(/draggable="true"/g) ?? []).length
if (rows !== catalog.length) { fails++; console.log(`FAIL EffectBrowser: ${rows} draggable rows, want ${catalog.length}`) }
console.log(`ok   EffectBrowser lists all ${rows} catalog effects, every row draggable`)

const cp = run('Properties/camera', () => renderToString(<Properties />))
console.log(`ok   camera properties render; hFOV field: ${cp.includes('hFOV')}; keyframe diamonds: ${(cp.match(/class="diamond/g) ?? []).length}`)

setInit({ selection: { kind: 'object', id: 1 } })
const op = run('Properties/object', () => renderToString(<Properties />))
console.log(`ok   object properties render; 4096-unit rot note: ${op.includes('4096')}`)

// every catalog effect through the generated panel
let widgets = 0
const uis = new Map<string, number>()
for (const def of catalog) {
  const scene = mockScene()
  const e: EffectInstance = {
    uid: 'probe', effectId: def.effectId, execType: def.execType, packetId: 0x99,
    start: 100, end: 220, label: def.cname, params: paramChannels(def), dirty: true,
  }
  scene.effects.push(e)
  setInit({ scene: scene as Scene, frame: 150, selection: { kind: 'effect', uid: 'probe' } })
  const html = run(`Properties/${def.hexid} ${def.cname}`, () => renderToString(<Properties />))
  if (!html) continue
  widgets += def.params.length
  for (const p of def.params) uis.set(p.ui, (uis.get(p.ui) ?? 0) + 1)
  if (def.params.length > 0 && !html.includes(def.hexid)) {
    fails++; console.log(`FAIL ${def.hexid}: panel did not name the effect`)
  }
  if (def.execType === 1 && !html.includes('DM_MAX_EFFECT')) {
    fails++; console.log(`FAIL ${def.hexid}: exec-1 panel is missing the re-emit warning`)
  }
}
console.log(`ok   all ${catalog.length} catalog effects render a properties panel (${widgets} param widgets total)`)
console.log(`ok   widget kinds exercised: ${[...uis.entries()].sort((a, b) => b[1] - a[1]).map(([k, v]) => `${k}:${v}`).join(' ')}`)

console.log(fails ? `\n${fails} FAILURES` : '\nALL PASS')
process.exit(fails ? 1 : 0)
