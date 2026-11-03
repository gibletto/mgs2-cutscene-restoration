// Package scene is the editable model of a cutscene: the thing the timeline UI
// binds to, and the thing the baker turns back into a .sdt.
//
// Two rules shape everything here.
//
//  1. The engine has NO runtime interpolation. Camera, motion and effect
//     parameters are all hard-set every 60 Hz frame.
//     So a Channel is authored as keys+curve and BAKED to one value per frame.
//     The stream IS the keyframe bake.
//
//  2. Anything the user has not touched must bake back to Konami's exact bytes.
//     So every track keeps its imported raw packets, and the baker only
//     re-encodes what actually changed. Untouched demo => byte-identical file.
package scene

import "mgs2-restoration/cutscene-studio/internal/sdt"

// Interp is how a channel gets from one key to the next.
type Interp string

const (
	Hold   Interp = "hold" // step -- the engine's native "cut"
	Linear Interp = "linear"
	Bezier Interp = "bezier" // with per-key tangents
	Smooth Interp = "smooth" // auto (Catmull-Rom-ish), retangented on edit
)

// Key is one keyframe on a scalar channel. Frame is a demo frame index (60 Hz).
type Key struct {
	Frame  int     `json:"frame"`
	Value  float64 `json:"value"`
	Interp Interp  `json:"interp"`
	// Bezier handles, in (frames, value) offsets from the key. Ignored unless Interp==Bezier.
	InTanX  float64 `json:"inTanX,omitempty"`
	InTanY  float64 `json:"inTanY,omitempty"`
	OutTanX float64 `json:"outTanX,omitempty"`
	OutTanY float64 `json:"outTanY,omitempty"`
}

// Channel is one animatable scalar.
//
// A channel imported from a shipped demo arrives as Baked: one sample per frame,
// exactly as Konami authored it. It stays that way (and bakes back bit-for-bit)
// until the user touches it, at which point the UI converts the region it edits
// into Keys. Both forms can coexist: Keys override Baked where they exist.
type Channel struct {
	Baked []float64 `json:"baked,omitempty"` // len == scene.Frames, or nil
	Keys  []Key     `json:"keys,omitempty"`
	Dirty bool      `json:"dirty"` // true once edited: the baker must re-encode this channel's packet
}

// CameraTrack is the DEMO_FRAME packet stream.
//
// Screen is the projection-plane distance, NOT a field of view: fov = 2*atan(1/(ax*screen)).
// Roll is radians. There is no up-vector -- up is derived from world (0,-1,0), which is
// why an exactly-vertical view vector is illegal.
type CameraTrack struct {
	PosX   Channel `json:"posX"`
	PosY   Channel `json:"posY"`
	PosZ   Channel `json:"posZ"`
	TrgX   Channel `json:"trgX"`
	TrgY   Channel `json:"trgY"`
	TrgZ   Channel `json:"trgZ"`
	Screen Channel `json:"screen"`
	Roll   Channel `json:"roll"`
}

// Visibility is a step channel: the frame at which an object shows or hides.
// It bakes to COMMAND(OBJECT_VISIBLE) packets, emitted only on change frames.
// NOTE: the command targets the BODY id, not the packet header id.
type Visibility struct {
	Frame   int  `json:"frame"`
	Visible bool `json:"visible"`
}

// ObjectTrack is one cast member: a DEMO_OBJECT plus its motion and visibility.
type ObjectTrack struct {
	ID         int32        `json:"id"`                // the demo object handle. LOAD-BEARING: stable, unique, never reused.
	Name       int32        `json:"name"`              // 24-bit GV_StrCode of the scene-node name
	NameStr    string       `json:"nameStr,omitempty"` // resolved, if we can find it
	CacheID    int32        `json:"cacheId"`           // EVM, or 0 for a KMS-only rigid object (79% of retail)
	KMSCacheID int32        `json:"kmsCacheId"`        // never 0
	Flag       int32        `json:"flag"`              // 0 = rigid/jointless, 1 = jointed, 2 = preshaded, ...
	Label      string       `json:"label"`             // editor-side friendly name
	Visibility []Visibility `json:"visibility"`

	// Rigid transform channels (the object centre). Rot is in the engine's
	// 12-bit fixed point: 4096 == 360 degrees.
	PosX Channel `json:"posX"`
	PosY Channel `json:"posY"`
	PosZ Channel `json:"posZ"`
	RotX Channel `json:"rotX"`
	RotY Channel `json:"rotY"`
	RotZ Channel `json:"rotZ"`

	// Joints holds the decoded skeletal animation, [frame][joint].
	// Nil for objects the user never edits -- their motion passes through as raw bytes.
	Joints   [][]Joint `json:"-"`
	NJoints  int       `json:"nJoints"`
	MotionOK bool      `json:"motionOk"` // false if this object has no MOTION_PACK entry
	Added    bool      `json:"added"`    // authored in the editor: it has motion on every frame

	// PreviewScale is a viewport-only uniform scale for placed props. The engine's demo-object
	// motion has no scale channel (putobj -s is init-time, in the .gcx), so this is never baked
	// into the .sdt -- it only lets the editor preview a putobj at its real -s size. 0 == 1.
	PreviewScale [3]float32 `json:"previewScale,omitempty"`
}

