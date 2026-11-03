package validate

import (
	"fmt"

	"mgs2-restoration/cutscene-studio/internal/sdt"
)

const (
	rawDemo    = int32(0x00000002)
	rawXAudio  = int32(0x00040001)
	rawCaption = int32(0x00000004)
	audioLead  = 300 // ticks a chunk may sit ahead of its neighbours before the ring starves
)

func isData(c *sdt.Chunk) bool {
	t := c.Type()
	return t != sdt.CSystem && t != sdt.CEnd
}

func chunkSize(c *sdt.Chunk) int { return 16 + len(c.Payload) }

// container runs C1-C20 (C1 lives in Bytes, it needs the raw file head).
func (c *check) container(s *sdt.Stream) {
	if len(s.Chunks) == 0 {
		c.add("C3", Fatal, -1, -1, -1, "no chunks: the file decoded to nothing", "a stream starts with a 16-byte SYSTEM tag; check you opened the right file")
		return
	}

	sysOpts := map[int32]int{} // option -> chunk index of the SYSTEM that announces it
	firstData := map[int32]int{}
	lastTime := map[int32]int32{}
	var demoTimes []int32
	var demoIdx []int
	pcmSeen := 0
	endIdx := -1
	lastDemoTime := int32(-1)

	for i := range s.Chunks {
		ch := &s.Chunks[i]
		size := chunkSize(ch)

		// C2: a chunk size that is not a positive multiple of 16 desyncs the chunk walk.
		if size < 16 || size%16 != 0 {
			c.add("C2", Fatal, i, -1, -1, fmt.Sprintf("chunk size %d is not a multiple of 16", size),
				"pad every chunk body to 16 bytes; PS2 DMA alignment is still enforced by the walker")
		}

		switch ch.Type() {
		case sdt.CSystem:
			// C5/S2: SYSTEM is size 16, time 0, empty body, and lives at the head.
			if size != 16 || ch.Time != 0 || len(ch.Payload) != 0 {
				c.add("C5", Warn, i, -1, -1, fmt.Sprintf("SYSTEM chunk has size=%d time=%d body=%d, retail is size=16 time=0 body=0", size, ch.Time, len(ch.Payload)),
					"emit SYSTEM as type=0x10 size=16 time=0 with an empty body")
			}
			// C7: the option IS the announced raw_type. Zero it and the driver never binds.
			if ch.Option == 0 {
				c.add("C7", Fatal, i, -1, -1, "SYSTEM chunk has option=0: it announces no stream type",
					"set option to the full 32-bit raw_type of the data it announces (0x2 demo, 0x40001 audio, 0x4 caption)")
			}
			if _, dup := sysOpts[ch.Option]; !dup {
				sysOpts[ch.Option] = i
			}
		case sdt.CEnd:
			endIdx = i
			// C15: END terminates the file: type 0xF0, size 16, option 0.
			if size != 16 || ch.Option != 0 || ch.RawType != 0xF0 {
				c.add("C15", Warn, i, -1, -1, fmt.Sprintf("END chunk is raw_type=0x%x size=%d option=%d, retail is 0xF0/16/0", uint32(ch.RawType), size, ch.Option),
					"emit END as type=0xF0 size=16 option=0 time=last_demo_time+300")
			}
		}

		if isData(ch) {
			if _, ok := firstData[ch.RawType]; !ok {
				firstData[ch.RawType] = i
			}
			// C13: a driver scans forward for the first chunk of its type and assumes it is
			// the oldest, so going backwards in time stalls the ring. Fatal for DEMO (the
			// driver is strictly time-paced); a warning for the rest, because
			// us/demo/_bp/p040_05_p03.sdt ships its final xWMA packet with time=0 and plays.
			if t, ok := lastTime[ch.RawType]; ok && ch.Time < t {
				sev := Warn
				if ch.Type() == sdt.CDemo {
					sev = Fatal
				}
				c.add("C13", sev, i, -1, -1, fmt.Sprintf("raw_type 0x%x goes backwards in time: %d after %d", uint32(ch.RawType), ch.Time, t),
					"sort chunks of one raw_type by time; the ring buffer never rewinds")
			}
			lastTime[ch.RawType] = ch.Time
		}

		switch ch.Type() {
		case sdt.CDemo:
			// C8: the demo driver pulls with the literal constant 2. Anything else is invisible to it.
			if ch.RawType != rawDemo {
				c.add("C8", Fatal, i, -1, -1, fmt.Sprintf("DEMO chunk raw_type is 0x%08x, must be exactly 0x00000002", uint32(ch.RawType)),
					"write raw_type 0x2 on every DEMO chunk (demod.c hard-codes CHANK_TYPE_DEMO)")
			}
			// C12: demo option is unread and always 0 in retail.
			if ch.Option != 0 {
				c.add("C12", Warn, i, -1, -1, fmt.Sprintf("DEMO chunk option=%d, retail is always 0", ch.Option), "write option=0 on DEMO chunks")
			}
			// C17: the ring-wrap relocator throttles on very large chunks.
			if size > 64*1024 {
				c.add("C17", Warn, i, -1, -1, fmt.Sprintf("DEMO chunk is %d bytes; retail's largest is ~28 KB", size),
					"split the frame's payload, or drop effect packets; keep DEMO chunks under 64 KB")
			}
			demoTimes = append(demoTimes, ch.Time)
			demoIdx = append(demoIdx, i)
			lastDemoTime = ch.Time
		case sdt.CPCM:
			// C9: STREAM_TYPE_XAUDIO, never a bare 0x1.
			if ch.RawType != rawXAudio {
				c.add("C9", Fatal, i, -1, -1, fmt.Sprintf("PCM chunk raw_type is 0x%08x, must be 0x00040001 (XAUDIO)", uint32(ch.RawType)),
					"write raw_type 0x40001 on audio chunks and announce the same value in SYSTEM")
			}
			pcmSeen++
			if pcmSeen > 1 {
				// C16: the header chunk carries option=0; data chunks carry the unpadded byte count.
				if want := 16 + ((int(ch.Option) + 15) &^ 15); size != want {
					c.add("C16", Fatal, i, -1, -1, fmt.Sprintf("PCM data chunk size=%d but option=%d implies %d", size, ch.Option, want),
						"set option to the unpadded payload length and size to 16+align16(option)")
				}
				// C14: audio may lead by up to 150 ticks; lagging starves the sound driver.
				if lead := ch.Time - lastDemoTime; lastDemoTime >= 0 && lead > 150 {
					c.add("C14", Warn, i, -1, -1, fmt.Sprintf("audio chunk leads the demo by %d ticks (limit 150 = 0.5 s)", lead),
						"interleave audio packet i at the first demo tick t where its start < (t+150)/300")
				}
			}
		case sdt.CCaption:
			// C10: MC's new-font build silently DROPS any CAPTION chunk with a nonzero subtype.
			if ch.RawType != rawCaption {
				c.add("C10", Fatal, i, -1, -1, fmt.Sprintf("CAPTION chunk raw_type is 0x%08x, must be exactly 0x00000004", uint32(ch.RawType)),
					"caption chunks are subtype 0; put the language inside the records, not in the tag")
			}
		}
	}

	// C3: the walk must terminate at an END chunk.
	if endIdx < 0 {
		c.add("C3", Fatal, -1, -1, -1, "no END chunk (type 0xF0): libfs never learns the file is over",
			"append END: type=0xF0 size=16 time=last_demo_time+300 option=0")
	} else if endIdx != len(s.Chunks)-1 {
		c.add("C3", Fatal, endIdx, -1, -1, fmt.Sprintf("END is chunk %d of %d: %d chunks sit past it and are never read", endIdx, len(s.Chunks)-1, len(s.Chunks)-1-endIdx),
			"END goes last")
	}

	// C4: the file ends exactly at END+16. No sector padding.
	if len(s.Tail) != 0 {
		c.add("C4", Warn, -1, -1, -1, fmt.Sprintf("%d bytes of trailing data after the END chunk", len(s.Tail)),
			"truncate the file at END+16; MC streams are loose files and are never sector-padded")
	}

	// C5: chunk 0 is a SYSTEM chunk (every shipped stream opens with one).
	if s.Chunks[0].Type() != sdt.CSystem {
		c.add("C5", Warn, 0, -1, -1, fmt.Sprintf("chunk 0 is %s, retail always opens with SYSTEM", s.Chunks[0].Type()),
			"put the SYSTEM handshake chunks at the head of the file")
	}

	// C6: the handshake. A data type with no SYSTEM announcement is never consumed, so
	// StreamSync cannot advance data_top past it: the ring freezes and the demo never ends.
	for raw, at := range firstData {
		sysAt, ok := sysOpts[raw]
		if !ok {
			c.add("C6", Fatal, at, -1, -1, fmt.Sprintf("raw_type 0x%08x has no SYSTEM chunk announcing it (option must equal it, exact 32-bit)", uint32(raw)),
				fmt.Sprintf("emit SYSTEM type=0x10 size=16 time=0 option=0x%08x at the head of the file", uint32(raw)))
			continue
		}
		if sysAt > at {
			c.add("C6", Warn, sysAt, -1, -1, fmt.Sprintf("SYSTEM for raw_type 0x%08x sits at chunk %d, after its first data chunk %d", uint32(raw), sysAt, at),
				"the handshake only runs over the initial buffer fill: put every SYSTEM chunk at the head")
		}
	}

	// C11: one DEMO chunk == one 60 Hz frame; time = 5*frame, no gaps, no duplicates.
	for i, t := range demoTimes {
		if want := int32(5 * i); t != want {
			c.add("C11", Fatal, demoIdx[i], i, -1, fmt.Sprintf("DEMO chunk %d has time=%d, must be %d (0,5,10,... exactly)", i, t, want),
				"renumber DEMO chunk times as 5*frame_index; the clock is 1/300 s and a demo frame is 5 ticks")
		}
	}

	// C15: END must sit past the last demo frame or the stream ends early.
	if endIdx >= 0 && lastDemoTime >= 0 && s.Chunks[endIdx].Time <= lastDemoTime {
		c.add("C15", Warn, endIdx, -1, -1, fmt.Sprintf("END time=%d is not past the last DEMO chunk time=%d", s.Chunks[endIdx].Time, lastDemoTime),
			"END time = last_demo_time + 300")
	}

	// C13 (global): a chunk far ahead of its neighbours pins data_top and starves the ring.
	for i := 0; i+1 < len(s.Chunks); i++ {
		a, b := &s.Chunks[i], &s.Chunks[i+1]
		if !isData(a) || !isData(b) {
			continue
		}
		if a.Time-b.Time > audioLead {
			c.add("C13", Warn, i, -1, -1, fmt.Sprintf("chunk at time=%d sits before a chunk at time=%d (%d ticks out of order)", a.Time, b.Time, a.Time-b.Time),
				"interleave chunks in time order; audio may lead, but only by ~150 ticks")
		}
	}

	c.slot(s, lastDemoTime, pcmSeen)
}

