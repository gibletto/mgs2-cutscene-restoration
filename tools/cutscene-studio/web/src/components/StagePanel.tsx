import { useEffect, useState } from 'react'
import { useStore } from '../store'
import type { Stage } from '../lib/stage'
import { fetchStage } from '../lib/stage'

/**
 * The stage's own switches and models.
 *
 * Two things a cutscene can do to its level WITHOUT rebuilding the .gcx:
 *  - FIRE one of the stage's toggle procs (DM_ExecProc). d080p07 ships a "Verrazano bridge ->
 *    Manhattan" switch that Konami wrote, scheduled, and then commented the call out.
 *  - PLACE any model the stage already has resident as a DEMO_OBJECT, which makes it movable
 *    per-frame via MOTION_PACK. The stage's own copies are frozen at the -p the .gcx baked in.
 */
export function StagePanel() {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const [stage, setStage] = useState<Stage | null>(null)
  const [msg, setMsg] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!scene) return
    void fetchStage(scene.sourcePath).then(setStage)
  }, [scene?.sourcePath])

  if (!scene) return null

  const post = async (path: string, body: unknown, what: string) => {
    setBusy(true)
    try {
      const r = await fetch(path, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(body),
      })
      const j = await r.json()
      if (!r.ok) throw new Error(j.error ?? String(r.status))
      setMsg(`${what} — bake to write it`)
      await useStore.getState().reload()
    } catch (e) {
      setMsg(String(e))
    } finally {
      setBusy(false)
    }
  }

  const toggles = stage?.toggles ?? []
  const placements = stage?.placements ?? []

  return (
    <div className="panel" style={{ flex: 1, minWidth: 0 }}>
      <div className="panel-head">
        <span className="mono dim">{stage?.name ?? '—'}</span>
      </div>

      <div style={{ overflow: 'auto', fontSize: 11 }}>
        {msg && <div className="mono dim" style={{ padding: '4px 6px', color: '#e0b46f' }}>{msg}</div>}

        <div className="panel-head" style={{ position: 'sticky', top: 0 }}>length</div>
        <div style={{ padding: '5px 6px', borderBottom: '1px solid #1a1f27' }}>
          <div className="dim">{scene.frames} frames &middot; {(scene.frames / 60).toFixed(1)}s</div>
          {scene.hasAudio && scene.audioFrames < scene.frames && (
            <div className="dim" style={{ color: '#d78e98' }}>audio ends at {(scene.audioFrames / 60).toFixed(1)}s &mdash; the rest runs on the wall clock</div>
          )}
          <div style={{ display: 'flex', gap: 4, marginTop: 4 }}>
            <button className="btn" disabled={busy} style={{ flex: 1 }}
              title="extend by continuation: the original frames stay exact, the ship keeps sailing"
              onClick={() => void post('/api/extend', { path: scene.sourcePath, frames: scene.frames * 2 }, `extended to ${scene.frames * 2} frames`)}>
              extend 2&times;
            </button>
            <button className="btn" disabled={busy} style={{ flex: 1 }}
              onClick={() => void post('/api/extend', { path: scene.sourcePath, frames: Math.round(scene.frames * 1.5) }, 'extended 1.5×')}>
              1.5&times;
            </button>
          </div>
        </div>

        {toggles.length > 0 && (
          <>
            <div className="panel-head" style={{ position: 'sticky', top: 0 }}>switches</div>
            {toggles.map((t) => {
              // Go marshals a nil slice as null, not []
              const on = t.on ?? []
              const off = t.off ?? []
              return (
              <div key={t.procId} style={{ padding: '5px 6px', borderBottom: '1px solid #1a1f27' }}>
                <div className="mono" style={{ color: '#c8cdd6' }}>{t.proc}</div>
                <div className="dim" style={{ margin: '2px 0' }}>
                  on: {on.slice(0, 3).join(', ')}{on.length > 3 ? ` +${on.length - 3}` : ''}
                </div>
                <div className="dim">
                  off: {off.slice(0, 3).join(', ')}{off.length > 3 ? ` +${off.length - 3}` : ''}
                </div>
                <button
                  className="btn" disabled={busy} style={{ marginTop: 4, width: '100%' }}
                  title="add a DM_ExecProc packet at the current frame -- no .gcx rebuild"
                  onClick={() => void post('/api/stage/toggle',
                    { path: scene.sourcePath, procId: t.procId, frame }, `switch fires at frame ${frame}`)}
                >
                  fire at frame {frame}
                </button>
              </div>
              )
            })}
          </>
        )}

        <div className="panel-head" style={{ position: 'sticky', top: 0 }}>
          models ({placements.length})
        </div>
        {placements.map((p, i) => (
          <div key={`${p.name}${i}`} style={{ padding: '4px 6px', borderBottom: '1px solid #14181f' }}>
            <div style={{ display: 'flex', gap: 4, alignItems: 'baseline' }}>
              <span className="mono" style={{ color: p.visible && p.created ? '#c8cdd6' : '#6e7683' }}>{p.name}</span>
              <div className="spacer" style={{ flex: 1 }} />
              {!(p.visible && p.created) && <span className="dim" title="built by the stage but never switched on">off</span>}
            </div>
            {p.label && <div className="dim">{p.label}</div>}
            <button
              className="btn" disabled={busy} style={{ marginTop: 3, width: '100%' }}
              title="declare it as a DEMO_OBJECT so it can be keyed and moved on the timeline"
              onClick={() => void post('/api/stage/place',
                { path: scene.sourcePath, cacheId: p.cacheId, name: p.name, pos: p.pos, rot: [0, 0, 0] },
                `${p.name} added as a movable object`)}
            >
              place as object
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}
