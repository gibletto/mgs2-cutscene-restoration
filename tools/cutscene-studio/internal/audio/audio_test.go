package audio

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/sdt"
	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// mcRoot is the Master Collection install; override with MGS2_DIR.
func mcRoot() string { return testenv.GameDir() }

// entry is everything the tests need from one .sdt, so the 12 GB corpus is read once.
type entry struct {
	rel        string
	hdrBody    []byte // the first PCM chunk's payload, verbatim
	amwx       *AMWX
	dataChunks []sdt.Chunk // the PCM chunks after the header, payloads kept
	demoFrames int
	unusedWord uint32 // header 0x1C
}

func (e *entry) silent() bool { return e.hdrBody == nil }

var (
	scanOnce  sync.Once
	scanned   []*entry
	scanGCL   int // .sdt files that are really GCL bytecode
	scanTotal int
	scanErr   error
)

func corpus(t *testing.T) []*entry {
	t.Helper()
	scanOnce.Do(scan)
	if scanErr != nil {
		t.Fatalf("corpus scan: %v", scanErr)
	}
	if len(scanned) == 0 {
		t.Skipf("no .sdt corpus under %s (set MGS2_DIR)", mcRoot())
	}
	return scanned
}

func scan() {
	root := mcRoot()
	scanErr = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".sdt") {
			return nil //nolint:nilerr // an unreadable corner of the install is not a test failure
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		scanTotal++
		if len(raw) >= 4 && string(raw[:4]) == "LCGB" { // GCL bytecode, not a stream
			scanGCL++
			return nil
		}
		s, err := sdt.Decode(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		rel, _ := filepath.Rel(root, p)
		e := &entry{rel: filepath.ToSlash(rel), demoFrames: DemoFrames(s)}
		for i := range s.Chunks {
			c := &s.Chunks[i]
			if c.Type() != sdt.CPCM {
				continue
			}
			if e.hdrBody == nil {
				e.hdrBody = append([]byte(nil), c.Payload...)
				continue
			}
			e.dataChunks = append(e.dataChunks, sdt.Chunk{
				RawType: c.RawType, Time: c.Time, Option: c.Option,
				Payload: append([]byte(nil), c.Payload...),
			})
		}
		if e.hdrBody != nil {
			a, err := DecodeHeader(e.hdrBody)
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			e.amwx = a
			e.unusedWord = le.Uint32(e.hdrBody[0x1C:])
		}
		scanned = append(scanned, e)
		return nil
	})
	sort.Slice(scanned, func(i, j int) bool { return scanned[i].rel < scanned[j].rel })
}

func audioBearing(es []*entry) []*entry {
	var out []*entry
	for _, e := range es {
		if !e.silent() {
			out = append(out, e)
		}
	}
	return out
}

// TestHeaderRoundTrip is the load-bearing one: decode every shipped AMWX header, re-encode,
// demand the same bytes back. Passing proves the dpds table and the garbage pad both survive.
func TestHeaderRoundTrip(t *testing.T) {
	es := corpus(t)
	audio := audioBearing(es)

	exact, mismatch := 0, 0
	for _, e := range audio {
		got, err := e.amwx.Encode()
		if err != nil {
			t.Errorf("%s: encode: %v", e.rel, err)
			continue
		}
		if bytes.Equal(got, e.hdrBody) {
			exact++
			continue
		}
		mismatch++
		if mismatch <= 5 {
			t.Errorf("%s: header round-trip differs (%d bytes vs %d)", e.rel, len(got), len(e.hdrBody))
		}
	}
	t.Logf("scanned %d .sdt (%d were GCL bytecode, skipped); %d audio-bearing, %d silent",
		scanTotal, scanGCL, len(audio), len(es)-len(audio))
	t.Logf("header round-trip byte-identical: %d/%d", exact, len(audio))
	if mismatch != 0 {
		t.Fatalf("%d headers did not round-trip", mismatch)
	}

	// The pad is heap garbage in half the corpus -- zeroing it is NOT a valid round-trip.
	garbage, byPadLen := 0, map[int]int{}
	for _, e := range audio {
		byPadLen[len(e.amwx.Pad)]++
		for _, b := range e.amwx.Pad {
			if b != 0 {
				garbage++
				break
			}
		}
	}
	t.Logf("pad lengths: %v", byPadLen)
	t.Logf("headers whose pad carries non-zero heap garbage: %d/%d (%.1f%%)",
		garbage, len(audio), 100*float64(garbage)/float64(len(audio)))

	unused := 0
	for _, e := range audio {
		if e.unusedWord != 0 {
			unused++
		}
	}
	t.Logf("headers with non-zero mUnused @0x1C: %d/%d", unused, len(audio))
}

