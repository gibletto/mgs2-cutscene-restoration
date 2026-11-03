package scene

import (
	"fmt"
	"math"

	"mgs2-restoration/cutscene-studio/internal/caption"
	"mgs2-restoration/cutscene-studio/internal/catalog"
	"mgs2-restoration/cutscene-studio/internal/diet"
	"mgs2-restoration/cutscene-studio/internal/sdt"
)

// Packet type words. Every kind carries NOSKIP except the camera, which is the one
// thing frameskip is allowed to drop.
const (
	ptTermSeq    = int32(-0x80000000) | 0
	ptTermFrame  = int32(-0x80000000) | 1
	ptCommand    = int32(-0x80000000) | 2
	ptFrame      = int32(3)
	ptObject     = int32(-0x80000000) | 4
	ptEffect     = int32(-0x80000000) | 6
	ptMotionPack = int32(-0x80000000) | 7
)

// Bake turns the scene back into a .sdt.
//
// A scene with no edits returns the original bytes untouched -- byte-identical by
// construction, not by luck. Anything else is rebuilt frame by frame, substituting
// only the packets whose track is dirty and passing every other packet through verbatim.
func (s *Scene) Bake(cat *catalog.Catalog) ([]byte, error) {
	if s.Base == nil {
		return nil, fmt.Errorf("scene has no base stream")
	}
	if !s.Dirty() {
		return s.Base.Encode()
	}
	if s.Frames < 1 {
		return nil, fmt.Errorf("a demo needs at least one frame")
	}

	payloads, err := s.bakeFrames(cat)
	if err != nil {
		return nil, err
	}

	out := &sdt.Stream{Tail: s.Base.Tail}
	baseDemo := len(s.BaseFrames)
	lastDemoChunk := -1
	if baseDemo > 0 {
		lastDemoChunk = s.ChunkOfFrame[baseDemo-1]
	}

	frameOfChunk := make(map[int]int, baseDemo)
	for f, ci := range s.ChunkOfFrame {
		frameOfChunk[ci] = f
	}

	for i := range s.Base.Chunks {
		c := s.Base.Chunks[i]

		switch c.Type() {
		case sdt.CDemo:
			f := frameOfChunk[i]
			if f >= s.Frames {
				continue // demo shortened: drop the surplus chunks
			}
			c.Payload = payloads[f]
			c.Time = int32(5 * f)
			out.Chunks = append(out.Chunks, c)

		case sdt.CCaption:
			body, err := s.bakeCaption(c.Payload)
			if err != nil {
				return nil, err
			}
			c.Payload = body
			c.Option = 0 // ofs_level is 0 in 100% of shipped data
			out.Chunks = append(out.Chunks, c)

		case sdt.CEnd:
			// Emitted after the appended frames, below.

		default:
			out.Chunks = append(out.Chunks, c)
		}

		// Demo lengthened: the new frames go straight after the last original one,
		// which keeps the audio/caption interleave exactly where Konami put it.
		if i == lastDemoChunk {
			for f := baseDemo; f < s.Frames; f++ {
				out.Chunks = append(out.Chunks, sdt.Chunk{
					RawType: int32(sdt.CDemo),
					Time:    int32(5 * f),
					Option:  0,
					Payload: payloads[f],
				})
			}
		}
	}

	out.Chunks = append(out.Chunks, sdt.Chunk{
		RawType: int32(sdt.CEnd),
		Time:    int32(5*(s.Frames-1) + 300),
		Option:  0,
	})

	return out.Encode()
}

// bakeFrames builds every frame's packet payload in one forward pass, because the
// motion codec is a sequential per-object delta stream and cannot be re-encoded
// a frame at a time.
func (s *Scene) bakeFrames(cat *catalog.Catalog) ([][]byte, error) {
	enc := diet.NewEncoder()
	out := make([][]byte, s.Frames)

	for f := 0; f < s.Frames; f++ {
		pkts, err := s.bakeFrame(f, cat, enc)
		if err != nil {
			return nil, fmt.Errorf("frame %d: %w", f, err)
		}
		// TERM_SEQ only sets sequence_end; it does NOT end the frame, so the last chunk
		// needs both, in this order. Reuse the originals where they exist:
		// their _id is a running packet counter the game never reads, but rewriting it
		// to 0 would needlessly differ from Konami's bytes in 2,180 packets.
		var base []sdt.Packet
		if f < len(s.BaseFrames) {
			base = s.BaseFrames[f]
		}
		if f == s.Frames-1 {
			pkts = append(pkts, termOrNew(base, sdt.PTermSeq, ptTermSeq))
		}
		pkts = append(pkts, termOrNew(base, sdt.PTermFrame, ptTermFrame))

		fr := &sdt.Frame{Packets: pkts}
		body, err := fr.Encode()
		if err != nil {
			return nil, fmt.Errorf("frame %d: %w", f, err)
		}
		out[f] = body
	}
	return out, nil
}

