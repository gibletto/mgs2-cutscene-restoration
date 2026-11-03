import { create } from 'zustand'
import * as api from './api'
import { removeKey, sample, setKey, setValue } from './lib/channel'
import type { Channel, ChannelPath, DemoEntry, EffectDef, EffectInstance, Finding, Scene, Selection } from './types'

export type ViewMode = 'free' | 'camera'
export type BottomMode = 'timeline' | 'curve'

interface State {
  scene: Scene | null
  catalog: EffectDef[]
  demos: DemoEntry[]
  backend: api.Backend
  loading: boolean

  frame: number
  playing: boolean
  loopIn: number
  loopOut: number
  loopOn: boolean

  selection: Selection
  activeChannel: ChannelPath | null
  cameraExpanded: boolean
  viewMode: ViewMode
  bottomMode: BottomMode

  pxPerFrame: number
  scrollX: number      // in frames
  scrollY: number      // in px

  findings: Finding[]
  baking: boolean
  bakeMsg: string
  staged: string
  deploying: boolean
  wire: boolean
  showStage: boolean
  showHidden: boolean
  putobjMode: 'scripted' | 'all' | 'off'
  cyclePutobjMode: () => void

  load: (path?: string) => Promise<void>
  setFrame: (f: number) => void
  step: (d: number) => void
  togglePlay: () => void
  setPlaying: (p: boolean) => void
  setLoop: (a: number, b: number) => void
  toggleLoop: () => void

  select: (s: Selection) => void
  setActiveChannel: (p: ChannelPath | null) => void
  setViewMode: (m: ViewMode) => void
  setBottomMode: (m: BottomMode) => void
  toggleCameraExpanded: () => void

  zoomBy: (mul: number, anchorFrame?: number) => void
  setScroll: (x: number, y: number) => void

  editChannel: (path: ChannelPath, fn: (c: Channel) => Channel) => void
  setChannelValue: (path: ChannelPath, v: number) => void
  toggleKeyAt: (path: ChannelPath, frame: number) => void

  addEffect: (def: EffectDef, frame: number) => void
  moveEffect: (uid: string, start: number, end: number) => void
  removeEffect: (uid: string) => void

  runBake: () => Promise<void>
  runDeploy: () => Promise<void>
  toggleWire: () => void
  reload: () => Promise<void>
  toggleStage: () => void
  toggleHidden: () => void
}

export const emptyCh = (): Channel => ({ dirty: false })

export function getChannel(s: Scene, path: ChannelPath): Channel | undefined {
  const p = path.split('.')
  if (p[0] === 'camera') return (s.camera as unknown as Record<string, Channel>)[p[1]]
  if (p[0] === 'object') {
    const o = s.objects.find((x) => String(x.id) === p[1])
    return o ? (o as unknown as Record<string, Channel>)[p[2]] : undefined
  }
  if (p[0] === 'effect') {
    const e = s.effects.find((x) => x.uid === p[1])
    return e?.params[p.slice(2).join('.')]
  }
  return undefined
}

function withChannel(s: Scene, path: ChannelPath, fn: (c: Channel) => Channel): Scene {
  const p = path.split('.')
  if (p[0] === 'camera') {
    const cam = { ...s.camera } as unknown as Record<string, Channel>
    cam[p[1]] = fn(cam[p[1]] ?? emptyCh())
    return { ...s, camera: cam as unknown as Scene['camera'] }
  }
  if (p[0] === 'object') {
    const objects = s.objects.map((o) => {
      if (String(o.id) !== p[1]) return o
      const oo = { ...o } as unknown as Record<string, unknown>
      oo[p[2]] = fn((oo[p[2]] as Channel) ?? emptyCh())
      return oo as unknown as (typeof s.objects)[number]
    })
    return { ...s, objects }
  }
  if (p[0] === 'effect') {
    const key = p.slice(2).join('.')
    const effects = s.effects.map((e) => {
      if (e.uid !== p[1]) return e
      return { ...e, dirty: true, params: { ...e.params, [key]: fn(e.params[key] ?? emptyCh()) } }
    })
    return { ...s, effects }
  }
  return s
}

