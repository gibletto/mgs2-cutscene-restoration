package scene

import (
	"encoding/binary"
	"fmt"
	"math"

	"mgs2-restoration/cutscene-studio/internal/audio"
	"mgs2-restoration/cutscene-studio/internal/caption"
	"mgs2-restoration/cutscene-studio/internal/catalog"
	"mgs2-restoration/cutscene-studio/internal/diet"
	"mgs2-restoration/cutscene-studio/internal/sdt"
)

var le = binary.LittleEndian

// Opts tunes how much of a demo we hold in memory.
type Opts struct {
	// Joints retains the full per-frame skeletal pose of every object. It is only
	// needed to EDIT skeletal motion; the viewport just wants each object's centre.
	// Off by default: p082_04_p03 is 65 MB and would want gigabytes of joint arrays.
	Joints bool
}

// Import turns a shipped .sdt into an editable scene. Every packet keeps a link back
// to where it came from, so anything the user does not touch bakes back byte-for-byte.
//
// cat may be nil: effects still import, they just have no named parameters.
func Import(path string, raw []byte, cat *catalog.Catalog) (*Scene, error) {
	return ImportWith(path, raw, cat, Opts{})
}

// ImportWith is Import with control over what gets retained.
func ImportWith(path string, raw []byte, cat *catalog.Catalog, opt Opts) (*Scene, error) {
	if len(raw) >= 4 && string(raw[:4]) == "LCGB" {
		return nil, fmt.Errorf("%s: GCL bytecode, not a stream container", path)
	}
	st, err := sdt.Decode(raw)
	if err != nil {
		return nil, err
	}

	s := &Scene{
		SourcePath: path,
		FPS:        60,
		Camera:     &CameraTrack{},
		Base:       st,
	}

	// Pass 1: locate the demo frames and the caption chunk.
	for i := range st.Chunks {
		c := &st.Chunks[i]
		switch c.Type() {
		case sdt.CDemo:
			fr, err := sdt.DecodeFrame(c.Payload)
			if err != nil {
				return nil, fmt.Errorf("chunk %d: %w", i, err)
			}
			s.BaseFrames = append(s.BaseFrames, fr.Packets)
			s.ChunkOfFrame = append(s.ChunkOfFrame, i)
		case sdt.CCaption:
			tbl, err := caption.Decode(c.Payload)
			if err != nil {
				return nil, fmt.Errorf("caption chunk %d: %w", i, err)
			}
			for _, r := range tbl.Records {
				// Keep TICKS. Subtitle times are 300 Hz and are NOT frame-aligned --
				// rounding them through demo frames shifts every line by up to 13 ms.
				s.Captions = append(s.Captions, Caption{
					Start: int(r.Start),
					End:   int(r.End),
					Lang:  r.Lang,
					Name:  r.Name,
					Text:  r.Text,
				})
			}
		}
	}
	s.Frames = len(s.BaseFrames)
	if s.Frames == 0 {
		return nil, fmt.Errorf("%s: no DEMO chunks -- not a cutscene", path)
	}

	s.HasAudio = audio.HasAudio(st)
	if s.HasAudio {
		if af, err := audio.DurationFrames(st); err == nil {
			s.AudioFrames = af
		}
	}

	// Camera channels arrive fully baked: one sample per frame, exactly as authored.
	n := s.Frames
	cam := s.Camera
	for _, ch := range []*Channel{&cam.PosX, &cam.PosY, &cam.PosZ, &cam.TrgX, &cam.TrgY, &cam.TrgZ, &cam.Screen, &cam.Roll} {
		ch.Baked = make([]float64, n)
	}

	// Retaining every joint of every frame is what OOM'd this once: p082_04_p03 is 65 MB.
	// Decide once, from the real shape of the demo, rather than guessing from the file size.
	holdJoints := opt.Joints
	objByID := map[int32]*ObjectTrack{}
	instByKey := map[effectKey]*EffectInstance{}
	s.EffectOwner = make([]map[int]int, n)

	dec := diet.NewDecoder()

	for f := 0; f < n; f++ {
		pkts := s.BaseFrames[f]
		s.EffectOwner[f] = map[int]int{}

		// Objects FIRST. Frame 0's order is [CAMERA_CREATE] FRAME MOTION_PACK OBJECT×N ...
		// -- the motion pack arrives BEFORE the objects it animates, so a single in-order
		// pass drops frame 0's motion entirely, and any object that only appears there
		// (three of them in the crash demo) ends up with no motion at all.
		for pi := range pkts {
			p := &pkts[pi]
			if p.Kind() != sdt.PObject {
				continue
			}
			o, ok := p.Object()
			if !ok {
				return nil, fmt.Errorf("frame %d: malformed object packet", f)
			}
			if objByID[p.ID] != nil {
				continue // re-issuing an id is illegal; ignore the duplicate
			}
			ot := &ObjectTrack{
				ID:         p.ID,
				Name:       o.Name,
				CacheID:    o.CacheID,
				KMSCacheID: o.KMSCacheID,
				Flag:       o.Flag,
				Label:      fmt.Sprintf("obj%d", p.ID),
			}
			for _, ch := range []*Channel{&ot.PosX, &ot.PosY, &ot.PosZ, &ot.RotX, &ot.RotY, &ot.RotZ} {
				ch.Baked = make([]float64, n)
			}
			objByID[p.ID] = ot
			s.Objects = append(s.Objects, ot)
		}

		for pi := range pkts {
			p := &pkts[pi]
			switch p.Kind() {
			case sdt.PFrame:
				c, ok := p.Camera()
				if !ok {
					return nil, fmt.Errorf("frame %d: malformed camera packet", f)
				}
				cam.PosX.Baked[f] = float64(c.Pos[0])
				cam.PosY.Baked[f] = float64(c.Pos[1])
				cam.PosZ.Baked[f] = float64(c.Pos[2])
				cam.TrgX.Baked[f] = float64(c.Trg[0])
				cam.TrgY.Baked[f] = float64(c.Trg[1])
				cam.TrgZ.Baked[f] = float64(c.Trg[2])
				cam.Screen.Baked[f] = float64(c.Screen)
				cam.Roll.Baked[f] = float64(c.ZRotate)

			case sdt.PCommand:
				cmd, ok := p.Command()
				if !ok || cmd.Command != sdt.CmdObjectVisible {
					continue
				}
				// The target is the BODY id, not the packet header id.
				if ot := objByID[cmd.ID]; ot != nil {
					ot.Visibility = append(ot.Visibility, Visibility{Frame: f, Visible: cmd.Param1 == 0})
				}

			case sdt.PMotionPack:
				// Stateful across frames: an omitted component holds its previous value.
				objs, err := dec.DecodePack(p.Body)
				if err != nil {
					return nil, fmt.Errorf("frame %d: motion: %w", f, err)
				}
				for _, om := range objs {
					ot := objByID[om.ObjectID]
					if ot == nil {
						continue
					}
					ot.MotionOK = true
					ot.NJoints = len(om.Joints)
					ot.PosX.Baked[f] = float64(om.Pos[0])
					ot.PosY.Baked[f] = float64(om.Pos[1])
					ot.PosZ.Baked[f] = float64(om.Pos[2])
					ot.RotX.Baked[f] = float64(om.Rot[0])
					ot.RotY.Baked[f] = float64(om.Rot[1])
					ot.RotZ.Baked[f] = float64(om.Rot[2])
					if holdJoints {
						if est := int64(n) * int64(len(om.Joints)) * int64(len(objByID)) * 28; est > 200<<20 {
							holdJoints = false // too big to hold; the cast just cannot be edited
						}
					}
					if holdJoints {
						if ot.Joints == nil {
							ot.Joints = make([][]Joint, n)
						}
						js := make([]Joint, len(om.Joints))
						for i, j := range om.Joints {
							js[i] = Joint{Quat: j.Quat, Trans: j.Trans}
						}
						ot.Joints[f] = js
					}
				}

			case sdt.PEffect:
				e, ok := p.Effect()
				if !ok {
					continue
				}
				k := keyOf(e, f)
				inst, seen := instByKey[k]
				if !seen {
					inst = newInstance(e, p.ID, f, cat)
					inst.index = len(s.Effects)
					instByKey[k] = inst
					s.Effects = append(s.Effects, inst)
				}
				if f+1 > inst.End {
					inst.End = f + 1
				}
				inst.Raw[f] = p.Body
				s.EffectOwner[f][pi] = inst.index
				sampleParams(inst, e, f, n, cat)
			}
		}
	}

	s.JointsHeld = holdJoints

	// exec-1 effects declare their own span via `life`; trust that over the packet count.
	for _, e := range s.Effects {
		if e.ExecType == 1 && cat != nil {
			if def := cat.ByEffectID(e.EffectID); def != nil {
				if body, ok := e.Raw[e.Start]; ok {
					if life, ok := def.Life(body); ok && life > 0 {
						e.End = e.Start + int(life)
					}
				}
			}
		}
	}

	return s, nil
}

