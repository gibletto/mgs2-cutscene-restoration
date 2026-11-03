// Every call hits the Go server first and falls back to the mock, so the UI is fully
// demonstrable with nothing running. ?mock=1 forces the mock; ?live=1 disables the fallback.

import { mockDemos, mockScene } from './mock'
import type { AppConfig, Asset, BakeResult, DeployResult, DemoEntry, EffectDef, Scene } from './types'

const qs = new URLSearchParams(location.search)
export const FORCE_MOCK = qs.get('mock') === '1'
export const FORCE_LIVE = qs.get('live') === '1'

export type Backend = 'live' | 'mock'
let backend: Backend = FORCE_MOCK ? 'mock' : 'live'
export const getBackend = () => backend

async function get<T>(path: string, fallback: () => T | Promise<T>): Promise<T> {
  if (FORCE_MOCK) { backend = 'mock'; return fallback() }
  try {
    const r = await fetch(path, { headers: { accept: 'application/json' } })
    if (!r.ok) throw new Error(`${path}: ${r.status}`)
    const j = (await r.json()) as T
    backend = 'live'
    return j
  } catch (e) {
    if (FORCE_LIVE) throw e
    backend = 'mock'
    return fallback()
  }
}

export const getConfig = () =>
  get<AppConfig>('/api/config', () => ({
    gameDir: 'C:/Program Files (x86)/Steam/steamapps/common/MGS2',
    gameDataDir: '',
    toolsDir: '',
    ok: false,
    problems: ['mock backend: no Go server on /api'],
  }))

export const postConfig = (c: Pick<AppConfig, 'gameDir' | 'gameDataDir' | 'toolsDir'>) =>
  fetch('/api/config', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(c) })

export const getDemos = () => get<DemoEntry[]>('/api/demos', mockDemos)

export const getScene = (path: string) =>
  get<Scene>(`/api/scene?path=${encodeURIComponent(path)}`, mockScene).then(normaliseScene)

export const postScene = (s: Scene) =>
  fetch('/api/scene', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(s) })

export const getCatalog = () =>
  get<EffectDef[]>('/api/catalog', () => fetch('mock/effects.json').then((r) => r.json() as Promise<EffectDef[]>))

export const getAssets = () => get<Asset[]>('/api/assets', () => [] as Asset[])

/** Bakes to the staging dir. Never touches the game -- deploy() does that. */
export async function bake(path: string, force = false): Promise<BakeResult> {
  if (FORCE_MOCK || backend === 'mock') return mockBake()
  try {
    const r = await fetch('/api/bake', {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ path, force }),
    })
    if (!r.ok) throw new Error(String(r.status))
    return (await r.json()) as BakeResult
  } catch (e) {
    if (FORCE_LIVE) throw e
    return mockBake()
  }
}

/** Copies the staged bake over the game's file. The only call that writes into the install. */
export async function deploy(path: string, force = false): Promise<DeployResult> {
  const r = await fetch('/api/deploy', {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ path, force }),
  })
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error ?? String(r.status))
  return (await r.json()) as DeployResult
}

function mockBake(): BakeResult {
  return {
    ok: true,
    bytesWritten: 4_182_016,
    findings: [
      { rule: 'container.terminate', severity: 'info', msg: 'every chunk ends with TERMINATE_FRAME|NOSKIP', fix: '' },
      { rule: 'camera.gimbal', severity: 'warn', msg: 'view vector within 0.4 deg of vertical', frame: 1602, fix: 'clamp |dir.y| <= 0.9999' },
      { rule: 'length.hd_camera_track', severity: 'warn', msg: 'frame count unchanged (2180) -- HD widescreen keyframes stay in sync' },
    ],
  }
}

/**
 * scene.go declares PosX,PosY,PosZ in one grouped field with a single struct tag, so Go's
 * encoder cannot name them apart. Tolerate whatever it actually ships rather than crash.
 */
function normaliseScene(s: Scene): Scene {
  const empty = { dirty: false }
  const c = (s.camera ?? {}) as unknown as Record<string, unknown>
  for (const k of ['posX', 'posY', 'posZ', 'trgX', 'trgY', 'trgZ', 'screen', 'roll']) {
    if (!c[k]) c[k] = { ...empty }
  }
  s.camera = c as unknown as Scene['camera']
  for (const o of s.objects ?? []) {
    const oo = o as unknown as Record<string, unknown>
    for (const k of ['posX', 'posY', 'posZ', 'rotX', 'rotY', 'rotZ']) if (!oo[k]) oo[k] = { ...empty }
    o.visibility ??= []
  }
  for (const e of s.effects ?? []) e.params ??= {}
  s.objects ??= []
  s.effects ??= []
  s.captions ??= []
  s.markers ??= []
  s.fps ||= 60
  return s
}

/** The Go server pushes scene/findings/progress. Silently inert when nothing is listening. */
export function connectWS(onMsg: (m: { type: string; [k: string]: unknown }) => void): () => void {
  if (FORCE_MOCK) return () => {}
  let ws: WebSocket | null = null
  try {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    ws = new WebSocket(`${proto}//${location.host}/ws`)
    ws.onmessage = (ev) => { try { onMsg(JSON.parse(ev.data)) } catch { /* not ours */ } }
    ws.onerror = () => ws?.close()
  } catch { /* no server */ }
  return () => ws?.close()
}
