import { useEffect, useRef, useState } from 'react'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'
import { AX_CUTSCENE, AY_EFF_CUTSCENE, applyEngineBasis, engineProjection, frustumCorners, hFov, rotUnitsToRad, vFov } from '../lib/camera'
import { sample } from '../lib/channel'
import { lookDev } from '../lib/lookdev'
import { buildStage, fetchStage } from '../lib/stage'
import { LAYER_CHARA, StageLighting, fetchLights } from '../lib/lights'
import { num } from '../lib/format'
import { useStore } from '../store'
import type { Scene } from '../types'

const NEAR = 20
const FAR = 600_000

function labelSprite(s: string, color = '#c8cdd6') {
  const cv = document.createElement('canvas')
  const ctx = cv.getContext('2d')!
  ctx.font = '600 28px Inter, sans-serif'
  cv.width = Math.ceil(ctx.measureText(s).width) + 16
  cv.height = 40
  const c2 = cv.getContext('2d')!
  c2.font = '600 28px Inter, sans-serif'
  c2.fillStyle = '#000000aa'; c2.fillRect(0, 0, cv.width, cv.height)
  c2.fillStyle = color; c2.textBaseline = 'middle'; c2.fillText(s, 8, 21)
  const tex = new THREE.CanvasTexture(cv)
  const sp = new THREE.Sprite(new THREE.SpriteMaterial({ map: tex, depthTest: false, transparent: true }))
  sp.scale.set(cv.width * 3.2, cv.height * 3.2, 1)
  return sp
}

/** the world point an effect is anchored to, if any of its params is a vec3 */
function effectAnchor(params: Record<string, unknown>, get: (k: string) => number): THREE.Vector3 | null {
  const base = Object.keys(params).find((k) => k.endsWith('.x') && params[k.slice(0, -2) + '.y'] && params[k.slice(0, -2) + '.z'])
  if (!base) return null
  const p = base.slice(0, -2)
  return new THREE.Vector3(get(`${p}.x`), get(`${p}.y`), get(`${p}.z`))
}

