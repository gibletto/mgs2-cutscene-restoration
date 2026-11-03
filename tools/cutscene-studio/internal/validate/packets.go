package validate

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"sort"

	"mgs2-restoration/cutscene-studio/internal/sdt"
)

var le = binary.LittleEndian

const (
	flagNoSkip        = int32(-0x80000000) // 0x80000000
	flagEndianSwapped = int32(0x40000000)

	maxObjects  = 256 // DM_MAX_OBJECT
	maxEffects  = 512 // DM_MAX_EFFECT: exec_type 1 births, cumulative, never reset
	maxMessages = 128 // GV message bus, per frame
	maxMsgInts  = 512

	// birthRunLimit: consecutive frames of a byte-identical exec_type 1 birth before we
	// call it a re-emission bug. Retail's longest run is 1.
	birthRunLimit = 4

	fxControlChange = int32(0xffff0) // DM_ControlChange
	fxControlBoy    = int32(0xfff04) // NewControlBoy
	fxSendMessage   = int32(0x8000)  // DM_SendEffectMessage (aliased at 0x4001/0x4003/0x100b)
)

func isMessageEffect(id int32) bool {
	return id == fxSendMessage || id == 0x4001 || id == 0x4003 || id == 0x100b
}

func packetSize(p *sdt.Packet) int { return 16 + len(p.Body) }

// walk pulls every packet out of a DEMO chunk payload. sdt.DecodeFrame stops at the first
// terminator (correct for playback, wrong for validation: the last chunk of every demo has
// TERM_SEQ *then* TERM_FRAME), so keep decoding its tail until the bytes run out.
func walk(payload []byte) (pkts []sdt.Packet, tail []byte) {
	for len(payload) >= 16 {
		f, err := sdt.DecodeFrame(payload)
		if err != nil || len(f.Packets) == 0 {
			return pkts, payload
		}
		pkts = append(pkts, f.Packets...)
		payload = f.Tail
	}
	return pkts, payload
}

type objInfo struct {
	frame int
	obj   sdt.Object
}

type birthRun struct{ last, run int }

type site struct{ ci, fi, pi int }

type demoState struct {
	objects   map[int32]objInfo
	names     map[int32]int32
	shown     map[int32]bool
	joints    map[int32]int  // object id -> n_joints, from its first MOTION_PACK
	ctlNames  map[int32]bool // control-point names any named effect registers
	ctlUse    map[int32]site // control-point names DM_ControlChange drives
	motionRef map[int32]site // object id -> where a MOTION_PACK first referenced it
	births    int            // cumulative exec_type 1 births
	termSeq   int            // TERMINATE_SEQUENCE count
	camCreate int
	birthRuns map[string]birthRun // exec1 birth signature -> consecutive-frame run
}

