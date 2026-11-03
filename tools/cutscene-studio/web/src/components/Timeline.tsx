import { useCallback, useMemo, useRef, useState } from 'react'
import { clipRect, crisp, text, useCanvas } from '../lib/canvas'
import { sample } from '../lib/channel'
import { timecode } from '../lib/format'
import { getChannel, useStore } from '../store'
import type { Channel, EffectDef, Scene } from '../types'

export const HEADER_W = 190
const RULER_H = 30
const ROW_H = 20

type Row =
  | { kind: 'camera'; label: string }
  | { kind: 'camchan'; label: string; path: string }
  | { kind: 'object'; label: string; id: number }
  | { kind: 'effect'; label: string; uid: string }
  | { kind: 'captions'; label: string }
  | { kind: 'audio'; label: string }

const CAM_CHANNELS: Array<[string, string]> = [
  ['pos.x', 'posX'], ['pos.y', 'posY'], ['pos.z', 'posZ'],
  ['trg.x', 'trgX'], ['trg.y', 'trgY'], ['trg.z', 'trgZ'],
  ['screen', 'screen'], ['roll', 'roll'],
]

function buildRows(s: Scene, camExpanded: boolean): Row[] {
  const rows: Row[] = [{ kind: 'camera', label: 'CAMERA' }]
  if (camExpanded) for (const [label, key] of CAM_CHANNELS) rows.push({ kind: 'camchan', label, path: `camera.${key}` })
  for (const o of s.objects) rows.push({ kind: 'object', label: o.label || String(o.id), id: o.id })
  for (const e of s.effects) rows.push({ kind: 'effect', label: e.label, uid: e.uid })
  rows.push({ kind: 'captions', label: 'CAPTIONS' })
  rows.push({ kind: 'audio', label: 'AUDIO' })
  return rows
}

const TICK_STEPS = [1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 1200, 3600]

type Drag =
  | { mode: 'scrub' }
  | { mode: 'loop'; anchor: number }
  | { mode: 'clip'; uid: string; grab: 'move' | 'in' | 'out'; f0: number; s0: number; e0: number }
  | { mode: 'pan'; x0: number; sx0: number }