/** Catalog defaults -> one channel per scalar. Vectors expand to .x/.y/.z and colours to .r/.g/.b/.a. */
export function paramChannels(def: EffectDef): Record<string, Channel> {
  const out: Record<string, Channel> = {}
  const k = (v: number): Channel => ({ keys: [{ frame: 0, value: v, interp: 'hold' }], dirty: true })
  for (const p of def.params) {
    const d = p.default
    switch (p.ui) {
      case 'rgba-color': {
        const c = Array.isArray(d) ? d : [128, 128, 128, 128]
        out[`${p.name}.r`] = k(c[0]); out[`${p.name}.g`] = k(c[1])
        out[`${p.name}.b`] = k(c[2]); out[`${p.name}.a`] = k(c[3] ?? 128)
        break
      }
      case 'vec3-float': case 'vec3-short': case 'point-helper': {
        const c = Array.isArray(d) ? d : [0, 0, 0]
        out[`${p.name}.x`] = k(c[0] ?? 0); out[`${p.name}.y`] = k(c[1] ?? 0); out[`${p.name}.z`] = k(c[2] ?? 0)
        break
      }
      case 'string-128': break   // text is not keyable
      default:
        out[p.name] = k(typeof d === 'number' ? d : 0)
    }
  }
  return out
}

