// Package audio is the AMWX/xWMA header codec for a .sdt's audio track, plus the toolkit
// for reasoning about demo length.
//
// Worth knowing first: the audio does NOT cap demo
// length. When the track runs out the sound driver's own Die() zeroes sd_duration, and the
// demo clock falls back to a vsync wall clock. 24 shipped demos have no audio at
// all. What DOES bite is ripping the audio out of a slot whose stream-table tagflag reserves
// a sound channel: the runtime prints "NO SOUND DATA!!" and calls HANGUP(). So StripAudio
// refuses unless you show it a tagflag with no sound bit.
//
// Round-trip rule, same as package sdt: Decode -> Encode must reproduce the input bytes.
// That means preserving the trailing pad, which is uninitialised heap garbage in 52% of
// shipped files.
package audio

import (
	"encoding/binary"
	"fmt"
)

var le = binary.LittleEndian

// RawTypeXAudio is STREAM_TYPE_XAUDIO: the raw_type of every PCM chunk and of the SYSTEM
// record that starts the audio driver. Never bare 1.
const RawTypeXAudio int32 = 0x00040001

// The MSVC literal 'XWMA'; on disk the bytes read "AMWX".
const cookieAMWX uint32 = 0x58574D41

// hdrFixed is everything before the dpds table. It is 6 mod 16, which is why the trailing
// pad is always 2, 6, 10 or 14 bytes.
const hdrFixed = 0x36

// AMWX is the body of the first PCM chunk: BP_AudioStreamHeader, a WAVEFORMATEX, and the
// dpds table that XAUDIO2_BUFFER_WMA requires (without it xWMA playback simply fails).
//
//	0x00 u32    mCookie "AMWX"
//	0x04 s32    mFormat             0x161 WMAUDIO2 / 0x162 WMAUDIO3
//	0x08 s32    mChannels
//	0x0C s32    mFrequency
//	0x10 s32    mSampleSize         total COMPRESSED bytes == packetCount * mBlockAlign
//	0x14 s32    mAvgBytesPerSecond
//	0x18 s32    mBlockAlign         xWMA packet size
//	0x1C s32    mUnused             0
//	0x20 [18]B  WAVEFORMATEX
//	0x32 u32    dpdsCount
//	0x36 u32[N] dpds                cumulative DECODED pcm bytes after each packet
//	           pad to a 16-byte multiple
type AMWX struct {
	Format         int32
	Channels       int32
	Frequency      int32
	SampleSize     int32
	AvgBytesPerSec int32
	BlockAlign     int32
	WFX            [18]byte
	DPDS           []uint32
	Pad            []byte
}

func align16(n int) int { return (n + 15) &^ 15 }

// DecodeHeader parses the body of a stream's FIRST PCM chunk.
func DecodeHeader(body []byte) (*AMWX, error) {
	if len(body) < hdrFixed {
		return nil, fmt.Errorf("amwx: header body is %d bytes, want at least %d", len(body), hdrFixed)
	}
	if got := le.Uint32(body); got != cookieAMWX {
		return nil, fmt.Errorf("amwx: cookie is %#08x, want %#08x (\"AMWX\")", got, cookieAMWX)
	}
	a := &AMWX{
		Format:         int32(le.Uint32(body[0x04:])),
		Channels:       int32(le.Uint32(body[0x08:])),
		Frequency:      int32(le.Uint32(body[0x0C:])),
		SampleSize:     int32(le.Uint32(body[0x10:])),
		AvgBytesPerSec: int32(le.Uint32(body[0x14:])),
		BlockAlign:     int32(le.Uint32(body[0x18:])),
	}
	copy(a.WFX[:], body[0x20:0x32])

	n := int(le.Uint32(body[0x32:]))
	if n > (len(body)-hdrFixed)/4 {
		return nil, fmt.Errorf("amwx: dpdsCount %d overruns a %d-byte body", n, len(body))
	}
	a.DPDS = make([]uint32, n)
	for i := range a.DPDS {
		a.DPDS[i] = le.Uint32(body[hdrFixed+4*i:])
	}
	a.Pad = append([]byte(nil), body[hdrFixed+4*n:]...)
	return a, nil
}

