package catalog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// The DEMO_EFFECT body (the packet with its 16-byte header stripped):
//
//	0x00 s32    effect_id
//	0x04 s32    exec_type
//	0x08 s32[2] pad -- uninitialised retail garbage; 6.58M of 6.59M shipped packets are nonzero here
//	0x10 s32[]  data[] -- [life,0,0,0] then Data when exec 1, Data straight away when exec 0
const (
	offEffectID = 0x00
	offExecType = 0x04
	offData     = 0x10
)

var le = binary.LittleEndian

func i32(b []byte, off int) int32       { return int32(le.Uint32(b[off:])) }
func putI32(b []byte, off int, v int32) { le.PutUint32(b[off:], uint32(v)) }
func f32(b []byte, off int) float32     { return math.Float32frombits(le.Uint32(b[off:])) }
func putF32(b []byte, off int, v float32) {
	le.PutUint32(b[off:], math.Float32bits(v))
}

func roundup16(n int) int { return (n + 15) &^ 15 }

// PacketSize is the bake formula. Never 32 + sizeof(Data): the tail pads to 16 and exec 1
// carries a life preamble, and a short packet desyncs the packet walker.
func (e *Effect) PacketSize() int {
	return 32 + e.DataBaseOffset + roundup16(e.DataStructSize)
}

// NewBody returns a fresh body of the right size with effect_id/exec_type set, and life
// written when the effect is an actor. life is ignored for exec 0.
func (e *Effect) NewBody(life int32) []byte {
	body := make([]byte, e.PacketSize()-16)
	putI32(body, offEffectID, e.EffectID)
	putI32(body, offExecType, int32(e.ExecType))
	if e.ExecType == 1 {
		putI32(body, offData, life)
	}
	return body
}

// Life is data[0], the actor's lifetime in demo frames. Only exec 1 has one.
func (e *Effect) Life(body []byte) (int32, bool) {
	if e.ExecType != 1 || len(body) < offData+4 {
		return 0, false
	}
	return i32(body, offData), true
}

func (e *Effect) SetLife(body []byte, life int32) error {
	if e.ExecType != 1 {
		return fmt.Errorf("effect 0x%x (%s): exec 0 has no life", e.ID, e.CName)
	}
	if len(body) < offData+4 {
		return fmt.Errorf("effect 0x%x (%s): body is %d bytes, needs %d", e.ID, e.CName, len(body), offData+4)
	}
	putI32(body, offData, life)
	return nil
}

// Components is how many scalars a param holds: 1 for int/float, 4 for the vectors
// (the w/pad lane included -- it is a real field and shipped packets do use it).
func (p *Param) Components() int {
	switch p.CType {
	case TSVector, TIVector, TFVector:
		return 4
	case TChar:
		return 0
	}
	return 1
}

func (p *Param) componentSize() int {
	if p.CType == TSVector {
		return 2
	}
	return 4
}

// offsetOf resolves a component to its byte offset in the body, taking the exec-type base
// with it -- the 16-byte miss that mis-reads most retail packets happens here or nowhere.
func (e *Effect) offsetOf(body []byte, p *Param, comp int) (int, error) {
	if p == nil {
		return 0, fmt.Errorf("effect 0x%x (%s): nil param", e.ID, e.CName)
	}
	n := p.Components()
	if n == 0 {
		return 0, fmt.Errorf("effect 0x%x (%s): %s is %s[%d], use GetString/SetString", e.ID, e.CName, p.Name, p.CType, p.ArrayLen)
	}
	if comp < 0 || comp >= n {
		return 0, fmt.Errorf("effect 0x%x (%s): %s (%s) has %d component(s), asked for %d", e.ID, e.CName, p.Name, p.CType, n, comp)
	}
	off := offData + e.DataBaseOffset + p.ByteOffset + comp*p.componentSize()
	if off+p.componentSize() > len(body) {
		return 0, fmt.Errorf("effect 0x%x (%s): %s component %d ends at %d, body is %d bytes", e.ID, e.CName, p.Name, comp, off+p.componentSize(), len(body))
	}
	return off, nil
}

