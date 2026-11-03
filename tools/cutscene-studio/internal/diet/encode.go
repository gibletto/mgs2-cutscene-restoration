package diet

import "fmt"

// Encoder holds the per-object previous-frame cache the omit mask is computed against.
// One Encoder per stream; feed it every frame in order. Konami's cache keeps the raw floats
// and re-quantises on each comparison, which is the same thing as caching the quantised values.
type Encoder struct {
	objs map[int32][]quantJoint
}

// quantJoint is a joint as the stream sees it. The omit test compares the full-width quat ints
// (conv.c does `QUAT_INT(old) == QUAT_INT(src)` on ints) while the run-fold compares the shorts
// that actually got written -- the two only differ for quats outside [-2,2], but match them anyway.
type quantJoint struct {
	quat  [4]int32
	trans [3]uint16
}

func identityQuant() quantJoint {
	return quantJoint{quat: [4]int32{0, 0, 0, quantQuat(1)}}
}

func NewEncoder() *Encoder { return &Encoder{objs: make(map[int32][]quantJoint)} }

// EncodePack builds one MOTION_PACK packet body. Info offsets are packet-relative (body offset
// + 16). The result length is a multiple of 16.
func (e *Encoder) EncodePack(objs []ObjectMotion) ([]byte, error) {
	head := packHeader + infoSize*len(objs)
	base := packetHeader + head // packet-relative start of the data area == data_offset

	blob := make([]byte, 0, 64*len(objs))
	links := make([]link, len(objs))
	for i := range objs {
		o := &objs[i]
		if len(o.Joints) > 255 {
			return nil, fmt.Errorf("object %d: %d joints, n_joints is a u8", o.ObjectID, len(o.Joints))
		}
		off := base + len(blob)
		if off > 0xFFFF {
			return nil, fmt.Errorf("object %d: data offset %#x does not fit in the u16 info field", o.ObjectID, off)
		}
		links[i] = link{
			objectID:   o.ObjectID,
			flag:       o.Flag,
			startJoint: o.StartJoint,
			nJoints:    uint8(len(o.Joints)),
			offset:     uint16(off),
			frame:      1, // dead in the demo player, and retail always writes 1
		}
		blob = e.appendObject(blob, o)
	}

	body := make([]byte, align16(base+len(blob))-packetHeader)
	le.PutUint32(body, uint32(len(objs)))
	le.PutUint32(body[4:], uint32(base))
	for i, l := range links {
		o := packHeader + i*infoSize
		le.PutUint32(body[o:], uint32(l.objectID))
		le.PutUint16(body[o+4:], l.flag)
		body[o+6] = l.startJoint
		body[o+7] = l.nJoints
		le.PutUint16(body[o+8:], l.offset)
		le.PutUint16(body[o+10:], l.frame)
	}
	copy(body[head:], blob)
	return body, nil
}

func (e *Encoder) appendObject(dst []byte, o *ObjectMotion) []byte {
	if o.Flag&FlagMov != 0 {
		for _, v := range o.Pos {
			dst = appendF32(dst, v)
		}
		dst = le.AppendUint32(dst, o.posW)
	}
	if o.Flag&FlagRot != 0 {
		for _, v := range o.Rot {
			dst = appendU16(dst, uint16(v))
		}
		dst = appendU16(dst, o.rotPad)
	}

	prev := e.objs[o.ObjectID]
	for len(prev) < len(o.Joints) {
		prev = append(prev, identityQuant())
	}
	e.objs[o.ObjectID] = prev

	hasRot := o.Flag&(FlagRots|FlagAbsRots) != 0
	hasTrans := o.Flag&FlagTrans != 0

	lastHdr := -1 // where in dst the open block's header sits
	var lastQuat [4]int16
	var lastTrans [3]uint16

	for j := range o.Joints {
		var cur quantJoint
		var quat [4]int16
		for c, v := range o.Joints[j].Quat {
			cur.quat[c] = quantQuat(v)
			quat[c] = int16(cur.quat[c])
		}
		for c, v := range o.Joints[j].Trans {
			cur.trans[c] = FtoMF(v)
		}

		var flag uint16
		if hasRot {
			for c, m := range rotBits {
				if prev[j].quat[c] == cur.quat[c] {
					flag |= m
				}
			}
		} else {
			flag |= omitRot
		}
		if hasTrans {
			for c, m := range transBits {
				if prev[j].trans[c] == cur.trans[c] {
					flag |= m
				}
			}
		} else {
			flag |= omitTrans
		}
		prev[j] = cur // the cache is the whole current frame, folded joints included

		if lastHdr >= 0 {
			hdr := le.Uint16(dst[lastHdr:])
			if hdr&^runMask == flag && hdr&runMask < maxRun && samePayload(flag, quat, cur.trans, lastQuat, lastTrans) {
				le.PutUint16(dst[lastHdr:], hdr+1) // extend the run, emit no payload
				continue
			}
		}

		lastHdr = len(dst)
		dst = appendU16(dst, flag|1)
		for c, m := range rotBits {
			if flag&m == 0 {
				dst = appendU16(dst, uint16(quat[c]))
			}
		}
		for c, m := range transBits {
			if flag&m == 0 {
				dst = appendU16(dst, cur.trans[c])
			}
		}
		lastQuat, lastTrans = quat, cur.trans
	}
	return dst
}

func samePayload(flag uint16, quat [4]int16, trans [3]uint16, lastQuat [4]int16, lastTrans [3]uint16) bool {
	for c, m := range rotBits {
		if flag&m == 0 && quat[c] != lastQuat[c] {
			return false
		}
	}
	for c, m := range transBits {
		if flag&m == 0 && trans[c] != lastTrans[c] {
			return false
		}
	}
	return true
}