// bakeFrame emits packets in the order retail uses:
//
//	[CAMERA_CREATE] FRAME MOTION_PACK OBJECT×N COMMAND×N EFFECT×k [TERM_SEQ] TERM_FRAME
//
// Every kind is passed through from the base unless its track is dirty, so an edit to
// one effect leaves every other byte in the frame exactly as Konami wrote it.
func (s *Scene) bakeFrame(f int, cat *catalog.Catalog, enc *diet.Encoder) ([]sdt.Packet, error) {
	var out []sdt.Packet
	// Regenerate the camera only where authored keys actually reach. A blanket "camera is
	// dirty" would rebuild every original FRAME packet too, losing Konami's _id counters
	// for nothing -- keys on the extended half must not cost half 1 its byte-exactness.
	camDirty := s.cameraKeyedAt(f)
	extended := f >= len(s.BaseFrames)

	var base []sdt.Packet
	if !extended {
		base = s.BaseFrames[f]
	}

	if f == 0 {
		// NOSKIP is mandatory here: a frame-skip on frame 0 would drop it and leave the
		// entire camera track inert.
		if cc := findCameraCreate(base); cc != nil {
			out = append(out, *cc)
		} else {
			out = append(out, sdt.Packet{PType: ptCommand, Body: cmdBody(sdt.CmdCameraCreate, 0, 0, 0)})
		}
	}

	// Exactly one FRAME packet per chunk, always.
	if p := findKind(base, sdt.PFrame); p != nil && !camDirty {
		out = append(out, *p)
	} else {
		out = append(out, s.cameraPacket(f))
	}

	mp, err := s.motionPacket(f, enc, extended)
	if err != nil {
		return nil, err
	}
	if mp != nil {
		out = append(out, *mp)
	}

	if f == 0 {
		if !s.CastDirty && base != nil {
			for _, p := range base {
				if p.Kind() == sdt.PObject {
					out = append(out, p)
				}
			}
		} else {
			for _, o := range s.Objects {
				out = append(out, sdt.Packet{PType: ptObject, ID: o.ID, Body: objectBody(o)})
			}
		}
	}

	// Visibility. Objects spawn INVISIBLE, so everything you want to see needs an
	// explicit show on frame 0; later frames only carry changes.
	if !s.CastDirty && base != nil {
		for _, p := range base {
			if p.Kind() != sdt.PCommand {
				continue
			}
			if c, ok := p.Command(); ok && c.Command == sdt.CmdObjectVisible {
				out = append(out, p)
			}
		}
	} else {
		for _, o := range s.Objects {
			for _, v := range o.Visibility {
				if v.Frame != f && !(f == 0 && v.Frame <= 0) {
					continue
				}
				p1 := int32(0)
				if !v.Visible {
					p1 = 15 // any nonzero hides; retail writes 15
				}
				out = append(out, sdt.Packet{PType: ptCommand, Body: cmdBody(sdt.CmdObjectVisible, o.ID, p1, 0)})
			}
		}
	}

	// Effects: walk the base packets so untouched ones keep their exact bytes -- including
	// DEMO_EFFECT.pad[2], uninitialised garbage that cannot be zeroed.
	for pi, p := range base {
		if p.Kind() != sdt.PEffect {
			continue
		}
		ei, ok := s.EffectOwner[f][pi]
		if !ok || ei < 0 || ei >= len(s.Effects) {
			out = append(out, p)
			continue
		}
		inst := s.Effects[ei]
		if inst.Deleted {
			continue
		}
		if !inst.Dirty {
			out = append(out, p)
			continue
		}
		body, err := s.effectBody(inst, f, cat)
		if err != nil {
			return nil, err
		}
		out = append(out, sdt.Packet{PType: ptEffect, ID: inst.PacketID, Body: body})
	}

	for _, inst := range s.Effects {
		// Added effects have no base packets anywhere. Continue effects are imported per-frame
		// channels (the wake, the lights) that a length extension carries into the new half --
		// past their base frames, so they must be emitted here or the wake dies at the seam.
		emit := (inst.Added || (inst.Continue && extended)) && !inst.Deleted && inst.activeAt(f)
		if !emit {
			continue
		}
		body, err := s.effectBody(inst, f, cat)
		if err != nil {
			return nil, err
		}
		out = append(out, sdt.Packet{PType: ptEffect, ID: inst.PacketID, Body: body})
	}

	return out, nil
}

