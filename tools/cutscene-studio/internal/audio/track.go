package audio

import (
	"errors"
	"fmt"

	"mgs2-restoration/cutscene-studio/internal/sdt"
)

// ErrNoAudio means the stream ships silent. 24 shipped demos do.
var ErrNoAudio = errors.New("audio: stream has no PCM chunks")

// HasAudio reports whether the stream carries an audio track at all.
func HasAudio(s *sdt.Stream) bool {
	for i := range s.Chunks {
		if s.Chunks[i].Type() == sdt.CPCM {
			return true
		}
	}
	return false
}

// Track returns the AMWX header and the xWMA data packets in order, each trimmed to its real
// length: the chunk body is zero-padded to 16, and the tag's option carries the true size.
func Track(s *sdt.Stream) (*AMWX, [][]byte, error) {
	var a *AMWX
	var packets [][]byte
	for i := range s.Chunks {
		c := &s.Chunks[i]
		if c.Type() != sdt.CPCM {
			continue
		}
		if a == nil {
			h, err := DecodeHeader(c.Payload)
			if err != nil {
				return nil, nil, fmt.Errorf("chunk %d: %w", i, err)
			}
			a = h
			continue
		}
		n := int(c.Option)
		if n <= 0 || n > len(c.Payload) {
			n = len(c.Payload)
		}
		packets = append(packets, c.Payload[:n])
	}
	if a == nil {
		return nil, nil, ErrNoAudio
	}
	return a, packets, nil
}

// DurationFrames is the audio length in 60 Hz demo frames; 0 for a silent stream.
func DurationFrames(s *sdt.Stream) (int, error) {
	if !HasAudio(s) {
		return 0, nil
	}
	a, _, err := Track(s)
	if err != nil {
		return 0, err
	}
	return a.Frames(), nil
}

// DemoFrames counts the DEMO chunks: one per 60 Hz frame, always.
func DemoFrames(s *sdt.Stream) int {
	n := 0
	for i := range s.Chunks {
		if s.Chunks[i].Type() == sdt.CDemo {
			n++
		}
	}
	return n
}

// MaxShippedOverhang is the largest overrun Konami shipped, measured: p001_01_p02 runs 2580
// demo frames over 2480 frames of audio. 16 shipped demos outrun their track at all.
const MaxShippedOverhang = 100

// Length is a stream's audio-vs-demo budget, in demo frames.
type Length struct {
	Silent      bool
	AudioFrames int
	DemoFrames  int
	Overhang    int // demo frames past the end of the audio; negative means audio to spare
}

// Measure works out how the demo track and the audio track line up.
func Measure(s *sdt.Stream) (Length, error) {
	l := Length{Silent: !HasAudio(s), DemoFrames: DemoFrames(s)}
	if l.Silent {
		return l, nil
	}
	n, err := DurationFrames(s)
	if err != nil {
		return l, err
	}
	l.AudioFrames = n
	l.Overhang = l.DemoFrames - n
	return l, nil
}

// Uncharted reports a demo that outruns its audio by more than anything Konami shipped.
// It will still play -- the wall clock takes over the moment the track ends -- but nobody
// has tested it at scale, and demod's fallback does not compensate for pause, so pausing in
// the silent tail fast-forwards the demo on resume.
func (l Length) Uncharted() bool { return !l.Silent && l.Overhang > MaxShippedOverhang }

// Sound bits of a stream-table tagflag (strctrl.h). Any of them and the runtime reserves a
// sound channel for the slot; a stream with no PCM data then reaches
// printf("NO SOUND DATA!!"); HANGUP() -- a hard hang, seen at exe+0x825e6.
const (
	TagFlagSoundAC3   uint32 = 1 << 28
	TagFlagSoundVAG   uint32 = 1 << 29
	TagFlagSound8Bit  uint32 = 1 << 30
	TagFlagSoundExist uint32 = TagFlagSoundAC3 | TagFlagSoundVAG | TagFlagSound8Bit
)

// ErrSoundReserved is why StripAudio said no.
var ErrSoundReserved = errors.New("audio: the slot reserves a sound channel; a silent stream would hang the game")

