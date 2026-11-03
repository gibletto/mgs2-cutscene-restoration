package caption

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/sdt"
	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// mcRoot is the Master Collection install; override with MGS2_DIR.
func mcRoot() string { return testenv.GameDir() }

// The cutscene streams, and every other stream directory that ships captions. The 926
// files under */codec/_bp are GCL bytecode, not streams -- never glob those.
var (
	demoDirs = []string{`us\demo\_bp`, `us\demo2\_bp`, `jp\demo\_bp`, `jp\demo2\_bp`}
	allDirs  = append(append([]string{}, demoDirs...),
		`us\movie\_bp`, `us\movievr\_bp`, `us\vox\_bp`,
		`jp\movie\_bp`, `jp\movievr\_bp`, `jp\vox\_bp`)
)

type chunk struct {
	file   string
	body   []byte
	option int32
}

// captions collects the CAPTION chunk of every stream under dirs that has one.
func captions(t *testing.T, dirs []string) []chunk {
	t.Helper()
	var out []chunk
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(mcRoot(), dir, "*.sdt"))
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			s, err := sdt.Decode(raw)
			if err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			for i := range s.Chunks {
				c := &s.Chunks[i]
				if c.Type() == sdt.CCaption {
					out = append(out, chunk{f, c.Payload, c.Option})
				}
			}
		}
	}
	if len(out) == 0 {
		t.Skipf("no caption corpus under %s (set MGS2_DIR)", mcRoot())
	}
	return out
}

// TestGoldenRoundTrip: every shipped CAPTION chunk must decode and re-encode to identical
// bytes -- pad, private-use codepoints, opaque speaker ids and all.
func TestGoldenRoundTrip(t *testing.T) {
	for _, dirs := range []struct {
		name string
		dirs []string
	}{
		{"demos", demoDirs},
		{"all-streams", allDirs},
	} {
		t.Run(dirs.name, func(t *testing.T) {
			chunks := captions(t, dirs.dirs)
			exact, records, actions := 0, 0, 0
			for _, c := range chunks {
				tab, err := Decode(c.body)
				if err != nil {
					t.Errorf("%s: decode: %v", c.file, err)
					continue
				}
				// The inferred caption/action split must reproduce the tag's ofs_base.
				if tab.Option() != c.option {
					t.Errorf("%s: Option() = %#x, tag says %#x", c.file, tab.Option(), c.option)
					continue
				}
				got, err := tab.Encode()
				if err != nil {
					t.Errorf("%s: encode: %v", c.file, err)
					continue
				}
				if !bytes.Equal(got, c.body) {
					t.Errorf("%s: round-trip differs (%d bytes, want %d)", c.file, len(got), len(c.body))
					continue
				}
				exact++
				records += len(tab.Records)
				actions += len(tab.Actions)
			}
			t.Logf("%d/%d chunks byte-identical (%.2f%%), %d records, %d actions",
				exact, len(chunks), 100*float64(exact)/float64(len(chunks)), records, actions)
		})
	}
}