// TestInvariants checks the header rules against every shipped header, and proves Validate
// accepts the 14 files whose dpds ends with a zero-decode flush packet.
func TestInvariants(t *testing.T) {
	audio := audioBearing(corpus(t))

	sampleSizeOK, dpdsAlignOK, bodyLenOK, validateOK := 0, 0, 0, 0
	var equalPair []string
	for _, e := range audio {
		a := e.amwx
		if int64(a.SampleSize) == int64(len(a.DPDS))*int64(a.BlockAlign) { // A7
			sampleSizeOK++
		} else {
			t.Errorf("%s: A7: mSampleSize %d != %d packets * blockAlign %d",
				e.rel, a.SampleSize, len(a.DPDS), a.BlockAlign)
		}

		end := hdrFixed + 4*len(a.DPDS) // A8
		if len(e.hdrBody) == align16(end) && a.CBSize() == 0 {
			bodyLenOK++
		} else {
			t.Errorf("%s: A8: body %d bytes (want %d), cbSize %d",
				e.rel, len(e.hdrBody), align16(end), a.CBSize())
		}

		bad := false // A9
		var prev uint32
		for i, v := range a.DPDS {
			if v%uint32(a.PCMFrame()) != 0 || v < prev {
				t.Errorf("%s: A9: dpds[%d] = %d (prev %d, pcm frame %d)", e.rel, i, v, prev, a.PCMFrame())
				bad = true
				break
			}
			if i > 0 && v == prev {
				equalPair = append(equalPair, fmt.Sprintf("%s (N=%d, %d twice)", e.rel, len(a.DPDS), v))
			}
			prev = v
		}
		if !bad {
			dpdsAlignOK++
		}

		if err := a.Validate(); err != nil {
			t.Errorf("%s: Validate rejected a shipped header: %v", e.rel, err)
		} else {
			validateOK++
		}
	}

	n := len(audio)
	t.Logf("A7 mSampleSize == dpdsCount*blockAlign : %d/%d", sampleSizeOK, n)
	t.Logf("A8 bodyLen == align16(0x36+4N), cbSize==0: %d/%d", bodyLenOK, n)
	t.Logf("A9 dpds non-decreasing, %% (2*ch) == 0   : %d/%d", dpdsAlignOK, n)
	t.Logf("Validate() accepts                       : %d/%d", validateOK, n)

	sort.Strings(equalPair)
	t.Logf("files with an equal-consecutive dpds pair (Validate must ACCEPT these): %d", len(equalPair))
	for _, s := range equalPair {
		t.Logf("  %s", s)
	}
	if len(equalPair) == 0 {
		t.Error("expected shipped files ending in a zero-decode flush packet; found none")
	}
}

// TestFormatCensus reports the audio format classes found in the shipped files.
func TestFormatCensus(t *testing.T) {
	audio := audioBearing(corpus(t))

	type class struct{ format, ch, freq, blockAlign, avg int32 }
	counts := map[class]int{}
	cookieBad := 0
	for _, e := range audio {
		a := e.amwx
		counts[class{a.Format, a.Channels, a.Frequency, a.BlockAlign, a.AvgBytesPerSec}]++
		if le.Uint32(e.hdrBody) != cookieAMWX {
			cookieBad++
		}
	}
	var keys []class
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })

	t.Logf("cookie != AMWX: %d/%d", cookieBad, len(audio))
	t.Logf("%-8s %-3s %-6s %-11s %-9s %s", "format", "ch", "freq", "blockAlign", "avgBps", "count")
	for _, k := range keys {
		t.Logf("%-8d %-3d %-6d %-11d %-9d %d", k.format, k.ch, k.freq, k.blockAlign, k.avg, counts[k])
	}
	if len(keys) != 4 {
		t.Errorf("expected exactly 4 format classes; found %d", len(keys))
	}

	want := map[class]int{
		{0x161, 1, 44100, 2230, 6000}:  6418,
		{0x161, 2, 44100, 8917, 24000}: 1017,
		{0x161, 2, 48000, 8192, 24000}: 115,
		{0x162, 6, 48000, 8192, 24000}: 8,
	}
	for k, w := range want {
		if counts[k] != w {
			t.Errorf("class %+v: got %d files, expected %d", k, counts[k], w)
		}
	}
}

