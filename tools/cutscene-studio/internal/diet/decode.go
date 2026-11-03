package diet

import (
	"fmt"
	"math/bits"
)

// Decoder holds the persistent per-object expand buffers the codec deltas against.
// One Decoder per stream; feed it every MOTION_PACK in order.
type Decoder struct {
	objs map[int32]*objState
}

type objState struct {
	pos    [3]float32
	rot    [3]int16
	posW   uint32
	rotPad uint16
	joints []Joint
}

func NewDecoder() *Decoder { return &Decoder{objs: make(map[int32]*objState)} }

// DecodePack decodes one MOTION_PACK packet body (header stripped) and advances per-object state.
func (d *Decoder) DecodePack(body []byte) ([]ObjectMotion, error) {
	links, _, err := parseLinks(body)
	if err != nil {
		return nil, err
	}
	out := make([]ObjectMotion, len(links))
	for i, l := range links {
		out[i], _, err = d.decodeInfo(body, l)
		if err != nil {
			return nil, fmt.Errorf("info %d (object %d): %w", i, l.objectID, err)
		}
	}
	return out, nil
}

// decodeInfo resolves one info against the object's state and returns the body offset one past
// the last byte its diet stream consumed -- which must be the next info's data start.
func (d *Decoder) decodeInfo(body []byte, l link) (ObjectMotion, int, error) {
	p := int(l.offset) - packetHeader
	if p < 0 || p > len(body) {
		return ObjectMotion{}, 0, fmt.Errorf("offset %#x is outside a %d-byte body", l.offset, len(body))
	}
	st := d.objs[l.objectID]
	if st == nil {
		st = &objState{}
		d.objs[l.objectID] = st
	}
	// The game sizes the expand buffer from the first packet and never grows it; grow instead
	// of running off the end, seeding fresh slots exactly as demo_mtn.c does.
	for len(st.joints) < int(l.nJoints) {
		st.joints = append(st.joints, Joint{Quat: [4]float32{0, 0, 0, 1}})
	}

	if l.flag&FlagMov != 0 {
		if p+16 > len(body) {
			return ObjectMotion{}, 0, fmt.Errorf("pos runs past the body")
		}
		for i := 0; i < 3; i++ {
			st.pos[i] = f32(body, p+4*i)
		}
		st.posW = le.Uint32(body[p+12:])
		p += 16
	}
	if l.flag&FlagRot != 0 {
		if p+8 > len(body) {
			return ObjectMotion{}, 0, fmt.Errorf("rot runs past the body")
		}
		for i := 0; i < 3; i++ {
			st.rot[i] = int16(le.Uint16(body[p+2*i:]))
		}
		st.rotPad = le.Uint16(body[p+6:])
		p += 8
	}

	hasRot := l.flag&(FlagRots|FlagAbsRots) != 0
	hasTrans := l.flag&FlagTrans != 0

	// A component is only consumed if its packet flag is present, so the block's size depends
	// on the flag as well as the mask.
	var consumed uint16
	if hasRot {
		consumed |= omitRot
	}
	if hasTrans {
		consumed |= omitTrans
	}

	for j := 0; j < int(l.nJoints); {
		if p+2 > len(body) {
			return ObjectMotion{}, 0, fmt.Errorf("block header at joint %d runs past the body", j)
		}
		hdr := le.Uint16(body[p:])
		size := 2 + 2*bits.OnesCount16(^hdr&consumed)
		if p+size > len(body) {
			return ObjectMotion{}, 0, fmt.Errorf("block %#04x at joint %d runs past the body", hdr, j)
		}
		run := int(hdr & runMask)
		if run < 1 {
			run = 1 // demo_mtn.c tolerates a 0 count as a 1-joint block; retail never writes one
		}
		for k := 0; k < run && j < int(l.nJoints); k, j = k+1, j+1 {
			v := p + 2 // re-derived per joint: every joint in the run reads the SAME payload
			jt := &st.joints[j]
			if hasRot {
				for c, m := range rotBits {
					if hdr&m == 0 {
						jt.Quat[c] = dequantQuat(int16(le.Uint16(body[v:])))
						v += 2
					}
				}
			} else {
				jt.Quat = [4]float32{0, 0, 0, 1}
			}
			if hasTrans {
				for c, m := range transBits {
					if hdr&m == 0 {
						jt.Trans[c] = MFtoF(le.Uint16(body[v:]))
						v += 2
					}
				}
			} else {
				jt.Trans = [3]float32{}
			}
		}
		p += size
	}

	om := ObjectMotion{
		ObjectID:   l.objectID,
		Flag:       l.flag,
		StartJoint: l.startJoint,
		Pos:        st.pos,
		Rot:        st.rot,
		posW:       st.posW,
		rotPad:     st.rotPad,
		Joints:     append([]Joint(nil), st.joints[:l.nJoints]...),
	}
	return om, p, nil
}
