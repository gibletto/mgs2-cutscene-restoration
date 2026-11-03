import type { Channel, Interp, Key } from '../types'

/** Mirrors scene.Channel.Sample: keys win where they exist, else the imported baked sample. */
export function sample(ch: Channel | undefined, frame: number): number {
  if (!ch) return 0
  const keys = ch.keys
  if (keys && keys.length > 0) {
    const v = sampleKeys(ch, keys, frame)
    if (v !== null) return v
  }
  const b = ch.baked
  if (b && frame >= 0 && frame < b.length) return b[frame]
  return 0
}

function sampleKeys(ch: Channel, k: Key[], frame: number): number | null {
  if (frame < k[0].frame || frame > k[k.length - 1].frame) {
    if (ch.baked && ch.baked.length > 0) return null
    return frame < k[0].frame ? k[0].value : k[k.length - 1].value
  }
  // binary search, not scene.go's linear scan: the timeline resamples every channel once per
  // pixel column every repaint, and a 19,978-frame demo can carry that many keys.
  let lo = 0, hi = k.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (k[mid].frame <= frame) lo = mid
    else hi = mid - 1
  }
  const i = lo
  if (i === k.length - 1 || k[i].frame === frame) return k[i].value

  const a = k[i], b = k[i + 1]
  const span = b.frame - a.frame
  if (span <= 0) return a.value
  const t = (frame - a.frame) / span

  switch (a.interp) {
    case 'hold': return a.value
    case 'bezier': return bezier(a, b, t, span)
    case 'smooth': { const s = t * t * (3 - 2 * t); return a.value + (b.value - a.value) * s }
    default: return a.value + (b.value - a.value) * t
  }
}

/** Handles live in (frames, value) offsets from the key, AE-style; solve x(u)=t by bisection. */
function bezier(a: Key, b: Key, t: number, span: number): number {
  const x1 = (a.outTanX ?? span / 3) / span, y1 = a.value + (a.outTanY ?? 0)
  const x2 = 1 + (b.inTanX ?? -span / 3) / span, y2 = b.value + (b.inTanY ?? 0)
  let u = t
  for (let i = 0; i < 16; i++) {
    const mu = 1 - u
    const x = 3 * mu * mu * u * x1 + 3 * mu * u * u * x2 + u * u * u
    if (x < t) u += (1 - u) / 2
    else u -= u / 2
  }
  const mu = 1 - u
  return 3 * mu * mu * u * y1 + 3 * mu * u * u * y2 + u * u * u * b.value
}

export function keyAt(ch: Channel | undefined, frame: number): Key | undefined {
  return ch?.keys?.find((k) => k.frame === frame)
}

export function setKey(ch: Channel, frame: number, value: number, interp: Interp = 'smooth'): Channel {
  const keys = [...(ch.keys ?? [])]
  const i = keys.findIndex((k) => k.frame === frame)
  const key: Key = { frame, value, interp }
  if (i >= 0) keys[i] = { ...keys[i], value }
  else keys.push(key)
  keys.sort((a, b) => a.frame - b.frame)
  return { ...ch, keys, dirty: true }
}

export function removeKey(ch: Channel, frame: number): Channel {
  const keys = (ch.keys ?? []).filter((k) => k.frame !== frame)
  return { ...ch, keys, dirty: true }
}

/** Editing a value with no key at the playhead moves the whole channel: key it, or offset the bake. */
export function setValue(ch: Channel, frame: number, value: number): Channel {
  if (ch.keys && ch.keys.length > 0) return setKey(ch, frame, value)
  if (ch.baked && ch.baked.length > 0) {
    const baked = [...ch.baked]
    baked[frame] = value
    return { ...ch, baked, dirty: true }
  }
  return setKey(ch, frame, value)
}

export function channelRange(ch: Channel | undefined, frames: number): [number, number] {
  let lo = Infinity, hi = -Infinity
  if (ch?.baked?.length) for (const v of ch.baked) { if (v < lo) lo = v; if (v > hi) hi = v }
  if (ch?.keys?.length) for (const k of ch.keys) { if (k.value < lo) lo = k.value; if (k.value > hi) hi = k.value }
  if (!isFinite(lo)) { lo = 0; hi = 1 }
  if (hi - lo < 1e-6) { lo -= 1; hi += 1 }
  void frames
  return [lo, hi]
}

export const emptyChannel = (): Channel => ({ dirty: false })
