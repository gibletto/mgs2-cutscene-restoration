import * as THREE from 'three'
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js'

export type Placement = {
  name: string
  label?: string
  cacheId: number
  ext: string
  pos: [number, number, number]
  rot: [number, number, number]
  scale: [number, number, number]
  visible: boolean
  created: boolean
}

export type SkyWall = { name: string; cacheId: number; ext: string; tint: [number, number, number] }
export type Toggle = { proc: string; procId: number; on: string[]; off: string[] }

export type Stage = {
  name: string
  unknown?: boolean
  toggles?: Toggle[]
  placements: Placement[]
  sky?: { kind: string; params: { baseColor?: [number, number, number]; walls?: SkyWall[] } }
  sea?: { kind: string; pos: [number, number, number]; size: [number, number] }
  fog?: { color: [number, number, number]; near: number; far: number }
}

export async function fetchStage(demoPath: string): Promise<Stage | null> {
  try {
    const r = await fetch(`/api/stage?path=${encodeURIComponent(demoPath)}`)
    if (!r.ok) return null
    const s = (await r.json()) as Stage
    return s.unknown ? null : s
  } catch {
    return null
  }
}

const rgb = (c: [number, number, number]) => new THREE.Color(c[0] / 255, c[1] / 255, c[2] / 255)

/**
 * Build the level. Placements whose proc never runs, or that the stage switches off, are
 * still returned by the API -- the Manhattan skyline is BUILT and simply never enabled --
 * so they come in as a separate group the UI can reveal.
 */
export function buildStage(st: Stage, wire: boolean) {
  const live = new THREE.Group()
  const hidden = new THREE.Group()
  hidden.visible = false
  const loader = new GLTFLoader()
  loader.setRequestHeader({ 'Cache-Control': 'no-cache' })

  // Live geometry keeps the model's own textured materials -- overriding them is how you
  // silently throw the game's textures away.
  //
  // GHOST geometry (built by the stage but never switched on -- the Manhattan skyline is the
  // famous one) gets an UNLIT overlay instead. A dimmed PBR material is worthless here: these
  // stages are night-lit, so 30% opacity on an unlit-by-anything surface is invisible. Unlit
  // means it shows up regardless of what the cutscene's lights are doing.
  // Ghost = solid and TEXTURED, but UNLIT. Wireframe reads as a bug in a solid scene, and a
  // lit material would be black: these stages are night-lit and we do not yet parse the .lt2
  // lighting sets. Unlit keeps the real texture visible whatever the lights are doing.
  const ghost = (src: THREE.Material) => {
    const from = src as THREE.MeshStandardMaterial
    return new THREE.MeshBasicMaterial({
      map: from.map ?? null,
      color: from.map ? 0x9fd8e4 : 0x6fb6c6,
      transparent: true,
      opacity: 0.7,
      side: THREE.DoubleSide,
      wireframe: wire,
      depthWrite: true,
      fog: false,
    })
  }

  const dress = (o: THREE.Object3D, dim: boolean) => {
    o.traverse((n) => {
      const mesh = n as THREE.Mesh
      if (!mesh.isMesh) return
      const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material]
      if (dim) {
        mesh.material = Array.isArray(mesh.material) ? mats.map(ghost) : ghost(mats[0])
        return
      }
      for (const m of mats) {
        const mat = m as THREE.MeshStandardMaterial
        mat.wireframe = wire
        mat.side = THREE.DoubleSide
      }
    })
  }

  const place = (p: Placement, group: THREE.Group, dim: boolean) => {
    if (!p.cacheId) return
    loader.load(
      `/api/gltf?cacheId=${p.cacheId}&ext=${p.ext}&origin=1`,
      (g) => {
        // origin=1: the server bakes DG_DEF.tx/ty/tz into the vertices. A map area has no -p
        // in the script -- its world position lives in the KMS header -- and translating the
        // node here would NOT work, because three.js cancels a skinned mesh's own world matrix.
        const o = g.scene
        const at = new THREE.Group()
        at.position.set(...p.pos)
        at.rotation.set(p.rot[0], p.rot[1], p.rot[2])
        at.scale.set(...p.scale)
        at.add(o)
        dress(o, dim)
        group.add(at)
      },
      undefined,
      () => {},
    )
  }

  for (const p of st.placements) {
    const on = p.created && p.visible
    place(p, on ? live : hidden, !on)
  }

  // The sky is real geometry: a flat base colour plus up to 3 tinted wall meshes.
  // No cubemap, no dome, no procedural gradient.
  const walls = st.sky?.params?.walls ?? []
  for (const w of walls) {
    loader.load(
      `/api/gltf?cacheId=${w.cacheId}&ext=${w.ext}&origin=1`,
      (g) => {
        const o = g.scene
        o.traverse((n) => {
          const m = n as THREE.Mesh
          if (!m.isMesh) return
          m.material = new THREE.MeshBasicMaterial({ color: rgb(w.tint), side: THREE.BackSide, fog: false })
        })
        live.add(o)
      },
      undefined,
      () => {},
    )
  }

  if (st.sea) {
    const sea = new THREE.Mesh(
      new THREE.PlaneGeometry(st.sea.size[0], st.sea.size[1], 1, 1),
      new THREE.MeshLambertMaterial({ color: 0x1b2c3a, transparent: true, opacity: 0.55, side: THREE.DoubleSide, wireframe: wire }),
    )
    sea.rotation.x = -Math.PI / 2
    sea.position.set(st.sea.pos[0], st.sea.pos[1], st.sea.pos[2])
    live.add(sea)
  }

  return { live, hidden, base: st.sky?.params?.baseColor ? rgb(st.sky.params.baseColor) : null }
}
