import { useEffect, useRef } from 'react'

/** DPR-aware canvas that repaints on resize and whenever draw() changes identity. */
export function useCanvas(draw: (ctx: CanvasRenderingContext2D, w: number, h: number) => void) {
  const ref = useRef<HTMLCanvasElement | null>(null)
  const sizeRef = useRef({ w: 0, h: 0 })

  useEffect(() => {
    const cv = ref.current
    if (!cv) return
    const paint = () => {
      const parent = cv.parentElement
      if (!parent) return
      const w = parent.clientWidth, h = parent.clientHeight
      const dpr = Math.min(2, window.devicePixelRatio || 1)
      if (cv.width !== Math.round(w * dpr) || cv.height !== Math.round(h * dpr)) {
        cv.width = Math.round(w * dpr); cv.height = Math.round(h * dpr)
      }
      sizeRef.current = { w, h }
      const ctx = cv.getContext('2d')
      if (!ctx) return
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, w, h)
      draw(ctx, w, h)
    }
    paint()
    const ro = new ResizeObserver(paint)
    if (cv.parentElement) ro.observe(cv.parentElement)
    return () => ro.disconnect()
  }, [draw])

  return { ref, size: sizeRef }
}

/** ctx is taken only so call sites read like the other helpers; 1px lines need a half-pixel offset. */
export function crisp(_ctx: CanvasRenderingContext2D, x: number) { return Math.round(x) + 0.5 }

export function clipRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, fn: () => void) {
  ctx.save(); ctx.beginPath(); ctx.rect(x, y, w, h); ctx.clip(); fn(); ctx.restore()
}

export function text(ctx: CanvasRenderingContext2D, s: string, x: number, y: number, color: string, font = '11px Inter, sans-serif') {
  ctx.font = font; ctx.fillStyle = color; ctx.textBaseline = 'middle'; ctx.fillText(s, x, y)
}