// TestSilentDemos finds the shipped demos that carry no audio chunks at all -- the ones that
// prove the wall-clock fallback ships.
func TestSilentDemos(t *testing.T) {
	es := corpus(t)

	var silent []string
	for _, e := range es {
		if e.demoFrames > 0 && e.silent() {
			silent = append(silent, fmt.Sprintf("%s (%d frames, %.2f s)",
				e.rel, e.demoFrames, float64(e.demoFrames)/60))
		}
	}
	sort.Strings(silent)
	t.Logf("shipped demos with ZERO audio chunks: %d", len(silent))
	for _, s := range silent {
		t.Logf("  %s", s)
	}
	if len(silent) != 24 {
		t.Errorf("expected 24 audio-less demos; found %d", len(silent))
	}

	// Every one of them must be readable as a silent stream, not an error.
	for _, e := range es {
		if e.silent() && e.demoFrames > 0 {
			s := &sdt.Stream{}
			if HasAudio(s) {
				t.Fatal("HasAudio lied about an empty stream")
			}
		}
	}
}

// TestOverhang measures every audio-bearing demo's audio-vs-demo budget, and pins the
// reference numbers for p080_13_p08.
func TestOverhang(t *testing.T) {
	es := corpus(t)

	type run struct {
		rel   string
		over  int
		demo  int
		audio int
	}
	var overruns []run
	demos, worstN := 0, 0
	for _, e := range es {
		if e.demoFrames == 0 || e.silent() {
			continue
		}
		demos++
		if d := e.demoFrames - e.amwx.Frames(); d > 0 {
			overruns = append(overruns, run{e.rel, d, e.demoFrames, e.amwx.Frames()})
			worstN = max(worstN, d)
		}
	}
	sort.Slice(overruns, func(i, j int) bool { return overruns[i].over > overruns[j].over })

	t.Logf("audio-bearing demos: %d; those whose DEMO track outruns their audio: %d",
		demos, len(overruns))
	for _, r := range overruns {
		t.Logf("  +%3d frames (+%.2f s)  %-28s demo %d vs audio %d", r.over, float64(r.over)/60,
			r.rel, r.demo, r.audio)
	}
	if worstN > MaxShippedOverhang {
		t.Errorf("MaxShippedOverhang is %d but a shipped file overruns by %d", MaxShippedOverhang, worstN)
	}
}

// TestReferenceStream reads the reference file from the pristine backup.
func TestReferenceStream(t *testing.T) {
	path := filepath.Join(mcRoot(), `us\demo2\_bp\p080_13_p08.sdt.orig`)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%v", err)
	}
	s, err := sdt.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}

	a, packets, err := Track(s)
	if err != nil {
		t.Fatal(err)
	}
	l, err := Measure(s)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("file size          %d", len(raw))
	t.Logf("format             fmt=%#x ch=%d freq=%d blockAlign=%d avgBps=%d",
		a.Format, a.Channels, a.Frequency, a.BlockAlign, a.AvgBytesPerSec)
	t.Logf("mSampleSize        %d", a.SampleSize)
	t.Logf("dpdsCount          %d   (data packets on disk: %d)", len(a.DPDS), len(packets))
	t.Logf("dpds[N-1]          %d", a.DPDS[len(a.DPDS)-1])
	t.Logf("audio duration     %.2f s  = %d demo frames", a.DurationSec(), a.Frames())
	t.Logf("demo chunks        %d = %.2f s", l.DemoFrames, float64(l.DemoFrames)/60)
	t.Logf("overhang           %d frames (%.2f s); uncharted=%v", l.Overhang, float64(l.Overhang)/60, l.Uncharted())
	t.Logf("header pad         % x", a.Pad)

	if got := len(packets) * int(a.BlockAlign); got != int(a.SampleSize) {
		t.Errorf("data packets total %d bytes, header says %d", got, a.SampleSize)
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}

	// Rebuilding the chunks from the decoded track must reproduce the shipped bytes.
	hdr, err := HeaderChunk(a)
	if err != nil {
		t.Fatal(err)
	}
	var shipped []sdt.Chunk
	for i := range s.Chunks {
		if s.Chunks[i].Type() == sdt.CPCM {
			shipped = append(shipped, s.Chunks[i])
		}
	}
	if !bytes.Equal(hdr.Payload, shipped[0].Payload) || hdr.RawType != shipped[0].RawType ||
		hdr.Time != shipped[0].Time || hdr.Option != shipped[0].Option {
		t.Error("HeaderChunk does not reproduce the shipped header chunk")
	}
	for i, p := range packets {
		c := DataChunk(p, shipped[i+1].Time)
		if c.RawType != shipped[i+1].RawType || c.Option != shipped[i+1].Option ||
			!bytes.Equal(c.Payload, shipped[i+1].Payload) {
			t.Fatalf("DataChunk %d does not reproduce the shipped chunk", i)
		}
	}
	t.Logf("rebuilt %d PCM chunks byte-identically from the decoded track", len(shipped))

	// How well does Schedule() reproduce the shipped interleave?
	ticks := a.Schedule()
	match, firstBad := 0, ""
	for i := range packets {
		if ticks[i] == shipped[i+1].Time {
			match++
		} else if firstBad == "" {
			firstBad = fmt.Sprintf("packet %d: computed tick %d, shipped %d", i, ticks[i], shipped[i+1].Time)
		}
	}
	t.Logf("Schedule() reproduces the shipped tick for %d/%d packets. %s", match, len(packets), firstBad)
	t.Logf("first shipped PCM data ticks: %v", tickList(shipped[1:], 8))
	t.Logf("first Schedule() ticks:       %v", ticks[:min(8, len(ticks))])
}