// demo runs P1-P36 across every DEMO chunk, carrying the state the game carries: the
// object list, the effect birth count and the motion expand buffers all live for the whole
// demo, not for one frame.
func (c *check) demo(s *sdt.Stream) {
	st := &demoState{
		objects:   map[int32]objInfo{},
		names:     map[int32]int32{},
		shown:     map[int32]bool{},
		joints:    map[int32]int{},
		ctlNames:  map[int32]bool{},
		ctlUse:    map[int32]site{},
		motionRef: map[int32]site{},
		birthRuns: map[string]birthRun{},
	}
	frame := 0
	lastFrame, lastChunk := -1, -1
	nDemo := 0
	for i := range s.Chunks {
		if s.Chunks[i].Type() == sdt.CDemo {
			nDemo++
		}
	}
	for i := range s.Chunks {
		ch := &s.Chunks[i]
		if ch.Type() != sdt.CDemo {
			continue
		}
		c.frame(st, ch, i, frame, frame == nDemo-1)
		lastFrame, lastChunk = frame, i
		frame++
	}
	if frame == 0 {
		return
	}

	// P9: no TERMINATE_SEQUENCE and the driver reads past the last chunk, gets NULL and
	// hangs on the read-error path with a disc icon.
	switch {
	case st.termSeq == 0:
		c.add("P9", Fatal, lastChunk, lastFrame, -1, "no TERMINATE_SEQUENCE packet anywhere: the demo never ends",
			"put one TERM_SEQ (0x80000000, size 16) immediately before the final TERM_FRAME")
	case st.termSeq > 1:
		c.add("P8", Fatal, -1, -1, -1, fmt.Sprintf("%d TERMINATE_SEQUENCE packets, retail ships exactly 1 per demo", st.termSeq),
			"keep one TERM_SEQ, in the last chunk, immediately before its TERM_FRAME")
	}

	// P14: without a CAMERA_CREATE the camera actor never exists. 294 of 296 shipped demos
	// have one; scndemo_w04b_01 does not, so a missing one is a warning, not a hard fail.
	switch {
	case st.camCreate == 0:
		c.add("P14", Warn, -1, -1, -1, "no CAMERA_CREATE command: the demo's camera track is inert unless another stream created the camera",
			"emit COMMAND(cmd=3) with NOSKIP in chunk 0, before the FRAME packet")
	case st.camCreate > 1:
		c.add("P14", Fatal, -1, -1, -1, fmt.Sprintf("%d CAMERA_CREATE commands, exactly 1 is allowed", st.camCreate),
			"one CAMERA_CREATE, in chunk 0")
	}

	// P30: n_effect never resets or decrements. Overrun silently corrupts DM_WORK in release.
	if st.births > maxEffects {
		c.add("P30", Fatal, -1, -1, -1, fmt.Sprintf("%d exec_type 1 effect births over the demo, the cap is %d (retail worst case 319)", st.births, maxEffects),
			"cut long-lived effects, or split the cutscene; exec_type 0 effects are unbounded")
	}

	// P18: objects are created INVISIBLE (obj_act.c:1012), so an object with no
	// OBJECT_VISIBLE(id, 0) draws nothing for the whole demo.
	for _, id := range sortedKeys(st.objects) {
		if !st.shown[id] {
			o := st.objects[id]
			c.add("P18", Warn, -1, o.frame, -1, fmt.Sprintf("object id=%d (name=0x%06x) is never made visible", id, uint32(o.obj.Name)),
				"emit COMMAND(cmd=0, body id=<id>, param1=0) once; objects spawn hidden (locators legitimately stay hidden)")
		}
	}

	// P22: a MOTION_PACK for an object nobody declared logs BP WARNING: MISSING DEMO
	// (CUTSCENE) OBJECT and animates nothing. Checked at the end because frame 0 ships the
	// MOTION_PACK *before* its OBJECT packets.
	for _, id := range sortedKeys(st.motionRef) {
		if _, live := st.objects[id]; !live {
			s := st.motionRef[id]
			c.add("P22", Fatal, s.ci, s.fi, s.pi, fmt.Sprintf("motion info targets object_id=%d, which no OBJECT packet declares", id),
				"declare the object in frame 0, or drop the info")
		}
	}

	// P34: same deal -- a control point may be registered by an effect later in the file.
	for _, name := range sortedKeys(st.ctlUse) {
		if !st.ctlNames[name] {
			s := st.ctlUse[name]
			c.add("P34", Warn, s.ci, s.fi, s.pi, fmt.Sprintf("DM_ControlChange drives control point 0x%06x, which no effect in this demo registers: a silent no-op plus log spam", uint32(name)),
				"birth a fff04 NewControlBoy with that name (life = the span you drive it over)")
		}
	}
}

