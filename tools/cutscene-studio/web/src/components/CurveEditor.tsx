import { useCallback, useMemo, useRef, useState } from 'react'
import { crisp, text, useCanvas } from '../lib/canvas'
import { channelRange, sample, setKey } from '../lib/channel'
import { timecode } from '../lib/format'
import { getChannel, useStore } from '../store'
import type { Interp, Key } from '../types'

const PAD_L = 62
const PAD_T = 12
const PAD_B = 20

type Grab =
  | { kind: 'key'; i: number }
  | { kind: 'in' | 'out'; i: number }
  | { kind: 'scrub' }

export function CurveEditor() {
  const scene = useStore((s) => s.scene)
  const path = useStore((s) => s.activeChannel)
  const frame = useStore((s) => s.frame)
  const pxPerFrame = useStore((s) => s.pxPerFrame)
  const scrollX = useStore((s) => s.scrollX)

  const ch = useMemo(() => (scene && path ? getChannel(scene, path) : undefined), [scene, path])
  const [view, setView] = useState<{ lo: number; hi: number } | null>(null)
  const grab = useRef<Grab | null>(null)
  const sizeRef = useRef({ w: 0, h: 0 })

  const range = useMemo(() => {
    if (view) return view
    const [lo, hi] = channelRange(ch, scene?.frames ?? 0)
    const pad = (hi - lo) * 0.12
    return { lo: lo - pad, hi: hi + pad }
  }, [ch, view, scene?.frames])

  const f2x = useCallback((f: number) => PAD_L + (f - scrollX) * pxPerFrame, [scrollX, pxPerFrame])
  const x2f = useCallback((x: number) => scrollX + (x - PAD_L) / pxPerFrame, [scrollX, pxPerFrame])
  const v2y = useCallback((v: number, h: number) => {
    const inner = h - PAD_T - PAD_B
    return PAD_T + inner - ((v - range.lo) / (range.hi - range.lo)) * inner
  }, [range])
  const y2v = useCallback((y: number, h: number) => {
    const inner = h - PAD_T - PAD_B
    return range.lo + ((PAD_T + inner - y) / inner) * (range.hi - range.lo)
  }, [range])

  const draw = useCallback((ctx: CanvasRenderingContext2D, w: number, h: number) => {
    sizeRef.current = { w, h }
    ctx.fillStyle = '#0e1013'; ctx.fillRect(0, 0, w, h)
    if (!scene || !path || !ch) {
      text(ctx, 'select a channel in the timeline or the properties panel', 16, h / 2, '#6e7683')
      return
    }

    // value grid
    const steps = 6
    for (let i = 0; i <= steps; i++) {
      const v = range.lo + ((range.hi - range.lo) * i) / steps
      const y = crisp(ctx, v2y(v, h))
      ctx.strokeStyle = i === 0 || i === steps ? '#262b33' : '#191c22'
      ctx.beginPath(); ctx.moveTo(PAD_L, y); ctx.lineTo(w, y); ctx.stroke()
      text(ctx, fmt(v), 6, y, '#5f6773', '10px SF Mono, Consolas, monospace')
    }
    // frame grid
    const step = [1, 5, 10, 30, 60, 120, 300, 600, 1200].find((s) => s * pxPerFrame >= 60) ?? 1800
    for (let f = Math.floor(scrollX / step) * step; f <= scene.frames; f += step) {
      const x = crisp(ctx, f2x(f))
      if (x < PAD_L) continue
      if (x > w) break
      ctx.strokeStyle = '#191c22'
      ctx.beginPath(); ctx.moveTo(x, PAD_T); ctx.lineTo(x, h - PAD_B); ctx.stroke()
      text(ctx, timecode(f), x + 3, h - PAD_B / 2, '#5f6773', '10px SF Mono, Consolas, monospace')
    }

    // the curve, one sample per pixel column
    ctx.strokeStyle = '#ff9a3c'; ctx.lineWidth = 1.5
    ctx.beginPath()
    for (let x = PAD_L; x < w; x++) {
      const f = Math.round(x2f(x))
      if (f < 0 || f >= scene.frames) continue
      const y = v2y(sample(ch, f), h)
      x === PAD_L ? ctx.moveTo(x, y) : ctx.lineTo(x, y)
    }
    ctx.stroke()
    ctx.lineWidth = 1

    // the imported bake underneath, so an edit is visibly a departure from Konami's data
    if (ch.baked?.length && ch.keys?.length) {
      ctx.strokeStyle = '#3a4552'; ctx.setLineDash([2, 3]); ctx.beginPath()
      for (let x = PAD_L; x < w; x++) {
        const f = Math.round(x2f(x))
        if (f < 0 || f >= ch.baked.length) continue
        const y = v2y(ch.baked[f], h)
        x === PAD_L ? ctx.moveTo(x, y) : ctx.lineTo(x, y)
      }
      ctx.stroke(); ctx.setLineDash([])
    }

    // keys + bezier handles
    const keys = ch.keys ?? []
    keys.forEach((k, i) => {
      const x = f2x(k.frame), y = v2y(k.value, h)
      if (x < PAD_L - 20 || x > w + 20) return
      if (k.interp === 'bezier') {
        const hx = f2x(k.frame + (k.outTanX ?? 12)), hy = v2y(k.value + (k.outTanY ?? 0), h)
        const gx = f2x(k.frame + (k.inTanX ?? -12)), gy = v2y(k.value + (k.inTanY ?? 0), h)
        ctx.strokeStyle = '#6a7686'
        ctx.beginPath(); ctx.moveTo(gx, gy); ctx.lineTo(x, y); ctx.lineTo(hx, hy); ctx.stroke()
        ctx.fillStyle = '#9fb0c4'
        ctx.fillRect(hx - 2.5, hy - 2.5, 5, 5); ctx.fillRect(gx - 2.5, gy - 2.5, 5, 5)
      }
      ctx.fillStyle = k.frame === frame ? '#ffffff' : '#ff9a3c'
      ctx.beginPath(); ctx.moveTo(x, y - 4); ctx.lineTo(x + 4, y); ctx.lineTo(x, y + 4); ctx.lineTo(x - 4, y); ctx.closePath(); ctx.fill()
      if (k.interp === 'hold') { ctx.strokeStyle = '#ff9a3c'; ctx.strokeRect(crisp(ctx, x + 4), crisp(ctx, y - 4), 8, 8) }
      void i
    })

    // playhead + readout
    const px = f2x(frame)
    if (px >= PAD_L) {
      ctx.strokeStyle = '#ff9a3c88'
      ctx.beginPath(); ctx.moveTo(crisp(ctx, px), 0); ctx.lineTo(crisp(ctx, px), h - PAD_B); ctx.stroke()
    }
    text(ctx, `${path}  =  ${fmt(sample(ch, frame))}`, PAD_L + 6, PAD_T / 2 + 2, '#8b939f', '10px SF Mono, Consolas, monospace')
  }, [scene, path, ch, frame, range, pxPerFrame, scrollX, f2x, x2f, v2y])

  const { ref } = useCanvas(draw)

  const hitKey = (x: number, y: number): Exclude<Grab, { kind: 'scrub' }> | null => {
    const keys = ch?.keys ?? []
    const h = sizeRef.current.h
    for (let i = keys.length - 1; i >= 0; i--) {
      const k = keys[i]
      const kx = f2x(k.frame), ky = v2y(k.value, h)
      if (k.interp === 'bezier') {
        const hx = f2x(k.frame + (k.outTanX ?? 12)), hy = v2y(k.value + (k.outTanY ?? 0), h)
        const gx = f2x(k.frame + (k.inTanX ?? -12)), gy = v2y(k.value + (k.inTanY ?? 0), h)
        if (Math.hypot(x - hx, y - hy) < 6) return { kind: 'out', i }
        if (Math.hypot(x - gx, y - gy) < 6) return { kind: 'in', i }
      }
      if (Math.hypot(x - kx, y - ky) < 7) return { kind: 'key', i }
    }
    return null
  }

  const onDown = (ev: React.MouseEvent) => {
    if (!scene || !path || !ch) return
    const r = ev.currentTarget.getBoundingClientRect()
    const x = ev.clientX - r.left, y = ev.clientY - r.top
    const hit = hitKey(x, y)
    const st = useStore.getState()
    if (!hit) {
      if (ev.detail === 2) {   // double-click on empty graph = new key on the curve
        const f = Math.round(x2f(x))
        st.editChannel(path, (c) => setKey(c, f, y2v(y, sizeRef.current.h)))
        return
      }
      st.setFrame(Math.round(x2f(x)))
      grab.current = { kind: 'scrub' }
      return
    }
    if (ev.altKey && hit.kind === 'key') { st.toggleKeyAt(path, (ch.keys ?? [])[hit.i].frame); return }
    grab.current = hit
    st.setFrame((ch.keys ?? [])[hit.i].frame)
  }

  const onMove = (ev: React.MouseEvent) => {
    const g = grab.current
    if (!g || !path || !ch) return
    const r = ev.currentTarget.getBoundingClientRect()
    const x = ev.clientX - r.left, y = ev.clientY - r.top
    const st = useStore.getState()
    if (g.kind === 'scrub') { st.setFrame(Math.round(x2f(x))); return }

    const h = sizeRef.current.h
    st.editChannel(path, (c) => {
      const keys = [...(c.keys ?? [])]
      const k = keys[g.i]
      if (!k) return c
      if (g.kind === 'key') {
        keys[g.i] = { ...k, frame: Math.max(0, Math.round(x2f(x))), value: y2v(y, h) }
        keys.sort((a, b) => a.frame - b.frame)
      } else {
        const dx = x2f(x) - k.frame, dy = y2v(y, h) - k.value
        keys[g.i] = g.kind === 'out'
          ? { ...k, outTanX: Math.max(1, dx), outTanY: dy }
          : { ...k, inTanX: Math.min(-1, dx), inTanY: dy }
      }
      return { ...c, keys, dirty: true }
    })
  }

  const onUp = () => { grab.current = null }

  const onWheel = (ev: React.WheelEvent) => {
    const st = useStore.getState()
    if (ev.shiftKey) {   // vertical value zoom
      const mid = (range.lo + range.hi) / 2, half = ((range.hi - range.lo) / 2) * (ev.deltaY < 0 ? 0.85 : 1.18)
      setView({ lo: mid - half, hi: mid + half })
      return
    }
    const r = ev.currentTarget.getBoundingClientRect()
    st.zoomBy(ev.deltaY < 0 ? 1.18 : 1 / 1.18, x2f(ev.clientX - r.left))
  }

  const key = ch?.keys?.find((k) => k.frame === frame)
  const setInterp = (i: Interp) => {
    if (!path || !key) return
    useStore.getState().editChannel(path, (c) => ({
      ...c,
      dirty: true,
      keys: (c.keys ?? []).map((k): Key => (k.frame === frame
        ? { ...k, interp: i, outTanX: k.outTanX ?? 12, inTanX: k.inTanX ?? -12, outTanY: k.outTanY ?? 0, inTanY: k.inTanY ?? 0 }
        : k)),
    }))
  }

  return (
    <div className="tl-wrap" style={{ display: 'flex', flexDirection: 'column' }}>
      <div className="panel-head">
        <span>curve</span>
        <span className="mono" style={{ color: '#ff9a3c' }}>{path ?? '--'}</span>
        <div className="spacer" />
        {(['hold', 'linear', 'bezier', 'smooth'] as Interp[]).map((i) => (
          <button key={i} className={'btn' + (key?.interp === i ? ' on' : '')} disabled={!key} onClick={() => setInterp(i)}>{i}</button>
        ))}
        <div className="sep" />
        <button className="btn" onClick={() => setView(null)}>fit</button>
      </div>
      <div style={{ flex: 1, minHeight: 0, position: 'relative' }}>
        <canvas ref={ref} onMouseDown={onDown} onMouseMove={onMove} onMouseUp={onUp} onMouseLeave={onUp} onWheel={onWheel} />
      </div>
    </div>
  )
}

function fmt(v: number) {
  const a = Math.abs(v)
  if (a >= 1000) return v.toFixed(0)
  if (a >= 10) return v.toFixed(1)
  if (a >= 0.1) return v.toFixed(3)
  return v.toExponential(1)
}
