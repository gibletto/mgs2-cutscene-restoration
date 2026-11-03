// Package diet implements the MOTION_PACK (kind 7) "diet" motion codec.
//
// The codec is stateful and sequential, per object. A block header's high byte is an OMIT
// mask: an omitted component keeps the value that joint had in the PREVIOUS packet for the
// SAME object. Its low byte is a run count: one payload, read again by each of N consecutive
// joints in the same frame. So neither decoding nor encoding a single frame in isolation is
// possible -- drive one Decoder/Encoder over the whole stream, in order.
//
// Read side mirrors game/demo_mtn.c:461-525, write side mirrors tools/diet_de2/conv.c:444-546.
package diet

import (
	"encoding/binary"
	"fmt"
	"math"
)

var le = binary.LittleEndian

// DEMO_MOTION_INFO.flag (fmt_demo.h:208). Retail is always FlagMov|FlagRot|FlagRots|FlagTrans.
const (
	FlagMov     uint16 = 0x01 // leading FVECTOR pos (object centre, world)
	FlagRot     uint16 = 0x02 // then SVECTOR rot (object centre, 12-bit angles)
	FlagRots    uint16 = 0x04 // joint quats, relative to the parent joint
	FlagAbsRots uint16 = 0x08 // joint quats, absolute from the object centre
	FlagTrans   uint16 = 0x10 // joint translations
	FlagRetail  uint16 = 0x17
)

// Block header bits (fmt_demo.h:167). The low byte is the run count, not part of the mask.
const (
	omitRotX   uint16 = 0x0100
	omitRotY   uint16 = 0x0200
	omitRotZ   uint16 = 0x0400
	omitRotW   uint16 = 0x0800
	omitTransX uint16 = 0x1000
	omitTransY uint16 = 0x2000
	omitTransZ uint16 = 0x4000

	omitRot   = omitRotX | omitRotY | omitRotZ | omitRotW
	omitTrans = omitTransX | omitTransY | omitTransZ
	omitAll   = omitRot | omitTrans // 0x7F00
	runMask   = 0x00FF
	maxRun    = 255
)

var (
	rotBits   = [4]uint16{omitRotX, omitRotY, omitRotZ, omitRotW}
	transBits = [3]uint16{omitTransX, omitTransY, omitTransZ}
)

const (
	packetHeader = 16   // DEMO_MOTION_INFO.offset is packet-relative; our bodies are header-stripped
	packHeader   = 0x10 // body offset of the info table
	infoSize     = 12
	quatScale    = 16383.0 // demo scale; MST uses 32767 -- do not mix them
)

// Joint is one skeleton joint: a quaternion (x,y,z,w) and a translation added to the joint's
// rest offset. Quats come back exactly as they were quantised, NOT normalised -- the game
// normalises after decode but Konami's encoder cache holds the raw values, and matching the
// encoder is what makes a re-bake byte-exact.
type Joint struct {
	Quat  [4]float32
	Trans [3]float32
}

// ObjectMotion is one DEMO_MOTION_INFO's worth of state for one object, after the temporal
// mask has been resolved: every component is populated, whether this packet carried it or not.
//
// posW and rotPad are the FVECTOR w and SVECTOR pad slots. Retail ships uninitialised junk
// there (NaN, denormals) which nothing ever reads; Decoder carries it so a decode/encode of a
// shipped pack is byte-exact, and authored motion leaves it zero, as a baker should.
type ObjectMotion struct {
	ObjectID   int32
	Flag       uint16     // retail is always 0x17
	StartJoint uint8      // retail always 0
	Pos        [3]float32 // world, object centre
	Rot        [3]int16   // 12-bit fixed, 4096 == 360 deg
	Joints     []Joint

	posW   uint32
	rotPad uint16
}

// link is a DEMO_MOTION_INFO (12 bytes).
type link struct {
	objectID   int32
	flag       uint16
	startJoint uint8
	nJoints    uint8
	offset     uint16
	frame      uint16
}

func parseLinks(body []byte) (links []link, dataOffset int32, err error) {
	if len(body) < packHeader {
		return nil, 0, fmt.Errorf("motion pack body is %d bytes, want at least %d", len(body), packHeader)
	}
	n := int32(le.Uint32(body))
	dataOffset = int32(le.Uint32(body[4:]))
	if n < 0 || packHeader+int(n)*infoSize > len(body) {
		return nil, 0, fmt.Errorf("n_lists %d does not fit in a %d-byte body", n, len(body))
	}
	links = make([]link, n)
	for i := range links {
		o := packHeader + i*infoSize
		links[i] = link{
			objectID:   int32(le.Uint32(body[o:])),
			flag:       le.Uint16(body[o+4:]),
			startJoint: body[o+6],
			nJoints:    body[o+7],
			offset:     le.Uint16(body[o+8:]),
			frame:      le.Uint16(body[o+10:]),
		}
	}
	return links, dataOffset, nil
}

// MFtoF expands Konami's private 16-bit float (s1 e5 f10, bias 8) used for joint translations.
// An exponent of 0 yields an IEEE denormal around 1e-42, i.e. numerically zero.
func MFtoF(mf uint16) float32 {
	s := uint32(mf&0x8000) << 16
	e := uint32(mf & 0x7c00)
	if e != 0 {
		e += (127 - 8) << 10
		e <<= 13
	}
	f := uint32(mf&0x03ff) << 13
	return math.Float32frombits(s | e | f)
}

// FtoMF is the inverse, and it TRUNCATES. The encoder's omit test is FtoMF(prev) == FtoMF(cur),
// so rounding here would change which components get masked and thus the byte stream.
func FtoMF(f float32) uint16 {
	d := math.Float32bits(f)
	s := uint16((d & 0x80000000) >> 16)
	e := d & 0x7f800000
	if e != 0 {
		if e <= (127-8)<<23 { // |f| < 2^-7: flush to zero
			return s
		}
		if e > (127-8+31)<<23 { // |f| >= 2^24: saturate
			return s | 0x7fff
		}
		e -= (127 - 8) << 23
		e >>= 13
	}
	return s | uint16(e) | uint16((d&0x007fffff)>>13)
}

// quantQuat is conv.c's QUAT_INT: C truncation toward zero, and the omit test compares these
// full-width ints even though only the low 16 bits reach the stream.
func quantQuat(f float32) int32 { return int32(f * quatScale) }

func dequantQuat(v int16) float32 { return float32(v) / quatScale }

func align16(n int) int { return (n + 15) &^ 15 }

func f32(b []byte, off int) float32 { return math.Float32frombits(le.Uint32(b[off:])) }

func appendF32(b []byte, v float32) []byte {
	return le.AppendUint32(b, math.Float32bits(v))
}

func appendU16(b []byte, v uint16) []byte { return le.AppendUint16(b, v) }