func termOrNew(base []sdt.Packet, k sdt.PacketKind, ptype int32) sdt.Packet {
	if p := findKind(base, k); p != nil {
		return *p
	}
	return sdt.Packet{PType: ptype}
}

func findKind(pkts []sdt.Packet, k sdt.PacketKind) *sdt.Packet {
	for i := range pkts {
		if pkts[i].Kind() == k {
			return &pkts[i]
		}
	}
	return nil
}

func findCameraCreate(pkts []sdt.Packet) *sdt.Packet {
	for i := range pkts {
		if pkts[i].Kind() != sdt.PCommand {
			continue
		}
		if c, ok := pkts[i].Command(); ok && c.Command == sdt.CmdCameraCreate {
			return &pkts[i]
		}
	}
	return nil
}

// activeAt: an exec-1 effect is born once and lives on its baked `life`; an exec-0
// effect is a per-frame channel and must be re-emitted every frame it covers.
func (e *EffectInstance) activeAt(f int) bool {
	if e.ExecType == 1 {
		return f == e.Start
	}
	return f >= e.Start && f < e.End
}

func (s *Scene) cameraPacket(f int) sdt.Packet {
	c := s.Camera
	pos := [3]float32{float32(c.PosX.Sample(f)), float32(c.PosY.Sample(f)), float32(c.PosZ.Sample(f))}
	trg := [3]float32{float32(c.TrgX.Sample(f)), float32(c.TrgY.Sample(f)), float32(c.TrgZ.Sample(f))}

	// A view vector parallel to world Y degenerates the basis cross product and makes
	// the engine reuse a file-static from the previous frame. Nudge it off.
	dx, dy, dz := float64(trg[0]-pos[0]), float64(trg[1]-pos[1]), float64(trg[2]-pos[2])
	if l := math.Sqrt(dx*dx + dy*dy + dz*dz); l > 0 {
		if math.Abs(dy/l) > 0.9999 {
			trg[2] += float32(l * 0.0142) // ~0.81 degrees off vertical
		}
	}

	p := sdt.Packet{PType: ptFrame, Body: make([]byte, 48)}
	p.SetCamera(sdt.CameraFrame{
		Screen:  float32(c.Screen.Sample(f)),
		ZRotate: float32(c.Roll.Sample(f)),
		Pos:     pos,
		Trg:     trg,
	})
	return p
}

func (s *Scene) motionPacket(f int, enc *diet.Encoder, extended bool) (*sdt.Packet, error) {
	// An original frame with no motion edit passes through byte-exact. An extended frame is
	// always re-encoded, so a length extension continues the ship's travel instead of holding
	// it. (The continued objects are rigid, so a fresh encoder at the seam is correct.)
	if !extended && !s.MotionDirty {
		if f < 0 || f >= len(s.BaseFrames) {
			return nil, nil
		}
		for _, p := range s.BaseFrames[f] {
			if p.Kind() == sdt.PMotionPack {
				return &p, nil
			}
		}
		return nil, nil
	}

	var objs []diet.ObjectMotion
	for _, o := range s.Objects {
		if !o.MotionOK {
			continue
		}
		// Only emit an info where the demo actually had one. Three of the crash demo's
		// objects appear ONLY in frame 0's pack; re-emitting them every frame would bloat
		// the file and stop being what Konami wrote.
		if !o.Added && (f >= len(o.Joints) || o.Joints[f] == nil) {
			continue
		}
		om := diet.ObjectMotion{
			ObjectID: o.ID,
			Flag:     0x17,
			Pos: [3]float32{
				float32(o.PosX.Sample(f)), float32(o.PosY.Sample(f)), float32(o.PosZ.Sample(f)),
			},
			Rot: [3]int16{
				int16(o.RotX.Sample(f)), int16(o.RotY.Sample(f)), int16(o.RotZ.Sample(f)),
			},
		}
		if src := jointsAt(o, f); src != nil {
			om.Joints = make([]diet.Joint, len(src))
			for i, j := range src {
				om.Joints[i] = diet.Joint{Quat: j.Quat, Trans: j.Trans}
			}
		}
		objs = append(objs, om)
	}
	if len(objs) == 0 {
		return nil, nil
	}
	body, err := enc.EncodePack(objs)
	if err != nil {
		return nil, err
	}
	return &sdt.Packet{PType: ptMotionPack, Body: body}, nil
}