// TestSpecInvariants measures the claims the bake rules rest on.
func TestSpecInvariants(t *testing.T) {
	chunks := captions(t, allDirs)

	var records, actions int
	var descending, overlapping, zeroDur, badLang, badLevel, badPad int
	langs := map[int16]int{}
	optHist := map[int32]int{}

	for _, c := range chunks {
		if c.option>>16 != 0 {
			badLevel++
		}
		optHist[c.option]++

		tab, err := Decode(c.body)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if len(tab.Pad) > 15 {
			badPad++
		}

		prev := map[int16]*Record{}
		for i := range tab.Records {
			r := &tab.Records[i]
			records++
			langs[r.Lang]++
			if r.Lang < LangDefault || r.Lang > LangJapanese {
				badLang++
			}
			if p := prev[r.Lang]; p != nil {
				switch {
				case r.Start < p.Start:
					descending++
				case r.Start < p.End:
					overlapping++
				}
			}
			if r.End == r.Start {
				zeroDur++
			}
			prev[r.Lang] = r
		}
		actions += len(tab.Actions)
	}

	rawRecords, badLen := rawWalk(t, chunks)
	if rawRecords != records {
		t.Errorf("raw s16 len walk found %d records, decoder found %d", rawRecords, records)
	}
	for _, n := range []struct {
		what string
		got  int
	}{
		{"ofs_level != 0", badLevel},
		{"lang out of range", badLang},
		{"len != 16+strlen+1", badLen},
		{"descending", descending},
		{"overlapping", overlapping},
		{"pad >= 16", badPad},
	} {
		if n.got != 0 {
			t.Errorf("%s: %d", n.what, n.got)
		}
	}
	t.Logf("%d chunks, %d records (raw walk agrees: %d), %d actions", len(chunks), records, rawRecords, actions)
	t.Logf("ofs_level==0: %d/%d (100%%); descending %d; overlapping %d; zero-duration %d; len != 16+strlen+1: %d",
		len(chunks)-badLevel, len(chunks), descending, overlapping, zeroDur, badLen)
	t.Logf("records per lang: %v", langs)
	t.Logf("tag.option histogram: %v", optHist)
}

// rawWalk re-walks every chunk straight off the bytes -- s16 len at +0x0C, ofs_base from
// the tag, no validation -- so the invariant test is not just re-measuring the decoder's
// own opinion. Returns the record count and the number that break len == 16+strlen+1.
func rawWalk(t *testing.T, chunks []chunk) (records, badLen int) {
	t.Helper()
	for _, c := range chunks {
		size := int(int32(le.Uint32(c.body[4:])))
		end := 8 + size - int(c.option&0xFFFF)
		for off := 8; off < end; {
			l := int(int16(le.Uint16(c.body[off+12:])))
			if l < 17 || off+l > end {
				t.Fatalf("%s: record at +%d: len %d", c.file, off, l)
			}
			if bytes.IndexByte(c.body[off+16:off+l], 0) != l-17 {
				badLen++
			}
			off += l
			records++
		}
	}
	return records, badLen
}

