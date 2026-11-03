export const TICKS_PER_FRAME = 5   // the container clock is 300 Hz; a demo frame is 60 Hz

export const frameToTick = (f: number) => f * TICKS_PER_FRAME

/** 60 fps timecode: mm:ss:ff */
export function timecode(frame: number, fps = 60): string {
  const f = Math.max(0, Math.round(frame))
  const ff = f % fps
  const total = Math.floor(f / fps)
  const ss = total % 60
  const mm = Math.floor(total / 60)
  return `${pad(mm)}:${pad(ss)}:${pad(ff)}`
}

export const pad = (n: number, w = 2) => String(n).padStart(w, '0')

export function seconds(frame: number, fps = 60): string {
  return (frame / fps).toFixed(3) + 's'
}

/** Fixed-width numerics: the panel must not reflow while scrubbing. */
export function num(v: number, dp = 2): string {
  if (!isFinite(v)) return '--'
  return v.toFixed(dp)
}

export const hex8 = (v: number) => '0x' + (v >>> 0).toString(16).toUpperCase().padStart(8, '0')

export function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1048576).toFixed(2)} MB`
}