export function Timeline({ onDropEffect }: { onDropEffect: (def: EffectDef, frame: number) => void }) {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const pxPerFrame = useStore((s) => s.pxPerFrame)
  const scrollX = useStore((s) => s.scrollX)
  const scrollY = useStore((s) => s.scrollY)
  const selection = useStore((s) => s.selection)
  const activeChannel = useStore((s) => s.activeChannel)
  const camExpanded = useStore((s) => s.cameraExpanded)
  const loopIn = useStore((s) => s.loopIn)
  const loopOut = useStore((s) => s.loopOut)
  const loopOn = useStore((s) => s.loopOn)
  const catalog = useStore((s) => s.catalog)

  const [hover, setHover] = useState<{ x: number; y: number } | null>(null)
  const drag = useRef<Drag | null>(null)
  const wrapRef = useRef<HTMLDivElement | null>(null)

  const rows = useMemo(() => (scene ? buildRows(scene, camExpanded) : []), [scene, camExpanded])

  const x2f = useCallback((x: number) => scrollX + (x - HEADER_W) / pxPerFrame, [scrollX, pxPerFrame])
  const f2x = useCallback((f: number) => HEADER_W + (f - scrollX) * pxPerFrame, [scrollX, pxPerFrame])

  const draw = useCallback((ctx: CanvasRenderingContext2D, w: number, h: number) => {
    if (!scene) return
    const F = scene.frames

    ctx.fillStyle = '#0e1013'; ctx.fillRect(0, 0, w, h)
    ctx.fillStyle = '#101318'; ctx.fillRect(HEADER_W, RULER_H, w - HEADER_W, h - RULER_H)

    // beyond the end of the demo
    const xEnd = f2x(F)
    if (xEnd < w) { ctx.fillStyle = '#08090b'; ctx.fillRect(Math.max(HEADER_W, xEnd), RULER_H, w - Math.max(HEADER_W, xEnd), h - RULER_H) }

    // loop range
    if (loopOn) {
      const a = Math.max(HEADER_W, f2x(loopIn)), b = Math.min(w, f2x(loopOut))
      if (b > a) { ctx.fillStyle = '#1a2634'; ctx.fillRect(a, RULER_H, b - a, h - RULER_H) }
    }

    // ---- ruler ----
    ctx.fillStyle = '#171a1f'; ctx.fillRect(0, 0, w, RULER_H)
    const step = TICK_STEPS.find((s) => s * pxPerFrame >= 58) ?? 3600
    const sub = step / (step % 5 === 0 ? 5 : 2)
    clipRect(ctx, HEADER_W, 0, w - HEADER_W, RULER_H, () => {
      const f0 = Math.max(0, Math.floor(scrollX / sub) * sub)
      for (let f = f0; f <= F; f += sub) {
        const x = crisp(ctx, f2x(f))
        if (x < HEADER_W - 2) continue
        if (x > w) break
        const major = f % step === 0
        ctx.strokeStyle = major ? '#3b434f' : '#242830'
        ctx.beginPath(); ctx.moveTo(x, major ? 10 : 20); ctx.lineTo(x, RULER_H); ctx.stroke()
        if (major) text(ctx, timecode(f), x + 3, 15, '#8b939f', '10px SF Mono, Consolas, monospace')
      }
      // markers/shots
      for (const m of scene.markers) {
        const x = crisp(ctx, f2x(m.frame))
        ctx.strokeStyle = m.color ?? '#888'; ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, RULER_H); ctx.stroke()
        ctx.fillStyle = m.color ?? '#888'; ctx.fillRect(x, 0, 5, 5)
        text(ctx, m.label, x + 8, 5, '#6e7683', '9px Inter, sans-serif')
      }
    })
    ctx.strokeStyle = '#262b33'
    ctx.beginPath(); ctx.moveTo(0, crisp(ctx, RULER_H)); ctx.lineTo(w, crisp(ctx, RULER_H)); ctx.stroke()

    // ---- rows ----
    clipRect(ctx, 0, RULER_H, w, h - RULER_H, () => {
      rows.forEach((r, i) => {
        const y = RULER_H + i * ROW_H - scrollY
        if (y + ROW_H < RULER_H || y > h) return
        const sel = isSelected(r)
        ctx.fillStyle = i % 2 ? '#121519' : '#0f1216'
        ctx.fillRect(HEADER_W, y, w - HEADER_W, ROW_H)
        if (sel) { ctx.fillStyle = '#16202e'; ctx.fillRect(HEADER_W, y, w - HEADER_W, ROW_H) }

        drawRow(ctx, r, y, w)

        ctx.strokeStyle = '#191c22'
        ctx.beginPath(); ctx.moveTo(HEADER_W, crisp(ctx, y + ROW_H)); ctx.lineTo(w, crisp(ctx, y + ROW_H)); ctx.stroke()

        // gutter
        ctx.fillStyle = sel ? '#1c2532' : i % 2 ? '#15181d' : '#131519'
        ctx.fillRect(0, y, HEADER_W, ROW_H)
        ctx.strokeStyle = '#191c22'
        ctx.beginPath(); ctx.moveTo(0, crisp(ctx, y + ROW_H)); ctx.lineTo(HEADER_W, crisp(ctx, y + ROW_H)); ctx.stroke()
        drawGutter(ctx, r, y, sel)
      })
    })
    ctx.strokeStyle = '#262b33'
    ctx.beginPath(); ctx.moveTo(crisp(ctx, HEADER_W), 0); ctx.lineTo(crisp(ctx, HEADER_W), h); ctx.stroke()

    // ---- playhead ----
    const px = f2x(frame)
    if (px >= HEADER_W && px <= w) {
      ctx.strokeStyle = '#ff9a3c'; ctx.lineWidth = 1
      ctx.beginPath(); ctx.moveTo(crisp(ctx, px), 0); ctx.lineTo(crisp(ctx, px), h); ctx.stroke()
      ctx.fillStyle = '#ff9a3c'
      ctx.beginPath(); ctx.moveTo(px - 5, 0); ctx.lineTo(px + 5, 0); ctx.lineTo(px, 7); ctx.closePath(); ctx.fill()
    }
    if (hover && hover.x > HEADER_W) {
      ctx.strokeStyle = '#ffffff22'
      ctx.beginPath(); ctx.moveTo(crisp(ctx, hover.x), RULER_H); ctx.lineTo(crisp(ctx, hover.x), h); ctx.stroke()
    }

    function isSelected(r: Row) {
      if (r.kind === 'camera' || r.kind === 'camchan') return selection.kind === 'camera'
      if (r.kind === 'object') return selection.kind === 'object' && selection.id === r.id
      if (r.kind === 'effect') return selection.kind === 'effect' && selection.uid === r.uid
      return false
    }

    function drawGutter(c: CanvasRenderingContext2D, r: Row, y: number, sel: boolean) {
      const mid = y + ROW_H / 2
      const col = sel ? '#eef1f5' : '#c8cdd6'
      switch (r.kind) {
        case 'camera':
          text(c, camExpanded ? '▾' : '▸', 6, mid, '#6e7683', '9px monospace')
          text(c, 'CAMERA', 18, mid, col, '600 10px Inter, sans-serif')
          break
        case 'camchan': {
          const ch = scene ? getChannel(scene, r.path) : undefined
          text(c, r.label, 30, mid, activeChannel === r.path ? '#ff9a3c' : '#8b939f', '10px SF Mono, Consolas, monospace')
          text(c, String(ch?.keys?.length ?? 0) + 'k', HEADER_W - 26, mid, '#4d545e', '9px SF Mono, monospace')
          break
        }
        case 'object': {
          const o = scene!.objects.find((x) => x.id === r.id)!
          c.fillStyle = o.cacheId ? '#54a06b' : '#5c6570'
          c.fillRect(6, mid - 3, 6, 6)
          text(c, r.label, 18, mid, col, '11px Inter, sans-serif')
          text(c, o.nJoints ? `${o.nJoints}j` : 'rigid', HEADER_W - 34, mid, '#4d545e', '9px SF Mono, monospace')
          break
        }
        case 'effect': {
          const e = scene!.effects.find((x) => x.uid === r.uid)!
          c.fillStyle = e.execType === 1 ? '#9cb87f' : '#7ba3cf'
          c.fillRect(6, mid - 3, 6, 6)
          text(c, r.label, 18, mid, col, '11px Inter, sans-serif')
          text(c, 'e' + e.execType, HEADER_W - 20, mid, '#4d545e', '9px SF Mono, monospace')
          break
        }
        case 'captions': text(c, 'CAPTIONS', 6, mid, '#c8cdd6', '600 10px Inter, sans-serif'); break
        case 'audio': text(c, 'AUDIO', 6, mid, '#c8cdd6', '600 10px Inter, sans-serif'); break
      }
    }

    function drawRow(c: CanvasRenderingContext2D, r: Row, y: number, wid: number) {
      const x0 = Math.max(HEADER_W, f2x(0))
      const x1 = Math.min(wid, f2x(scene!.frames))
      switch (r.kind) {
        case 'camera':
          drawSpeedBand(c, y, x0, x1, camSpeed)
          drawCutMarks(c, y, x0, x1)
          break
        case 'camchan':
          drawMiniCurve(c, y, x0, x1, getChannel(scene!, r.path), activeChannel === r.path)
          break
        case 'object': {
          const o = scene!.objects.find((x) => x.id === r.id)!
          // visibility as a solid bar
          let vis = false, from = 0
          const segs: Array<[number, number]> = []
          for (const v of [...o.visibility].sort((a, b) => a.frame - b.frame)) {
            if (v.visible && !vis) { vis = true; from = v.frame }
            else if (!v.visible && vis) { vis = false; segs.push([from, v.frame]) }
          }
          if (vis) segs.push([from, scene!.frames])
          for (const [a, b] of segs) {
            const xa = Math.max(HEADER_W, f2x(a)), xb = Math.min(wid, f2x(b))
            if (xb <= xa) continue
            c.fillStyle = '#243a2b'; c.fillRect(xa, y + 3, xb - xa, ROW_H - 6)
            c.strokeStyle = '#3d6b4c'; c.strokeRect(crisp(c, xa), crisp(c, y + 3), xb - xa, ROW_H - 6)
          }
          drawSpeedBand(c, y, x0, x1, (f) => objSpeed(o, f))
          break
        }
        case 'effect': {
          const e = scene!.effects.find((x) => x.uid === r.uid)!
          const xa = f2x(e.start), xb = f2x(e.end)
          const va = Math.max(HEADER_W, xa), vb = Math.min(wid, xb)
          if (vb <= va) break
          const on = selection.kind === 'effect' && selection.uid === e.uid
          c.fillStyle = e.execType === 1 ? (on ? '#3f5233' : '#2c3a24') : (on ? '#2b4767' : '#1e3247')
          c.fillRect(va, y + 2, vb - va, ROW_H - 4)
          c.strokeStyle = on ? '#ff9a3c' : e.execType === 1 ? '#5d7a49' : '#3d6390'
          c.strokeRect(crisp(c, va), crisp(c, y + 2), vb - va, ROW_H - 4)
          if (e.execType === 1) {
            // birth marker: an exec-1 clip is ONE packet; the bar is its baked life, not a re-emit span
            c.fillStyle = '#cfe3b5'
            c.beginPath(); c.moveTo(xa + 1, y + 3); c.lineTo(xa + 6, y + ROW_H / 2); c.lineTo(xa + 1, y + ROW_H - 3); c.closePath()
            if (xa > HEADER_W - 8) c.fill()
          }
          clipRect(c, va, y, vb - va, ROW_H, () => {
            text(c, e.label, va + (e.execType === 1 ? 10 : 4), y + ROW_H / 2, on ? '#ffe0c0' : '#9fb0c4', '10px Inter, sans-serif')
          })
          break
        }
        case 'captions':
          for (const cap of scene!.captions) {
            // caption times are 300 Hz ticks; the ruler is 60 Hz frames
            const xa = Math.max(HEADER_W, f2x(cap.start / 5)), xb = Math.min(wid, f2x(cap.end / 5))
            if (xb <= xa) continue
            c.fillStyle = '#2a2440'; c.fillRect(xa, y + 3, xb - xa, ROW_H - 6)
            c.strokeStyle = '#4d4276'; c.strokeRect(crisp(c, xa), crisp(c, y + 3), xb - xa, ROW_H - 6)
            clipRect(c, xa + 2, y, xb - xa - 4, ROW_H, () => {
              text(c, cap.text.replace(/\|/g, ' / '), xa + 4, y + ROW_H / 2, '#b6a9e0', '10px Inter, sans-serif')
            })
          }
          break
        case 'audio': {
          if (!scene!.hasAudio) {
            clipRect(c, HEADER_W, y, wid - HEADER_W, ROW_H, () => {
              text(c, 'no PCM track -- runs on the vsync wall clock', x0 + 4, y + ROW_H / 2, '#6e7683', '10px Inter, sans-serif')
            })
            break
          }
          const xa = Math.max(HEADER_W, f2x(0)), xb = Math.min(wid, f2x(scene!.audioFrames))
          if (xb > xa) {
            c.fillStyle = '#1c2f38'; c.fillRect(xa, y + 3, xb - xa, ROW_H - 6)
            c.strokeStyle = '#356070'; c.strokeRect(crisp(c, xa), crisp(c, y + 3), xb - xa, ROW_H - 6)
            // waveform placeholder: deterministic, just enough to read as audio
            c.strokeStyle = '#4f8ba1'; c.beginPath()
            for (let x = xa; x < xb; x += 2) {
              const f = x2f(x)
              const a = (Math.sin(f / 7) * Math.sin(f / 31) * Math.sin(f / 3.3) + Math.sin(f / 97)) * 0.35
              const hgt = Math.abs(a) * (ROW_H - 8)
              c.moveTo(crisp(c, x), y + ROW_H / 2 - hgt / 2); c.lineTo(crisp(c, x), y + ROW_H / 2 + hgt / 2)
            }
            c.stroke()
          }
          // where the audio ends. The demo may legally outrun it -- clock falls back to vsync.
          const xe = f2x(scene!.audioFrames)
          if (scene!.audioFrames < scene!.frames && xe < wid) {
            c.strokeStyle = '#e04a5c'; c.setLineDash([3, 2])
            c.beginPath(); c.moveTo(crisp(c, xe), y); c.lineTo(crisp(c, xe), y + ROW_H); c.stroke()
            c.setLineDash([])
            clipRect(c, Math.max(HEADER_W, xe), y, wid, ROW_H, () => {
              text(c, ' audio ends -- wall clock beyond here', xe + 3, y + ROW_H / 2, '#a05460', '10px Inter, sans-serif')
            })
          }
          break
        }
      }
    }

    function camSpeed(f: number) {
      const c0 = scene!.camera
      const dx = sample(c0.posX, f) - sample(c0.posX, f - 1)
      const dy = sample(c0.posY, f) - sample(c0.posY, f - 1)
      const dz = sample(c0.posZ, f) - sample(c0.posZ, f - 1)
      return Math.hypot(dx, dy, dz)
    }
    function objSpeed(o: Scene['objects'][number], f: number) {
      const dx = sample(o.posX, f) - sample(o.posX, f - 1)
      const dy = sample(o.posY, f) - sample(o.posY, f - 1)
      const dz = sample(o.posZ, f) - sample(o.posZ, f - 1)
      return Math.hypot(dx, dy, dz)
    }

    /** one sample per pixel column, not per frame -- 2,000-frame demos stay smooth */
    function drawSpeedBand(c: CanvasRenderingContext2D, y: number, xa: number, xb: number, fn: (f: number) => number) {
      if (xb <= xa) return
      const n = Math.ceil(xb - xa)
      const vals: number[] = new Array(n)
      let mx = 1e-6
      for (let i = 0; i < n; i++) {
        const f = Math.round(x2f(xa + i))
        const v = fn(f); vals[i] = v; if (v > mx) mx = v
      }
      c.fillStyle = '#2b3d52'
      c.beginPath(); c.moveTo(xa, y + ROW_H - 2)
      for (let i = 0; i < n; i++) c.lineTo(xa + i, y + ROW_H - 2 - (vals[i] / mx) * (ROW_H - 5))
      c.lineTo(xa + n - 1, y + ROW_H - 2); c.closePath(); c.fill()
    }

    /** a jump bigger than 40x the local median reads as a cut, not a move */
    function drawCutMarks(c: CanvasRenderingContext2D, y: number, xa: number, xb: number) {
      for (const m of scene!.markers) {
        const x = f2x(m.frame)
        if (x < xa || x > xb) continue
        c.strokeStyle = m.color ?? '#888'
        c.setLineDash([2, 2]); c.beginPath(); c.moveTo(crisp(c, x), y); c.lineTo(crisp(c, x), y + ROW_H); c.stroke(); c.setLineDash([])
      }
    }

    function drawMiniCurve(c: CanvasRenderingContext2D, y: number, xa: number, xb: number, ch: Channel | undefined, on: boolean) {
      if (!ch || xb <= xa) return
      const n = Math.ceil(xb - xa)
      const vals: number[] = new Array(n)
      let lo = Infinity, hi = -Infinity
      for (let i = 0; i < n; i++) {
        const v = sample(ch, Math.round(x2f(xa + i)))
        vals[i] = v; if (v < lo) lo = v; if (v > hi) hi = v
      }
      if (hi - lo < 1e-9) { lo -= 1; hi += 1 }
      c.strokeStyle = on ? '#ff9a3c' : '#5d7185'
      c.beginPath()
      for (let i = 0; i < n; i++) {
        const yy = y + ROW_H - 3 - ((vals[i] - lo) / (hi - lo)) * (ROW_H - 6)
        i ? c.lineTo(xa + i, yy) : c.moveTo(xa + i, yy)
      }
      c.stroke()
      for (const k of ch.keys ?? []) {
        const x = f2x(k.frame)
        if (x < xa - 4 || x > xb + 4) continue
        const yy = y + ROW_H - 3 - ((k.value - lo) / (hi - lo)) * (ROW_H - 6)
        c.fillStyle = '#ff9a3c'
        c.beginPath(); c.moveTo(x, yy - 3); c.lineTo(x + 3, yy); c.lineTo(x, yy + 3); c.lineTo(x - 3, yy); c.closePath(); c.fill()
      }
    }
  }, [scene, rows, frame, pxPerFrame, scrollX, scrollY, selection, activeChannel, camExpanded, loopIn, loopOut, loopOn, hover, f2x, x2f])

  const { ref } = useCanvas(draw)

  const rowAt = (y: number): { row: Row; index: number } | null => {
    const i = Math.floor((y - RULER_H + scrollY) / ROW_H)
    return i >= 0 && i < rows.length ? { row: rows[i], index: i } : null
  }

  const onDown = (ev: React.MouseEvent) => {
    if (!scene) return
    const r = ev.currentTarget.getBoundingClientRect()
    const x = ev.clientX - r.left, y = ev.clientY - r.top
    const f = Math.round(x2f(x))
    const st = useStore.getState()

    if (ev.button === 1) { drag.current = { mode: 'pan', x0: x, sx0: scrollX }; return }

    if (y < RULER_H) {
      if (ev.shiftKey) { st.setLoop(f, f); drag.current = { mode: 'loop', anchor: f } }
      else { st.setFrame(f); drag.current = { mode: 'scrub' } }
      return
    }
    const hit = rowAt(y)
    if (!hit) return
    const { row } = hit

    if (x < HEADER_W) {
      if (row.kind === 'camera') { st.toggleCameraExpanded(); st.select({ kind: 'camera' }) }
      else if (row.kind === 'camchan') { st.select({ kind: 'camera' }); st.setActiveChannel(row.path) }
      else if (row.kind === 'object') st.select({ kind: 'object', id: row.id })
      else if (row.kind === 'effect') st.select({ kind: 'effect', uid: row.uid })
      return
    }

    if (row.kind === 'effect') {
      const e = scene.effects.find((x2) => x2.uid === row.uid)!
      const xa = f2x(e.start), xb = f2x(e.end)
      if (x >= xa - 4 && x <= xb + 4) {
        st.select({ kind: 'effect', uid: e.uid })
        const grab = x < xa + 5 ? 'in' : x > xb - 5 ? 'out' : 'move'
        drag.current = { mode: 'clip', uid: e.uid, grab, f0: f, s0: e.start, e0: e.end }
        return
      }
    }
    if (row.kind === 'camchan') { st.select({ kind: 'camera' }); st.setActiveChannel(row.path) }
    if (row.kind === 'object') st.select({ kind: 'object', id: row.id })
    st.setFrame(f)
    drag.current = { mode: 'scrub' }
  }

  const onMove = (ev: React.MouseEvent) => {
    const r = ev.currentTarget.getBoundingClientRect()
    const x = ev.clientX - r.left, y = ev.clientY - r.top
    setHover({ x, y })
    const d = drag.current
    if (!d) return
    const f = Math.round(x2f(x))
    const st = useStore.getState()
    switch (d.mode) {
      case 'scrub': st.setFrame(f); break
      case 'loop': st.setLoop(d.anchor, f); if (!st.loopOn) st.toggleLoop(); break
      case 'pan': st.setScroll(d.sx0 - (x - d.x0) / pxPerFrame, scrollY); break
      case 'clip': {
        const dd = f - d.f0
        if (d.grab === 'move') st.moveEffect(d.uid, d.s0 + dd, d.e0 + dd)
        else if (d.grab === 'in') st.moveEffect(d.uid, Math.min(d.s0 + dd, d.e0 - 1), d.e0)
        else st.moveEffect(d.uid, d.s0, Math.max(d.e0 + dd, d.s0 + 1))
        break
      }
    }
  }

  const onUp = () => { drag.current = null }

  const onWheel = (ev: React.WheelEvent) => {
    const r = ev.currentTarget.getBoundingClientRect()
    const x = ev.clientX - r.left
    const st = useStore.getState()
    if (ev.ctrlKey || ev.altKey || ev.metaKey) st.zoomBy(ev.deltaY < 0 ? 1.18 : 1 / 1.18, x2f(x))
    else if (ev.shiftKey) st.setScroll(scrollX + ev.deltaY / pxPerFrame, scrollY)
    else {
      const maxY = Math.max(0, rows.length * ROW_H - (wrapRef.current?.clientHeight ?? 0) + RULER_H + 8)
      st.setScroll(scrollX, Math.min(maxY, Math.max(0, scrollY + ev.deltaY)))
    }
  }

  const onDrop = (ev: React.DragEvent) => {
    ev.preventDefault()
    const hex = ev.dataTransfer.getData('text/effect-hexid')
    if (!hex) return
    const def = catalog.find((d) => d.hexid === hex)
    if (!def) return
    const r = ev.currentTarget.getBoundingClientRect()
    onDropEffect(def, Math.max(0, Math.round(x2f(ev.clientX - r.left))))
  }

  const cursor = (() => {
    if (!hover || !scene) return 'default'
    if (hover.y < RULER_H) return 'ew-resize'
    const hit = rowAt(hover.y)
    if (hit?.row.kind === 'effect' && hover.x > HEADER_W) {
      const e = scene.effects.find((x) => x.uid === (hit.row as { uid: string }).uid)!
      const xa = f2x(e.start), xb = f2x(e.end)
      if ((hover.x >= xa - 4 && hover.x <= xa + 5) || (hover.x >= xb - 5 && hover.x <= xb + 4)) return 'ew-resize'
      if (hover.x > xa && hover.x < xb) return 'grab'
    }
    return 'default'
  })()

  return (
    <div className="tl-wrap" ref={wrapRef}>
      <canvas
        ref={ref}
        style={{ cursor }}
        onMouseDown={onDown}
        onMouseMove={onMove}
        onMouseUp={onUp}
        onMouseLeave={() => { onUp(); setHover(null) }}
        onWheel={onWheel}
        onDragOver={(e) => e.preventDefault()}
        onDrop={onDrop}
      />
    </div>
  )
}
