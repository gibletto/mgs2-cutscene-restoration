package validate

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/datadir"
	"mgs2-restoration/cutscene-studio/internal/sdt"
	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// corpusRoot is the game install (set MGS2_DIR).
func corpusRoot() string { return testenv.GameDir() }

var demoDirs = []string{"us/demo2/_bp", "us/demo/_bp", "jp/demo2/_bp", "jp/demo/_bp"}

func corpusFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, d := range demoDirs {
		m, err := filepath.Glob(filepath.Join(corpusRoot(), d, "*.sdt"))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, m...)
	}
	if len(out) == 0 {
		t.Skipf("corpus not found under %q (set MGS2_DIR)", corpusRoot())
	}
	sort.Strings(out)
	return out
}

func load(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestCorpusIsClean is the calibration: Konami's own shipped cutscenes must produce zero
// Fatal findings. A rule that fires on retail data is a wrong rule.
func TestCorpusIsClean(t *testing.T) {
	files := corpusFiles(t)

	type tally struct {
		sev   Severity
		hits  int
		files int
		where string
	}
	rules := map[string]*tally{}
	var demos, skipped, frames int
	fatalFiles := map[string]int{}

	for _, f := range files {
		buf := load(t, f)
		if len(buf) >= 4 && string(buf[:4]) == "LCGB" {
			skipped++
			continue
		}
		s, err := sdt.Decode(buf)
		if err != nil {
			t.Fatalf("%s: decode: %v", f, err)
		}
		nDemo := 0
		for i := range s.Chunks {
			if s.Chunks[i].Type() == sdt.CDemo {
				nDemo++
			}
		}
		if nDemo == 0 {
			skipped++
			continue
		}
		demos++
		frames += nDemo

		res := Bytes(buf, &Context{Path: f, FileSize: int64(len(buf))})
		for _, fd := range res.Findings {
			r, ok := rules[fd.Rule]
			if !ok {
				r = &tally{sev: fd.Severity, where: filepath.Base(f) + " " + fd.Msg}
				rules[fd.Rule] = r
			}
			if fd.Severity < r.sev {
				r.sev = fd.Severity
				r.where = filepath.Base(f) + " " + fd.Msg
			}
		}
		seen := map[string]bool{}
		for rule, n := range res.Counts {
			r, ok := rules[rule]
			if !ok {
				r = &tally{sev: Info}
				rules[rule] = r
			}
			r.hits += n
			if !seen[rule] {
				r.files++
				seen[rule] = true
			}
		}
		if HasFatal(res.Findings) {
			fatalFiles[f] = res.Counts["_"]
			for _, fd := range res.Findings {
				if fd.Severity == Fatal {
					t.Errorf("RETAIL FATAL %s: %s", filepath.Base(f), fd)
				}
			}
		}
	}

	t.Logf("scanned %d files: %d cutscene demos, %d non-demo/GCL skipped, %d demo frames", len(files), demos, skipped, frames)
	names := make([]string, 0, len(rules))
	for r := range rules {
		names = append(names, r)
	}
	sort.Strings(names)
	t.Log("rule            sev   hits    files  example")
	for _, n := range names {
		r := rules[n]
		ex := r.where
		if len(ex) > 96 {
			ex = ex[:96]
		}
		t.Logf("%-14s %-5s %7d %6d  %s", n, r.sev, r.hits, r.files, ex)
	}
	if len(fatalFiles) > 0 {
		t.Errorf("%d of %d shipped demos produced Fatal findings; the rules are wrong, not the data", len(fatalFiles), demos)
	}
}

// --- negative tests: corrupt a real demo and prove the right rule fires ----------------

func sample(t *testing.T) (path string, buf []byte) {
	t.Helper()
	for _, f := range corpusFiles(t) {
		b := load(t, f)
		if len(b) < 4 || string(b[:4]) == "LCGB" || len(b) > 4<<20 {
			continue
		}
		s, err := sdt.Decode(b)
		if err != nil {
			continue
		}
		for i := range s.Chunks {
			if s.Chunks[i].Type() == sdt.CDemo {
				return f, b
			}
		}
	}
	t.Skip("no usable demo in the corpus")
	return "", nil
}

func fires(t *testing.T, f []Finding, rule string, sev Severity) {
	t.Helper()
	for _, x := range f {
		if x.Rule == rule && x.Severity == sev {
			t.Logf("  %s", x)
			return
		}
	}
	t.Fatalf("%s (%s) did not fire; got %v", rule, sev, f)
}

func demoChunks(s *sdt.Stream) []int {
	var out []int
	for i := range s.Chunks {
		if s.Chunks[i].Type() == sdt.CDemo {
			out = append(out, i)
		}
	}
	return out
}

func decode(t *testing.T, buf []byte) *sdt.Stream {
	t.Helper()
	s, err := sdt.Decode(buf)
	if err != nil {
		t.Fatal(err)
	}
	// Decode aliases the input; copy every payload so a mutation cannot bleed across tests.
	for i := range s.Chunks {
		p := make([]byte, len(s.Chunks[i].Payload))
		copy(p, s.Chunks[i].Payload)
		s.Chunks[i].Payload = p
	}
	return s
}

// clean asserts the untouched sample is Fatal-free, so every negative test below proves
// the corruption -- not a pre-existing complaint -- fired the rule.
func TestSampleIsClean(t *testing.T) {
	path, buf := sample(t)
	res := Bytes(buf, &Context{Path: path, FileSize: int64(len(buf))})
	if HasFatal(res.Findings) {
		t.Fatalf("%s is not clean: %v", path, res.Findings)
	}
	t.Logf("%s: %d chunks, findings %v", filepath.Base(path), len(decode(t, buf).Chunks), res.Counts)
}

func TestDropTermFrame(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	d := demoChunks(s)
	ch := &s.Chunks[d[3]]
	ch.Payload = ch.Payload[:len(ch.Payload)-16] // the last packet of a chunk is always TERM_FRAME
	fires(t, Stream(s), "P6", Fatal)
}

func TestEndianSwappedFlag(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	ch := &s.Chunks[demoChunks(s)[2]]
	p := binary.LittleEndian.Uint32(ch.Payload[0:])
	binary.LittleEndian.PutUint32(ch.Payload[0:], p|0x40000000)
	fires(t, Stream(s), "P3", Fatal)
}

func TestNoSkipStrippedFromTermFrame(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	ch := &s.Chunks[demoChunks(s)[4]]
	off := len(ch.Payload) - 16
	binary.LittleEndian.PutUint32(ch.Payload[off:], 0x00000001) // TERM_FRAME, NOSKIP cleared
	f := Stream(s)
	fires(t, f, "P7", Fatal)
	fires(t, f, "P4", Fatal)
}

// requireCatalog skips a test whose rule needs the effect catalog's layouts.
func requireCatalog(t *testing.T) {
	t.Helper()
	ok, err := DefaultCatalogLoaded()
	if err != nil {
		t.Fatalf("effect catalog: %v", err)
	}
	if !ok {
		t.Skipf("effects.json not found in the data dir (generate it with tools/cnf2json.py, or set %s)", datadir.EnvVar)
	}
}

func TestMissizedEffect(t *testing.T) {
	requireCatalog(t)
	_, buf := sample(t)
	s := decode(t, buf)
	// Grow an effect packet by 16 bytes and shrink the terminator's slack: the packet walk
	// still parses, but the size no longer matches the catalog's Data layout.
	for _, ci := range demoChunks(s) {
		ch := &s.Chunks[ci]
		pkts, _ := walk(ch.Payload)
		off := 0
		for pi := range pkts {
			sz := packetSize(&pkts[pi])
			if pkts[pi].Kind() == sdt.PEffect {
				grown := make([]byte, 0, len(ch.Payload)+16)
				grown = append(grown, ch.Payload[:off]...)
				grown = append(grown, ch.Payload[off:off+sz]...)
				grown = append(grown, make([]byte, 16)...)
				grown = append(grown, ch.Payload[off+sz:]...)
				binary.LittleEndian.PutUint32(grown[off+4:], uint32(sz+16))
				ch.Payload = grown
				fires(t, Stream(s), "P26", Fatal)
				return
			}
			off += sz
		}
	}
	t.Skip("sample has no effect packets")
}

func TestBadExecType(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	for _, ci := range demoChunks(s) {
		ch := &s.Chunks[ci]
		pkts, _ := walk(ch.Payload)
		off := 0
		for pi := range pkts {
			sz := packetSize(&pkts[pi])
			if pkts[pi].Kind() == sdt.PEffect {
				binary.LittleEndian.PutUint32(ch.Payload[off+0x14:], 2) // exec_type 2 does not exist
				fires(t, Stream(s), "P27", Fatal)
				return
			}
			off += sz
		}
	}
	t.Skip("sample has no effect packets")
}

func TestMissingSystemHandshake(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	for i := range s.Chunks {
		if s.Chunks[i].Type() == sdt.CSystem && s.Chunks[i].Option == 2 {
			s.Chunks = append(s.Chunks[:i], s.Chunks[i+1:]...)
			break
		}
	}
	fires(t, Stream(s), "C6", Fatal)
}

func TestDemoTimesOffGrid(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	s.Chunks[demoChunks(s)[5]].Time = 27 // demo chunks step exactly 5 ticks
	fires(t, Stream(s), "C11", Fatal)
}

func TestVerticalCamera(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	ch := &s.Chunks[demoChunks(s)[1]]
	pkts, _ := walk(ch.Payload)
	off := 0
	for pi := range pkts {
		sz := packetSize(&pkts[pi])
		if pkts[pi].Kind() == sdt.PFrame {
			put := func(o int, v float32) { binary.LittleEndian.PutUint32(ch.Payload[off+o:], mathBits(v)) }
			put(0x20, 100) // pos
			put(0x24, 0)
			put(0x28, 100)
			put(0x30, 100) // trg: straight down
			put(0x34, -5000)
			put(0x38, 100)
			fires(t, Stream(s), "P13", Fatal)
			return
		}
		off += sz
	}
	t.Fatal("no FRAME packet in the chunk")
}

func TestObjectDestroy(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	ch := &s.Chunks[demoChunks(s)[0]]
	pkts, _ := walk(ch.Payload)
	off := 0
	for pi := range pkts {
		sz := packetSize(&pkts[pi])
		if pkts[pi].Kind() == sdt.PCommand {
			if c, _ := pkts[pi].Command(); c.Command == sdt.CmdObjectVisible {
				binary.LittleEndian.PutUint32(ch.Payload[off+0x10:], 1) // OBJECT_DESTROY
				fires(t, Stream(s), "P15", Fatal)
				return
			}
		}
		off += sz
	}
	t.Skip("sample has no OBJECT_VISIBLE command")
}

func TestGCLBytecodeRejected(t *testing.T) {
	res := Bytes([]byte("LCGB\x00\x00\x00\x00"), nil)
	fires(t, res.Findings, "C1", Fatal)
}

func TestCaptionOverlap(t *testing.T) {
	files := corpusFiles(t)
	for _, f := range files {
		buf := load(t, f)
		if len(buf) < 4 || string(buf[:4]) == "LCGB" || len(buf) > 8<<20 {
			continue
		}
		s := decode(t, buf)
		for i := range s.Chunks {
			ch := &s.Chunks[i]
			if ch.Type() != sdt.CCaption || len(ch.Payload) < 48 {
				continue
			}
			if HasFatal(Stream(s)) {
				continue
			}
			// Rewind the second record of its language to tick 0: the caption scan is
			// forward-only, so an out-of-order line flashes for one frame instead of playing.
			ln := int(binary.LittleEndian.Uint16(ch.Payload[8+12:]))
			if 8+ln+16 > len(ch.Payload) {
				continue
			}
			binary.LittleEndian.PutUint32(ch.Payload[8+ln:], 0)   // start = 0
			binary.LittleEndian.PutUint32(ch.Payload[8+ln+4:], 1) // end = 1
			fires(t, Stream(s), "A4", Fatal)
			return
		}
	}
	t.Skip("no caption-bearing demo in the corpus")
}

func TestTruncatedAudio(t *testing.T) {
	files := corpusFiles(t)
	for _, f := range files {
		buf := load(t, f)
		if len(buf) < 4 || string(buf[:4]) == "LCGB" || len(buf) > 8<<20 {
			continue
		}
		s := decode(t, buf)
		if HasFatal(Stream(s)) {
			continue
		}
		hdr := -1
		var drop int
		for i := range s.Chunks {
			if s.Chunks[i].Type() == sdt.CPCM {
				if hdr < 0 {
					hdr = i
				} else {
					drop = i
				}
			}
		}
		if hdr < 0 || drop == 0 {
			continue
		}
		s.Chunks = append(s.Chunks[:drop], s.Chunks[drop+1:]...) // one xWMA packet short of the header's claim
		fires(t, Stream(s), "A11", Fatal)
		return
	}
	t.Skip("no audio-bearing demo in the corpus")
}

func TestTermSeqRemoved(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	d := demoChunks(s)
	ch := &s.Chunks[d[len(d)-1]]
	n := len(ch.Payload)
	ch.Payload = append(ch.Payload[:n-32:n-32], ch.Payload[n-16:]...) // drop TERM_SEQ, keep TERM_FRAME
	fires(t, Stream(s), "P9", Fatal)
}

func TestObjectVisibleRetargeted(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	ch := &s.Chunks[demoChunks(s)[0]]
	pkts, _ := walk(ch.Payload)
	off := 0
	for pi := range pkts {
		sz := packetSize(&pkts[pi])
		if c, ok := pkts[pi].Command(); ok && c.Command == sdt.CmdObjectVisible {
			binary.LittleEndian.PutUint32(ch.Payload[off+0x14:], 9999) // an id no OBJECT declares
			fires(t, Stream(s), "P17", Fatal)
			return
		}
		off += sz
	}
	t.Skip("sample has no OBJECT_VISIBLE command")
}

func TestDietRunCountZero(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	ch := &s.Chunks[demoChunks(s)[2]]
	pkts, _ := walk(ch.Payload)
	off := 0
	for pi := range pkts {
		sz := packetSize(&pkts[pi])
		if m, ok := pkts[pi].MotionPack(); ok && len(m.Links) > 0 {
			l := m.Links[0]
			blk := off + int(l.Offset) + 16 + 8 // packet start + info.offset, past FVECTOR pos + SVECTOR rot
			ch.Payload[blk] = 0                 // the low byte is the run count, never 0
			fires(t, Stream(s), "P23", Fatal)
			return
		}
		off += sz
	}
	t.Skip("sample has no MOTION_PACK")
}

// TestEffectBirthSpam is the classic authoring mistake: re-emitting an exec_type 1 birth
// packet every frame to "animate" it. Each one is a new actor against DM_MAX_EFFECT.
func TestEffectBirthSpam(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	d := demoChunks(s)
	var fx []byte
	for _, ci := range d {
		pkts, _ := walk(s.Chunks[ci].Payload)
		off := 0
		for pi := range pkts {
			sz := packetSize(&pkts[pi])
			if e, ok := pkts[pi].Effect(); ok && e.ExecType == 1 {
				fx = append([]byte(nil), s.Chunks[ci].Payload[off:off+sz]...)
			}
			off += sz
		}
		if fx != nil {
			break
		}
	}
	if fx == nil || len(d) < 600 {
		t.Skip("sample has no exec_type 1 effect, or is shorter than 600 frames")
	}
	for _, ci := range d[:600] {
		p := s.Chunks[ci].Payload
		n := len(p)
		spliced := make([]byte, 0, n+len(fx))
		spliced = append(spliced, p[:n-16]...) // before TERM_FRAME
		spliced = append(spliced, fx...)
		spliced = append(spliced, p[n-16:]...)
		s.Chunks[ci].Payload = spliced
	}
	f := Stream(s)
	fires(t, f, "P30", Fatal)
	fires(t, f, "P35", Warn)
}

func TestDemoRawTypeRetagged(t *testing.T) {
	_, buf := sample(t)
	s := decode(t, buf)
	s.Chunks[demoChunks(s)[7]].RawType = 0x00010002 // the demo driver pulls the literal 2
	f := Stream(s)
	fires(t, f, "C8", Fatal)
	fires(t, f, "C6", Fatal)
}

func TestPCMSizeOptionMismatch(t *testing.T) {
	files := corpusFiles(t)
	for _, f := range files {
		buf := load(t, f)
		if len(buf) < 4 || string(buf[:4]) == "LCGB" || len(buf) > 8<<20 {
			continue
		}
		s := decode(t, buf)
		if HasFatal(Stream(s)) {
			continue
		}
		n := 0
		for i := range s.Chunks {
			if s.Chunks[i].Type() != sdt.CPCM {
				continue
			}
			if n++; n == 3 {
				s.Chunks[i].Option = 99 // option is the unpadded byte count; size must follow it
				fires(t, Stream(s), "C16", Fatal)
				return
			}
		}
	}
	t.Skip("no audio-bearing demo in the corpus")
}

func mathBits(v float32) uint32 { return math.Float32bits(v) }
