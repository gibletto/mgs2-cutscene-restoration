import { useCallback, useEffect, useRef, useState } from 'react'
import { connectWS } from './api'
import { CurveEditor } from './components/CurveEditor'
import { EffectBrowser } from './components/EffectBrowser'
import { StagePanel } from './components/StagePanel'
import { useState as useTabState } from 'react'
import { Properties } from './components/Properties'
import { Timeline } from './components/Timeline'
import { TransportBar } from './components/TransportBar'
import { Viewport } from './components/Viewport'
import { useStore } from './store'
import type { EffectDef, Finding } from './types'

function RightPanel() {
  const [tab, setTab] = useTabState<'props' | 'stage'>('props')
  return (
    <div style={{ display: 'flex', flexDirection: 'column', minHeight: 0, borderLeft: '1px solid var(--line)' }}>
      <div className="panel-head">
        <button className={'btn' + (tab === 'props' ? ' on' : '')} onClick={() => setTab('props')}>properties</button>
        <button className={'btn' + (tab === 'stage' ? ' on' : '')} onClick={() => setTab('stage')}>stage</button>
      </div>
      <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
        {tab === 'props' ? <Properties /> : <StagePanel />}
      </div>
    </div>
  )
}

export function App() {
  const scene = useStore((s) => s.scene)
  const loading = useStore((s) => s.loading)
  const bottomMode = useStore((s) => s.bottomMode)
  const [bottomH, setBottomH] = useState(320)
  const resizing = useRef(false)

  useEffect(() => { void useStore.getState().load() }, [])

  useEffect(() => connectWS((m) => {
    if (m.type === 'findings') useStore.setState({ findings: (m.findings ?? []) as Finding[] })
    if (m.type === 'scene') void useStore.getState().load()
  }), [])

  // playback: real time, 60 Hz demo frames, honouring the loop range
  useEffect(() => {
    let raf = 0, last = performance.now(), acc = 0
    const tick = (t: number) => {
      raf = requestAnimationFrame(tick)
      const st = useStore.getState()
      const dt = t - last
      last = t
      if (!st.playing || !st.scene) { acc = 0; return }
      acc += dt
      const adv = Math.floor(acc / (1000 / 60))
      if (adv <= 0) return
      acc -= adv * (1000 / 60)
      let f = st.frame + adv
      const hi = st.loopOn ? st.loopOut : st.scene.frames - 1
      const lo = st.loopOn ? st.loopIn : 0
      if (f > hi) f = st.loopOn ? lo + ((f - lo) % Math.max(1, hi - lo + 1)) : hi
      st.setFrame(f)
      if (!st.loopOn && f >= hi) st.setPlaying(false)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      if (t && (t.tagName === 'INPUT' || t.tagName === 'SELECT' || t.tagName === 'TEXTAREA')) return
      const st = useStore.getState()
      const big = e.shiftKey ? 10 : 1
      switch (e.key) {
        case ' ': e.preventDefault(); st.togglePlay(); break
        case 'ArrowLeft': case 'ArrowRight': case 'ArrowUp': case 'ArrowDown':
          // in FREE view the arrows belong to the camera (turn in place, Viewport)
          if (st.viewMode === 'free') break
          e.preventDefault()
          if (e.key === 'ArrowLeft') st.step(-big)
          if (e.key === 'ArrowRight') st.step(big)
          break
        case 'Home': st.setFrame(0); break
        case 'End': if (st.scene) st.setFrame(st.scene.frames - 1); break
        case 'k': case 'K': if (st.activeChannel) st.toggleKeyAt(st.activeChannel, st.frame); break
        case '+': case '=': st.zoomBy(1.25); break
        case '-': case '_': st.zoomBy(1 / 1.25); break
        case 'l': case 'L': st.toggleLoop(); break
        case 'c': case 'C': st.setBottomMode(st.bottomMode === 'curve' ? 'timeline' : 'curve'); break
        case 'Delete': case 'Backspace':
          if (st.selection.kind === 'effect') st.removeEffect(st.selection.uid)
          break
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!resizing.current) return
      setBottomH(Math.max(120, Math.min(window.innerHeight - 260, window.innerHeight - e.clientY)))
    }
    const onUp = () => { resizing.current = false }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    return () => { window.removeEventListener('mousemove', onMove); window.removeEventListener('mouseup', onUp) }
  }, [])

  const onDropEffect = useCallback((def: EffectDef, frame: number) => {
    useStore.getState().addEffect(def, frame)
  }, [])

  if (loading || !scene) {
    return <div style={{ display: 'grid', placeItems: 'center', height: '100%', color: '#6e7683' }}>loading catalog + scene…</div>
  }

  return (
    <div className="app" style={{ ['--bottom-h' as string]: `${bottomH}px` }}>
      <TransportBar />
      <div className="mid">
        <EffectBrowser />
        <div className="panel">
          <div className="panel-head">
            <span>viewport</span>
            <div className="spacer" />
            <span className="mono dim">{scene.sourcePath}</span>
          </div>
          <Viewport />
        </div>
        <RightPanel />
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', minHeight: 0 }}>
        <div className="splitter" onMouseDown={() => { resizing.current = true }} />
        <div style={{ flex: 1, minHeight: 0 }}>
          {bottomMode === 'timeline'
            ? <Timeline onDropEffect={onDropEffect} />
            : <CurveEditor />}
        </div>
      </div>
    </div>
  )
}