// slot runs the rules that need out-of-band knowledge: C18 (size envelope), C19 (name),
// C20 (the slot's sound tagflag). Without a Context they are skipped, except C20 which
// still flags an audio-less demo as a risk.
func (c *check) slot(s *sdt.Stream, lastDemoTime int32, pcmSeen int) {
	if pcmSeen == 0 {
		sev, fix := Info, "fine if this slot's tagflag has no sound bit (the 24 shipped scndemo_* demos are like this)"
		if c.ctx != nil && c.ctx.SoundSlot {
			sev = Fatal
			fix = "keep at least a silence track: SoundStreamInit does printf(\"NO SOUND DATA!!\"); HANGUP();"
		}
		c.add("C20", sev, -1, -1, -1, "the stream carries no audio chunks", fix)
	}
	if c.ctx == nil {
		return
	}
	// C18: bp_streams.txt is a sorted RANGE key; overrun and reads resolve into the next stream.
	if c.ctx.Envelope > 0 && c.ctx.FileSize > c.ctx.Envelope {
		c.add("C18", Fatal, -1, -1, -1, fmt.Sprintf("file is %d bytes, the slot's bp_streams envelope is %d (%d over)", c.ctx.FileSize, c.ctx.Envelope, c.ctx.FileSize-c.ctx.Envelope),
			"shrink the stream, or repoint it at a slot with a bigger gap to the next offset")
	}
	// C19: BP_FindStreamName does *(strstr(name,".sdt")) = 0 unguarded.
	if p := c.ctx.Path; p != "" && !hasSuffixFold(p, ".sdt") {
		c.add("C19", Fatal, -1, -1, -1, fmt.Sprintf("%q does not end in .sdt: the reverse name lookup null-derefs", p),
			"overwrite the original .sdt in place; never rename it")
	}
}

func hasSuffixFold(s, suf string) bool {
	if len(s) < len(suf) {
		return false
	}
	s = s[len(s)-len(suf):]
	for i := range suf {
		a, b := s[i], suf[i]
		if 'A' <= a && a <= 'Z' {
			a += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
