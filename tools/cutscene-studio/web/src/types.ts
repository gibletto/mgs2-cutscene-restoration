// Mirror of internal/scene/scene.go and the catalog in data/effects.json.
// Keep the field names identical to the Go json tags -- this is the wire format.

export type Interp = 'hold' | 'linear' | 'bezier' | 'smooth'

export interface Key {
  frame: number
  value: number
  interp: Interp
  inTanX?: number
  inTanY?: number
  outTanX?: number
  outTanY?: number
}

/** Imported demos arrive as `baked` (one sample per frame). Keys override baked where they exist. */
export interface Channel {
  baked?: number[]
  keys?: Key[]
  dirty: boolean
}

/** screen is a projection-plane distance, not an FOV. roll is radians. */
export interface CameraTrack {
  posX: Channel; posY: Channel; posZ: Channel
  trgX: Channel; trgY: Channel; trgZ: Channel
  screen: Channel
  roll: Channel
}

export interface Visibility {
  frame: number
  visible: boolean
}

export interface ObjectTrack {
  id: number
  name: number
  nameStr?: string
  cacheId: number
  kmsCacheId: number
  flag: number
  label: string
  visibility: Visibility[]
  posX: Channel; posY: Channel; posZ: Channel
  rotX: Channel; rotY: Channel; rotZ: Channel   // 4096 units == 360 deg
  nJoints: number
  motionOk: boolean
  previewScale?: [number, number, number]   // viewport-only; putobj -s size, never baked
}

/** exec 0 = per-frame channel packet. exec 1 = spawned once at start with a baked `life`. */
export interface EffectInstance {
  deleted?: boolean
  uid: string
  effectId: number
  execType: number
  packetId: number
  start: number
  end: number
  label: string
  params: Record<string, Channel>
  dirty: boolean
}

export interface Caption {
  /** TICKS (1/300 s), not frames -- Konami's subtitle times are not frame-aligned. */
  start: number
  end: number
  lang: number
  name: number
  text: string   // '|' is a line break
}

export interface Marker {
  frame: number
  label: string
  color?: string
}

export interface Scene {
  sourcePath: string
  frames: number
  fps: number
  camera: CameraTrack
  objects: ObjectTrack[]
  effects: EffectInstance[]
  captions: Caption[]
  markers: Marker[]
  hasAudio: boolean
  audioFrames: number
}

// ---- catalog (data/effects.json) ----

export type ParamUI =
  | 'implicit-int' | 'implicit-float' | 'string-128' | 'strcode-text' | 'float-edit'
  | 'implicit-effect-name' | 'implicit-key-max' | 'implicit-key-min' | 'point-helper'
  | 'link-implicit' | 'scene-object-picker' | 'rgba-color' | 'int-edit' | 'vec3-float'
  | 'float-slider' | 'int-slider' | 'vec3-short' | 'internal-message' | 'combo'
  | 'model-joint-picker' | 'flags-listbox' | 'implicit-frame-rate'

export interface EffectParam {
  name: string           // d0, d1, ... -- the index has HOLES, never renumber
  ctype: string
  arrayLen: number | null
  widget: string
  ui: ParamUI
  implicit: boolean
  widgetArgsNamed?: Record<string, unknown>
  label_ja: string | null
  label_en: string | null
  default: number | number[] | string | null
  min: number | null
  max: number | null
  byteOffset: number
  byteSize: number
}

export interface EffectDef {
  id: number
  hexid: string
  effectId: number
  effectIdHex: string
  cname: string
  sourceFile: string
  displayName_ja: string | null
  displayName_en: string | null
  description_ja: string | null
  description_en: string | null
  execType: 0 | 1
  dataBaseOffset: 0 | 16
  dataStructSize: number
  packetBodySize: number
  options: {
    no_birth: boolean
    force_effect: boolean
    efct_on: number | null
    efct_off: number | null
    add_src: string[]
  }
  lifeField: { byteOffset: number; ctype: string } | null
  params: EffectParam[]
}

// ---- API ----

export interface DemoEntry {
  name: string
  path: string
  frames: number
  durationSec: number
  hasAudio: boolean
  sizeBytes: number
}

export interface AppConfig {
  gameDir: string
  gameDataDir: string
  toolsDir: string
  ok: boolean
  problems: string[]
}

export interface Finding {
  rule: string
  severity: 'fatal' | 'warn' | 'info'
  msg: string
  frame?: number
  fix?: string
}

export interface BakeResult {
  ok: boolean
  bytesWritten: number
  findings: Finding[]
  outPath?: string
  byteIdentical?: boolean
  fatal?: boolean
}

export interface DeployResult {
  ok: boolean
  deployedTo?: string
  backup?: string
  bytes?: number
  findings?: Finding[]
}

export interface Asset {
  name: string
  cacheId: number
  kind: string
  hasGltf?: boolean
}

// ---- editor-side selection ----

export type Selection =
  | { kind: 'none' }
  | { kind: 'camera' }
  | { kind: 'object'; id: number }
  | { kind: 'effect'; uid: string }
  | { kind: 'caption'; index: number }

/** A dotted path to one scalar channel: "camera.posX", "object.7.rotY", "effect.<uid>.d3". */
export type ChannelPath = string
