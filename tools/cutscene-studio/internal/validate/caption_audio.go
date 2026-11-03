package validate

import (
	"bytes"
	"fmt"

	"mgs2-restoration/cutscene-studio/internal/sdt"
)

// captions runs A1-A6. The whole subtitle table -- every language -- ships in one CAPTION
// chunk at the head of the file.
func (c *check) captions(s *sdt.Stream) {
	var idx []int
	sysAt := -1
	for i := range s.Chunks {
		switch {
		case s.Chunks[i].Type() == sdt.CCaption:
			idx = append(idx, i)
		case s.Chunks[i].Type() == sdt.CSystem && s.Chunks[i].Option == rawCaption && sysAt < 0:
			sysAt = i
		}
	}
	if len(idx) == 0 {
		return
	}
	// A1: one chunk, time 0, after its SYSTEM announcement.
	if len(idx) > 1 {
		c.add("A1", Fatal, idx[1], -1, -1, fmt.Sprintf("%d CAPTION chunks; every shipped stream that has subtitles has exactly one", len(idx)),
			"merge every language's records into the single time=0 CAPTION chunk")
	}
	ci := idx[0]
	ch := &s.Chunks[ci]
	if ch.Time != 0 {
		c.add("A1", Fatal, ci, -1, -1, fmt.Sprintf("CAPTION chunk time=%d, must be 0 (it is read once, at the head)", ch.Time), "set time=0")
	}
	if sysAt >= 0 && sysAt > ci {
		c.add("A1", Fatal, ci, -1, -1, "the CAPTION chunk precedes the SYSTEM chunk that announces it", "SYSTEM(option=4) first, then the CAPTION chunk")
	}

	body := ch.Payload
	if len(body) < 8 {
		c.add("A2", Fatal, ci, -1, -1, fmt.Sprintf("CAPTION body is %d bytes, too small for the BCAP header", len(body)), "body = 'PACB' + size + records")
		return
	}
	// A2: the magic on disk is the little-endian spelling of 'BCAP'.
	if !bytes.Equal(body[:4], []byte("PACB")) {
		c.add("A2", Fatal, ci, -1, -1, fmt.Sprintf("CAPTION magic is % x, must be 50 41 43 42 (\"PACB\")", body[:4]), "write the 'BCAP' fourcc little-endian")
		return
	}
	size := int(i32(body, 4))
	ofsBase := int(ch.Option & 0xFFFF)      // total ACTION bytes
	ofsLevel := int(ch.Option>>16) & 0xFFFF // total LEVEL bytes
	// A5: no shipped MGS2 stream has a level table.
	if ofsLevel != 0 {
		c.add("A5", Fatal, ci, -1, -1, fmt.Sprintf("caption option has ofs_level=%d; it is 0 in 100%% of shipped data", ofsLevel), "bake option = ofs_base only (usually 0)")
	}
	if size < 0 || 8+size > len(body) {
		c.add("A2", Fatal, ci, -1, -1, fmt.Sprintf("caption size field = %d, but the body is only %d bytes", size, len(body)),
			"size counts the bytes from 0x08 to font_top: all caption records plus all action records")
		return
	}
	recEnd := 8 + size - ofsBase
	if recEnd < 8 {
		c.add("A2", Fatal, ci, -1, -1, fmt.Sprintf("ofs_base=%d exceeds the %d-byte record+action region", ofsBase, size), "option's low half is the ACTION byte count")
		return
	}

	type span struct{ start, end int32 }
	lastByLang := map[int16]span{}
	total, n := 0, 0
	for off := 8; off < recEnd; {
		if off+16 > recEnd {
			c.add("A3", Fatal, ci, -1, -1, fmt.Sprintf("caption record %d starts at %d with only %d bytes left", n, off, recEnd-off), "records are 16 bytes + a NUL-terminated UTF-8 string")
			return
		}
		start, end := i32(body, off), i32(body, off+4)
		ln := int(int16(le.Uint16(body[off+12:])))
		lang := int16(le.Uint16(body[off+14:]))
		if ln < 17 || off+ln > recEnd {
			c.add("A3", Fatal, ci, -1, -1, fmt.Sprintf("caption record %d has len=%d (must be 16 + strlen + 1, and fit in the chunk)", n, ln), "len is the whole record, header included")
			return
		}
		// A3: len == 16 + strlen(utf8) + 1. Get it wrong and the walk lands mid-record.
		str := body[off+16 : off+ln]
		if z := bytes.IndexByte(str, 0); z != len(str)-1 {
			c.add("A3", Fatal, ci, -1, -1, fmt.Sprintf("caption record %d (lang %d): len=%d but the string's NUL is at %d, so len != 16 + strlen + 1", n, lang, ln, z),
				"len = 16 + len(utf8 bytes) + 1; there is no padding between records")
		}
		// A4: the per-language scan is forward-only and never rewound, and it tracks one
		// caption at a time. Out of order or overlapping = a line that flashes for a frame.
		if prev, ok := lastByLang[lang]; ok && start < prev.end {
			c.add("A4", Fatal, ci, -1, -1, fmt.Sprintf("caption record %d (lang %d) starts at tick %d, before the previous record of that language ends at %d", n, lang, start, prev.end),
				"within a language, records must be ascending and non-overlapping in file order")
		}
		if end < start {
			c.add("A4", Fatal, ci, -1, -1, fmt.Sprintf("caption record %d (lang %d) ends at %d, before it starts at %d", n, lang, end, start), "end_count >= start_count, in 1/300 s ticks")
		}
		lastByLang[lang] = span{start, end}
		total += ln
		off += ln
		n++
	}

	// A6: actions are read in order against a rising tick.
	nAct := ofsBase / 16
	if ofsBase%16 != 0 {
		c.add("A6", Fatal, ci, -1, -1, fmt.Sprintf("ofs_base=%d is not a multiple of the 16-byte action record", ofsBase), "action records are exactly 16 bytes")
	}
	prevStart := int32(-1 << 31)
	for a := 0; a < nAct; a++ {
		off := recEnd + a*16
		if off+16 > len(body) {
			break
		}
		start := i32(body, off)
		if start < prevStart {
			c.add("A6", Fatal, ci, -1, -1, fmt.Sprintf("action record %d starts at tick %d, before action %d at %d", a, start, a-1, prevStart), "sort action records ascending by start_count")
		}
		prevStart = start
	}

	// A2: the size field must account for exactly the records plus the actions.
	if want := total + 16*nAct; size != want {
		c.add("A2", Fatal, ci, -1, -1, fmt.Sprintf("caption size field = %d, but the records sum to %d and %d actions add %d (= %d)", size, total, nAct, 16*nAct, want),
			"size = sum(record len) + 16*n_actions")
	}
}