// jointsAt holds the last authored pose past the end of the imported data, which is
// what an extended demo wants.
func jointsAt(o *ObjectTrack, f int) []Joint {
	if len(o.Joints) == 0 {
		return nil
	}
	if f < len(o.Joints) && o.Joints[f] != nil {
		return o.Joints[f]
	}
	for i := min(f, len(o.Joints)-1); i >= 0; i-- {
		if o.Joints[i] != nil {
			return o.Joints[i]
		}
	}
	return nil
}

func (s *Scene) effectBody(inst *EffectInstance, f int, cat *catalog.Catalog) ([]byte, error) {
	if cat == nil {
		return nil, fmt.Errorf("effect %s: no catalog loaded", inst.UID)
	}
	def := cat.ByEffectID(inst.EffectID)
	if def == nil {
		return nil, fmt.Errorf("effect 0x%08x: not in the catalog", inst.EffectID)
	}

	var body []byte
	if raw, ok := inst.Raw[f]; ok && len(raw) == def.PacketSize()-16 {
		body = append([]byte(nil), raw...) // keep the original pad bytes
	} else if raw, ok := inst.Raw[inst.Start]; ok && len(raw) == def.PacketSize()-16 {
		body = append([]byte(nil), raw...)
	} else {
		life := int32(inst.End - inst.Start)
		body = def.NewBody(life)
	}

	if def.ExecType == 1 {
		if err := def.SetLife(body, int32(inst.End-inst.Start)); err != nil {
			return nil, err
		}
	}

	for i := range def.Params {
		p := &def.Params[i]
		for c := 0; c < components(p); c++ {
			ch := inst.Params[paramChannel(p, c)]
			if ch == nil || (len(ch.Keys) == 0 && len(ch.Baked) == 0) {
				continue
			}
			if err := def.SetFloat(body, p, c, ch.Sample(f)); err != nil {
				return nil, err
			}
		}
	}
	return body, nil
}

func (s *Scene) bakeCaption(orig []byte) ([]byte, error) {
	if !s.CaptionsDirty {
		return orig, nil // untouched subtitles keep Konami's exact bytes
	}
	t := &caption.Table{}
	for _, c := range s.Captions {
		t.Records = append(t.Records, caption.Record{
			Start: int32(c.Start), // already ticks
			End:   int32(c.End),
			Name:  c.Name,
			Lang:  c.Lang,
			Text:  c.Text,
		})
	}
	return t.Encode()
}

func visibleAt(o *ObjectTrack, f int) bool {
	vis := false
	for _, v := range o.Visibility {
		if v.Frame <= f {
			vis = v.Visible
		}
	}
	return vis
}

func cmdBody(cmd sdt.CommandID, id, p1, p2 int32) []byte {
	b := make([]byte, 16)
	le.PutUint32(b[0:], uint32(cmd))
	le.PutUint32(b[4:], uint32(id))
	le.PutUint32(b[8:], uint32(p1))
	le.PutUint32(b[12:], uint32(p2))
	return b
}

func objectBody(o *ObjectTrack) []byte {
	b := make([]byte, 16)
	le.PutUint32(b[0:], uint32(o.CacheID))
	le.PutUint32(b[4:], uint32(o.Flag))
	le.PutUint32(b[8:], uint32(o.Name))
	le.PutUint32(b[12:], uint32(o.KMSCacheID))
	return b
}
