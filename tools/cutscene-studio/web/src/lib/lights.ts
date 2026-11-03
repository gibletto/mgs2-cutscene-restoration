import * as THREE from 'three'

export type StageLight = {
  kind: 'ambient' | 'parallel' | 'point' | 'spot' | 'line' | 'blackpoint'
  color: [number, number, number]
  pos: [number, number, number]
  dir: [number, number, number]
  range: number
  cone: number
  inner: number
  force: number
  chara: boolean
  bg: boolean
  length: number
  min: [number, number, number]
  max: [number, number, number]
}

export async function fetchLights(demoPath: string): Promise<StageLight[]> {
  try {
    const r = await fetch(`/api/lights?path=${encodeURIComponent(demoPath)}`)
    if (!r.ok) return []
    const j = (await r.json()) as { lights?: StageLight[] }
    return j.lights ?? []
  } catch {
    return []
  }
}

/** Layer 0 = the level. Layer 1 = the cast. The engine lights them on separate paths. */
export const LAYER_BG = 0
export const LAYER_CHARA = 1

const BG_POOL = 16
const SPOT_POOL = 4
const CHARA_POOL = 2 // the engine keeps exactly two (light.c:706: `if (offset == 2) continue`)

const v = new THREE.Vector3()
const a = new THREE.Vector3()
const b = new THREE.Vector3()

/**
 * A LINE light is a segment, and the engine shades against the nearest point ON the segment
 * with the same linear falloff. So it collapses to a point light that slides along the line --
 * which is exactly what a point light at the closest point gives you.
 */
function nearestOnLine(l: StageLight, p: THREE.Vector3, out: THREE.Vector3) {
  a.set(l.pos[0], l.pos[1], l.pos[2])
  b.set(l.dir[0], l.dir[1], l.dir[2]).multiplyScalar(l.length || 0)
  const len2 = b.lengthSq()
  if (len2 === 0) return out.copy(a)
  const t = Math.max(0, Math.min(1, v.copy(p).sub(a).dot(b) / len2))
  return out.copy(a).addScaledVector(b, t)
}

/** i = max(0, N.L) * 1.5 * (Range - d)/Range, zero at Range. Linear, not inverse-square. */
function falloff(range: number, dist: number) {
  if (range <= 0 || dist >= range) return 0
  return (1.5 * (range - dist)) / range
}

/**
 * MGS2's two lighting paths, reproduced.
 *
 *  BACKGROUND (pshade.c): ambient + one parallel + every BGONLY point/spot/line in reach.
 *  CHARACTERS (light.c:686): a 3-light matrix -- the TOP TWO CHARAONLY lights by
 *  `falloff x force`, plus the parallel. The 1.5x centre boost is applied TWICE on this path
 *  (light.c:699-700), so the same lamp lights a character ~1.5x brighter than the wall behind
 *  him. That is not a bug to fix; it is the look.
 */
export class StageLighting {
  private ambient = new THREE.AmbientLight(0x000000, 1)
  private sun = new THREE.DirectionalLight(0xffffff, 0)
  private bgPool: THREE.PointLight[] = []
  private spotPool: THREE.SpotLight[] = []
  private charaPool: THREE.PointLight[] = []

  private points: StageLight[] = [] // point + line, BG
  private spots: StageLight[] = []
  private chara: StageLight[] = [] // point + line, CHARAONLY

  constructor(world: THREE.Scene) {
    this.ambient.layers.enableAll()
    this.sun.layers.enableAll() // the parallel lights both the level and the cast
    world.add(this.ambient, this.sun, this.sun.target)

    for (let i = 0; i < BG_POOL; i++) {
      const l = new THREE.PointLight(0xffffff, 0, 1, 0) // decay 0: see the note above
      l.visible = false
      l.layers.set(LAYER_BG)
      world.add(l)
      this.bgPool.push(l)
    }
    for (let i = 0; i < SPOT_POOL; i++) {
      const l = new THREE.SpotLight(0xffffff, 0, 1, 0.5, 0.4, 0)
      l.visible = false
      l.layers.set(LAYER_BG)
      world.add(l, l.target)
      this.spotPool.push(l)
    }
    for (let i = 0; i < CHARA_POOL; i++) {
      const l = new THREE.PointLight(0xffffff, 0, 1, 0)
      l.visible = false
      l.layers.set(LAYER_CHARA)
      world.add(l)
      this.charaPool.push(l)
    }
  }