// Joint is one joint's pose for one frame: a parent-relative quaternion plus a
// translation added to the rest offset.
type Joint struct {
	Quat  [4]float32 `json:"quat"`
	Trans [3]float32 `json:"trans"`
}

// EffectInstance is one authored effect on the timeline.
//
// exec_type 0 effects are per-frame channels: the packet is re-emitted every frame
// of [Start,End) with the sampled parameter values. exec_type 1 effects are spawned
// ONCE at Start with a baked `life` -- re-emitting them per frame would birth a new
// actor every frame and blow DM_MAX_EFFECT.
type EffectInstance struct {
	UID      string `json:"uid"`      // editor-side identity, stable across edits
	EffectID int32  `json:"effectId"` // 0x01000000 | composer_id
	ExecType int    `json:"execType"` // fixed property of the effect id -- never invent one
	PacketID int32  `json:"packetId"` // the packet header _id
	Start    int    `json:"start"`    // demo frame
	End      int    `json:"end"`      // exclusive. For exec 1 this is Start+life.
	Label    string `json:"label"`

	// Params maps a catalog param name (d0, d1, ...) to a channel. Scalars only;
	// vector params expand to one channel per component (e.g. "d4.x").
	Params map[string]*Channel `json:"params"`

	// Raw is the imported packet body for each frame this instance appears on. An
	// exec-0 channel effect has one per frame, with varying params -- that IS how the
	// engine animates. Preserved so an untouched effect bakes back byte-for-byte,
	// including DEMO_EFFECT.pad[2], which is uninitialised garbage in 6,577,294 of
	// 6,587,772 shipped packets and cannot be zeroed.
	Raw map[int][]byte `json:"-"`

	Dirty    bool `json:"dirty"`
	Deleted  bool `json:"deleted"`
	Added    bool `json:"added"`    // authored in the editor, has no base packets
	Continue bool `json:"continue"` // imported per-frame effect a length extension carries past its base frames

	index int // position in Scene.Effects, so EffectOwner can point at it in O(1)
}

// Caption is one subtitle line.
//
// Times are TICKS (1/300 s), not demo frames. Konami's are not frame-aligned -- the first
// line of the crash demo starts at tick 208, which is frame 41.6 -- so storing frames and
// multiplying back shifts every subtitle. One demo frame = 5 ticks.
type Caption struct {
	Start int    `json:"start"` // ticks (1/300 s)
	End   int    `json:"end"`   // ticks, exclusive
	Lang  int16  `json:"lang"`  // 0 default, 1 EN, ... 7 JA
	Name  int32  `json:"name"`  // StrCode speaker id, 0 = nobody
	Text  string `json:"text"`  // literal UTF-8. '|' is a line break.
}

// Marker is an editor-only annotation (a shot boundary, a note). Never baked.
type Marker struct {
	Frame int    `json:"frame"`
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
}

// Scene is the whole cutscene.
type Scene struct {
	SourcePath string `json:"sourcePath"`
	Frames     int    `json:"frames"` // demo frames; duration = Frames/60 s
	FPS        int    `json:"fps"`    // always 60; here so the UI never hardcodes it

	Camera   *CameraTrack      `json:"camera"`
	Objects  []*ObjectTrack    `json:"objects"`
	Effects  []*EffectInstance `json:"effects"`
	Captions []Caption         `json:"captions"`
	Markers  []Marker          `json:"markers"`

	// HasAudio is false for the 24 shipped demos that run on the wall clock.
	// Changing the frame count of a demo WITH audio needs the audio extended too,
	// and desyncs the out-of-band HD widescreen keyframe table.
	HasAudio    bool `json:"hasAudio"`
	AudioFrames int  `json:"audioFrames"` // audio length expressed in demo frames

	// Base is the imported stream, kept whole. The baker walks it for every packet
	// no track has claimed, so unedited content survives byte-for-byte.
	Base *sdt.Stream `json:"-"`

	// BaseFrames[f] is the decoded packet list of demo frame f.
	BaseFrames [][]sdt.Packet `json:"-"`
	// ChunkOfFrame[f] is the index into Base.Chunks of demo frame f.
	ChunkOfFrame []int `json:"-"`
	// EffectOwner[f][packetIndex] = index into Effects. Lets the baker substitute a
	// dirty effect in place and leave its neighbours' bytes untouched.
	EffectOwner []map[int]int `json:"-"`

	// JointsHeld is false when a demo was too big to retain its skeletal animation. Editing
	// the cast needs it (a new object means re-encoding the whole MOTION_PACK), so the API
	// refuses rather than silently baking a demo with no motion.
	JointsHeld bool `json:"jointsHeld"`

	// CastDirty forces frame 0's OBJECT + visibility block to be rebuilt.
	CastDirty     bool `json:"castDirty"`
	MotionDirty   bool `json:"motionDirty"`
	CaptionsDirty bool `json:"captionsDirty"`
}

