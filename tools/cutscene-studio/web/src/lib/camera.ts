import * as THREE from 'three'

/**
 * `screen` is a zoom scalar, not an FOV:
 *   hFOV = 2*atan(1/(ax*screen))   vFOV = 2*atan(1/(ay_eff*screen))
 * Retail cutscenes run cinema-bars/fullscreen with mCameraRatio = 1.0 -> ax = 1.0,
 * ay_eff = 1.121*(1+1/3)*512/448 = 1.70819. That is a VERT- crop, not Hor+.
 */
export const AX_CUTSCENE = 1.0
export const AY_EFF_CUTSCENE = 1.70819
export const AX_GAMEPLAY = 0.75
export const AY_EFF_GAMEPLAY = 1.28114

export function axFromRatio(ratio: number) { return 0.75 + 0.25 * ratio }
export function ayEffFromRatio(ratio: number) { return 1.121 * (1 + ratio / 3) * (512 / 448) }

export const hFov = (screen: number, ax = AX_CUTSCENE) => 2 * Math.atan(1 / (ax * screen))
export const vFov = (screen: number, ay = AY_EFF_CUTSCENE) => 2 * Math.atan(1 / (ay * screen))
export const screenFromHFov = (fovRad: number, ax = AX_CUTSCENE) => 1 / (ax * Math.tan(fovRad / 2))

/** Roll quantises to 2*pi/4096 = 0.0879 deg; sub-step detail is destroyed by the engine. */
export const ROLL_STEP = (2 * Math.PI) / 4096
export const quantiseRoll = (r: number) => Math.trunc(r / ROLL_STEP) * ROLL_STEP

const LOWER = new THREE.Vector3(0, -1, 0)

export interface CamSample { pos: THREE.Vector3; trg: THREE.Vector3; screen: number; roll: number }

/**
 * DG_MakeCameraMatrix (frame.c:831). z = trg-pos, x = (0,-1,0) x z, y = z x x.
 * Engine camera +Z is forward and +Y is screen-DOWN, so three's basis is (x, -y, -z).
 * prevX carries the degenerate case the way the engine does: a file-static from last frame.
 */
export function applyEngineBasis(cam: THREE.Object3D, s: CamSample, prevX?: THREE.Vector3): THREE.Vector3 {
  const z = new THREE.Vector3().subVectors(s.trg, s.pos)
  const x = new THREE.Vector3().crossVectors(LOWER, z)
  if (x.lengthSq() < 1e-12) x.copy(prevX ?? new THREE.Vector3(1, 0, 0))
  x.normalize()
  if (z.lengthSq() < 1e-12) z.set(0, 0, 1)
  z.normalize()
  const y = new THREE.Vector3().crossVectors(z, x)

  const m = new THREE.Matrix4().makeBasis(x, y.clone().negate(), z.clone().negate())
  cam.position.copy(s.pos)
  cam.quaternion.setFromRotationMatrix(m)
  cam.rotateZ(s.roll)          // +roll tips the horizon clockwise on screen
  cam.updateMatrixWorld(true)
  return x
}

/**
 * The engine's exact projection: ndc.x = ax*screen*X/Z, ndc.y = ay*screen*Y/Z, NDC in [-1,+1].
 * three's own PerspectiveCamera cannot express it (it derives x from fov*aspect), so set the
 * matrix directly. The 1.70819/1.0 = 1.708 content aspect is then stretched onto the 16:9 frame.
 */
export function engineProjection(screen: number, near: number, far: number, ax = AX_CUTSCENE, ay = AY_EFF_CUTSCENE) {
  const a = ax * screen, b = ay * screen
  return new THREE.Matrix4().set(
    a, 0, 0, 0,
    0, b, 0, 0,
    0, 0, -(far + near) / (far - near), (-2 * far * near) / (far - near),
    0, 0, -1, 0,
  )
}

/** Frustum corner rays in three camera space (engine +Y is down, so it is symmetric anyway). */
export function frustumCorners(screen: number, depth: number, ax = AX_CUTSCENE, ay = AY_EFF_CUTSCENE) {
  const hx = depth / (ax * screen), hy = depth / (ay * screen)
  return [
    new THREE.Vector3(-hx, hy, -depth), new THREE.Vector3(hx, hy, -depth),
    new THREE.Vector3(hx, -hy, -depth), new THREE.Vector3(-hx, -hy, -depth),
  ]
}

/** Object rotation channels are 12-bit fixed point: 4096 == 360 degrees. */
export const rotUnitsToRad = (u: number) => (u * Math.PI * 2) / 4096