  set(lights: StageLight[]) {
    // Blackpoints are negative lights and three.js has no such thing; they are skipped, and
    // the HUD says so rather than pretending.
    this.points = lights.filter((l) => (l.kind === 'point' || l.kind === 'line') && l.bg && l.range > 0)
    this.spots = lights.filter((l) => l.kind === 'spot' && l.bg && l.range > 0)
    this.chara = lights.filter((l) => (l.kind === 'point' || l.kind === 'line') && l.chara && l.range > 0)

    for (const l of lights) {
      if (l.kind === 'ambient') this.ambient.color.setRGB(l.color[0], l.color[1], l.color[2])
      if (l.kind === 'parallel') {
        this.sun.color.setRGB(l.color[0], l.color[1], l.color[2])
        this.sun.intensity = 1
        // dir is the TRAVEL direction; three.js shines from position toward target.
        this.sun.position.set(-l.dir[0], -l.dir[1], -l.dir[2]).multiplyScalar(100_000)
      }
    }
  }

  /** The cutscene's own per-frame light packets override the stage's globals. */
  override(ambient: THREE.Color | null, parallel: { colour: THREE.Color; dir: THREE.Vector3 } | null) {
    if (ambient) this.ambient.color.copy(ambient)
    if (parallel) {
      this.sun.color.copy(parallel.colour)
      this.sun.position.copy(parallel.dir).multiplyScalar(-100_000)
    }
  }

  /** camPos frames the level; heroPos is where the cast is, and picks their two lights. */
  update(camPos: THREE.Vector3, heroPos: THREE.Vector3 | null) {
    this.pointsAt(this.points, camPos, this.bgPool, 1.5)
    this.spotsAt(camPos)
    // 2.25x: the engine applies the 1.5 boost twice on the character path.
    this.pointsAt(this.chara, heroPos ?? camPos, this.charaPool, 2.25, true)
  }

  private pointsAt(src: StageLight[], at: THREE.Vector3, pool: THREE.PointLight[], boost: number, byForce = false) {
    const p = new THREE.Vector3()
    const near = src
      .map((l) => {
        const pos = l.kind === 'line' ? nearestOnLine(l, at, new THREE.Vector3()) : p.clone().set(l.pos[0], l.pos[1], l.pos[2])
        const d = pos.distanceTo(at)
        // The engine ranks by strength, not distance: falloff x force.
        const strength = falloff(l.range, d) * (byForce ? l.force || 1 : 1)
        return { l, pos, d, strength }
      })
      .filter((x) => x.strength > 0)
      .sort((x, y) => y.strength - x.strength)
      .slice(0, pool.length)

    for (let i = 0; i < pool.length; i++) {
      const lp = pool[i]
      const x = near[i]
      if (!x) {
        lp.visible = false
        continue
      }
      lp.visible = true
      lp.position.copy(x.pos)
      lp.color.setRGB(x.l.color[0], x.l.color[1], x.l.color[2])
      lp.distance = x.l.range
      lp.intensity = boost * (byForce ? x.l.force || 1 : 1)
    }
  }

  private spotsAt(at: THREE.Vector3) {
    const near = this.spots
      .map((l) => ({ l, d: v.set(l.pos[0], l.pos[1], l.pos[2]).distanceTo(at) }))
      .filter((x) => x.d < x.l.range * 3)
      .sort((x, y) => x.d - y.d)
      .slice(0, this.spotPool.length)

    for (let i = 0; i < this.spotPool.length; i++) {
      const sp = this.spotPool[i]
      const x = near[i]
      if (!x) {
        sp.visible = false
        continue
      }
      const l = x.l
      sp.visible = true
      sp.position.set(l.pos[0], l.pos[1], l.pos[2])
      sp.target.position.set(l.pos[0] + l.dir[0], l.pos[1] + l.dir[1], l.pos[2] + l.dir[2])
      sp.target.updateMatrixWorld()
      sp.color.setRGB(l.color[0], l.color[1], l.color[2])
      sp.distance = l.range
      sp.angle = THREE.MathUtils.degToRad(Math.max(1, l.cone))
      // umbra .. penumbra: the ramp between the inner and outer cone
      sp.penumbra = l.cone > 0 ? Math.max(0, 1 - l.inner / l.cone) : 0.4
      sp.intensity = 1.5
    }
  }
}
