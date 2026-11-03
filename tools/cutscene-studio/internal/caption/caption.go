// Package caption is a round-trip-exact codec for CAPTION chunks (ctype 4) -- the on-screen
// cutscene subtitles.
//
// One chunk carries the whole cutscene in every language; the runtime walks the records
// forward, skipping any whose lang is not the player's. Times are ticks (1/300 s), so a
// 60 fps demo frame N is tick 5N.
package caption

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// Magic is 'BCAP' as a C multi-char constant; on disk the LE bytes read "PACB".
const Magic uint32 = 0x42434150

const (
	TicksPerSecond = 300
	TicksPerFrame  = 5 // 60 fps demo
)

// GM_LANG_* (g_define.h:91-101). Shipped data only ever uses 1-5 and 7.
const (
	LangDefault  int16 = 0
	LangEnglish  int16 = 1
	LangFrench   int16 = 2
	LangGerman   int16 = 3
	LangItalian  int16 = 4
	LangSpanish  int16 = 5
	LangKorean   int16 = 6
	LangJapanese int16 = 7
)

// Record is one subtitle line: shown while Start <= tick < End.
type Record struct {
	Start, End int32  // ticks (1/300 s) from stream start
	Name       int32  // 24-bit StrCode speaker id, 0 = nobody
	Lang       int16  // GM_LANG_*
	Text       string // literal UTF-8; '|' is a line break
}

// Action fires the demo's script proc at Start with (stream_id, Name, Value).
type Action struct {
	Start, Name, Value, OptionLang int32
}

// Lang is Action's language filter; 0 (DEFAULT) fires for every language.
func (a *Action) Lang() int16 { return int16(uint32(a.OptionLang) >> 24) }

// Option is the value the script proc sees in OptionLang's low 24 bits.
func (a *Action) Option() int32 { return a.OptionLang & 0xFFFFFF }

// Table is the decoded body of a CAPTION chunk.
type Table struct {
	Records []Record
	Actions []Action
	Pad     []byte // trailing pad to 16, preserved verbatim
}

var le = binary.LittleEndian

func (r *Record) size() int { return 16 + len(r.Text) + 1 }

// Option is the value to put in the chunk's tag: (ofs_level << 16) | ofs_base.
// ofs_level is LEVEL data, which the MGS2 writer never emits -- always 0.
func (t *Table) Option() int32 { return int32(16 * len(t.Actions)) }

// Ticks converts a 60 fps demo frame to the caption clock.
func Ticks(frame int32) int32 { return frame * TicksPerFrame }

// Decode parses the body of a CAPTION chunk. The caption/action split is inferred (see
// recordAt); DecodeOption takes it from the chunk's tag instead.
func Decode(body []byte) (*Table, error) { return decode(body, -1) }

// DecodeOption is Decode for a caller holding the chunk's tag option, which states the
// action-area size outright.
func DecodeOption(body []byte, option int32) (*Table, error) {
	if lvl := option >> 16; lvl != 0 {
		return nil, fmt.Errorf("caption: ofs_level %d: LEVEL data is not supported", lvl)
	}
	return decode(body, int(option&0xFFFF))
}

func decode(body []byte, ofsBase int) (*Table, error) {
	if len(body) < 8 {
		return nil, fmt.Errorf("caption: body is %d bytes, want at least 8", len(body))
	}
	if m := le.Uint32(body); m != Magic {
		return nil, fmt.Errorf("caption: magic %#08x, want %#08x ('BCAP')", m, Magic)
	}
	size := int(int32(le.Uint32(body[4:])))
	fontTop := 8 + size
	if size < 0 || fontTop > len(body) {
		return nil, fmt.Errorf("caption: size %d overruns a %d-byte body", size, len(body))
	}

	capEnd := fontTop
	if ofsBase >= 0 {
		capEnd = fontTop - ofsBase
		if capEnd < 8 {
			return nil, fmt.Errorf("caption: %d bytes of actions overrun the records", ofsBase)
		}
	}

	t := &Table{Pad: body[fontTop:]}
	off := 8
	for off < capEnd {
		r, ok := recordAt(body, off, capEnd)
		if !ok {
			if ofsBase >= 0 {
				return nil, fmt.Errorf("caption: malformed record at +%d", off)
			}
			break // inferring: this is where the actions start
		}
		t.Records = append(t.Records, r)
		off += r.size()
	}

	if n := fontTop - off; n%16 != 0 {
		return nil, fmt.Errorf("caption: %d bytes before font_top are not whole 16-byte actions", n)
	}
	for ; off < fontTop; off += 16 {
		t.Actions = append(t.Actions, Action{
			Start:      int32(le.Uint32(body[off:])),
			Name:       int32(le.Uint32(body[off+4:])),
			Value:      int32(le.Uint32(body[off+8:])),
			OptionLang: int32(le.Uint32(body[off+12:])),
		})
	}
	return t, nil
}

