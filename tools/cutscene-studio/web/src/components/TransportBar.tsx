import { useState } from 'react'
import { frameToTick, timecode } from '../lib/format'
import { useStore } from '../store'

export function TransportBar() {
  const scene = useStore((s) => s.scene)
  const frame = useStore((s) => s.frame)
  const playing = useStore((s) => s.playing)
  const loopOn = useStore((s) => s.loopOn)
  const loopIn = useStore((s) => s.loopIn)
  const loopOut = useStore((s) => s.loopOut)
  const backend = useStore((s) => s.backend)
  const viewMode = useStore((s) => s.viewMode)
  const bottomMode = useStore((s) => s.bottomMode)
  const findings = useStore((s) => s.findings)
  const baking = useStore((s) => s.baking)
  const bakeMsg = useStore((s) => s.bakeMsg)
  const staged = useStore((s) => s.staged)
  const wire = useStore((s) => s.wire)
  const showStage = useStore((s) => s.showStage)
  const putobjMode = useStore((s) => s.putobjMode)
  const showHidden = useStore((s) => s.showHidden)
  const deploying = useStore((s) => s.deploying)
  const [showFindings, setShowFindings] = useState(false)

  if (!scene) return <div className="transport"><span className="brand">MGS2 <b>CUTSCENE STUDIO</b></span><span className="dim">loading&hellip;</span></div>

  const st = useStore.getState()
  const fatal = findings.filter((f) => f.severity === 'fatal').length
  const warn = findings.filter((f) => f.severity === 'warn').length
  const dirty = scene.effects.some((e) => e.dirty)

  return (
    <>
      <div className="transport">
        <span className="brand">MGS2 <b>CUTSCENE STUDIO</b></span>

        <button className="btn" title="Home" onClick={() => st.setFrame(0)}>|&lt;</button>
        <button className="btn" title="step -1 (left arrow)" onClick={() => st.step(-1)}>&lt;</button>
        <button className={'btn' + (playing ? ' on' : '')} style={{ minWidth: 34 }} title="space" onClick={() => st.togglePlay()}>
          {playing ? '‖' : '▶'}
        </button>
        <button className="btn" title="step +1 (right arrow)" onClick={() => st.step(1)}>&gt;</button>
        <button className="btn" title="End" onClick={() => st.setFrame(scene.frames - 1)}>&gt;|</button>

        <div className="sep" />
        <span className="tc">{timecode(frame)}</span>
        <input
          type="number" className="tc mono" style={{ width: 66 }} value={frame}
          onChange={(e) => st.setFrame(Number(e.target.value))}
        />
        <span className="tc small" title="the container clock is 300 Hz; one demo frame = 5 ticks">
          t {frameToTick(frame)} / {frameToTick(scene.frames)}
        </span>
        <span className="dim mono">{scene.frames}f &middot; {(scene.frames / 60).toFixed(2)}s</span>

        <div className="sep" />
        <button className={'btn' + (loopOn ? ' on' : '')} onClick={() => st.toggleLoop()}>loop</button>
        <span className="mono dim">{loopIn}&ndash;{loopOut}</span>
        <button className="btn" title="set loop in/out to the current frame" onClick={() => st.setLoop(frame, loopOut)}>[</button>
        <button className="btn" onClick={() => st.setLoop(loopIn, frame)}>]</button>

        <div className="sep" />
        <button className={'btn' + (viewMode === 'free' ? ' on' : '')} onClick={() => st.setViewMode('free')}>free</button>
        <button className={'btn' + (viewMode === 'camera' ? ' on' : '')} onClick={() => st.setViewMode('camera')}>camera</button>
        <button className={'btn' + (wire ? ' on' : '')} title="draw models as wireframe" onClick={() => st.toggleWire()}>wire</button>
        <button className={'btn' + (showStage ? ' on' : '')} title="draw the stage: map geometry, sky, sea" onClick={() => st.toggleStage()}>stage</button>
        <button className={'btn' + (showHidden ? ' on' : '')} title="reveal geometry the stage builds but never switches on (e.g. the Manhattan skyline)" onClick={() => st.toggleHidden()}>hidden</button>
        <button className={'btn' + (putobjMode !== 'off' ? ' on' : '')}
          title="putobj ghosts: scripted = play the mesg on/off stream, all = show everything, off = hide"
          onClick={() => st.cyclePutobjMode()}>po:{putobjMode}</button>
        <div className="sep" />
        <button className={'btn' + (bottomMode === 'timeline' ? ' on' : '')} onClick={() => st.setBottomMode('timeline')}>timeline</button>
        <button className={'btn' + (bottomMode === 'curve' ? ' on' : '')} onClick={() => st.setBottomMode('curve')}>curves</button>

        <div className="spacer" style={{ flex: 1 }} />

        {!scene.hasAudio && <span className="badge" title="no PCM track: the demo runs on the vsync wall clock">no audio</span>}
        {scene.hasAudio && scene.audioFrames < scene.frames && (
          <span className="badge" style={{ borderColor: '#7a3a44', color: '#d78e98' }}
            title="the demo outruns its audio; the clock falls back to vsync. Legal, not an error.">
            audio ends {timecode(scene.audioFrames)}
          </span>
        )}
        <span className={'badge ' + backend}>{backend}</span>
        {dirty && <span className="badge" style={{ borderColor: '#7a5a2f', color: '#e0b46f' }}>modified</span>}

        <div className="sep" />
        {findings.length > 0 && (
          <button className="btn" onClick={() => setShowFindings((v) => !v)}>
            {fatal > 0 && <span style={{ color: 'var(--fatal)' }}>{fatal} fatal</span>}
            {fatal > 0 && warn > 0 && <span className="dim">/</span>}
            {warn > 0 && <span style={{ color: 'var(--warn)' }}>{warn} warn</span>}
            {fatal === 0 && warn === 0 && <span style={{ color: 'var(--ok)' }}>clean</span>}
          </button>
        )}
        {bakeMsg && <span className="mono dim">{bakeMsg}</span>}
        <button
          className="btn primary" disabled={baking || fatal > 0}
          title={fatal > 0 ? 'fatal findings block the bake' : 'bake to the staging folder (does not touch the game)'}
          onClick={() => { void st.runBake().then(() => setShowFindings(true)) }}
        >
          {baking ? 'baking…' : 'BAKE'}
        </button>
        <button
          className="btn" disabled={!staged || deploying || fatal > 0}
          title={staged
            ? `overwrite the game's .sdt with the staged bake (keeps a .orig backup)`
            : 'bake first'}
          onClick={() => { void st.runDeploy() }}
        >
          {deploying ? 'deploying…' : 'DEPLOY'}
        </button>
      </div>

      {showFindings && findings.length > 0 && (
        <div className="findings">
          <div className="panel-head">
            <span>validator</span>
            <div className="spacer" />
            <button className="btn" onClick={() => setShowFindings(false)}>close</button>
          </div>
          {findings.map((f, i) => (
            <div key={i} className={'finding ' + f.severity}>
              <span className="sev">{f.severity}</span>
              <div className="msg">
                <div>{f.msg}</div>
                <div className="rule">{f.rule}{f.fix ? ` — ${f.fix}` : ''}</div>
              </div>
              {f.frame !== undefined && (
                <span className="frm" onClick={() => st.setFrame(f.frame!)}>f{f.frame}</span>
              )}
            </div>
          ))}
        </div>
      )}
    </>
  )
}