func sortedKeys[V any](m map[int32]V) []int32 {
	out := make([]int32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (c *check) frame(st *demoState, ch *sdt.Chunk, ci, fi int, last bool) {
	pkts, tail := walk(ch.Payload)

	// P1: the packet pointer advances *before* any validation, so a size of 0 spins the
	// walker on the same packet forever and a misaligned size desyncs it into garbage.
	if len(tail) >= 16 {
		sz := int32(le.Uint32(tail[4:]))
		c.add("P1", Fatal, ci, fi, len(pkts), fmt.Sprintf("packet %d declares _size=%d (%d bytes left in the chunk)", len(pkts), sz, len(tail)),
			"every packet _size must be >= 16, a multiple of 16, and fit inside the chunk")
	} else if len(tail) > 0 {
		c.add("P10", Warn, ci, fi, -1, fmt.Sprintf("%d bytes of junk after the last packet", len(tail)),
			"a chunk ends exactly on its TERM_FRAME; trailing bytes are dead")
	}

	termFrame := -1
	nFrames := 0
	for pi := range pkts {
		p := &pkts[pi]
		size := packetSize(p)
		kind := p.Kind()

		if size < 16 || size%16 != 0 {
			c.add("P1", Fatal, ci, fi, pi, fmt.Sprintf("%s packet _size=%d is not a positive multiple of 16", kind, size),
				"pad the packet body to 16 bytes")
		}
		// P2: _reserved is 0 in all 10,161,944 shipped packets.
		if p.Reserved != 0 {
			c.add("P2", Warn, ci, fi, pi, fmt.Sprintf("%s packet _reserved=%d, retail is always 0", kind, p.Reserved), "write 0")
		}
		// P3: ENDIAN_SWAPPED is a runtime marker. On disk it tells the game the payload is
		// already swapped, so a big-endian build reads the bytes raw.
		if p.PType&flagEndianSwapped != 0 {
			c.add("P3", Fatal, ci, fi, pi, fmt.Sprintf("%s packet has ENDIAN_SWAPPED (0x40000000) set; it appears on 0 of 10.2 M shipped packets", kind),
				"clear bit 30 of _packet_type; it is an in-RAM marker, never a disk flag")
		}
		// P4/P5: NOSKIP on everything except the camera. Drop a NOSKIP packet on a
		// catch-up frame and the scene state is permanently wrong.
		if kind == sdt.PFrame {
			if p.PType != 0x00000003 {
				c.add("P5", Warn, ci, fi, pi, fmt.Sprintf("FRAME _packet_type=0x%08x, retail is a bare 0x00000003 (no NOSKIP)", uint32(p.PType)),
					"leave FRAME skippable: the camera is a per-frame hard-set and the next chunk restores it")
			}
		} else if !p.NoSkip() {
			c.add("P4", Fatal, ci, fi, pi, fmt.Sprintf("%s packet lacks NOSKIP (_packet_type=0x%08x)", kind, uint32(p.PType)),
				"OR 0x80000000 into _packet_type on every kind except FRAME")
		}

		switch kind {
		case sdt.PTermFrame:
			if termFrame < 0 {
				termFrame = pi
				// P7: on a skipped chunk the loop `continue`s before dispatch, so a
				// TERM_FRAME without NOSKIP never sets frame_end. Timing-dependent crash.
				if !p.NoSkip() {
					c.add("P7", Fatal, ci, fi, pi, "TERM_FRAME lacks NOSKIP: on a frame-skipped chunk it never executes and the packet loop runs off the end of the chunk",
						"write _packet_type = 0x80000001")
				}
			}
		case sdt.PTermSeq:
			st.termSeq++
			if !p.NoSkip() {
				c.add("P8", Fatal, ci, fi, pi, "TERM_SEQ lacks NOSKIP: a skipped final chunk never ends the demo",
					"write _packet_type = 0x80000000")
			}
		case sdt.PFrame:
			nFrames++
			c.camera(p, ci, fi, pi)
		case sdt.PCommand:
			c.command(st, p, ci, fi, pi)
		case sdt.PObject:
			c.object(st, p, ci, fi, pi)
		case sdt.PEffect:
			c.effect(st, p, ci, fi, pi)
		case sdt.PMotionPack:
			c.motion(st, p, ci, fi, pi)
		case sdt.PMotion, sdt.PMST:
			// P25: kind 5 and MST are parsed by the runtime but exercised by zero shipped bytes.
			c.add("P25", Warn, ci, fi, pi, fmt.Sprintf("%s packet: 0 occurrences in any shipped demo, the path is untested", kind),
				"pack skeletal animation into MOTION_PACK (kind 7) instead")
		}
	}

	// P6: the packet loop's only exit is frame_end. No TERM_FRAME and it marches off the
	// end of the chunk (measured: 413,984 bytes) into an access violation or a hard hang.
	if termFrame < 0 {
		c.add("P6", Fatal, ci, fi, -1, fmt.Sprintf("chunk has no TERM_FRAME packet (%d packets); the packet loop has no exit", len(pkts)),
			"end every DEMO chunk with TERM_FRAME: _packet_type=0x80000001, _size=16")
	} else if termFrame != len(pkts)-1 {
		// P10: everything after the executed TERM_FRAME is dead, not fatal -- but silently missing.
		c.add("P10", Warn, ci, fi, termFrame, fmt.Sprintf("%d packets sit after TERM_FRAME and are never executed", len(pkts)-1-termFrame),
			"move them before the terminator")
	}

	// P8: TERM_SEQ only sets sequence_end. It cannot substitute for TERM_FRAME.
	if termFrame >= 0 && termFrame > 0 && pkts[termFrame-1].Kind() == sdt.PTermSeq && !last {
		c.add("P8", Warn, ci, fi, termFrame-1, "TERMINATE_SEQUENCE in a chunk that is not the last one: the demo ends here and every later chunk is dead",
			"put TERM_SEQ in the final chunk only")
	}
	if last && st.termSeq > 0 && (termFrame < 1 || pkts[termFrame-1].Kind() != sdt.PTermSeq) {
		c.add("P8", Fatal, ci, fi, termFrame, "the final chunk does not carry TERM_SEQ immediately before its TERM_FRAME",
			"order the tail as ... TERM_SEQ(0x80000000) TERM_FRAME(0x80000001)")
	}

	// P11: every shipped chunk has exactly one FRAME. Zero is a one-frame hole where
	// scripted camera adjust leaks through; two overflows the 8-slot camera list.
	if nFrames != 1 {
		c.add("P11", Fatal, ci, fi, -1, fmt.Sprintf("%d FRAME packets in this chunk, must be exactly 1", nFrames),
			"emit one camera FRAME packet per DEMO chunk (kind 3, size 64)")
	}
	c.messages(st, pkts, ci, fi)
}

// camera runs P11/P12/P13 on a FRAME packet.
func (c *check) camera(p *sdt.Packet, ci, fi, pi int) {
	if packetSize(p) != 64 {
		c.add("P11", Fatal, ci, fi, pi, fmt.Sprintf("FRAME packet is %d bytes, must be 64", packetSize(p)),
			"DEMO_FRAME is a 16-byte header + no/screen/z_rotate/chanl + two FVECTORs")
		return
	}
	cam, ok := p.Camera()
	if !ok {
		return
	}
	// P11: DM_GetFrameData(0) returns NULL for any other camera number -- the packet is inert.
	if cam.No != 0 {
		c.add("P11", Fatal, ci, fi, pi, fmt.Sprintf("FRAME no=%d; only camera 0 is ever read", cam.No), "set no=0")
	}
	if cam.Chanl != 0 {
		c.add("P11", Warn, ci, fi, pi, fmt.Sprintf("FRAME chanl=%d, retail is always 0 (the field is dead)", cam.Chanl), "set chanl=0")
	}
	// P12: the w lanes are never read. The spec's "[M] garbage in 58 packets" is wrong:
	// 136,974 of the 1,173,986 shipped FRAME packets carry junk in both lanes. Info, not
	// Warn -- and a byte-exact re-bake must preserve it.
	if w := f32(p.Body, 0x1C); w != 0 {
		c.add("P12", Info, ci, fi, pi, fmt.Sprintf("camera_pos.w = %g (exporter junk; the engine never reads it)", w), "write 0.0 in new data; preserve it in a round-trip")
	}
	if w := f32(p.Body, 0x2C); w != 0 {
		c.add("P12", Info, ci, fi, pi, fmt.Sprintf("camera_trg.w = %g (exporter junk; the engine never reads it)", w), "write 0.0 in new data; preserve it in a round-trip")
	}
	// P13: DG_MakeCameraMatrix builds x_vec = (0,-1,0) x (trg-pos) = (-dz, 0, dx). It is
	// exactly zero only when dx and dz both are, and then the engine silently reuses
	// prev_vx -- a file-static from the previous frame, or from the previous SCENE on frame
	// 0. Near-vertical is fine: retail ships |ny| up to 0.9999999.
	dx32, dz32 := cam.Trg[0]-cam.Pos[0], cam.Trg[2]-cam.Pos[2]
	dx, dy, dz := float64(dx32), float64(cam.Trg[1]-cam.Pos[1]), float64(dz32)
	l := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if l == 0 {
		c.add("P13", Fatal, ci, fi, pi, "camera_trg == camera_pos: the view vector is zero and the camera basis is undefined",
			"separate the target from the position")
		return
	}
	if dx32 == 0 && dz32 == 0 {
		c.add("P13", Fatal, ci, fi, pi, fmt.Sprintf("view vector is exactly vertical (trg-pos = (0, %g, 0)): the cross product with (0,-1,0) is zero, so roll inherits prev_vx from the last frame -- or from the last scene", dy),
			"nudge trg.x or trg.z off the axis; 0 of 1,173,986 shipped frames do this")
		return
	}
	if ny := math.Abs(dy / l); ny > 0.9999 {
		c.add("P13", Warn, ci, fi, pi, fmt.Sprintf("view vector is near-vertical (|normalize(trg-pos).y| = %.7f): the x basis comes from a tiny cross product and roll gets noisy", ny),
			"tilt the shot; retail does ship 2,224 such frames (max 0.9999999), so it works, but there is no up-vector to fall back on")
	}
}

// command runs P14-P17 on a COMMAND packet.
func (c *check) command(st *demoState, p *sdt.Packet, ci, fi, pi int) {
	if packetSize(p) != 32 {
		c.add("P1", Fatal, ci, fi, pi, fmt.Sprintf("COMMAND packet is %d bytes, must be 32", packetSize(p)), "DEMO_COMMAND is 16 + command/id/param1/param2")
		return
	}
	cmd, ok := p.Command()
	if !ok {
		return
	}
	switch cmd.Command {
	case sdt.CmdObjectVisible:
		// P17: the receiver reads the BODY id at +0x14, not the header _id at +0x08.
		if _, live := st.objects[cmd.ID]; !live {
			c.add("P17", Fatal, ci, fi, pi, fmt.Sprintf("OBJECT_VISIBLE targets id=%d, which no OBJECT packet declared (header _id is %d -- did you write the id into the header?)", cmd.ID, p.ID),
				"put the object id in the packet BODY at +0x14; the header _id is a free running counter")
		}
		if cmd.Param1 == 0 {
			st.shown[cmd.ID] = true
		}
	case sdt.CmdObjectDestroy:
		// P15: DM_DestroyObject compacts obj_list but never decrements n_object.
		c.add("P15", Fatal, ci, fi, pi, fmt.Sprintf("OBJECT_DESTROY on id=%d: DM_DestroyObject never decrements n_object, leaving a duplicated tail entry and a read past the live range", cmd.ID),
			"hide the object instead: OBJECT_VISIBLE(id, 15)")
	case sdt.CmdEffectDestroy:
		// P16: the handler body is commented out; DM_DestroyEffect has zero callers.
		c.add("P16", Info, ci, fi, pi, "EFFECT_DESTROY is a no-op in the shipped code (the handler body is commented out)",
			"give the effect a life at birth; that is its only clock")
	case sdt.CmdCameraCreate:
		st.camCreate++
		// P14: without NOSKIP a frame-skip on frame 0 drops it and the camera track is inert.
		if !p.NoSkip() {
			c.add("P14", Fatal, ci, fi, pi, "CAMERA_CREATE lacks NOSKIP: a skipped frame 0 drops it and the whole camera track goes silently inert",
				"write _packet_type = 0x80000002")
		}
		if fi != 0 {
			c.add("P14", Fatal, ci, fi, pi, fmt.Sprintf("CAMERA_CREATE in frame %d; all 294 shipped ones are in chunk 0", fi),
				"create the camera once, in frame 0, before the first FRAME packet")
		}
		if cmd.ID != 0 || cmd.Param1 != 0 || cmd.Param2 != 0 {
			c.add("P14", Warn, ci, fi, pi, fmt.Sprintf("CAMERA_CREATE has id=%d param1=%d param2=%d; all shipped ones are all-zero (the fields are dead)", cmd.ID, cmd.Param1, cmd.Param2),
				"zero id/param1/param2; the camera's flags come from the GCL -c option")
		}
	default:
		c.add("P16", Warn, ci, fi, pi, fmt.Sprintf("unknown command %d; only 0..3 exist", int32(cmd.Command)), "use OBJECT_VISIBLE (0) or CAMERA_CREATE (3)")
	}
}

// object runs P19-P21 on an OBJECT packet.
func (c *check) object(st *demoState, p *sdt.Packet, ci, fi, pi int) {
	if packetSize(p) != 32 {
		c.add("P1", Fatal, ci, fi, pi, fmt.Sprintf("OBJECT packet is %d bytes, must be 32", packetSize(p)), "DEMO_OBJECT is 16 + cache_id/flag/name/kms_cache_id")
		return
	}
	o, ok := p.Object()
	if !ok {
		return
	}
	// P21: all 4,488 shipped OBJECT packets live in frame 0. Mid-stream spawns would work
	// but no shipped byte exercises them.
	if fi != 0 {
		c.add("P21", Warn, ci, fi, pi, fmt.Sprintf("OBJECT id=%d declared in frame %d; retail declares the whole cast in frame 0", p.ID, fi),
			"declare the object in frame 0 and reveal it later with OBJECT_VISIBLE")
	}
	// P19: DM_Packet_Object has no duplicate-id guard in release; re-issuing an id appends
	// a second entry for the same object.
	if prev, dup := st.objects[p.ID]; dup {
		c.add("P19", Fatal, ci, fi, pi, fmt.Sprintf("object id=%d is re-issued (already declared in frame %d): the release build appends a second entry for the same object", p.ID, prev.frame),
			"give every object a unique, stable id")
	}
	if len(st.objects) >= maxObjects {
		c.add("P19", Fatal, ci, fi, pi, fmt.Sprintf("more than %d objects (DM_MAX_OBJECT): the overrun silently corrupts DM_WORK in release", maxObjects),
			"cut the cast")
	}
	st.objects[p.ID] = objInfo{frame: fi, obj: o}

	if o.Name == 0 {
		c.add("P19", Warn, ci, fi, pi, fmt.Sprintf("object id=%d has name=0; retail never ships one", p.ID), "give it a GV_StrCode name (EUC-JP bytes, 24-bit)")
	} else if other, dup := st.names[o.Name]; dup {
		c.add("P19", Warn, ci, fi, pi, fmt.Sprintf("object id=%d reuses name 0x%06x (already on id=%d); names are unique in every shipped demo and publish the object in the where-list", p.ID, uint32(o.Name), other),
			"pick a unique name")
	} else {
		st.names[o.Name] = p.ID
	}

	// P20: cache ids are (spec << 24) | root24, spec = ext[0]-'a'. 'k' = 0x0A, 'e' = 0x04.
	if o.KMSCacheID>>24 != 0x0A || o.KMSCacheID&0xFFFFFF == 0 {
		c.add("P20", Fatal, ci, fi, pi, fmt.Sprintf("object id=%d has kms_cache_id=0x%08x; it must be 0x0A<<24 | a nonzero 24-bit id", p.ID, uint32(o.KMSCacheID)),
			"every demo object needs a KMS; the spec byte is 'k'-'a' = 0x0A")
	}
	if o.CacheID != 0 && o.CacheID>>24 != 0x04 {
		c.add("P20", Fatal, ci, fi, pi, fmt.Sprintf("object id=%d has cache_id=0x%08x; it must be 0 (KMS-only rigid prop) or 0x04<<24 | id (EVM)", p.ID, uint32(o.CacheID)),
			"zero cache_id for a rigid prop, or use the EVM spec byte 0x04")
	}
	if o.Flag&^0x17 != 0 {
		c.add("P20", Fatal, ci, fi, pi, fmt.Sprintf("object id=%d has flag=0x%x; only bits 0,1,2,4 (mask 0x17) are read", p.ID, uint32(o.Flag)),
			"flag & ~0x17 must be 0: 0x01 jointed, 0x02 pre-shaded, 0x04 translucent, 0x10 latterdraw")
	}
}

// effect runs P26-P30 and P34/P35 on an EFFECT packet.
func (c *check) effect(st *demoState, p *sdt.Packet, ci, fi, pi int) {
	e, ok := p.Effect()
	if !ok {
		c.add("P26", Fatal, ci, fi, pi, fmt.Sprintf("EFFECT packet is %d bytes, too small for effect_id/exec_type/pad", packetSize(p)),
			"an effect packet is at least 32 bytes")
		return
	}
	// P29: the class tag is the game's global CHARA registry id. Anything else resolves to
	// no chara and the packet is silently dropped.
	if e.EffectID&^0xFFFFF != 0x01000000 {
		c.add("P29", Fatal, ci, fi, pi, fmt.Sprintf("effect_id=0x%08x: the high bits must be 0x01000000 (class tag) | a 20-bit composer id", uint32(e.EffectID)),
			"effect_id = 0x01000000 | composer_id")
		return
	}
	id := e.ComposerID()
	spec, known := c.cat.Effect(id)
	if !known {
		c.add("P29", Warn, ci, fi, pi, fmt.Sprintf("composer id 0x%x is not in the 245-effect catalog", uint32(id)),
			"only ship effects whose launcher exists; an unknown id is silently dropped")
	}
	if c.ctx != nil && c.ctx.StageEffects != nil && !c.ctx.StageEffects[id] {
		c.add("P29", Fatal, ci, fi, pi, fmt.Sprintf("composer id 0x%x is not linked into this stage's chara table: GM_GetCharaID returns NULL and the packet is silently dropped", uint32(id)),
			"pick an effect from the stage's .lst allow-list, or the packet does nothing")
	}
	// P36: the launcher's own models/textures must be resident in the stage.
	if c.ctx != nil && c.ctx.StageAssets != nil {
		for _, a := range spec.AddSrc {
			if !c.ctx.StageAssets[a] {
				c.add("P36", Warn, ci, fi, pi, fmt.Sprintf("%s (0x%x) needs %q, which is not in the stage manifest", spec.Name, uint32(id), a),
					"add the asset to the stage's manifest.txt + bp_assets.txt, or drop the effect")
			}
		}
	}

	// P27: eft_act.c has exactly two cases. exec_type 2+ falls through and the packet dies.
	if e.ExecType != 0 && e.ExecType != 1 {
		c.add("P27", Fatal, ci, fi, pi, fmt.Sprintf("exec_type=%d: only 0 and 1 exist, anything else is silently dropped", e.ExecType),
			"take exec_type from the catalog; it is a fixed property of the effect id")
		return
	}
	if known && e.ExecType != spec.ExecType {
		c.add("P27", Fatal, ci, fi, pi, fmt.Sprintf("%s (0x%x) has exec_type=%d, the catalog says %d", spec.Name, uint32(id), e.ExecType, spec.ExecType),
			"exec_type is fixed per effect id and decides where Data starts (data[0] vs data[4])")
	}
	// P26/P32: size = 32 + preamble + roundup16(sizeof(Data)). 32 + sizeof(Data) desyncs the walker.
	if known && packetSize(p) != spec.BodySize+16 {
		c.add("P26", Fatal, ci, fi, pi, fmt.Sprintf("%s (0x%x, exec %d) packet is %d bytes, the catalog layout says %d (32 + %d preamble + roundup16(%d))",
			spec.Name, uint32(id), e.ExecType, packetSize(p), spec.BodySize+16, 16*int(spec.ExecType), spec.DataSize),
			"pad Data to a multiple of 16; IVECTOR/FVECTOR are 16-aligned, SVECTOR is 8 bytes aligned 2")
	}
	if e.ExecType == 1 {
		// P28: the [life,0,0,0] preamble. Data starts at data[4], not data[0].
		if len(e.Data) < 16 {
			c.add("P28", Fatal, ci, fi, pi, fmt.Sprintf("exec_type 1 packet has only %d bytes of data, too small for the 16-byte [life,0,0,0] preamble", len(e.Data)),
				"exec_type 1 prepends life + 3 reserved ints; Data starts at data[4]")
			return
		}
		life := i32(e.Data, 0)
		for w := 1; w < 4; w++ {
			if i32(e.Data, 4*w) != 0 {
				c.add("P28", Warn, ci, fi, pi, fmt.Sprintf("exec_type 1 preamble word %d = %d, retail is 0 in all 10,478 shipped packets", w, i32(e.Data, 4*w)),
					"zero data[1..3]; only data[0] (life, in demo frames) is read")
				break
			}
		}
		if life < 0 {
			c.add("P28", Info, ci, fi, pi, fmt.Sprintf("life=%d frames: the cushion actor dies on its first tick (retail ships 6 of these)", life),
				"life is in demo frames and is decremented by DM_FrameSkip+1 each tick")
		}
		st.births++
		c.noteBirth(st, p, e, ci, fi, pi)
	}

	// P34: a DM_ControlChange whose name nothing created is a silent no-op + log spam. The
	// creator is usually a fff04 NewControlBoy, but any of the 103 effects whose Data starts
	// with `int name` registers one -- checking only NewControlBoy misfires on 92 of the 262
	// shipped demos. Collected here, resolved once the whole demo has been read.
	base := 0
	if e.ExecType == 1 {
		base = 16
	}
	switch {
	case id == fxControlChange && len(e.Data) >= 4:
		st.ctlUse[i32(e.Data, 0)] = site{ci, fi, pi}
	case id != fxSendMessage && known && spec.HasName && len(e.Data) >= base+4:
		st.ctlNames[i32(e.Data, base)] = true
	}
}

// noteBirth catches P35: re-emitting an exec_type 1 birth packet every frame to "animate"
// it births a fresh actor every frame and walks into the 512-birth cap.
func (c *check) noteBirth(st *demoState, p *sdt.Packet, e sdt.Effect, ci, fi, pi int) {
	key := string(p.Body)
	r := st.birthRuns[key]
	if r.last == fi-1 {
		r.run++
	} else {
		r.run = 1
	}
	r.last = fi
	st.birthRuns[key] = r
	if r.run == birthRunLimit {
		spec, _ := c.cat.Effect(e.ComposerID())
		c.add("P35", Warn, ci, fi, pi, fmt.Sprintf("%s (0x%x) re-born byte-identically on %d consecutive frames: each one is a new actor against the %d-birth cap",
			spec.Name, uint32(e.ComposerID()), r.run, maxEffects),
			"birth an exec_type 1 effect once and drive it per-frame with an exec_type 0 channel (DM_ControlChange) or DM_SendEffectMessage")
	}
}

// messages runs P31: the GV bus is 128 messages / 512 ints per frame, and it is
// double-buffered, so anything over the cap is silently lost.
func (c *check) messages(st *demoState, pkts []sdt.Packet, ci, fi int) {
	n, ints := 0, 0
	for pi := range pkts {
		p := &pkts[pi]
		if p.Kind() != sdt.PEffect {
			continue
		}
		e, ok := p.Effect()
		if !ok || !isMessageEffect(e.ComposerID()) {
			continue
		}
		n++
		spec, known := c.cat.Effect(e.ComposerID())
		if known && e.ComposerID() == fxSendMessage && len(e.Data) >= spec.DataSize {
			num := int(i32(e.Data, spec.DataSize-4)) // `num` is the last member of the Data struct
			if num > 0 {
				ints += num
			}
		} else {
			ints++
		}
	}
	if n > maxMessages {
		c.add("P31", Fatal, ci, fi, -1, fmt.Sprintf("%d effect messages this frame, the GV bus holds %d", n, maxMessages),
			"spread the messages over several frames")
	}
	if ints > maxMsgInts {
		c.add("P31", Fatal, ci, fi, -1, fmt.Sprintf("%d message ints this frame, the GV bus holds %d", ints, maxMsgInts),
			"send fewer/shorter messages; `num` counts the command itself")
	}
}

// motion runs P22-P24 on a MOTION_PACK.
func (c *check) motion(st *demoState, p *sdt.Packet, ci, fi, pi int) {
	m, ok := p.MotionPack()
	if !ok {
		c.add("P22", Fatal, ci, fi, pi, fmt.Sprintf("MOTION_PACK info table does not fit in the %d-byte packet", packetSize(p)),
			"n_lists infos of 12 bytes start at packet+0x20")
		return
	}
	if p.ID != 0 {
		c.add("P22", Warn, ci, fi, pi, fmt.Sprintf("MOTION_PACK _id=%d, retail is 0 in all 1,153,176 packs", p.ID), "write _id = 0")
	}
	// P22: data_offset is documented as meaningless and never read -- but retail is exact,
	// and a reader that trusts it (ours does not) would walk into the info table.
	if want := int32(32 + 12*m.NLists); m.DataOffset != want {
		c.add("P22", Warn, ci, fi, pi, fmt.Sprintf("MOTION_PACK data_offset=%d, should be 32 + 12*%d = %d", m.DataOffset, m.NLists, want),
			"write data_offset = 32 + 12*n_lists")
	}

	seen := map[int32]bool{}
	body := p.Body
	for k, l := range m.Links {
		// P22: the expand buffer is keyed by object_id alone -- two infos for one object in
		// one pack fight over a single buffer sized by whichever came first.
		if seen[l.ObjectID] {
			c.add("P22", Fatal, ci, fi, pi, fmt.Sprintf("two motion infos for object_id=%d in one MOTION_PACK: they share one expand buffer", l.ObjectID),
				"one info per object per pack")
		}
		seen[l.ObjectID] = true
		if _, ok := st.motionRef[l.ObjectID]; !ok {
			st.motionRef[l.ObjectID] = site{ci, fi, pi}
		}
		if l.StartJoint != 0 {
			c.add("P22", Warn, ci, fi, pi, fmt.Sprintf("motion info start_joint=%d, retail is always 0", l.StartJoint), "write start_joint = 0")
		}
		if l.Flag != 0x17 {
			c.add("P22", Warn, ci, fi, pi, fmt.Sprintf("motion info flag=0x%04x, retail is always 0x0017 (MOV|ROT|ROTS|TRANS)", l.Flag), "write flag = 0x17")
		}
		// P22: the expand buffer is malloc'd once, from the first pack. A later pack with
		// more joints writes past it.
		if prev, ok := st.joints[l.ObjectID]; ok {
			if int(l.NJoints) > prev {
				c.add("P22", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d now has %d joints, its first MOTION_PACK had %d: the expand buffer was sized from the first one", l.ObjectID, l.NJoints, prev),
					"keep n_joints constant for an object across the whole demo")
			}
		} else {
			st.joints[l.ObjectID] = int(l.NJoints)
		}
		if l.NJoints == 0 {
			c.add("P22", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d has n_joints=0; the decoder still dereferences 2 bytes at the block pointer", l.ObjectID),
				"use n_joints = 1 with a 0x7F01 no-op block for a rigid prop")
		}
		// P24: offset is packet-relative (not body-relative, not info-relative) and only
		// 2-byte aligned. "Fixing" it to 16 breaks the walk.
		if l.Offset%2 != 0 {
			c.add("P24", Fatal, ci, fi, pi, fmt.Sprintf("motion info offset=%d is odd; it is packet-relative and 2-byte aligned", l.Offset), "align the diet blob to 2")
		}
		start := int(l.Offset) - 16 // packet-relative -> body-relative
		if start < 0 || start > len(body) {
			c.add("P24", Fatal, ci, fi, pi, fmt.Sprintf("motion info offset=%d falls outside the %d-byte packet", l.Offset, packetSize(p)),
				"offset counts from the start of the packet header")
			continue
		}
		end, last := len(body), true
		if k+1 < len(m.Links) {
			end, last = int(m.Links[k+1].Offset)-16, false
		}
		c.diet(body, start, end, last, l, ci, fi, pi)
	}
}

// diet walks one info's block stream: 16-byte pos + 8-byte rot preamble, then u16-headed
// blocks whose low byte is a RUN COUNT and whose high byte masks out unchanged components.
func (c *check) diet(body []byte, start, end int, last bool, l sdt.MotionLink, ci, fi, pi int) {
	cur := start
	if l.Flag&0x01 != 0 { // MOV: FVECTOR pos
		cur += 16
	}
	if l.Flag&0x02 != 0 { // ROT: SVECTOR rot
		cur += 8
	}
	if cur > len(body) {
		c.add("P24", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d: the pos/rot preamble runs past the end of the packet", l.ObjectID), "check info.offset")
		return
	}
	joints := 0
	for joints < int(l.NJoints) {
		if cur+2 > len(body) {
			c.add("P24", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d: the diet stream ran off the packet after %d of %d joints", l.ObjectID, joints, l.NJoints),
				"every joint must be covered by a block; a still joint costs 2 bytes (0x7F00|run)")
			return
		}
		h := le.Uint16(body[cur:])
		run := int(h & 0xFF)
		// P23: the encoder extends a run with *last_flag += 1, so 256 spills into the mask
		// bits. count 0 is tolerated by the decoder but appears in 0 retail blocks.
		if run == 0 {
			c.add("P23", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d: diet block 0x%04x has run count 0", l.ObjectID, h),
				"run counts are 1..255; break a longer run into another block")
			run = 1
		}
		vals := 7 - bits.OnesCount16(h&0x7F00)
		cur += 2 + 2*vals
		joints += run
	}
	if joints != int(l.NJoints) {
		c.add("P23", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d: the diet blocks cover %d joints, n_joints says %d", l.ObjectID, joints, l.NJoints),
			"a run must not overshoot n_joints; break it at the last joint")
	}
	// P24: retail lands exactly on the next info's data start, every time. The last info
	// only has the packet's 16-byte pad after it.
	switch {
	case !last && cur != end:
		c.add("P24", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d: the diet stream ends at packet+%d, the next info's data starts at packet+%d", l.ObjectID, cur+16, end+16),
			"pack infos back-to-back with no padding; offset = 32 + 12*n_lists + running blob length")
	case last && (cur > end || end-cur >= 16):
		c.add("P24", Fatal, ci, fi, pi, fmt.Sprintf("object_id=%d: the diet stream ends at packet+%d, %d bytes short of the %d-byte packet (only the 16-byte pad may follow)", l.ObjectID, cur+16, end-cur, end+16),
			"size the packet as align16(32 + 12*n_lists + blob length)")
	}
}

func f32(b []byte, off int) float32 {
	if off+4 > len(b) {
		return 0
	}
	return math.Float32frombits(le.Uint32(b[off:]))
}

func i32(b []byte, off int) int32 {
	if off+4 > len(b) {
		return 0
	}
	return int32(le.Uint32(b[off:]))
}