// recordAt reads the caption record at off. Its checks double as the caption/action split
// detector for Decode: an action's option_plus_lang lands its language byte in the lang
// slot (0x0100..0x0700), which no real record can hold, so the first action always fails
// here. Measured: this reproduces the tag's ofs_base exactly on all 3,164 shipped chunks.
func recordAt(body []byte, off, limit int) (Record, bool) {
	if off+16 > limit {
		return Record{}, false
	}
	n := int(int16(le.Uint16(body[off+12:])))
	lang := int16(le.Uint16(body[off+14:]))
	if n < 17 || off+n > limit || lang < LangDefault || lang > LangJapanese {
		return Record{}, false
	}
	s := body[off+16 : off+n]
	if s[len(s)-1] != 0 || bytes.IndexByte(s[:len(s)-1], 0) >= 0 {
		return Record{}, false
	}
	return Record{
		Start: int32(le.Uint32(body[off:])),
		End:   int32(le.Uint32(body[off+4:])),
		Name:  int32(le.Uint32(body[off+8:])),
		Lang:  lang,
		Text:  string(s[:len(s)-1]),
	}, true
}

// Encode bakes the chunk body, zero-padded to 16. Pad is reused verbatim when it is still
// the right length, which is what keeps an unedited table byte-exact.
func (t *Table) Encode() ([]byte, error) {
	size := 16 * len(t.Actions)
	for i := range t.Records {
		r := &t.Records[i]
		if r.Lang < LangDefault || r.Lang > LangJapanese {
			return nil, fmt.Errorf("caption: record %d: lang %d is out of range 0..7", i, r.Lang)
		}
		if bytes.IndexByte([]byte(r.Text), 0) >= 0 {
			return nil, fmt.Errorf("caption: record %d: text contains a NUL", i)
		}
		if r.size() > math.MaxInt16 {
			return nil, fmt.Errorf("caption: record %d: %d bytes overflows the s16 len field", i, r.size())
		}
		size += r.size()
	}

	pad := -(8 + size) & 15
	out := make([]byte, 8, 8+size+pad)
	le.PutUint32(out, Magic)
	le.PutUint32(out[4:], uint32(size))

	var hdr [16]byte
	for i := range t.Records {
		r := &t.Records[i]
		le.PutUint32(hdr[0:], uint32(r.Start))
		le.PutUint32(hdr[4:], uint32(r.End))
		le.PutUint32(hdr[8:], uint32(r.Name))
		le.PutUint16(hdr[12:], uint16(r.size()))
		le.PutUint16(hdr[14:], uint16(r.Lang))
		out = append(out, hdr[:]...)
		out = append(out, r.Text...)
		out = append(out, 0)
	}
	for i := range t.Actions {
		a := &t.Actions[i]
		le.PutUint32(hdr[0:], uint32(a.Start))
		le.PutUint32(hdr[4:], uint32(a.Name))
		le.PutUint32(hdr[8:], uint32(a.Value))
		le.PutUint32(hdr[12:], uint32(a.OptionLang))
		out = append(out, hdr[:]...)
	}

	if len(t.Pad) == pad {
		return append(out, t.Pad...), nil
	}
	return append(out, make([]byte, pad)...), nil
}

// Validate checks the bake invariants the runtime depends on. The per-language scan is
// forward-only and never rewound, so within each language records must be ascending and
// non-overlapping; a record that breaks this shows late, or flashes for one frame.
func (t *Table) Validate() error {
	prev := map[int16]*Record{}
	for i := range t.Records {
		r := &t.Records[i]
		if r.End < r.Start {
			return fmt.Errorf("caption: record %d: end %d is before start %d", i, r.End, r.Start)
		}
		if p := prev[r.Lang]; p != nil && r.Start < p.End {
			return fmt.Errorf("caption: record %d (lang %d): start %d overlaps the previous record's end %d",
				i, r.Lang, r.Start, p.End)
		}
		prev[r.Lang] = r
	}
	for i := 1; i < len(t.Actions); i++ {
		if t.Actions[i].Start < t.Actions[i-1].Start {
			return fmt.Errorf("caption: action %d: start %d is out of order", i, t.Actions[i].Start)
		}
	}
	return nil
}
