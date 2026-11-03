import * as THREE from 'three'
import { sample } from './channel'
import type { Channel, EffectInstance, Scene } from '../types'

/**
 * Several "effects" are not effects at all -- they ARE the scene's lighting and fog, and
 * the demo hard-sets them on every single frame (a ratio of 1.0 packets per frame in every
 * shipped cutscene). So the viewport does not need to symbolise them with a gizmo: it can
 * just apply them, and show you the shot the game will actually render.
 */
export const FX = {
  FOG: 0x01000000,       // NewFogSet_Demo:   d0 colour, d1 near, d2 far
  AMBIENT: 0x01000006,   // DM_ChangeAmbient: d0 colour
  PARALLEL: 0x01000007,  // DM_ChangeParallel: d0 colour, d1 direction
  FAR_FOCUS: 0x01000001, // d2 blur-min distance, d3 blur-max distance
  NEAR_FOCUS: 0x01000002,
} as const

/** Screen-space effects: nothing to draw in 3D, so the HUD just names the ones that are live. */
export const SCREEN_FX: Record<number, string> = {
  0x01000011: 'blur',
  0x01000015: 'concentrate blur',
  0x01000003: 'fade',
  0x01000008: 'fade (force)',
  0x01000009: 'contrast',
  0x0100000a: 'contrast (force)',
  0x01000012: 'crossfade',
  0x01000013: 'crossfade (custom)',
  0x0100000f: 'lens flare',
  0x0100001f: 'sun',
}

function ch(e: EffectInstance, name: string): Channel | undefined {
  return e.params?.[name]
}

function val(e: EffectInstance, name: string, frame: number, fallback = 0): number {
  const c = ch(e, name)
  return c ? sample(c, frame) : fallback
}

function find(scene: Scene, effectId: number): EffectInstance | undefined {
  return scene.effects.find((e) => e.effectId === effectId && !e.deleted)
}

function activeAt(e: EffectInstance, frame: number): boolean {
  return e.execType === 1 ? frame >= e.start && frame < e.end : frame >= e.start && frame < e.end
}

/**
 * Light colours are bytes where **128 == 1.0**, not 255. The engine's neutral "no light"
 * colour is literally 0x80 (NoShadeRGB), and 45% of the shipped .lt2 colour bytes exceed 128
 * -- that is deliberate overbright, not corruption. Dividing by 255 halves every light, which
 * is a large part of why interiors render black.
 */
const LIGHT_UNIT = 128

function colour(e: EffectInstance, base: string, frame: number): THREE.Color {
  return new THREE.Color(
    val(e, `${base}.r`, frame, 128) / LIGHT_UNIT,
    val(e, `${base}.g`, frame, 128) / LIGHT_UNIT,
    val(e, `${base}.b`, frame, 128) / LIGHT_UNIT,
  )
}

export type LookDev = {
  ambient: THREE.Color | null
  parallel: { colour: THREE.Color; dir: THREE.Vector3 } | null
  fog: { colour: THREE.Color; near: number; far: number } | null
  focus: { near: number | null; far: number | null }
  screen: string[]
}

/** What the cutscene's own effect stream says the lighting looks like on this frame. */
export function lookDev(scene: Scene, frame: number): LookDev {
  const out: LookDev = { ambient: null, parallel: null, fog: null, focus: { near: null, far: null }, screen: [] }

  const amb = find(scene, FX.AMBIENT)
  if (amb && activeAt(amb, frame)) out.ambient = colour(amb, 'd0', frame)

  const par = find(scene, FX.PARALLEL)
  if (par && activeAt(par, frame)) {
    const d = new THREE.Vector3(val(par, 'd1.x', frame, 1), val(par, 'd1.y', frame), val(par, 'd1.z', frame))
    if (d.lengthSq() === 0) d.set(1, 0, 0)
    out.parallel = { colour: colour(par, 'd0', frame), dir: d.normalize() }
  }

  const fog = find(scene, FX.FOG)
  if (fog && activeAt(fog, frame)) {
    const near = val(fog, 'd1', frame, 50_000)
    const far = val(fog, 'd2', frame, 100_000)
    if (far > near) out.fog = { colour: colour(fog, 'd0', frame), near, far }
  }

  // Focus planes are real world distances from the camera, so they can be drawn.
  const far = find(scene, FX.FAR_FOCUS)
  if (far && activeAt(far, frame)) out.focus.far = val(far, 'd2', frame)
  const near = find(scene, FX.NEAR_FOCUS)
  if (near && activeAt(near, frame)) out.focus.near = val(near, 'd3', frame)

  for (const e of scene.effects) {
    const name = SCREEN_FX[e.effectId]
    if (name && !e.deleted && activeAt(e, frame)) out.screen.push(name)
  }
  return out
}