// Encode serialises the header back to a chunk body of align16(0x36 + 4*len(DPDS)) bytes.
// An empty Pad is zero-filled -- fine for newly authored audio, since the runtime never
// reads past dpds[N-1]. To reproduce a shipped file you must hand back the pad you decoded.
func (a *AMWX) Encode() ([]byte, error) {
	n := len(a.DPDS)
	end := hdrFixed + 4*n
	padLen := align16(end) - end
	if len(a.Pad) != 0 && len(a.Pad) != padLen {
		return nil, fmt.Errorf("amwx: pad is %d bytes, want %d for %d packets", len(a.Pad), padLen, n)
	}

	out := make([]byte, align16(end))
	le.PutUint32(out[0x00:], cookieAMWX)
	le.PutUint32(out[0x04:], uint32(a.Format))
	le.PutUint32(out[0x08:], uint32(a.Channels))
	le.PutUint32(out[0x0C:], uint32(a.Frequency))
	le.PutUint32(out[0x10:], uint32(a.SampleSize))
	le.PutUint32(out[0x14:], uint32(a.AvgBytesPerSec))
	le.PutUint32(out[0x18:], uint32(a.BlockAlign))
	copy(out[0x20:], a.WFX[:])
	le.PutUint32(out[0x32:], uint32(n))
	for i, v := range a.DPDS {
		le.PutUint32(out[hdrFixed+4*i:], v)
	}
	copy(out[end:], a.Pad)
	return out, nil
}

// PCMFrame is the size of one decoded sample frame: 16-bit interleaved, so 2 bytes a channel.
func (a *AMWX) PCMFrame() int32 { return 2 * a.Channels }

// DurationSec is the decoded track length.
func (a *AMWX) DurationSec() float64 {
	if len(a.DPDS) == 0 || a.Channels <= 0 || a.Frequency <= 0 {
		return 0
	}
	return float64(a.DPDS[len(a.DPDS)-1]) / float64(a.PCMFrame()*a.Frequency)
}

// Frames is the track length in 60 Hz demo frames, floored -- so a demo of this many frames
// is entirely covered by the audio.
func (a *AMWX) Frames() int {
	if len(a.DPDS) == 0 || a.Channels <= 0 || a.Frequency <= 0 {
		return 0
	}
	den := int64(a.PCMFrame()) * int64(a.Frequency)
	return int(int64(a.DPDS[len(a.DPDS)-1]) * 60 / den)
}

// CBSize is WAVEFORMATEX.cbSize. It must be 0 or dpdsCount shifts off 0x32.
func (a *AMWX) CBSize() uint16 { return le.Uint16(a.WFX[16:]) }

// Validate checks the invariants the runtime and XAudio2 actually depend on.
//
// Two things it deliberately does NOT check, because either would reject shipped data:
// strictly increasing dpds (14 files end with a zero-decode flush packet) and zero padding
// (52% of files carry heap garbage there).
func (a *AMWX) Validate() error {
	if a.Channels <= 0 || a.Frequency <= 0 || a.BlockAlign <= 0 {
		return fmt.Errorf("amwx: bad format (channels %d, frequency %d, blockAlign %d)",
			a.Channels, a.Frequency, a.BlockAlign)
	}
	n := len(a.DPDS)
	if int64(a.SampleSize) != int64(n)*int64(a.BlockAlign) { // A7 -- sdstream.c:732 divides on this
		return fmt.Errorf("amwx: mSampleSize %d != dpdsCount %d * blockAlign %d",
			a.SampleSize, n, a.BlockAlign)
	}
	if cb := a.CBSize(); cb != 0 { // A8
		return fmt.Errorf("amwx: wfx.cbSize is %d, must be 0", cb)
	}
	end := hdrFixed + 4*n
	if padLen := align16(end) - end; len(a.Pad) != 0 && len(a.Pad) != padLen { // A8
		return fmt.Errorf("amwx: pad is %d bytes, want %d for %d packets", len(a.Pad), padLen, n)
	}
	frame := uint32(a.PCMFrame()) // A9
	var prev uint32
	for i, v := range a.DPDS {
		if v < prev {
			return fmt.Errorf("amwx: dpds[%d] = %d goes backwards from %d", i, v, prev)
		}
		if v%frame != 0 {
			return fmt.Errorf("amwx: dpds[%d] = %d is not a multiple of the %d-byte pcm frame", i, v, frame)
		}
		prev = v
	}
	return nil
}

// NewHeader builds an AMWX for freshly encoded xWMA (Recipe A). dpds comes out of
// xWMAEncode's RIFF "dpds" chunk -- you cannot derive it from the compressed bytes, and
// ffmpeg's wmav2 encoder does not emit one.
func NewHeader(format, channels, frequency, avgBytesPerSec, blockAlign int32, dpds []uint32) (*AMWX, error) {
	a := &AMWX{
		Format:         format,
		Channels:       channels,
		Frequency:      frequency,
		SampleSize:     int32(len(dpds)) * blockAlign,
		AvgBytesPerSec: avgBytesPerSec,
		BlockAlign:     blockAlign,
		DPDS:           dpds,
	}
	le.PutUint16(a.WFX[0:], uint16(format))
	le.PutUint16(a.WFX[2:], uint16(channels))
	le.PutUint32(a.WFX[4:], uint32(frequency))
	le.PutUint32(a.WFX[8:], uint32(avgBytesPerSec))
	le.PutUint16(a.WFX[12:], uint16(blockAlign))
	le.PutUint16(a.WFX[14:], 16)
	le.PutUint16(a.WFX[16:], 0)
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return a, nil
}
