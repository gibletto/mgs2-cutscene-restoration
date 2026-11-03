package sdt

import "math"

// Typed views over packet bodies. Layouts come from Konami's fmt_demo.h; each struct
// there begins with the 16-byte DEMO_PACKET header, so the offsets below are relative
// to Body (i.e. the header is already stripped) and are the struct offsets minus 16.
//
// FVECTOR is 16 bytes (4 floats, w padding), which is why camera_pos/camera_trg land
// on 0x10/0x20 rather than 0x10/0x1C.

func f32(b []byte, off int) float32 { return math.Float32frombits(le.Uint32(b[off:])) }
func putF32(b []byte, off int, v float32) {
	le.PutUint32(b[off:], math.Float32bits(v))
}
func i32(b []byte, off int) int32       { return int32(le.Uint32(b[off:])) }
func putI32(b []byte, off int, v int32) { le.PutUint32(b[off:], uint32(v)) }

// ---- DEMO_FRAME (kind 3): the camera ----------------------------------------

// Frame packet body layout (fmt_demo.h:124):
//
//	0x00 int   no         camera number
//	0x04 float screen     projection-plane distance (default 2.0f) -- NOT an fov in degrees
//	0x08 float z_rotate   camera roll about its Z axis
//	0x0C int   chanl      channel
//	0x10 FVECTOR camera_pos
//	0x20 FVECTOR camera_trg
type CameraFrame struct {
	No      int32
	Screen  float32
	ZRotate float32
	Chanl   int32
	Pos     [3]float32
	Trg     [3]float32
}

func (p *Packet) Camera() (c CameraFrame, ok bool) {
	if p.Kind() != PFrame || len(p.Body) < 0x2C {
		return c, false
	}
	c.No = i32(p.Body, 0x00)
	c.Screen = f32(p.Body, 0x04)
	c.ZRotate = f32(p.Body, 0x08)
	c.Chanl = i32(p.Body, 0x0C)
	for i := 0; i < 3; i++ {
		c.Pos[i] = f32(p.Body, 0x10+4*i)
		c.Trg[i] = f32(p.Body, 0x20+4*i)
	}
	return c, true
}

// SetCamera writes a camera in place, preserving every byte it does not own
// (padding, FVECTOR w components) so an untouched field round-trips exactly.
func (p *Packet) SetCamera(c CameraFrame) bool {
	if p.Kind() != PFrame || len(p.Body) < 0x2C {
		return false
	}
	b := make([]byte, len(p.Body))
	copy(b, p.Body)
	putI32(b, 0x00, c.No)
	putF32(b, 0x04, c.Screen)
	putF32(b, 0x08, c.ZRotate)
	putI32(b, 0x0C, c.Chanl)
	for i := 0; i < 3; i++ {
		putF32(b, 0x10+4*i, c.Pos[i])
		putF32(b, 0x20+4*i, c.Trg[i])
	}
	p.Body = b
	return true
}

// ---- DEMO_COMMAND (kind 2) ---------------------------------------------------

type CommandID int32

const (
	CmdObjectVisible CommandID = 0
	CmdObjectDestroy CommandID = 1
	CmdEffectDestroy CommandID = 2
	CmdCameraCreate  CommandID = 3
)

// Command body (fmt_demo.h:108): command, id, param1, param2.
type Command struct {
	Command CommandID
	ID      int32
	Param1  int32
	Param2  int32
}

func (p *Packet) Command() (c Command, ok bool) {
	if p.Kind() != PCommand || len(p.Body) < 0x10 {
		return c, false
	}
	c.Command = CommandID(i32(p.Body, 0x00))
	c.ID = i32(p.Body, 0x04)
	c.Param1 = i32(p.Body, 0x08)
	c.Param2 = i32(p.Body, 0x0C)
	return c, true
}

// ---- DEMO_OBJECT (kind 4) ----------------------------------------------------

// Object body (fmt_demo.h:136): cache_id, flag, name, kms_cache_id.
type Object struct {
	CacheID    int32 // EVM cache id (model id | extension id)
	Flag       int32 // model init flags
	Name       int32 // StrCode of the object name
	KMSCacheID int32
}

func (p *Packet) Object() (o Object, ok bool) {
	if p.Kind() != PObject || len(p.Body) < 0x10 {
		return o, false
	}
	o.CacheID = i32(p.Body, 0x00)
	o.Flag = i32(p.Body, 0x04)
	o.Name = i32(p.Body, 0x08)
	o.KMSCacheID = i32(p.Body, 0x0C)
	return o, true
}

// ---- DEMO_EFFECT (kind 6) ----------------------------------------------------

// Effect body (fmt_demo.h:180): effect_id, exec_type, pad[2], data[].
type Effect struct {
	EffectID int32
	ExecType int32
	Data     []byte // raw parameter block, starting at body 0x10
}

// ComposerID strips the 0x01000000 tag bit that demo effect ids carry.
func (e Effect) ComposerID() int32 { return e.EffectID & 0xFFFFF }

func (p *Packet) Effect() (e Effect, ok bool) {
	if p.Kind() != PEffect || len(p.Body) < 0x10 {
		return e, false
	}
	e.EffectID = i32(p.Body, 0x00)
	e.ExecType = i32(p.Body, 0x04)
	e.Data = p.Body[0x10:]
	return e, true
}

// ---- DEMO_MOTION_PACK (kind 7) -----------------------------------------------

// MotionPack flags (fmt_demo.h:208).
const (
	MPMov     uint16 = 0x0001 // centre translation
	MPRot     uint16 = 0x0002 // centre rotation
	MPRots    uint16 = 0x0004 // per-joint rotation, relative to parent
	MPAbsRots uint16 = 0x0008 // per-joint rotation, absolute from centre
	MPTrans   uint16 = 0x0010 // per-joint translation
)

// MotionLink is one DEMO_MOTION_INFO (12 bytes) inside a MotionPack.
type MotionLink struct {
	ObjectID   int32
	Flag       uint16
	StartJoint uint8
	NJoints    uint8
	Offset     uint16 // offset to this link's data
	Frame      uint16 // interpolation time in frames (0 = snap)
}

// MotionPack body (fmt_demo.h:201): n_lists, data_offset, pad[2], lists[].
type MotionPack struct {
	NLists     int32
	DataOffset int32
	Links      []MotionLink
	Body       []byte // the whole packet body, so Offset can be resolved against it
}

func (p *Packet) MotionPack() (m MotionPack, ok bool) {
	if p.Kind() != PMotionPack || len(p.Body) < 0x10 {
		return m, false
	}
	m.NLists = i32(p.Body, 0x00)
	m.DataOffset = i32(p.Body, 0x04)
	m.Body = p.Body
	if m.NLists < 0 || 0x10+int(m.NLists)*12 > len(p.Body) {
		return m, false
	}
	for j := 0; j < int(m.NLists); j++ {
		o := 0x10 + j*12
		m.Links = append(m.Links, MotionLink{
			ObjectID:   i32(p.Body, o),
			Flag:       le.Uint16(p.Body[o+4:]),
			StartJoint: p.Body[o+6],
			NJoints:    p.Body[o+7],
			Offset:     le.Uint16(p.Body[o+8:]),
			Frame:      le.Uint16(p.Body[o+10:]),
		})
	}
	return m, true
}