// audio runs A7-A12 over the AMWX header chunk and the xWMA data chunks.
func (c *check) audio(s *sdt.Stream) {
	hdr, hdrIdx := -1, -1
	var dataBytes int64
	var lastDemo int32
	for i := range s.Chunks {
		ch := &s.Chunks[i]
		switch ch.Type() {
		case sdt.CPCM:
			if hdr < 0 {
				hdr, hdrIdx = i, i
			} else {
				dataBytes += int64(ch.Option)
			}
		case sdt.CDemo:
			lastDemo = ch.Time
		}
	}
	if hdr < 0 {
		return
	}
	body := s.Chunks[hdrIdx].Payload
	if len(body) < 0x36 {
		c.add("A8", Fatal, hdrIdx, -1, -1, fmt.Sprintf("AMWX header body is %d bytes, the fixed part alone is 0x36", len(body)),
			"header = AMWX fields + an 18-byte WAVEFORMATEX + dpdsCount + dpds[]")
		return
	}
	if !bytes.Equal(body[:4], []byte("AMWX")) {
		c.add("A7", Fatal, hdrIdx, -1, -1, fmt.Sprintf("first PCM chunk starts with % x, not \"AMWX\": it is not an xWMA header", body[:4]),
			"the first PCM chunk is the header (time=0, option=0)")
		return
	}
	channels := int(i32(body, 0x08))
	freq := int(i32(body, 0x0C))
	sampleSize := int(i32(body, 0x10))
	blockAlign := int(i32(body, 0x18))
	dpdsCount := int(i32(body, 0x32))

	if channels <= 0 || freq <= 0 || blockAlign <= 0 {
		c.add("A7", Fatal, hdrIdx, -1, -1, fmt.Sprintf("AMWX header has channels=%d freq=%d blockAlign=%d", channels, freq, blockAlign), "fill the header from the encoder's output")
		return
	}
	// A7: XAudio2 sizes the buffer from these; a mismatch is the truncated-stream AV.
	if want := dpdsCount * blockAlign; sampleSize != want {
		c.add("A7", Fatal, hdrIdx, -1, -1, fmt.Sprintf("mSampleSize=%d but dpdsCount*mBlockAlign = %d*%d = %d", sampleSize, dpdsCount, blockAlign, want),
			"mSampleSize is the total compressed byte count and must divide exactly by mBlockAlign")
	}
	// A8: the body length is fully determined by the packet count.
	if want := (0x36 + 4*dpdsCount + 15) &^ 15; len(body) != want {
		c.add("A8", Fatal, hdrIdx, -1, -1, fmt.Sprintf("AMWX body is %d bytes, align16(0x36 + 4*%d) = %d", len(body), dpdsCount, want),
			"size the header chunk as 16 + align16(0x36 + 4*dpdsCount)")
	}
	if cb := int(le.Uint16(body[0x30:])); cb != 0 {
		c.add("A8", Fatal, hdrIdx, -1, -1, fmt.Sprintf("WAVEFORMATEX.cbSize=%d, must be 0", cb), "write cbSize = 0")
	}
	// A9: XAUDIO2_BUFFER_WMA needs the cumulative decoded-byte table. It is non-decreasing:
	// 14 shipped files end with a zero-decode flush packet.
	prev, last := int32(0), int32(0)
	for i := 0; i < dpdsCount; i++ {
		off := 0x36 + 4*i
		if off+4 > len(body) {
			break
		}
		v := i32(body, off)
		if v < prev {
			c.add("A9", Fatal, hdrIdx, -1, -1, fmt.Sprintf("dpds[%d]=%d is less than dpds[%d]=%d", i, v, i-1, prev), "dpds is cumulative decoded PCM bytes: non-decreasing")
		}
		if int(v)%(2*channels) != 0 {
			c.add("A9", Fatal, hdrIdx, -1, -1, fmt.Sprintf("dpds[%d]=%d is not a multiple of 2*channels (%d)", i, v, 2*channels), "decoded bytes are 16-bit samples per channel")
		}
		prev, last = v, v
	}
	// A11: the header must describe the data that is actually there. A short stream against
	// a long header is the AV at exe+0x1e0fd0.
	if dataBytes != int64(sampleSize) {
		c.add("A11", Fatal, hdrIdx, -1, -1, fmt.Sprintf("the PCM data chunks carry %d bytes but mSampleSize says %d", dataBytes, sampleSize),
			"after any audio surgery, rewrite mSampleSize/dpds to match the packets you shipped")
	}
	// A12: past the last audio packet the demo silently falls back to the vsync wall clock.
	if dpdsCount > 0 {
		audioTicks := int32(int64(last) * 300 / int64(2*channels*freq))
		if audioTicks < lastDemo {
			c.add("A12", Warn, hdrIdx, -1, -1, fmt.Sprintf("audio runs %.2f s, the demo runs %.2f s: the last %.2f s play on the wall clock, which does not compensate for pause",
				float64(audioTicks)/300, float64(lastDemo)/300, float64(lastDemo-audioTicks)/300),
				"pad the xWMA with silence (Recipe A); shipped demos overrun by at most 1.65 s")
		}
	}
}
