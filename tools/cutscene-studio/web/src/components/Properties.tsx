import { useMemo } from 'react'
import { ROLL_STEP, hFov, quantiseRoll, screenFromHFov, vFov } from '../lib/camera'
import { sample } from '../lib/channel'
import { hex8, num } from '../lib/format'
import { getChannel, useStore } from '../store'
import type { EffectDef, EffectParam } from '../types'

/** one keyable scalar: diamond + label + widget */
function Row({ path, label, children, dim }: { path?: string; label: string; children: React.ReactNode; dim?: boolean }) {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const active = useStore((s) => s.activeChannel)
  const ch = scene && path ? getChannel(scene, path) : undefined
  const keyed = !!ch?.keys?.some((k) => k.frame === frame)
  const animated = (ch?.keys?.length ?? 0) > 0 || (ch?.baked?.length ?? 0) > 1

  return (
    <div className={'row' + (path && active === path ? ' active' : '')}>
      {path ? (
        <button
          className={'diamond' + (keyed ? ' keyed' : animated ? ' animated' : '')}
          title={keyed ? 'remove key at this frame' : 'key at this frame (K)'}
          onClick={() => useStore.getState().toggleKeyAt(path, frame)}
        >
          <svg width="9" height="9" viewBox="0 0 10 10">
            <path d="M5 0 L10 5 L5 10 L0 5 Z" fill={keyed ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="1.4" />
          </svg>
        </button>
      ) : <span />}
      <label
        style={dim ? { color: '#4d545e' } : undefined}
        onClick={() => path && useStore.getState().setActiveChannel(path)}
      >{label}</label>
      {children}
    </div>
  )
}

function Num({ path, step = 1, min, max }: { path: string; step?: number; min?: number; max?: number }) {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const v = scene ? sample(getChannel(scene, path), frame) : 0
  return (
    <input
      type="number" step={step} min={min} max={max}
      value={Number.isInteger(v) ? v : Number(v.toFixed(4))}
      onChange={(e) => useStore.getState().setChannelValue(path, Number(e.target.value))}
    />
  )
}

function Slider({ path, min, max }: { path: string; min: number; max: number }) {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const v = scene ? sample(getChannel(scene, path), frame) : 0
  const set = (x: number) => useStore.getState().setChannelValue(path, x)
  return (
    <div className="slider">
      <input type="range" min={min} max={max} step={(max - min) / 200} value={v} onChange={(e) => set(Number(e.target.value))} />
      <input type="number" value={Number(v.toFixed(3))} onChange={(e) => set(Number(e.target.value))} />
    </div>
  )
}

function Vec3({ base }: { base: string }) {
  return (
    <div className="vec3">
      {(['x', 'y', 'z'] as const).map((a) => <Num key={a} path={`${base}.${a}`} step={0.01} />)}
    </div>
  )
}

function Color({ base }: { base: string }) {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const g = (c: string) => (scene ? Math.round(sample(getChannel(scene, `${base}.${c}`), frame)) : 0)
  const [r, gg, b, a] = [g('r'), g('g'), g('b'), g('a')]
  const hex = '#' + [r, gg, b].map((x) => Math.max(0, Math.min(255, x)).toString(16).padStart(2, '0')).join('')
  return (
    <div className="color-row">
      <input
        type="color" className="swatch" value={hex} style={{ padding: 0, border: 'none', background: 'none' }}
        onChange={(e) => {
          const v = e.target.value
          const st = useStore.getState()
          st.setChannelValue(`${base}.r`, parseInt(v.slice(1, 3), 16))
          st.setChannelValue(`${base}.g`, parseInt(v.slice(3, 5), 16))
          st.setChannelValue(`${base}.b`, parseInt(v.slice(5, 7), 16))
        }}
      />
      {(['r', 'g', 'b', 'a'] as const).map((c) => <Num key={c} path={`${base}.${c}`} step={1} min={0} max={255} />)}
      <span style={{ display: 'none' }}>{a}</span>
    </div>
  )
}

/** MaxInputComboBox / MaxInputListBox pack their choices as "label=value&label=value" */
function parseChoices(input: unknown): Array<[string, number]> {
  if (typeof input !== 'string') return []
  return input.split('&').map((kv) => {
    const i = kv.lastIndexOf('=')
    const label = kv.slice(0, i)
    const raw = kv.slice(i + 1).trim()
    return [label, raw.startsWith('0x') ? parseInt(raw, 16) : Number(raw)] as [string, number]
  }).filter(([, v]) => Number.isFinite(v))
}

function ParamWidget({ uid, p }: { uid: string; p: EffectParam }) {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const base = `effect.${uid}.${p.name}`
  const label = p.label_en || p.label_ja || p.name

  switch (p.ui) {
    case 'rgba-color':
      return <Row path={`${base}.r`} label={label}><Color base={base} /></Row>
    case 'vec3-float': case 'point-helper':
      return <Row path={`${base}.x`} label={`${label} (mm)`}><Vec3 base={base} /></Row>
    case 'vec3-short':
      return <Row path={`${base}.x`} label={`${label} (4096=360d)`}><Vec3 base={base} /></Row>
    case 'float-slider':
      return <Row path={base} label={label}><Slider path={base} min={p.min ?? 0} max={p.max ?? 1} /></Row>
    case 'int-slider':
      return <Row path={base} label={label}><Slider path={base} min={p.min ?? 0} max={p.max ?? 100} /></Row>
    case 'combo': {
      const choices = parseChoices((p.widgetArgsNamed as Record<string, unknown>)?.input)
      const v = scene ? sample(getChannel(scene, base), frame) : 0
      return (
        <Row path={base} label={label}>
          <select value={v} onChange={(e) => useStore.getState().setChannelValue(base, Number(e.target.value))}>
            {choices.map(([l, val]) => <option key={val} value={val}>{l} ({val})</option>)}
            {!choices.some(([, val]) => val === v) && <option value={v}>{v}</option>}
          </select>
        </Row>
      )
    }
    case 'flags-listbox': {
      const choices = parseChoices((p.widgetArgsNamed as Record<string, unknown>)?.input)
      const v = scene ? sample(getChannel(scene, base), frame) : 0
      return (
        <Row path={base} label={label}>
          <div>
            {choices.map(([l, bit]) => (
              <label key={bit} style={{ display: 'flex', gap: 4, alignItems: 'center', color: '#8b939f' }}>
                <input
                  type="checkbox" checked={(v & bit) !== 0}
                  onChange={(e) => useStore.getState().setChannelValue(base, e.target.checked ? v | bit : v & ~bit)}
                />
                <span className="mono" style={{ fontSize: 10 }}>{l} 0x{bit.toString(16)}</span>
              </label>
            ))}
            <Num path={base} step={1} />
          </div>
        </Row>
      )
    }
    case 'string-128':
      return <Row label={`${label} (char[128], not keyable)`}><input type="text" defaultValue="" maxLength={128} /></Row>
    case 'strcode-text': case 'implicit-effect-name': case 'link-implicit':
    case 'scene-object-picker': case 'model-joint-picker': case 'internal-message': {
      const v = scene ? sample(getChannel(scene, base), frame) : 0
      return (
        <Row path={base} label={label}>
          <div className="slider">
            <Num path={base} step={1} />
            <span className="mono dim" style={{ fontSize: 10 }}>{hex8(v)}</span>
          </div>
        </Row>
      )
    }
    case 'implicit-key-max': case 'implicit-key-min': case 'implicit-frame-rate':
      return <Row path={base} label={label} dim><Num path={base} step={1} /></Row>
    case 'int-edit': case 'implicit-int':
      return <Row path={base} label={label}><Num path={base} step={1} /></Row>
    default:
      return <Row path={base} label={label}><Num path={base} step={0.01} /></Row>
  }
}

function CameraProps() {
  const scene = useStore((s) => s.scene)!
  const frame = useStore((s) => s.frame)
  const screen = sample(scene.camera.screen, frame) || 2
  const roll = sample(scene.camera.roll, frame)
  return (
    <>
      <div className="prop-group">
        <h4>camera &mdash; DEMO_FRAME (hard-set every frame)</h4>
        <Row path="camera.posX" label="pos.x"><Num path="camera.posX" step={1} /></Row>
        <Row path="camera.posY" label="pos.y"><Num path="camera.posY" step={1} /></Row>
        <Row path="camera.posZ" label="pos.z"><Num path="camera.posZ" step={1} /></Row>
        <Row path="camera.trgX" label="trg.x"><Num path="camera.trgX" step={1} /></Row>
        <Row path="camera.trgY" label="trg.y"><Num path="camera.trgY" step={1} /></Row>
        <Row path="camera.trgZ" label="trg.z"><Num path="camera.trgZ" step={1} /></Row>
      </div>
      <div className="prop-group">
        <h4>projection</h4>
        <Row path="camera.screen" label="screen"><Num path="camera.screen" step={0.001} /></Row>
        <Row label="hFOV (deg)">
          <input
            type="number" step={0.1} value={Number(((hFov(screen) * 180) / Math.PI).toFixed(3))}
            onChange={(e) => useStore.getState().setChannelValue('camera.screen', screenFromHFov((Number(e.target.value) * Math.PI) / 180))}
          />
        </Row>
        <Row label="vFOV (deg)" dim>
          <input type="number" readOnly value={Number(((vFov(screen) * 180) / Math.PI).toFixed(3))} />
        </Row>
        <Row path="camera.roll" label="roll (rad)"><Num path="camera.roll" step={ROLL_STEP} /></Row>
        <Row label="roll quantised" dim>
          <input type="number" readOnly value={Number(((quantiseRoll(roll) * 180) / Math.PI).toFixed(4))} />
        </Row>
      </div>
      <div className="hint">
        <code>screen</code> is a zoom scalar, not an FOV: <code>fov = 2&middot;atan(1/(ax&middot;screen))</code>.
        Cutscene regime <code>ax = 1.0</code>, <code>ay_eff = 1.70819</code>.<br />
        Roll quantises to 2&pi;/4096 = 0.0879&deg; in the engine &mdash; finer detail is destroyed.<br />
        A cut and a move are the same thing: two FRAME packets. Never emit a second CAMERA_CREATE.
      </div>
    </>
  )
}

function ObjectProps({ id }: { id: number }) {
  const scene = useStore((s) => s.scene)!
  const frame = useStore((s) => s.frame)
  const o = scene.objects.find((x) => x.id === id)
  if (!o) return null
  let vis = false
  for (const v of o.visibility) if (v.frame <= frame) vis = v.visible
  const deg = (u: number) => num((u * 360) / 4096, 2)
  return (
    <>
      <div className="prop-group">
        <h4>object &mdash; {o.label}</h4>
        <Row label="id" dim><span className="mono">{o.id}</span></Row>
        <Row label="name (StrCode)" dim><span className="mono">{hex8(o.name)}</span></Row>
        <Row label="cache / kms" dim><span className="mono">{hex8(o.cacheId)} / {hex8(o.kmsCacheId)}</span></Row>
        <Row label="flag" dim><span className="mono">{o.flag} {o.flag === 0 ? '(rigid)' : o.flag === 1 ? '(jointed)' : '(preshaded)'}</span></Row>
        <Row label="visible here" dim><span className="mono" style={{ color: vis ? '#4fa46a' : '#6e7683' }}>{vis ? 'ON' : 'OFF'}</span></Row>
      </div>
      <div className="prop-group">
        <h4>transform</h4>
        <Row path={`object.${id}.posX`} label="pos.x"><Num path={`object.${id}.posX`} step={1} /></Row>
        <Row path={`object.${id}.posY`} label="pos.y"><Num path={`object.${id}.posY`} step={1} /></Row>
        <Row path={`object.${id}.posZ`} label="pos.z"><Num path={`object.${id}.posZ`} step={1} /></Row>
        <Row path={`object.${id}.rotX`} label={`rot.x (${deg(sample(o.rotX, frame))}d)`}><Num path={`object.${id}.rotX`} step={1} /></Row>
        <Row path={`object.${id}.rotY`} label={`rot.y (${deg(sample(o.rotY, frame))}d)`}><Num path={`object.${id}.rotY`} step={1} /></Row>
        <Row path={`object.${id}.rotZ`} label={`rot.z (${deg(sample(o.rotZ, frame))}d)`}><Num path={`object.${id}.rotZ`} step={1} /></Row>
      </div>
      <div className="hint">
        Rotation is 12-bit fixed point: <code>4096 == 360&deg;</code>.
        {o.nJoints > 0
          ? <> Skeletal motion ({o.nJoints} joints) is delta-compressed across the whole demo and passes through untouched.</>
          : <> Rigid prop, no MOTION_PACK.</>}
      </div>
    </>
  )
}

function EffectProps({ uid }: { uid: string }) {
  const scene = useStore((s) => s.scene)!
  const catalog = useStore((s) => s.catalog)
  const e = scene.effects.find((x) => x.uid === uid)
  const def: EffectDef | undefined = useMemo(
    () => catalog.find((d) => d.effectId === e?.effectId), [catalog, e?.effectId])
  if (!e) return null
  const life = e.end - e.start

  return (
    <>
      <div className="prop-group">
        <h4>effect &mdash; {def?.cname ?? e.label}</h4>
        <Row label="effect_id" dim><span className="mono">{hex8(e.effectId)}</span></Row>
        <Row label="exec_type" dim>
          <span className="mono" style={{ color: e.execType === 1 ? '#9cb87f' : '#7ba3cf' }}>
            {e.execType} {e.execType === 1 ? '-- actor, life baked at birth' : '-- per-frame channel'}
          </span>
        </Row>
        <Row label="packet size" dim><span className="mono">{def ? def.packetBodySize + 16 : '--'} B</span></Row>
        <Row label="start"><input type="number" value={e.start} onChange={(ev) => useStore.getState().moveEffect(uid, Number(ev.target.value), e.end)} /></Row>
        <Row label={e.execType === 1 ? `life (${life}f)` : 'end'}>
          <input type="number" value={e.end} onChange={(ev) => useStore.getState().moveEffect(uid, e.start, Number(ev.target.value))} />
        </Row>
      </div>
      {def && def.params.length > 0 && (
        <div className="prop-group">
          <h4>params &mdash; {def.hexid} ({def.params.length})</h4>
          {def.params.map((p) => <ParamWidget key={p.name} uid={uid} p={p} />)}
        </div>
      )}
      {!def && <div className="hint">effect {hex8(e.effectId)} is not in the catalog &mdash; params shown raw.</div>}
      <div className="hint">
        {e.execType === 1
          ? <>exec 1: ONE packet at the start frame, <code>life = {life}</code> frames in the preamble.
              Re-emitting it every frame births a new actor every frame and blows <code>DM_MAX_EFFECT = 512</code>.</>
          : <>exec 0: the packet is re-emitted on every frame of the span with the sampled values. The stream IS the keyframe bake.</>}
        {def?.options.add_src?.length ? <><br />requires assets: <code>{def.options.add_src.join(', ')}</code></> : null}
        <br />Availability is per-stage: an effect not linked into the stage's chara table is silently dropped.
      </div>
      <div className="prop-group">
        <div className="row">
          <span /><span />
          <button className="btn danger" onClick={() => useStore.getState().removeEffect(uid)}>delete effect</button>
        </div>
      </div>
    </>
  )
}

export function Properties() {
  const scene = useStore((s) => s.scene)
  const sel = useStore((s) => s.selection)
  if (!scene) return null
  return (
    <div className="panel">
      <div className="panel-head">
        <span>properties</span>
        <div className="spacer" />
        <span className="mono dim">{sel.kind}</span>
      </div>
      <div className="panel-body">
        {sel.kind === 'camera' && <CameraProps />}
        {sel.kind === 'object' && <ObjectProps id={sel.id} />}
        {sel.kind === 'effect' && <EffectProps uid={sel.uid} />}
        {sel.kind === 'none' && <div className="hint">nothing selected. Pick a track in the timeline, or drag an effect out of the browser.</div>}
      </div>
    </div>
  )
}