// GetFloat reads a component as a float. Integer-typed params convert, so a property panel
// can drive everything through one path without ever writing float bits into an int field.
func (e *Effect) GetFloat(body []byte, p *Param, comp int) (float64, error) {
	off, err := e.offsetOf(body, p, comp)
	if err != nil {
		return 0, err
	}
	switch p.CType {
	case TFloat, TFVector:
		return float64(f32(body, off)), nil
	case TSVector:
		return float64(int16(le.Uint16(body[off:]))), nil
	case TInt, TIVector:
		return float64(i32(body, off)), nil
	}
	return 0, fmt.Errorf("effect 0x%x (%s): %s is %s, not numeric", e.ID, e.CName, p.Name, p.CType)
}

func (e *Effect) SetFloat(body []byte, p *Param, comp int, v float64) error {
	off, err := e.offsetOf(body, p, comp)
	if err != nil {
		return err
	}
	switch p.CType {
	case TFloat, TFVector:
		putF32(body, off, float32(v))
		return nil
	case TSVector, TInt, TIVector:
		return e.SetInt(body, p, comp, int32(math.Round(v)))
	}
	return fmt.Errorf("effect 0x%x (%s): %s is %s, not numeric", e.ID, e.CName, p.Name, p.CType)
}

func (e *Effect) GetInt(body []byte, p *Param, comp int) (int32, error) {
	off, err := e.offsetOf(body, p, comp)
	if err != nil {
		return 0, err
	}
	switch p.CType {
	case TInt, TIVector:
		return i32(body, off), nil
	case TSVector:
		return int32(int16(le.Uint16(body[off:]))), nil
	case TFloat, TFVector:
		return int32(math.Round(float64(f32(body, off)))), nil
	}
	return 0, fmt.Errorf("effect 0x%x (%s): %s is %s, not numeric", e.ID, e.CName, p.Name, p.CType)
}

func (e *Effect) SetInt(body []byte, p *Param, comp int, v int32) error {
	off, err := e.offsetOf(body, p, comp)
	if err != nil {
		return err
	}
	switch p.CType {
	case TInt, TIVector:
		putI32(body, off, v)
		return nil
	case TSVector:
		if v < math.MinInt16 || v > math.MaxInt16 {
			return fmt.Errorf("effect 0x%x (%s): %s is SVECTOR (s16), %d does not fit", e.ID, e.CName, p.Name, v)
		}
		le.PutUint16(body[off:], uint16(int16(v)))
		return nil
	case TFloat, TFVector:
		putF32(body, off, float32(v))
		return nil
	}
	return fmt.Errorf("effect 0x%x (%s): %s is %s, not numeric", e.ID, e.CName, p.Name, p.CType)
}

// GetString reads a char[128] param (MaxInputString) up to its first NUL.
func (e *Effect) GetString(body []byte, p *Param) (string, error) {
	off, err := e.stringOffset(body, p)
	if err != nil {
		return "", err
	}
	s := body[off : off+p.ArrayLen]
	if i := bytes.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return string(s), nil
}

// SetString writes a char[128] param, NUL-padded to the full array (so no stale tail leaks).
func (e *Effect) SetString(body []byte, p *Param, s string) error {
	off, err := e.stringOffset(body, p)
	if err != nil {
		return err
	}
	if len(s) >= p.ArrayLen {
		return fmt.Errorf("effect 0x%x (%s): %s holds %d chars plus a NUL, got %d", e.ID, e.CName, p.Name, p.ArrayLen-1, len(s))
	}
	field := body[off : off+p.ArrayLen]
	for i := range field {
		field[i] = 0
	}
	copy(field, s)
	return nil
}

func (e *Effect) stringOffset(body []byte, p *Param) (int, error) {
	if p == nil || p.CType != TChar || p.ArrayLen <= 0 {
		return 0, fmt.Errorf("effect 0x%x (%s): not a string param", e.ID, e.CName)
	}
	off := offData + e.DataBaseOffset + p.ByteOffset
	if off+p.ArrayLen > len(body) {
		return 0, fmt.Errorf("effect 0x%x (%s): %s ends at %d, body is %d bytes", e.ID, e.CName, p.Name, off+p.ArrayLen, len(body))
	}
	return off, nil
}