func TestAuthorFromScratch(t *testing.T) {
	want := &Table{
		Records: []Record{
			{Start: Ticks(60), End: Ticks(150), Name: StrCode("demo_dummy"), Lang: LangEnglish,
				Text: "Snake, do you remember the sinking|of that tanker two years ago?"},
			{Start: Ticks(150), End: Ticks(240), Lang: LangEnglish, Text: "Two years..."},
		},
	}
	if err := want.Validate(); err != nil {
		t.Fatal(err)
	}

	body, err := want.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(body)%16 != 0 {
		t.Errorf("body is %d bytes, not 16-aligned", len(body))
	}
	if !bytes.HasPrefix(body, []byte("PACB")) {
		t.Errorf("body starts % x, want PACB", body[:4])
	}
	if want.Option() != 0 {
		t.Errorf("Option() = %#x, want 0 (no actions)", want.Option())
	}

	got, err := Decode(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 2 || len(got.Actions) != 0 {
		t.Fatalf("decoded %d records, %d actions", len(got.Records), len(got.Actions))
	}
	for i, r := range got.Records {
		if r != want.Records[i] {
			t.Errorf("record %d: got %+v, want %+v", i, r, want.Records[i])
		}
	}
	// 300 ticks = 1 s: frame 60 is 2.00 s in, like the real Big Shell briefing line.
	if got.Records[0].Start != 300 || got.Records[0].size() != 81 {
		t.Errorf("record 0: start %d len %d, want 300 / 81", got.Records[0].Start, got.Records[0].size())
	}
}

func TestAuthorWithActions(t *testing.T) {
	tab := &Table{
		Records: []Record{{Start: 0, End: 300, Lang: LangJapanese, Text: "字幕"}},
		Actions: []Action{
			{Start: 100, Name: StrCode("rai_def"), Value: 1, OptionLang: int32(LangJapanese) << 24},
			{Start: 200, Name: StrCode("rai_def"), Value: 2},
		},
	}
	if tab.Option() != 32 {
		t.Errorf("Option() = %d, want 32", tab.Option())
	}
	body, err := tab.Encode()
	if err != nil {
		t.Fatal(err)
	}

	// Inferred and tag-driven splits must agree.
	for _, got := range []*Table{mustDecode(t, body), mustDecodeOption(t, body, tab.Option())} {
		if len(got.Records) != 1 || len(got.Actions) != 2 {
			t.Fatalf("got %d records, %d actions", len(got.Records), len(got.Actions))
		}
		if got.Actions[0].Lang() != LangJapanese || got.Actions[1].Lang() != LangDefault {
			t.Errorf("action langs: %d, %d", got.Actions[0].Lang(), got.Actions[1].Lang())
		}
		if got.Records[0].Text != "字幕" {
			t.Errorf("text = %q", got.Records[0].Text)
		}
	}
}

func mustDecode(t *testing.T, body []byte) *Table {
	t.Helper()
	tab, err := Decode(body)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func mustDecodeOption(t *testing.T, body []byte, opt int32) *Table {
	t.Helper()
	tab, err := DecodeOption(body, opt)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func TestStrCode(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want int32
	}{
		{"demo_dummy", 6604458},  // 0x64C6AA
		{"rai_def", 0xC13A4E},    //
		{"rgb_alp_ovl", 7757344}, // hand-written in menu_sub.c:144
		{"", 1},                  // id 0 remaps to 1
	} {
		if got := StrCode(tc.s); got != tc.want {
			t.Errorf("StrCode(%q) = %d (%#x), want %d (%#x)", tc.s, got, got, tc.want, tc.want)
		}
	}
	if _, err := StrCodeStrict("スネーク"); err == nil {
		t.Error("StrCodeStrict: want an error on non-ASCII")
	}
}

func TestDecodeErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"short", []byte{1, 2, 3}},
		{"bad magic", []byte("XXXX\x00\x00\x00\x00")},
		{"size overruns", []byte("PACB\xff\x00\x00\x00")},
	} {
		if _, err := Decode(tc.body); err == nil {
			t.Errorf("%s: want an error", tc.name)
		}
	}
	if _, err := DecodeOption([]byte("PACB\x00\x00\x00\x00"), 1<<16); err == nil {
		t.Error("ofs_level != 0: want an error")
	}
}

func TestEncodeErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		tab  Table
	}{
		{"NUL in text", Table{Records: []Record{{Text: "a\x00b"}}}},
		{"lang out of range", Table{Records: []Record{{Lang: 8}}}},
		{"text overflows s16 len", Table{Records: []Record{{Text: string(make([]byte, 1<<15))}}}},
	} {
		if _, err := tc.tab.Encode(); err == nil {
			t.Errorf("%s: want an error", tc.name)
		}
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		tab  Table
		ok   bool
	}{
		{"same lang, touching", Table{Records: []Record{
			{Start: 0, End: 300, Lang: LangEnglish}, {Start: 300, End: 600, Lang: LangEnglish}}}, true},
		{"same lang, overlapping", Table{Records: []Record{
			{Start: 0, End: 300, Lang: LangEnglish}, {Start: 299, End: 600, Lang: LangEnglish}}}, false},
		{"different langs may interleave", Table{Records: []Record{
			{Start: 0, End: 300, Lang: LangEnglish}, {Start: 0, End: 300, Lang: LangFrench}}}, true},
		{"end before start", Table{Records: []Record{{Start: 300, End: 0, Lang: LangEnglish}}}, false},
		{"actions out of order", Table{Actions: []Action{{Start: 300}, {Start: 0}}}, false},
	} {
		if err := tc.tab.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: Validate() = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}