// StripAudio is Recipe B: drop every PCM chunk and the SYSTEM record that starts the audio
// driver, leaving the demo on the wall clock. 24 shipped demos ship exactly like this.
//
// tagflag is the slot's stream-table flag word, and it lives in the stage .gcx /
// bp_streams.txt -- NOT in the .sdt. There is no way to recover it from the file, so the
// caller has to go and read it. If it reserves a sound channel this refuses, because the
// game would hang rather than play silently.
func StripAudio(s *sdt.Stream, tagflag uint32) error {
	if tagflag&TagFlagSoundExist != 0 {
		return fmt.Errorf("%w (tagflag %#08x)", ErrSoundReserved, tagflag)
	}
	kept := s.Chunks[:0]
	for _, c := range s.Chunks {
		if c.Type() == sdt.CPCM || (c.Type() == sdt.CSystem && c.Option == RawTypeXAudio) {
			continue
		}
		kept = append(kept, c)
	}
	s.Chunks = kept
	return nil
}

// HeaderChunk wraps an AMWX as the stream's first PCM chunk. option is a literal 0 -- the
// size/option invariant that data chunks obey does not apply here.
func HeaderChunk(a *AMWX) (sdt.Chunk, error) {
	body, err := a.Encode()
	if err != nil {
		return sdt.Chunk{}, err
	}
	return sdt.Chunk{RawType: RawTypeXAudio, Time: 0, Option: 0, Payload: body}, nil
}

// DataChunk wraps one xWMA packet. option is the true payload size and the body is
// zero-padded to 16, so size == 16 + ((option+15) &^ 15).
func DataChunk(packet []byte, tick int32) sdt.Chunk {
	body := make([]byte, align16(len(packet)))
	copy(body, packet)
	return sdt.Chunk{RawType: RawTypeXAudio, Time: tick, Option: int32(len(packet)), Payload: body}
}

// SystemChunk is the driver-start record the audio track needs. Without it the ring buffer
// freezes at the first orphan PCM chunk and the stream never ends.
func SystemChunk() sdt.Chunk {
	return sdt.Chunk{RawType: int32(sdt.CSystem), Time: 0, Option: RawTypeXAudio}
}

// Packetize splits an .xwm data chunk into BlockAlign-sized packets.
func Packetize(a *AMWX, data []byte) ([][]byte, error) {
	if a.BlockAlign <= 0 {
		return nil, fmt.Errorf("audio: blockAlign is %d", a.BlockAlign)
	}
	if len(data)%int(a.BlockAlign) != 0 {
		return nil, fmt.Errorf("audio: %d bytes of xwma data is not a multiple of blockAlign %d",
			len(data), a.BlockAlign)
	}
	var out [][]byte
	for off := 0; off < len(data); off += int(a.BlockAlign) {
		out = append(out, data[off:off+int(a.BlockAlign)])
	}
	return out, nil
}

// leadTicks is CStreamWriter_XAudio's kMaxAudioLeadTicks: half a second at 300 ticks/s.
const leadTicks = 150

// Schedule returns the tick to emit each data packet at, reproducing the writer's audio
// lead: packet i goes out at the first demo tick t (a multiple of 5) at which the packet's
// decoded start time is still under (t + 150)/300 seconds. Leading is safe -- the driver
// back-pressures and pushes packets back -- but lagging starves the ring buffer.
func (a *AMWX) Schedule() []int32 {
	den := int64(a.PCMFrame()) * int64(a.Frequency)
	ticks := make([]int32, len(a.DPDS))
	if den <= 0 {
		return ticks
	}
	for i := range a.DPDS {
		var startBytes int64
		if i > 0 {
			startBytes = int64(a.DPDS[i-1])
		}
		// smallest t with t > startBytes*300/den - 150
		t := floorDiv(startBytes*300-leadTicks*den, den) + 1
		if t < 0 {
			t = 0
		}
		ticks[i] = int32((t + 4) / 5 * 5)
	}
	return ticks
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}