// effectKey identifies one logical effect instance across the frames it recurs on.
//
// An exec-0 effect is a per-frame CHANNEL: the same logical effect is re-emitted every
// frame with new values, and that IS the animation. So it must collapse to
// one clip on the timeline. The packet _id is a running counter and cannot be part of
// the key, or every frame would spawn a new instance. What actually distinguishes two
// concurrent channels of the same effect is their first data word -- `name`, the control
// point they drive -- which is why a frame can carry four DM_ControlChanges.
//
// An exec-1 effect is a one-shot birth with a baked lifetime, so every packet is its own
// instance and the frame joins the key.
type effectKey struct {
	effectID int32
	data0    uint32
	birth    int // 0 for channels; the frame index for one-shots
}

func keyOf(e sdt.Effect, f int) effectKey {
	var d0 uint32
	if len(e.Data) >= 4 {
		d0 = le.Uint32(e.Data)
	}
	k := effectKey{effectID: e.EffectID, data0: d0}
	if e.ExecType == 1 {
		k.birth = f + 1
	}
	return k
}

func newInstance(e sdt.Effect, pid int32, f int, cat *catalog.Catalog) *EffectInstance {
	// data0 joins the uid: concurrent channels of one effect (the 4 wake control points)
	// share pid and birth frame, and a colliding uid makes the scene POST merge them.
	var d0 uint32
	if len(e.Data) >= 4 {
		d0 = le.Uint32(e.Data)
	}
	inst := &EffectInstance{
		UID:      fmt.Sprintf("fx%d_%d_%d_%d", e.EffectID, pid, f, d0),
		EffectID: e.EffectID,
		ExecType: int(e.ExecType),
		PacketID: pid,
		Start:    f,
		End:      f + 1,
		Params:   map[string]*Channel{},
		Raw:      map[int][]byte{},
	}
	inst.Label = fmt.Sprintf("0x%05x", e.ComposerID())
	if cat != nil {
		if def := cat.ByEffectID(e.EffectID); def != nil {
			inst.Label = def.DisplayEN
			if inst.Label == "" {
				inst.Label = def.CName
			}
		}
	}
	return inst
}

