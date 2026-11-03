// Package sdt is a round-trip-exact codec for MGS2 .sdt streams (cutscene "demo" files).
//
// The golden invariant: Decode(bytes) -> Encode() must reproduce the input byte-for-byte,
// for every shipped demo. Anything the editor does not understand is carried through
// verbatim rather than dropped, so an unedited track always bakes back to Konami's bytes.
package sdt

import (
	"encoding/binary"
	"fmt"
)

// Chunk types (the low half of a chunk's raw_type).
type ChunkType uint16

const (
	CPCM     ChunkType = 1
	CDemo    ChunkType = 2
	CLip     ChunkType = 3
	CCaption ChunkType = 4
	CSystem  ChunkType = 16
	CEnd     ChunkType = 0xF0
)

func (c ChunkType) String() string {
	switch c {
	case CPCM:
		return "PCM"
	case CDemo:
		return "DEMO"
	case CLip:
		return "LIP"
	case CCaption:
		return "CAPTION"
	case CSystem:
		return "SYSTEM"
	case CEnd:
		return "END"
	}
	return fmt.Sprintf("UNKNOWN(%d)", uint16(c))
}

// Chunk is one STREAM_TAG record: a 16-byte header plus payload.
type Chunk struct {
	RawType int32 // channel<<16 | ctype -- kept raw so round-trip is exact
	Time    int32
	Option  int32
	Payload []byte
}

func (c *Chunk) Type() ChunkType { return ChunkType(c.RawType & 0xFFFF) }
func (c *Chunk) Channel() uint16 { return uint16((c.RawType >> 16) & 0xFFFF) }

// Stream is a decoded .sdt file.
type Stream struct {
	Chunks []Chunk
	Tail   []byte // trailing bytes after the END chunk (padding), preserved verbatim
}

// Packet kinds (the low 16 bits of a demo packet's _packet_type).
type PacketKind uint16

const (
	PTermSeq    PacketKind = 0 // DEMO_PACKET_TERMINATE_SEQUENCE -- end of the whole demo
	PTermFrame  PacketKind = 1 // DEMO_PACKET_TERMINATE_FRAME    -- end of this frame
	PCommand    PacketKind = 2
	PFrame      PacketKind = 3 // camera
	PObject     PacketKind = 4
	PMotion     PacketKind = 5
	PEffect     PacketKind = 6
	PMotionPack PacketKind = 7
	PMST        PacketKind = 0x1000
)

// Packet type flags (the high bits of _packet_type).
const (
	FlagNoSkip        = int32(-0x80000000) // 0x80000000: frameskip may not drop this packet
	FlagEndianSwapped = int32(0x40000000)  // Bluepoint: payload already byte-swapped in place
	KindMask          = int32(0x0000FFFF)
)

// Packet is one DEMO_PACKET: a 16-byte header plus body.
type Packet struct {
	PType    int32 // kind | flags -- kept raw
	ID       int32
	Reserved int32
	Body     []byte
}

func (p *Packet) Kind() PacketKind { return PacketKind(p.PType & KindMask) }
func (p *Packet) NoSkip() bool     { return p.PType&FlagNoSkip != 0 }

func (k PacketKind) String() string {
	switch k {
	case PTermSeq:
		return "TERM_SEQ"
	case PTermFrame:
		return "TERM_FRAME"
	case PCommand:
		return "COMMAND"
	case PFrame:
		return "FRAME"
	case PObject:
		return "OBJECT"
	case PMotion:
		return "MOTION"
	case PEffect:
		return "EFFECT"
	case PMotionPack:
		return "MOTION_PACK"
	case PMST:
		return "MST"
	}
	return fmt.Sprintf("UNKNOWN(%d)", uint16(k))
}

// Frame is the decoded packet list of one DEMO chunk (one demo frame).
type Frame struct {
	Packets []Packet
	Tail    []byte // bytes after the terminator inside the chunk payload, preserved
}

var le = binary.LittleEndian

// Decode parses a .sdt byte slice into chunks.
func Decode(buf []byte) (*Stream, error) {
	s := &Stream{}
	off := 0
	for off+16 <= len(buf) {
		rawType := int32(le.Uint32(buf[off:]))
		size := int32(le.Uint32(buf[off+4:]))
		time := int32(le.Uint32(buf[off+8:]))
		option := int32(le.Uint32(buf[off+12:]))
		if size < 16 || off+int(size) > len(buf) {
			break // malformed or padding: everything from here is tail
		}
		s.Chunks = append(s.Chunks, Chunk{
			RawType: rawType,
			Time:    time,
			Option:  option,
			Payload: buf[off+16 : off+int(size)],
		})
		off += int(size)
		if ChunkType(rawType&0xFFFF) == CEnd {
			break
		}
	}
	s.Tail = buf[off:]
	return s, nil
}

// Encode serialises chunks back to bytes.
func (s *Stream) Encode() ([]byte, error) {
	out := make([]byte, 0, 1<<20)
	var hdr [16]byte
	for i := range s.Chunks {
		c := &s.Chunks[i]
		size := 16 + len(c.Payload)
		if size%16 != 0 {
			return nil, fmt.Errorf("chunk %d (%s): size %d is not 16-byte aligned", i, c.Type(), size)
		}
		le.PutUint32(hdr[0:], uint32(c.RawType))
		le.PutUint32(hdr[4:], uint32(size))
		le.PutUint32(hdr[8:], uint32(c.Time))
		le.PutUint32(hdr[12:], uint32(c.Option))
		out = append(out, hdr[:]...)
		out = append(out, c.Payload...)
	}
	return append(out, s.Tail...), nil
}

// DecodeFrame parses the packet list inside a DEMO chunk payload.
func DecodeFrame(payload []byte) (*Frame, error) {
	f := &Frame{}
	off := 0
	for off+16 <= len(payload) {
		ptype := int32(le.Uint32(payload[off:]))
		size := int32(le.Uint32(payload[off+4:]))
		id := int32(le.Uint32(payload[off+8:]))
		reserved := int32(le.Uint32(payload[off+12:]))
		if size < 16 || off+int(size) > len(payload) {
			break
		}
		f.Packets = append(f.Packets, Packet{
			PType:    ptype,
			ID:       id,
			Reserved: reserved,
			Body:     payload[off+16 : off+int(size)],
		})
		off += int(size)
		// Only TERM_FRAME ends the frame. TERM_SEQ sets sequence_end and nothing else, so
		// the final chunk carries BOTH -- stop at TERM_SEQ and you never see the last
		// TERM_FRAME. It round-trips either way, since the leftovers survive
		// as Tail, but the packet is then invisible to anything that edits the frame.
		if PacketKind(ptype&KindMask) == PTermFrame {
			break
		}
	}
	f.Tail = payload[off:]
	return f, nil
}

// EncodeFrame serialises a packet list back into a DEMO chunk payload.
func (f *Frame) Encode() ([]byte, error) {
	out := make([]byte, 0, 256)
	var hdr [16]byte
	for i := range f.Packets {
		p := &f.Packets[i]
		size := 16 + len(p.Body)
		if size%16 != 0 {
			return nil, fmt.Errorf("packet %d (%s): size %d is not 16-byte aligned", i, p.Kind(), size)
		}
		le.PutUint32(hdr[0:], uint32(p.PType))
		le.PutUint32(hdr[4:], uint32(size))
		le.PutUint32(hdr[8:], uint32(p.ID))
		le.PutUint32(hdr[12:], uint32(p.Reserved))
		out = append(out, hdr[:]...)
		out = append(out, p.Body...)
	}
	return append(out, f.Tail...), nil
}