export function Viewport() {
  const host = useRef<HTMLDivElement | null>(null)
  const scene = useStore((s) => s.scene)
  const viewMode = useStore((s) => s.viewMode)
  const wireframe = useStore((s) => s.wire)
  const [guide, setGuide] = useState({ l: 0, t: 0, w: 0, h: 0 })
  const [hud, setHud] = useState({ screen: 0, hfov: 0, vfov: 0, roll: 0, px: 0, py: 0, pz: 0 })
  const [fx, setFx] = useState<string[]>([])
  const [freeCamHud, setFreeCamHud] = useState<string>('')
  const [picked, setPicked] = useState<{ name: string; kind: string; pos: number[]; rot: number[]; scale: number } | null>(null)

  useEffect(() => {
    const el = host.current
    if (!el || !scene) return

    const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false })
    renderer.setPixelRatio(Math.min(2, window.devicePixelRatio))
    renderer.setClearColor(0x0b0c0e)
    // The models are PBR with real textures; without tone mapping the stacked lights blow
    // every surface to flat white and you cannot see the texture at all.
    renderer.toneMapping = THREE.ACESFilmicToneMapping
    renderer.toneMappingExposure = 1.9 // the stages are night-lit; expose for the editor, not for fidelity
    el.appendChild(renderer.domElement)

    const world = new THREE.Scene()
    world.fog = new THREE.Fog(0x0b0c0e, 60_000, 400_000)

    const grid = new THREE.GridHelper(400_000, 80, 0x2c3440, 0x1a1f27)
    ;(grid.material as THREE.Material).depthWrite = false
    world.add(grid)
    world.add(new THREE.AxesHelper(4000))
    // Lighting comes from TWO places and they layer:
    //  - the STAGE's .lt2: ambient + one parallel + N point lights. This is the level's real
    //    rig, and it is what makes an interior an interior.
    //  - the CUTSCENE's own DM_ChangeAmbient / DM_ChangeParallel packets, hard-set every
    //    frame, which override the stage's globals for the shot.
    const lighting = new StageLighting(world)
    void fetchLights(scene.sourcePath).then((ls) => lighting.set(ls))

    // Depth-of-field planes: the focus effects carry real world distances from the camera.
    const mkPlane = (col: number) => {
      const m = new THREE.Mesh(
        new THREE.PlaneGeometry(1, 1),
        new THREE.MeshBasicMaterial({ color: col, transparent: true, opacity: 0.06, side: THREE.DoubleSide, depthWrite: false }),
      )
      const edge = new THREE.LineSegments(
        new THREE.EdgesGeometry(new THREE.PlaneGeometry(1, 1)),
        new THREE.LineBasicMaterial({ color: col, transparent: true, opacity: 0.5 }),
      )
      m.add(edge)
      m.visible = false
      world.add(m)
      return m
    }
    const nearPlane = mkPlane(0x64c8ff)
    const farPlane = mkPlane(0xff8a5c)

    // ---- the stage: the world the camera actually flies through ----
    let stageLive: THREE.Group | null = null
    let stageHidden: THREE.Group | null = null
    void fetchStage(scene.sourcePath).then((st) => {
      if (!st) return
      const built = buildStage(st, wireframe)
      stageLive = built.live
      stageHidden = built.hidden
      // the hidden layer is REFERENCE, not world: near-solid it reads as the
      // stage's old Manhattan standing beside the putobj one (the reference overlay)
      built.hidden.traverse((n) => {
        const mesh = n as THREE.Mesh
        if (!mesh.isMesh) return
        for (const m of (Array.isArray(mesh.material) ? mesh.material : [mesh.material])) {
          const sm = m as THREE.MeshStandardMaterial
          sm.transparent = true
          sm.opacity = 0.12
          sm.depthWrite = false
        }
      })
      world.add(built.live)
      world.add(built.hidden)
      if (built.base) renderer.setClearColor(built.base)
      if (st.fog) {
        const fg = world.fog as THREE.Fog
        fg.color.setRGB(st.fog.color[0] / 255, st.fog.color[1] / 255, st.fog.color[2] / 255)
        fg.near = Math.max(1, st.fog.near)
        fg.far = st.fog.far
      }
    })

    // ---- camera path ----
    const cam = scene.camera
    const pts: THREE.Vector3[] = []
    const step = Math.max(1, Math.floor(scene.frames / 1200))
    for (let f = 0; f < scene.frames; f += step) {
      pts.push(new THREE.Vector3(sample(cam.posX, f), sample(cam.posY, f), sample(cam.posZ, f)))
    }
    world.add(new THREE.Line(
      new THREE.BufferGeometry().setFromPoints(pts),
      new THREE.LineBasicMaterial({ color: 0xff9a3c }),
    ))
    const trgPts: THREE.Vector3[] = []
    for (let f = 0; f < scene.frames; f += step) {
      trgPts.push(new THREE.Vector3(sample(cam.trgX, f), sample(cam.trgY, f), sample(cam.trgZ, f)))
    }
    world.add(new THREE.Line(
      new THREE.BufferGeometry().setFromPoints(trgPts),
      new THREE.LineBasicMaterial({ color: 0x3d7fbf }),
    ))

    // key dots: wherever any camera channel carries a key
    const keyFrames = new Set<number>()
    for (const ch of [cam.posX, cam.posY, cam.posZ, cam.trgX, cam.trgY, cam.trgZ, cam.screen, cam.roll]) {
      for (const k of ch.keys ?? []) keyFrames.add(k.frame)
    }
    for (const m of scene.markers) keyFrames.add(m.frame)
    const dots = new THREE.Points(
      new THREE.BufferGeometry().setFromPoints([...keyFrames].map((f) =>
        new THREE.Vector3(sample(cam.posX, f), sample(cam.posY, f), sample(cam.posZ, f)))),
      new THREE.PointsMaterial({ color: 0xffffff, size: 220, sizeAttenuation: true }),
    )
    world.add(dots)

    // ---- cast proxies ----
    type Motion = { frames: number; nb: number; data: Float32Array; skins: THREE.SkinnedMesh[] }
    const motion = new Map<number, Motion>()
    const proxies = new Map<number, THREE.Object3D>()
    // Editor furniture: labels and bounds. Useful when flying around, but they cover the
    // shot in camera view -- which is the one view that has to show only what the game shows.
    const furniture: THREE.Object3D[] = []
    const loader = new GLTFLoader()
    // Revalidate. GLTFLoader fetches from script, so a hard reload does NOT bypass the disk
    // cache for these -- and a model's glTF changes whenever the decoder gains textures or
    // skins. Without this you silently keep rendering last week's geometry.
    loader.setRequestHeader({ 'Cache-Control': 'no-cache' })
    for (const o of scene.objects) {
      const g = new THREE.Group()
      const tall = o.nJoints > 0
      const box = new THREE.Mesh(
        new THREE.BoxGeometry(tall ? 500 : 900, tall ? 1700 : 700, tall ? 400 : 900),
        new THREE.MeshLambertMaterial({ color: tall ? 0x6f8fb0 : 0x6d7a68 }),
      )
      box.position.y = tall ? 850 : 350
      g.add(box)
      const bounds3 = new THREE.Box3Helper(new THREE.Box3().setFromObject(box), new THREE.Color(0x9fb0c4))
      g.add(bounds3)
      furniture.push(bounds3)
      const lab = labelSprite(o.label || String(o.id))
      lab.position.y = tall ? 1900 : 900
      g.add(lab)
      furniture.push(lab)
      // DEMO_OBJECTs are lit as CHARACTERS, not as level geometry: the top-2 CHARAONLY
      // lights at 2.25x, not the full BG rig (light.c:686). Layers keep the two apart.
      g.traverse((n) => n.layers.set(LAYER_CHARA))
      g.userData.demoObj = o
      world.add(g)
      proxies.set(o.id, g)

      // Real geometry when the Go side can cook one; the proxy stays as the fallback.
      // Prefer the EVM: cacheId is the skinned body the engine actually renders. It is 0 on
      // 79% of shipped objects -- a rigid jointless prop is KMS-only and perfectly legal --
      // and then the KMS mesh is the whole model.
      const skinned = o.cacheId !== 0 || (o.flag & 3) !== 0
      const meshId = o.cacheId || o.kmsCacheId
      if (meshId) {
        loader.load(
          `/api/gltf?cacheId=${meshId}`,
          (gltf) => {
            box.visible = false
            // DG_DEF.tx/ty/tz is added to the root ONLY for ONEPIECE (rigid) objects
            // (DG_SetObjsMatrix). 31 jointed demo models carry a nonzero one and must NOT
            // get it, so gate on the flag rather than applying it blindly.
            if ((o.flag & 3) === 0) {
              const org = (gltf.scene.userData?.origin as [number, number, number]) ?? [0, 0, 0]
              gltf.scene.position.set(org[0], org[1], org[2])
            }
            // Keep the model's real materials -- they carry the game's textures. Wireframe
            // is a flag ON those materials, not a replacement for them.
            gltf.scene.traverse((n) => {
              const mesh = n as THREE.Mesh
              if (!mesh.isMesh) return
              for (const m of (Array.isArray(mesh.material) ? mesh.material : [mesh.material])) {
                const mat = m as THREE.MeshStandardMaterial
                mat.wireframe = wireframe
                // Hair is six alpha-blended cards. They are single-sided planes and they
                // write depth, so by default they cull themselves and punch holes in each
                // other -- and you get a bald character.
                mat.side = THREE.DoubleSide
                if (mat.transparent) {
                  mat.depthWrite = false
                  mesh.renderOrder = 1
                }
              }
            })
            g.add(gltf.scene)
            gltf.scene.traverse((n) => n.layers.set(LAYER_CHARA))

            // Skeletal animation: the demo's joint stream, already posed onto this model's
            // skeleton by the server, so there is no matrix maths here -- just assignment.
            if (!skinned || o.nJoints === 0) return
            const skins: THREE.SkinnedMesh[] = []
            gltf.scene.traverse((n) => {
              const sm = n as THREE.SkinnedMesh
              if (sm.isSkinnedMesh) skins.push(sm)
            })
            if (!skins.length) return

            void fetch(`/api/motion?path=${encodeURIComponent(scene.sourcePath)}&object=${o.id}`)
              .then((r) => (r.ok ? r.arrayBuffer() : null))
              .then((buf) => {
                if (!buf) return
                const head = new Uint32Array(buf, 0, 2)
                const frames = head[0], nb = head[1]
                const data = new Float32Array(buf, 8)
                if (nb !== skins[0].skeleton.bones.length) {
                  console.warn(`obj${o.id}: ${nb} posed bones vs ${skins[0].skeleton.bones.length} in the skin`)
                  return
                }
                motion.set(o.id, { frames, nb, data, skins })
              })
              .catch(() => {})
          },
          undefined,
          () => {}, // no geometry for this id: keep the box
        )
      }
    }

    // ---- effect anchors ----
    const anchors = new Map<string, THREE.Object3D>()
    for (const e of scene.effects) {
      const g = new THREE.Group()
      const oct = new THREE.Mesh(
        new THREE.OctahedronGeometry(180),
        new THREE.MeshBasicMaterial({ color: e.execType === 1 ? 0x9cb87f : 0x7ba3cf, wireframe: true }),
      )
      g.add(oct)
      const lab = labelSprite(e.label, e.execType === 1 ? '#cfe3b5' : '#bcd6f0')
      lab.position.y = 380
      g.add(lab)
      furniture.push(lab)
      g.visible = false
      world.add(g)
      anchors.set(e.uid, g)
    }

    // ---- putobj ghosts: the mesg-driven world, played back per frame ----
    type PEvent = { frame: number; cmd: number; args: [number, number, number] }
    type PGhost = { g: THREE.Group; entry: any; events: PEvent[] }
    const ghosts: PGhost[] = []
    ;(window as any).__ghosts = ghosts
    void fetch(`/api/putobjs?path=${encodeURIComponent(scene.sourcePath)}`)
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (!data) return
        for (const entry of data.putobjs) {
          const g = new THREE.Group()
          g.position.set(entry.p[0], entry.p[1], entry.p[2])
          // the PORT's putobj composite, measured in-game: the spin applies LAST in
          // world frame = three 'YXZ' (the authored values are pre-converted to match)
          g.rotation.order = 'YXZ'
          g.rotation.set(rotUnitsToRad(entry.r[0]), rotUnitsToRad(entry.r[1]), rotUnitsToRad(entry.r[2]))
          g.scale.setScalar((entry.s?.[0] ?? 1000) / 100)
          g.visible = !entry.hidden
          g.userData.putobj = entry
          world.add(g)
          ghosts.push({ g, entry, events: data.tracks?.[entry.code] ?? [] })
          loader.load(
            `/api/gltf?cacheId=${entry.modelCacheId}`,
            (gltf) => {
              gltf.scene.traverse((n) => {
                const mesh = n as THREE.Mesh
                if (!mesh.isMesh) return
                for (const m of (Array.isArray(mesh.material) ? mesh.material : [mesh.material])) {
                  const sm = m as THREE.MeshStandardMaterial
                  sm.wireframe = wireframe
                  // ghosts must read against night scenes (flat teal on a black hull
                  // is invisible) -- a faint self-glow keeps them visible anywhere
                  if (sm.emissive) { sm.emissive.set(0x1e3d3d); sm.emissiveIntensity = 1.0 }
                }
              })
              g.add(gltf.scene)
            },
            undefined,
            () => {
              // no geometry for this id -- a marker keeps it visible and selectable
              const mk = new THREE.Mesh(
                new THREE.BoxGeometry(600, 600, 600),
                new THREE.MeshLambertMaterial({ color: 0xc9a15f }),
              )
              g.add(mk)
            },
          )
        }
      })
      .catch(() => {})

    // shift-click picks a putobj or demo object; the panel shows its live transform
    const ray = new THREE.Raycaster()
    ray.layers.enableAll()
    const pickedRef = { cur: null as THREE.Object3D | null }
    const guideRef = { l: 0, t: 0, w: 0, h: 0 }
    const describe = (n: THREE.Object3D) => {
      const po = n.userData.putobj
      const od = n.userData.demoObj
      setPicked({
        name: po ? po.name : (od?.label || `obj${od?.id}`),
        kind: po ? `putobj (code ${po.code})` : 'demo object',
        pos: [n.position.x, n.position.y, n.position.z],
        rot: [n.rotation.x, n.rotation.y, n.rotation.z].map((r) => (r * 4096) / (Math.PI * 2)),
        scale: n.scale.x,
      })
    }
    const onPick = (ev: PointerEvent) => {
      if (ev.button !== 0 || !ev.shiftKey) return
      const r0 = el.getBoundingClientRect()
      const stv = useStore.getState()
      let nx = ((ev.clientX - r0.left) / r0.width) * 2 - 1
      let ny = -((ev.clientY - r0.top) / r0.height) * 2 + 1
      if (stv.viewMode === 'camera' && guideRef.w > 0) {
        // the camera view renders letterboxed: rays must map through THAT rect
        nx = ((ev.clientX - r0.left - guideRef.l) / guideRef.w) * 2 - 1
        ny = -((ev.clientY - r0.top - guideRef.t) / guideRef.h) * 2 + 1
        if (nx < -1 || nx > 1 || ny < -1 || ny > 1) return
      }
      const cam2 = stv.viewMode === 'free' ? freeCam : gameCam
      ray.setFromCamera(new THREE.Vector2(nx, ny), cam2)
      ;(ray as any).camera = cam2   // Sprite.raycast dereferences it
      const cand: THREE.Object3D[] = []
      for (const gh of ghosts) if (gh.g.visible) cand.push(gh.g)
      for (const [, g] of proxies) if (g.visible) cand.push(g)
      const hits = ray.intersectObjects(cand, true)

      if (!hits.length) {
        pickedRef.cur = null
        setPicked(null)
        return
      }
      let n: THREE.Object3D | null = hits[0].object
      while (n && !n.userData.putobj && !n.userData.demoObj) n = n.parent
      if (!n) return
      pickedRef.cur = n
      describe(n)
    }
    el.addEventListener('pointerdown', onPick)

    // ---- frustum gizmo ----
    const gizmo = new THREE.Group()
    const flines = new THREE.LineSegments(
      new THREE.BufferGeometry().setAttribute('position', new THREE.BufferAttribute(new Float32Array(16 * 3), 3)),
      new THREE.LineBasicMaterial({ color: 0xffd9b0 }),
    )
    gizmo.add(flines)
    const up = new THREE.ArrowHelper(new THREE.Vector3(0, 1, 0), new THREE.Vector3(), 900, 0x54a06b)
    gizmo.add(up)
    world.add(gizmo)

    const gameCam = new THREE.PerspectiveCamera(50, 16 / 9, NEAR, FAR)
    const freeCam = new THREE.PerspectiveCamera(45, 1, NEAR, FAR)
    gameCam.layers.enableAll()
    freeCam.layers.enableAll()
    const controls = new OrbitControls(freeCam, renderer.domElement)

    // Frame whatever the demo actually occupies. Cutscenes are authored anywhere in the
    // stage -- the Tanker crash sits at Z ~ -204,000 -- so a fixed viewpoint stares into
    // empty space. Fit to the camera path plus the cast, and scale the grid, fog and far
    // plane to match, or a big scene clips and a small one is a dot.
    const bounds = new THREE.Box3()
    for (const p of pts) bounds.expandByPoint(p)
    for (const o of scene.objects) {
      for (let f = 0; f < scene.frames; f += step) {
        bounds.expandByPoint(new THREE.Vector3(sample(o.posX, f), sample(o.posY, f), sample(o.posZ, f)))
      }
    }
    const centre = bounds.isEmpty() ? new THREE.Vector3() : bounds.getCenter(new THREE.Vector3())
    const radius = bounds.isEmpty()
      ? 40_000
      : Math.max(bounds.getBoundingSphere(new THREE.Sphere()).radius, 5_000)

    freeCam.position.copy(centre).add(new THREE.Vector3(0.75, 0.5, 1).normalize().multiplyScalar(radius * 2.4))
    freeCam.far = radius * 40
    gameCam.far = radius * 40
    freeCam.updateProjectionMatrix()
    gameCam.updateProjectionMatrix()
    controls.target.copy(centre)

    world.fog = new THREE.Fog(0x0b0c0e, radius * 2, radius * 12)
    grid.position.set(centre.x, 0, centre.z)
    grid.scale.setScalar((radius * 6) / 400_000)

    controls.enableDamping = true
    controls.zoomSpeed = 1.4
    controls.panSpeed = 1.2
    ;(controls as unknown as { screenSpacePanning: boolean }).screenSpacePanning = true

    // Big-world navigation. Orbit speed scales with distance to the PIVOT, and the pivot
    // starts mid-harbour -- so flying to the city 200k away slows to a crawl. Three outs:
    // wheel zooms toward the cursor, double-click re-pivots onto whatever you hit, and
    // WASD+QE flies free (shift = fast). F snaps the pivot to the ship at the playhead.
    ;(controls as unknown as { zoomToCursor: boolean }).zoomToCursor = true
    const flyKeys = new Set<string>()
    const lookKeys = new Set<string>()
    let shiftDown = false
    let flyMult = 1
    let flyHitDist = Infinity
    let flyRayCooldown = 0
    const typing = (ev: KeyboardEvent) => {
      const t = ev.target as HTMLElement | null
      return !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)
    }
    const focusShip = () => {
      const st = useStore.getState()
      const sc2 = st.scene as Scene
      const ship = sc2.objects.find((o) => o.id === 8) ?? sc2.objects[0]
      const p = ship
        ? new THREE.Vector3(sample(ship.posX, st.frame), sample(ship.posY, st.frame) + 1500, sample(ship.posZ, st.frame))
        : centre.clone()
      const off = freeCam.position.clone().sub(controls.target)
      off.setLength(Math.min(off.length(), 45_000))
      controls.target.copy(p)
      freeCam.position.copy(p).add(off)
    }
    const onKeyDown = (ev: KeyboardEvent) => {
      if (typing(ev)) return
      shiftDown = ev.shiftKey
      const k = ev.key.toLowerCase()
      if (useStore.getState().viewMode !== 'free') return
      if (k === 'f') { focusShip(); return }
      if (k === '\\') { freeCam.fov = 45; freeCam.updateProjectionMatrix(); return }
      if (k === '[' || k === ']') {
        freeCam.fov = THREE.MathUtils.clamp(freeCam.fov * (k === '[' ? 1.15 : 1 / 1.15), 5, 100)
        freeCam.updateProjectionMatrix()
        return
      }
      if (k === '-' || k === '=') {
        flyMult = THREE.MathUtils.clamp(k === '-' ? flyMult / 2 : flyMult * 2, 0.0625, 16)
        return
      }
      if (k.length === 1 && 'wasdqe'.includes(k)) flyKeys.add(k)
      if (k.startsWith('arrow')) { ev.preventDefault(); lookKeys.add(k) }
    }
    const onKeyUp = (ev: KeyboardEvent) => {
      shiftDown = ev.shiftKey
      flyKeys.delete(ev.key.toLowerCase())
      lookKeys.delete(ev.key.toLowerCase())
    }
    const onBlur = () => { flyKeys.clear(); lookKeys.clear() }
    const caster = new THREE.Raycaster()
    const onDblClick = (ev: MouseEvent) => {
      if (useStore.getState().viewMode !== 'free') return
      const r = renderer.domElement.getBoundingClientRect()
      caster.setFromCamera(new THREE.Vector2(
        ((ev.clientX - r.left) / r.width) * 2 - 1,
        -((ev.clientY - r.top) / r.height) * 2 + 1,
      ), freeCam)
      const hit = caster.intersectObjects(world.children, true).find((h) => (h.object as THREE.Mesh).isMesh)
      const p = hit ? hit.point : caster.ray.at(freeCam.position.distanceTo(controls.target), new THREE.Vector3())
      controls.target.copy(p)
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    window.addEventListener('blur', onBlur)
    renderer.domElement.addEventListener('dblclick', onDblClick)

    let prevX: THREE.Vector3 | undefined
    let lastFreeLine = ''
    let lastViewMode = ''
    let raf = 0
    let lastFrame = -1

    const tick = () => {
      raf = requestAnimationFrame(tick)
      const st = useStore.getState()
      const sc = st.scene as Scene
      const f = st.frame
      const w = el.clientWidth, h = el.clientHeight
      if (!w || !h) return

      const s = {
        pos: new THREE.Vector3(sample(sc.camera.posX, f), sample(sc.camera.posY, f), sample(sc.camera.posZ, f)),
        trg: new THREE.Vector3(sample(sc.camera.trgX, f), sample(sc.camera.trgY, f), sample(sc.camera.trgZ, f)),
        screen: sample(sc.camera.screen, f) || 2,
        roll: sample(sc.camera.roll, f),
      }

      // entering free view: start AT the demo camera's current shot, lens included --
      // scouting becomes relative nudges from the real framing
      if (st.viewMode === 'free' && lastViewMode !== 'free') {
        freeCam.position.copy(s.pos)
        controls.target.copy(s.trg)
        freeCam.fov = (vFov(s.screen) * 180) / Math.PI
        freeCam.updateProjectionMatrix()
      }
      lastViewMode = st.viewMode

      prevX = applyEngineBasis(gameCam, s, prevX)
      gameCam.projectionMatrix.copy(engineProjection(s.screen, NEAR, FAR, AX_CUTSCENE, AY_EFF_CUTSCENE))
      gameCam.projectionMatrixInverse.copy(gameCam.projectionMatrix).invert()

      // frustum wireframe: apex + a rect at depth, in the game camera's frame
      const corners = frustumCorners(s.screen, 6000).map((v) => v.applyMatrix4(gameCam.matrixWorld))
      const apex = s.pos
      const seg: number[] = []
      for (let i = 0; i < 4; i++) {
        seg.push(apex.x, apex.y, apex.z, corners[i].x, corners[i].y, corners[i].z)
        const n = corners[(i + 1) % 4]
        seg.push(corners[i].x, corners[i].y, corners[i].z, n.x, n.y, n.z)
      }
      const attr = flines.geometry.getAttribute('position') as THREE.BufferAttribute
      attr.copyArray(new Float32Array(seg))
      attr.needsUpdate = true
      up.position.copy(apex)
      up.setDirection(new THREE.Vector3(0, 1, 0).applyQuaternion(gameCam.quaternion))

      // ---- the cutscene's own look: lighting, fog, focus planes ----
      const look = lookDev(sc, f)
      lighting.override(look.ambient, look.parallel)

      // The hero light is chosen by proximity to the CAST, not the camera.
      let hero: THREE.Vector3 | null = null
      let nHero = 0
      for (const o of sc.objects) {
        const g = proxies.get(o.id)
        if (!g?.visible) continue
        hero = (hero ?? new THREE.Vector3()).add(g.position)
        nHero++
      }
      if (hero && nHero) hero.divideScalar(nHero)
      lighting.update(s.pos, hero)
      if (look.fog) {
        const fg = world.fog as THREE.Fog
        fg.color.copy(look.fog.colour)
        fg.near = look.fog.near
        fg.far = look.fog.far
        renderer.setClearColor(look.fog.colour)
      }
      const fwd = new THREE.Vector3().subVectors(s.trg, s.pos).normalize()
      const sizePlane = (m: THREE.Mesh, dist: number | null) => {
        m.visible = dist != null && dist > 0
        if (!m.visible || dist == null) return
        const half = Math.tan(hFov(s.screen, AX_CUTSCENE) / 2) * dist
        m.scale.set(half * 2, (half * 2) * (9 / 16), 1)
        m.position.copy(s.pos).addScaledVector(fwd, dist)
        m.quaternion.copy(gameCam.quaternion)
      }
      sizePlane(nearPlane, look.focus.near)
      sizePlane(farPlane, look.focus.far)

      if (f !== lastFrame) {
        lastFrame = f

        for (const [, m] of motion) {
          const fr = Math.min(f, m.frames - 1)
          const base = fr * m.nb * 7
          const bones = m.skins[0].skeleton.bones
          for (let i = 0; i < m.nb; i++) {
            const k = base + i * 7
            bones[i].quaternion.set(m.data[k], m.data[k + 1], m.data[k + 2], m.data[k + 3])
            bones[i].position.set(m.data[k + 4], m.data[k + 5], m.data[k + 6])
          }
        }

        for (const o of sc.objects) {
          const g = proxies.get(o.id)
          if (!g) continue
          g.position.set(sample(o.posX, f), sample(o.posY, f), sample(o.posZ, f))
          g.rotation.set(rotUnitsToRad(sample(o.rotX, f)), rotUnitsToRad(sample(o.rotY, f)), rotUnitsToRad(sample(o.rotZ, f)))
          let vis = false
          for (const v of o.visibility) if (v.frame <= f) vis = v.visible
          g.visible = vis
        }
        for (const e of sc.effects) {
          const g = anchors.get(e.uid)
          if (!g) continue
          const live = f >= e.start && f < e.end
          const p = effectAnchor(e.params, (k) => sample(e.params[k], f))
          g.visible = live && !!p
          if (p) g.position.copy(p)
        }
        setHud({
          screen: s.screen,
          hfov: (hFov(s.screen) * 180) / Math.PI,
          vfov: (vFov(s.screen) * 180) / Math.PI,
          roll: (s.roll * 180) / Math.PI,
          px: s.pos.x, py: s.pos.y, pz: s.pos.z,
        })
        setFx(look.screen)
      }

      const poMode = st.putobjMode ?? 'scripted'
      for (const gh of ghosts) {
        let vis = !gh.entry.hidden
        let px2 = gh.entry.p[0], py2 = gh.entry.p[1], pz2 = gh.entry.p[2]
        let rx2 = gh.entry.r[0], ry2 = gh.entry.r[1], rz2 = gh.entry.r[2]
        for (const ev of gh.events) {
          if (ev.frame > f) break
          if (ev.cmd === 0 || ev.cmd === 5) vis = false
          else if (ev.cmd === 1) vis = true
          else if (ev.cmd === 2) { rx2 = ev.args[0]; ry2 = ev.args[1]; rz2 = ev.args[2] }
          else if (ev.cmd === 3) { px2 = ev.args[0]; py2 = ev.args[1]; pz2 = ev.args[2] }
        }
        gh.g.visible = poMode === 'off' ? false : poMode === 'all' ? true : vis
        // 'all' mode: script-hidden ghosts render as faint phantoms, not solid
        // world -- solid they read as doubled geometry (the Manhattan overlay)
        const ghostly = poMode === 'all' && !vis
        if ((gh as any).wasGhostly !== ghostly) {
          ;(gh as any).wasGhostly = ghostly
          gh.g.traverse((n) => {
            const mesh = n as THREE.Mesh
            if (!mesh.isMesh) return
            for (const m of (Array.isArray(mesh.material) ? mesh.material : [mesh.material])) {
              const sm = m as THREE.MeshStandardMaterial
              sm.transparent = ghostly
              sm.opacity = ghostly ? 0.15 : 1.0
              sm.depthWrite = !ghostly
            }
          })
        }
        gh.g.position.set(px2, py2, pz2)
        gh.g.rotation.set(rotUnitsToRad(rx2), rotUnitsToRad(ry2), rotUnitsToRad(rz2))
      }
      if (pickedRef.cur) describe(pickedRef.cur)

      if (stageLive) stageLive.visible = st.showStage
      if (stageHidden) stageHidden.visible = st.showStage && st.showHidden

      // Location scouting: surface the FREE camera's own pos/target so a framing found by
      // flying can be read straight off and keyed into the demo camera.
      if (st.viewMode === 'free') {
        const p2 = freeCam.position, t2 = controls.target
        const scr = 1 / (Math.tan((freeCam.fov * Math.PI) / 360) * freeCam.aspect)
        const line = `free cam pos (${p2.x.toFixed(0)} ${p2.y.toFixed(0)} ${p2.z.toFixed(0)})  target (${t2.x.toFixed(0)} ${t2.y.toFixed(0)} ${t2.z.toFixed(0)})  screen ${scr.toFixed(2)}  fly x${flyMult}`
        if (line !== lastFreeLine) { lastFreeLine = line; setFreeCamHud(line) }
      }

      renderer.setSize(w, h, false)
      const free = st.viewMode === 'free'
      gizmo.visible = free
      dots.visible = free
      grid.visible = free
      for (const o of furniture) o.visible = free

      if (st.viewMode === 'camera') {
        // the game renders that projection onto a 16:9 backbuffer -- letterbox to match
        const aw = Math.min(w, h * (16 / 9)), ah = aw / (16 / 9)
        const l = Math.round((w - aw) / 2), t = Math.round((h - ah) / 2)
        renderer.setScissorTest(true)
        renderer.setViewport(0, 0, w, h)
        renderer.setScissor(0, 0, w, h)
        renderer.setClearColor(0x000000)
        renderer.clear()
        renderer.setViewport(l, h - t - ah, aw, ah)
        renderer.setScissor(l, h - t - ah, aw, ah)
        renderer.setClearColor(0x0b0c0e)
        renderer.render(world, gameCam)
        renderer.setScissorTest(false)
        guideRef.l = l; guideRef.t = t; guideRef.w = aw; guideRef.h = ah
        setGuide((g) => (g.l === l && g.t === t && g.w === aw && g.h === ah ? g : { l, t, w: aw, h: ah }))
      } else {
        if (lookKeys.size) {
          // turn IN PLACE: rotate the pivot around the camera, not the camera around
          // the pivot -- the offset vector swings, the camera position never moves
          const rate = 0.02 * (shiftDown ? 2.5 : 1)
          const dir = new THREE.Vector3().subVectors(controls.target, freeCam.position)
          const yaw = (lookKeys.has('arrowleft') ? 1 : 0) - (lookKeys.has('arrowright') ? 1 : 0)
          if (yaw) dir.applyAxisAngle(new THREE.Vector3(0, 1, 0), yaw * rate)
          const pitch = (lookKeys.has('arrowup') ? 1 : 0) - (lookKeys.has('arrowdown') ? 1 : 0)
          if (pitch) {
            const right = new THREE.Vector3().crossVectors(dir, freeCam.up).normalize()
            const tilted = dir.clone().applyAxisAngle(right, pitch * rate)
            const flat = Math.hypot(tilted.x, tilted.z)
            if (flat > Math.abs(tilted.y) * 0.02) dir.copy(tilted) // no pole-flip
          }
          controls.target.copy(freeCam.position).add(dir)
        }
        if (flyKeys.size) {
          // speed follows the NEARER of the pivot and whatever is ahead of the lens: fast
          // over open water, micro-adjustable at street level. -/= halve/double on top.
          if (flyRayCooldown-- <= 0) {
            flyRayCooldown = 15
            caster.setFromCamera(new THREE.Vector2(0, 0), freeCam)
            const solids = [stageLive, stageHidden].filter(Boolean) as THREE.Object3D[]
            const hit = solids.length ? caster.intersectObjects(solids, true)[0] : undefined
            flyHitDist = hit ? hit.distance : Infinity
          }
          const basis = Math.min(freeCam.position.distanceTo(controls.target), flyHitDist)
          const speed = THREE.MathUtils.clamp(basis * 0.012, 8, 2500) * flyMult * (shiftDown ? 6 : 1)
          const fdir = freeCam.getWorldDirection(new THREE.Vector3())
          const rdir = new THREE.Vector3().crossVectors(fdir, freeCam.up).normalize()
          const mv = new THREE.Vector3()
          if (flyKeys.has('w')) mv.add(fdir)
          if (flyKeys.has('s')) mv.sub(fdir)
          if (flyKeys.has('d')) mv.add(rdir)
          if (flyKeys.has('a')) mv.sub(rdir)
          if (flyKeys.has('e')) mv.y += 1
          if (flyKeys.has('q')) mv.y -= 1
          if (mv.lengthSq()) {
            mv.normalize().multiplyScalar(speed)
            freeCam.position.add(mv)
            controls.target.add(mv)
          }
        }
        freeCam.aspect = w / h
        freeCam.updateProjectionMatrix()
        controls.update()
        renderer.setViewport(0, 0, w, h)
        renderer.setClearColor(0x0b0c0e)
        renderer.render(world, freeCam)
        setGuide((g) => (g.w === 0 ? g : { l: 0, t: 0, w: 0, h: 0 }))
      }
    }
    tick()

    return () => {
      el.removeEventListener('pointerdown', onPick)
      cancelAnimationFrame(raf)
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
      window.removeEventListener('blur', onBlur)
      renderer.domElement.removeEventListener('dblclick', onDblClick)
      controls.dispose()
      renderer.dispose()
      el.removeChild(renderer.domElement)
      world.traverse((o) => {
        const m = o as THREE.Mesh
        m.geometry?.dispose?.()
        const mat = m.material as THREE.Material | THREE.Material[] | undefined
        if (Array.isArray(mat)) mat.forEach((x) => x.dispose())
        else mat?.dispose?.()
      })
    }
  }, [scene, wireframe])

  return (
    <div className="viewport" ref={host}>
      {picked && (
        <div style={{
          position: 'absolute', top: 34, left: 10, zIndex: 20, pointerEvents: 'none',
          background: 'rgba(12,14,18,0.88)', border: '1px solid #3a4150',
          borderLeft: '3px solid #c9a15f', borderRadius: 4, padding: '7px 11px',
          font: '12px/1.5 ui-monospace, monospace', color: '#cfd6e0', whiteSpace: 'nowrap',
        }}>
          <div style={{ fontSize: 13 }}><b style={{ color: '#e8d5a8' }}>{picked.name}</b>
            <span style={{ opacity: 0.65, marginLeft: 8 }}>{picked.kind}</span></div>
          <div>pos <b>{picked.pos.map((v) => v.toFixed(0)).join('  ')}</b></div>
          <div>rot <b>{picked.rot.map((v) => v.toFixed(0)).join('  ')}</b> <span style={{ opacity: 0.6 }}>/4096</span>
            <span style={{ opacity: 0.8, marginLeft: 6 }}>({picked.rot.map((v) => ((v * 360) / 4096).toFixed(1)).join('  ')}&deg;)</span></div>
          <div>scale <b>x{picked.scale.toFixed(2)}</b> <span style={{ opacity: 0.5, marginLeft: 10 }}>shift-click to pick / re-pick</span></div>
        </div>
      )}
      <div className="vp-overlay">
        {viewMode === 'camera' && guide.w > 0 && (
          <div style={{
            position: 'absolute', left: guide.l, top: guide.t, width: guide.w, height: guide.h,
            outline: '1px solid #ffffff22',
            backgroundImage:
              'linear-gradient(to right, transparent 33.33%, #ffffff10 33.33%, #ffffff10 33.5%, transparent 33.5%, transparent 66.66%, #ffffff10 66.66%, #ffffff10 66.83%, transparent 66.83%),' +
              'linear-gradient(to bottom, transparent 33.33%, #ffffff10 33.33%, #ffffff10 33.6%, transparent 33.6%, transparent 66.66%, #ffffff10 66.66%, #ffffff10 66.93%, transparent 66.93%)',
          }}>
            <div style={{ position: 'absolute', right: 4, bottom: 2, color: '#6e7683' }}>16:9 FRAME</div>
          </div>
        )}
        <div className="vp-stats">
          <div>screen <b>{num(hud.screen, 4)}</b>  hFOV <b>{num(hud.hfov, 2)}&deg;</b>  vFOV <b>{num(hud.vfov, 2)}&deg;</b></div>
          <div>ax 1.0  ay_eff 1.70819  (cutscene, cinema bars -- vert- crop)</div>
          <div>roll <b>{num(hud.roll, 3)}&deg;</b>  pos {num(hud.px, 0)} {num(hud.py, 0)} {num(hud.pz, 0)}</div>
          {freeCamHud && <div style={{ color: '#8fd0a8' }}>{freeCamHud}  <span className="dim">WASD+QE fly · arrows look (in place) · shift fast · dblclick re-pivot · F = ship · [ ] zoom · \ reset · - = speed</span></div>}
          {fx.length > 0 && (
            <div style={{ marginTop: 4 }}>
              screen fx{' '}
              {fx.map((n) => (
                <span key={n} style={{ color: '#e0b46f', marginRight: 8 }}>{n}</span>
              ))}
              <span style={{ color: '#5c646f' }}>(post-process -- not drawn in 3D)</span>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