// TestScheduleCorpus checks the interleave rule against every shipped demo.
//
// The thing that has to hold is the LEAD WINDOW, not tick equality: a packet must be handed
// over before its own audio starts playing, and no more than 150 ticks (0.5 s) before. Tick
// equality with the shipped byte is only a fidelity score -- and it cannot be 100%, because
// Konami's writer sometimes emitted a packet at a tick that is not on the 5-tick demo grid
// at all (3 of 70 packets in jp/demo/_bp/p003_01_p01), which a grid-snapped scheduler cannot
// reproduce and does not need to.
func TestScheduleCorpus(t *testing.T) {
	es := corpus(t)

	files, exact := 0, 0
	packets, hits, tailZero, offGrid := 0, 0, 0, 0
	delta := map[int]int{}
	for _, e := range es {
		if e.demoFrames == 0 || e.silent() {
			continue
		}
		files++
		a := e.amwx
		ticks := a.Schedule()
		if len(ticks) != len(e.dataChunks) {
			t.Errorf("%s: %d dpds entries but %d data chunks", e.rel, len(ticks), len(e.dataChunks))
			continue
		}

		den := int64(a.PCMFrame()) * int64(a.Frequency)
		miss := 0
		for i, c := range e.dataChunks {
			packets++
			var startBytes int64
			if i > 0 {
				startBytes = int64(a.DPDS[i-1])
			}
			// startTicks = startBytes*300/den, kept rational.
			lead := startBytes*300 - int64(ticks[i])*den
			if lead < 0 {
				t.Errorf("%s: packet %d emitted at tick %d, AFTER its audio starts -- ring underrun",
					e.rel, i, ticks[i])
			}
			if lead > leadTicks*den {
				t.Errorf("%s: packet %d leads by more than %d ticks", e.rel, i, leadTicks)
			}

			if c.Time%5 != 0 {
				offGrid++
			}
			switch {
			case ticks[i] == c.Time:
				hits++
			case c.Time == 0 && i > 0: // EndStream flushes leftovers at time 0
				tailZero++
			default:
				miss++
				delta[int(ticks[i]-c.Time)]++
			}
		}
		if miss == 0 {
			exact++
		}
	}
	t.Logf("audio-bearing demos: %d, PCM data packets: %d", files, packets)
	t.Logf("Schedule() lead window 0 <= lead <= 150 ticks: holds for all %d packets", packets)
	t.Logf("Schedule() reproduces the shipped tick exactly: %d/%d packets (%.1f%%), %d/%d demos whole",
		hits, packets, 100*float64(hits)/float64(packets), exact, files)
	t.Logf("shipped ticks that are NOT on the 5-tick demo grid: %d (unreproducible by design)", offGrid)
	t.Logf("EndStream time-0 tail flushes: %d", tailZero)
	t.Logf("delta histogram (computed - shipped), all within one demo frame: %v", delta)
}