// sampleParams records this frame's parameter values so the timeline can show the curve
// Konami baked -- an exec-0 channel effect is literally a per-frame sample stream.
func sampleParams(inst *EffectInstance, e sdt.Effect, f, frames int, cat *catalog.Catalog) {
	if cat == nil {
		return
	}
	def := cat.ByEffectID(e.EffectID)
	if def == nil {
		return
	}
	body := inst.Raw[f]

	for i := range def.Params {
		p := &def.Params[i]
		for c := 0; c < components(p); c++ {
			name := paramChannel(p, c)
			ch := inst.Params[name]
			if ch == nil {
				ch = &Channel{Baked: make([]float64, frames)}
				inst.Params[name] = ch
			}
			v, err := def.GetFloat(body, p, c)
			if err != nil || f >= len(ch.Baked) {
				continue
			}
			ch.Baked[f] = v
		}
	}
}

// A vector param becomes one channel per component. The suffix depends on what the
// param MEANS, not on its C type: an IVECTOR that the composer tagged as a colour is
// r/g/b/a, everything else is x/y/z. The UI names them the same way, and if the two
// disagree the edit silently lands in a channel nobody reads.
func paramSuffixes(p *catalog.Param) []string {
	if p.Widget == "MaxInputSelectColor" || p.UI == "rgba-color" {
		return []string{"r", "g", "b", "a"}
	}
	switch p.CType {
	case "FVECTOR", "IVECTOR", "SVECTOR":
		return []string{"x", "y", "z"}
	}
	return nil
}

func components(p *catalog.Param) int {
	if s := paramSuffixes(p); s != nil {
		return len(s)
	}
	return 1
}

func paramChannel(p *catalog.Param, comp int) string {
	s := paramSuffixes(p)
	if s == nil {
		return p.Name
	}
	return p.Name + "." + s[comp]
}

// ViewDir is the normalised camera direction at a frame. The engine derives up from
// world (0,-1,0), so a view vector parallel to Y is degenerate.
func (s *Scene) ViewDir(frame int) [3]float64 {
	c := s.Camera
	d := [3]float64{
		c.TrgX.Sample(frame) - c.PosX.Sample(frame),
		c.TrgY.Sample(frame) - c.PosY.Sample(frame),
		c.TrgZ.Sample(frame) - c.PosZ.Sample(frame),
	}
	l := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
	if l == 0 {
		return [3]float64{0, 0, 1}
	}
	return [3]float64{d[0] / l, d[1] / l, d[2] / l}
}

// HFOV is the horizontal field of view in radians at a frame.
// ax defaults to 1.0: the retail cutscene regime, cinema bars up.
func (s *Scene) HFOV(frame int, ax float64) float64 {
	screen := s.Camera.Screen.Sample(frame)
	if screen <= 0 || ax <= 0 {
		return 0
	}
	return 2 * math.Atan(1/(ax*screen))
}