// cameraKeyedAt reports whether any camera channel has keys spanning frame f.
func (s *Scene) cameraKeyedAt(f int) bool {
	c := s.Camera
	for _, ch := range []*Channel{&c.PosX, &c.PosY, &c.PosZ, &c.TrgX, &c.TrgY, &c.TrgZ, &c.Screen, &c.Roll} {
		if n := len(ch.Keys); n > 0 && f >= ch.Keys[0].Frame && f <= ch.Keys[n-1].Frame {
			return true
		}
	}
	return false
}

// CameraDirty reports whether any camera channel was edited.
func (s *Scene) CameraDirty() bool {
	c := s.Camera
	for _, ch := range []*Channel{&c.PosX, &c.PosY, &c.PosZ, &c.TrgX, &c.TrgY, &c.TrgZ, &c.Screen, &c.Roll} {
		if ch.Dirty {
			return true
		}
	}
	return false
}

// Sample evaluates a channel at a frame: keys win where they exist, otherwise the
// imported baked sample, otherwise zero.
func (c *Channel) Sample(frame int) float64 {
	if len(c.Keys) > 0 {
		if v, ok := c.sampleKeys(frame); ok {
			return v
		}
	}
	if frame >= 0 && frame < len(c.Baked) {
		return c.Baked[frame]
	}
	return 0
}

func (c *Channel) sampleKeys(frame int) (float64, bool) {
	k := c.Keys
	if len(k) == 0 {
		return 0, false
	}
	// A single key is a VALUE, not a keyframe: hold it across the whole channel. Typing
	// 70 into a field means "this parameter is 70", and the UI records that as one key at
	// the playhead -- if we fell back to the imported data everywhere else, the edit would
	// silently do nothing at the frame the packet actually lives on.
	if len(k) == 1 {
		return k[0].Value, true
	}
	// With a real keyed RANGE, fall back to the baked data outside it. That is what lets
	// you key one shot of an imported demo and leave the other 2,000 frames untouched.
	if frame < k[0].Frame || frame > k[len(k)-1].Frame {
		if len(c.Baked) > 0 {
			return 0, false
		}
		if frame < k[0].Frame {
			return k[0].Value, true
		}
		return k[len(k)-1].Value, true
	}
	// Find the span.
	i := 0
	for i+1 < len(k) && k[i+1].Frame <= frame {
		i++
	}
	if i == len(k)-1 || k[i].Frame == frame {
		return k[i].Value, true
	}
	a, b := k[i], k[i+1]
	span := float64(b.Frame - a.Frame)
	if span <= 0 {
		return a.Value, true
	}
	t := float64(frame-a.Frame) / span

	switch a.Interp {
	case Hold:
		return a.Value, true
	case Bezier:
		return bezier(a, b, t, span), true
	case Smooth:
		// Smoothstep: cheap, monotone, no overshoot. Real Catmull-Rom needs neighbours
		// and can overshoot, which on a camera reads as a lurch.
		t = t * t * (3 - 2*t)
		return a.Value + (b.Value-a.Value)*t, true
	default: // Linear
		return a.Value + (b.Value-a.Value)*t, true
	}
}

// bezier evaluates a cubic with the keys' tangent handles, solving x(t)=frame by
// bisection (handles are in frame/value space, like AE's).
func bezier(a, b Key, t, span float64) float64 {
	x0, y0 := 0.0, a.Value
	x1, y1 := a.OutTanX/span, a.Value+a.OutTanY
	x2, y2 := 1+b.InTanX/span, b.Value+b.InTanY
	x3, y3 := 1.0, b.Value

	// Solve for the parameter u where x(u) == t.
	u := t
	for i := 0; i < 12; i++ {
		mu := 1 - u
		x := mu*mu*mu*x0 + 3*mu*mu*u*x1 + 3*mu*u*u*x2 + u*u*u*x3
		if x < t {
			u += (1 - u) / 2
		} else {
			u -= u / 2
		}
		_ = x
	}
	mu := 1 - u
	return mu*mu*mu*y0 + 3*mu*mu*u*y1 + 3*mu*u*u*y2 + u*u*u*y3
}

// Dirty reports whether anything in the scene needs re-encoding. A clean scene
// bakes straight from Base and is guaranteed byte-identical.
func (s *Scene) Dirty() bool {
	if s.CastDirty || s.MotionDirty || s.CaptionsDirty || s.CameraDirty() {
		return true
	}
	if s.Base != nil && s.Frames != len(s.BaseFrames) {
		return true
	}
	for _, e := range s.Effects {
		if e.Dirty || e.Deleted || e.Added {
			return true
		}
	}
	return false
}
