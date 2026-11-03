import { useMemo, useState } from 'react'
import { useStore } from '../store'
import type { EffectDef } from '../types'

export function EffectBrowser() {
  const catalog = useStore((s) => s.catalog)
  const demos = useStore((s) => s.demos)
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const load = useStore((s) => s.load)
  const [q, setQ] = useState('')
  const [execFilter, setExecFilter] = useState<'all' | 0 | 1>('all')

  const hits = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return catalog.filter((d) => {
      if (execFilter !== 'all' && d.execType !== execFilter) return false
      if (!needle) return true
      return (
        d.cname.toLowerCase().includes(needle) ||
        (d.displayName_en ?? '').toLowerCase().includes(needle) ||
        (d.displayName_ja ?? '').includes(q.trim()) ||
        d.hexid.includes(needle)
      )
    })
  }, [catalog, q, execFilter])

  const onDragStart = (ev: React.DragEvent, d: EffectDef) => {
    ev.dataTransfer.setData('text/effect-hexid', d.hexid)
    ev.dataTransfer.effectAllowed = 'copy'
  }

  return (
    <div className="panel">
      <div className="panel-head">
        <span>demos</span>
        <div className="spacer" />
        <span className="mono dim">{demos.length}</span>
      </div>
      <div className="list" style={{ maxHeight: 120, overflow: 'auto', flex: '0 0 auto', borderBottom: '1px solid var(--line)' }}>
        {demos.map((d) => (
          <div
            key={d.path}
            className={'item' + (scene?.sourcePath === d.path ? ' sel' : '')}
            style={{ cursor: 'pointer' }}
            onClick={() => load(d.path)}
          >
            <span className="nm mono">{d.name}</span>
            <span className="id">{d.frames}f</span>
            {!d.hasAudio && <span className="ex">no pcm</span>}
          </div>
        ))}
      </div>

      <div className="panel-head" style={{ marginTop: 0 }}>
        <span>effects</span>
        <div className="spacer" />
        <span className="mono dim">{hits.length}/{catalog.length}</span>
      </div>
      <div style={{ padding: 5, display: 'flex', gap: 4, borderBottom: '1px solid var(--line)' }}>
        <input className="search" placeholder="search 245 effects" value={q} onChange={(e) => setQ(e.target.value)} />
        {(['all', 0, 1] as const).map((x) => (
          <button key={String(x)} className={'btn' + (execFilter === x ? ' on' : '')} onClick={() => setExecFilter(x)}>
            {x === 'all' ? 'all' : 'e' + x}
          </button>
        ))}
      </div>
      <div className="panel-body">
        <div className="list">
          {hits.map((d) => (
            <div
              key={d.hexid + d.cname}
              className="item"
              draggable
              onDragStart={(e) => onDragStart(e, d)}
              onDoubleClick={() => useStore.getState().addEffect(d, frame)}
              title={`${d.cname}  ${d.effectIdHex}  exec ${d.execType}  ${d.params.length} params\ndrag onto the timeline, or double-click to insert at the playhead`}
            >
              <span className={'ex' + (d.execType === 1 ? ' e1' : '')}>e{d.execType}</span>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="nm">{d.displayName_en || d.cname}</div>
                <div className="ja">{d.displayName_ja ?? ''}</div>
              </div>
              <span className="id">{d.hexid}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