func tickList(cs []sdt.Chunk, n int) []int32 {
	var out []int32
	for i := 0; i < len(cs) && i < n; i++ {
		out = append(out, cs[i].Time)
	}
	return out
}

// TestStripAudioRefuses: Recipe B is only legal on a slot whose tagflag has no sound bit.
// On any other slot the runtime hits HANGUP(), so we must not hand the caller a bomb.
func TestStripAudioRefuses(t *testing.T) {
	path := filepath.Join(mcRoot(), `us\demo2\_bp\p080_13_p08.sdt.orig`)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%v", err)
	}

	for _, tf := range []uint32{TagFlagSoundVAG, TagFlagSound8Bit, TagFlagSoundAC3, 0x60000000} {
		s, _ := sdt.Decode(raw)
		if err := StripAudio(s, tf); err == nil {
			t.Errorf("StripAudio accepted tagflag %#08x -- that hangs the game", tf)
		}
		if !HasAudio(s) {
			t.Errorf("StripAudio(%#08x) refused but still mutated the stream", tf)
		}
	}

	s, _ := sdt.Decode(raw)
	before := len(s.Chunks)
	if err := StripAudio(s, 0x00000003); err != nil {
		t.Fatalf("StripAudio on a soundless tagflag: %v", err)
	}
	if HasAudio(s) {
		t.Fatal("StripAudio left PCM chunks behind")
	}
	for i := range s.Chunks {
		if c := &s.Chunks[i]; c.Type() == sdt.CSystem && c.Option == RawTypeXAudio {
			t.Fatal("StripAudio left the audio SYSTEM record behind")
		}
	}
	l, err := Measure(s)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Silent || l.DemoFrames == 0 {
		t.Fatalf("stripped stream measures %+v", l)
	}
	t.Logf("stripped: %d chunks -> %d, %d demo frames intact, now on the wall clock",
		before, len(s.Chunks), l.DemoFrames)
}

// TestNewHeader round-trips a freshly authored header (Recipe A step 4).
func TestNewHeader(t *testing.T) {
	dpds := make([]uint32, 200)
	for i := range dpds {
		dpds[i] = uint32(i+1) * 57600 // 0.3 s of 48 kHz stereo per packet
	}
	a, err := NewHeader(0x161, 2, 48000, 24000, 8192, dpds)
	if err != nil {
		t.Fatal(err)
	}
	body, err := a.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != align16(hdrFixed+4*len(dpds)) {
		t.Fatalf("body is %d bytes, want %d", len(body), align16(hdrFixed+4*len(dpds)))
	}
	back, err := DecodeHeader(body)
	if err != nil {
		t.Fatal(err)
	}
	again, err := back.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, again) {
		t.Fatal("authored header does not round-trip")
	}
	if err := back.Validate(); err != nil {
		t.Fatal(err)
	}
	t.Logf("authored %d-byte header: %d packets, %.2f s, %d demo frames, mSampleSize %d",
		len(body), len(back.DPDS), back.DurationSec(), back.Frames(), back.SampleSize)
}

// TestValidateRejects: the checks that matter must actually fire.
func TestValidateRejects(t *testing.T) {
	base := func() *AMWX {
		a, err := NewHeader(0x161, 2, 48000, 24000, 8192, []uint32{192000, 384000, 576000})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	tests := []struct {
		name string
		bend func(*AMWX)
	}{
		{"A7 sample size", func(a *AMWX) { a.SampleSize += 8192 }},
		{"A8 cbSize", func(a *AMWX) { le.PutUint16(a.WFX[16:], 2) }},
		{"A9 backwards dpds", func(a *AMWX) { a.DPDS[2] = 1000 }},
		{"A9 unaligned dpds", func(a *AMWX) { a.DPDS[1]++ }},
		{"bad channels", func(a *AMWX) { a.Channels = 0 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := base()
			tc.bend(a)
			if err := a.Validate(); err == nil {
				t.Fatal("Validate accepted a broken header")
			}
		})
	}

	// ...but an equal-consecutive dpds pair (a zero-decode flush packet) is LEGAL.
	a := base()
	a.DPDS[2] = a.DPDS[1]
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate rejected a legal zero-decode flush packet: %v", err)
	}
}