export const useStore = create<State>((set, get) => ({
  scene: null,
  catalog: [],
  demos: [],
  backend: 'mock',
  loading: true,

  frame: 0,
  playing: false,
  loopIn: 0,
  loopOut: 0,
  loopOn: false,

  selection: { kind: 'camera' },
  activeChannel: 'camera.posX',
  cameraExpanded: true,
  viewMode: 'free',
  bottomMode: 'timeline',

  pxPerFrame: 0.55,
  scrollX: 0,
  scrollY: 0,

  findings: [],
  baking: false,
  bakeMsg: '',
  staged: '',
  deploying: false,
  wire: false,
  showStage: true,
  showHidden: false,
  putobjMode: 'scripted' as const,
  cyclePutobjMode: () => set((st) => ({ putobjMode: st.putobjMode === 'scripted' ? 'all' : st.putobjMode === 'all' ? 'off' : 'scripted' })),

  async load(path) {
    set({ loading: true })
    const [demos, catalog] = await Promise.all([api.getDemos(), api.getCatalog()])
    const target = path ?? demos[0]?.path ?? ''
    const scene = await api.getScene(target)
    set({
      scene, catalog, demos, loading: false, backend: api.getBackend(),
      frame: 0, loopIn: 0, loopOut: scene.frames - 1, playing: false,
    })
  },

  setFrame(f) {
    const s = get().scene
    const max = s ? s.frames - 1 : 0
    set({ frame: Math.max(0, Math.min(max, Math.round(f))) })
  },
  step(d) { get().setFrame(get().frame + d) },
  togglePlay() { set({ playing: !get().playing }) },
  setPlaying(p) { set({ playing: p }) },
  setLoop(a, b) { set({ loopIn: Math.min(a, b), loopOut: Math.max(a, b) }) },
  toggleLoop() { set({ loopOn: !get().loopOn }) },

  select(sel) {
    set({ selection: sel })
    if (sel.kind === 'camera') set({ activeChannel: 'camera.posX' })
    if (sel.kind === 'object') set({ activeChannel: `object.${sel.id}.posX` })
    if (sel.kind === 'effect') {
      const e = get().scene?.effects.find((x) => x.uid === sel.uid)
      const first = e ? Object.keys(e.params)[0] : null
      set({ activeChannel: e && first ? `effect.${e.uid}.${first}` : null })
    }
  },
  setActiveChannel(p) { set({ activeChannel: p }) },
  setViewMode(m) { set({ viewMode: m }) },
  setBottomMode(m) { set({ bottomMode: m }) },
  toggleCameraExpanded() { set({ cameraExpanded: !get().cameraExpanded }) },

  zoomBy(mul, anchorFrame) {
    const { pxPerFrame, scrollX } = get()
    const next = Math.max(0.05, Math.min(40, pxPerFrame * mul))
    const a = anchorFrame ?? get().frame
    set({ pxPerFrame: next, scrollX: Math.max(0, a - (a - scrollX) * (pxPerFrame / next)) })
  },
  setScroll(x, y) { set({ scrollX: Math.max(0, x), scrollY: Math.max(0, y) }) },

  editChannel(path, fn) {
    const s = get().scene
    if (!s) return
    set({ scene: withChannel(s, path, fn) })
  },
  setChannelValue(path, v) {
    const { scene, frame } = get()
    if (!scene) return
    set({ scene: withChannel(scene, path, (c) => setValue(c, frame, v)) })
  },
  toggleKeyAt(path, frame) {
    const { scene } = get()
    if (!scene) return
    const ch = getChannel(scene, path)
    const has = ch?.keys?.some((k) => k.frame === frame)
    const v = sample(ch, frame)
    set({ scene: withChannel(scene, path, (c) => (has ? removeKey(c, frame) : setKey(c, frame, v))) })
  },

  addEffect(def, frame) {
    const s = get().scene
    if (!s) return
    // exec 1 is born once with a baked life; exec 0 is a per-frame channel over its span.
    const life = def.execType === 1 ? 120 : 60
    const nextPacketId = Math.max(0x30, ...s.effects.map((e) => e.packetId)) + 1
    const e: EffectInstance = {
      uid: `fx-${def.hexid}-${Date.now().toString(36)}`,
      effectId: def.effectId,
      execType: def.execType,
      packetId: nextPacketId,
      start: frame,
      end: Math.min(s.frames, frame + life),
      label: def.displayName_en || def.cname,
      params: paramChannels(def),
      dirty: true,
    }
    set({ scene: { ...s, effects: [...s.effects, e] }, selection: { kind: 'effect', uid: e.uid } })
  },
  moveEffect(uid, start, end) {
    const s = get().scene
    if (!s) return
    const effects = s.effects.map((e) =>
      e.uid === uid
        ? { ...e, start: Math.max(0, Math.round(start)), end: Math.min(s.frames, Math.max(Math.round(start) + 1, Math.round(end))), dirty: true }
        : e)
    set({ scene: { ...s, effects } })
  },
  removeEffect(uid) {
    const s = get().scene
    if (!s) return
    set({ scene: { ...s, effects: s.effects.filter((e) => e.uid !== uid) }, selection: { kind: 'none' } })
  },

  toggleWire() { set((s) => ({ wire: !s.wire })) },

  // The server holds the authoritative scene (it still knows Konami's bytes); after a
  // server-side edit, pull it back rather than trying to mirror the mutation here.
  async reload() {
    const s = get().scene
    if (!s) return
    const fresh = await api.getScene(s.sourcePath)
    set({ scene: fresh })
  },
  toggleStage() { set((s) => ({ showStage: !s.showStage })) },
  toggleHidden() { set((s) => ({ showHidden: !s.showHidden })) },

  async runBake() {
    const s = get().scene
    if (!s) return
    set({ baking: true, bakeMsg: '', staged: '' })
    await api.postScene(s).catch(() => {})
    const r = await api.bake(s.sourcePath)
    const same = r.byteIdentical ? ' (byte-identical to the original)' : ''
    set({
      baking: false,
      findings: r.findings ?? [],
      staged: r.ok ? (r.outPath ?? '') : '',
      bakeMsg: r.ok
        ? `staged ${r.bytesWritten.toLocaleString()} bytes${same} -- DEPLOY to write it into the game`
        : 'bake refused: fatal findings',
    })
  },

  // Deploy is the only thing that writes into the game install. It backs the original up
  // to .orig the first time and never overwrites that backup again.
  async runDeploy() {
    const s = get().scene
    if (!s || !get().staged) return
    set({ deploying: true })
    try {
      const r = await api.deploy(s.sourcePath)
      set({
        deploying: false,
        bakeMsg: r.ok
          ? `deployed to ${r.deployedTo} (backup: ${r.backup ?? 'none'})`
          : 'deploy refused: fatal findings',
        findings: r.findings ?? get().findings,
      })
    } catch (e) {
      set({ deploying: false, bakeMsg: `deploy failed: ${String(e)}` })
    }
  },
}))
