package catalog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/sdt"
	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// mcRoot is the Master Collection install; override with MGS2_DIR.
func mcRoot() string { return testenv.GameDir() }

// corpus is the shipped cutscene streams only: */codec/_bp holds GCL bytecode, not streams,
// and the *.sdt.orig / *.prev backups next to them are not Konami's.
func corpus(t testing.TB) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{`us\demo2\_bp`, `us\demo\_bp`, `jp\demo2\_bp`, `jp\demo\_bp`} {
		m, _ := filepath.Glob(filepath.Join(mcRoot(), dir, "*.sdt"))
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Skipf("no demo corpus under %s (set MGS2_DIR)", mcRoot())
	}
	return files
}

func load(t testing.TB) *Catalog {
	t.Helper()
	c, err := Load(testenv.DataFile(t, "effects.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

// eachEffectPacket walks every EFFECT packet in every shipped demo.
func eachEffectPacket(t testing.TB, fn func(file string, p *sdt.Packet)) {
	t.Helper()
	for _, f := range corpus(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		s, err := sdt.Decode(raw)
		if err != nil {
			t.Fatalf("%s: decode: %v", f, err)
		}
		for i := range s.Chunks {
			c := &s.Chunks[i]
			if c.Type() != sdt.CDemo {
				continue
			}
			fr, err := sdt.DecodeFrame(c.Payload)
			if err != nil {
				t.Fatalf("%s chunk %d: %v", f, i, err)
			}
			for j := range fr.Packets {
				if fr.Packets[j].Kind() == sdt.PEffect {
					fn(f, &fr.Packets[j])
				}
			}
		}
	}
}

func TestLoad(t *testing.T) {
	c := load(t)
	all := c.All()
	if len(all) != 245 {
		t.Fatalf("got %d effects, want 245", len(all))
	}

	exec := map[int]int{}
	for _, e := range all {
		exec[e.ExecType]++
	}
	if exec[1] != 222 || exec[0] != 23 {
		t.Fatalf("exec distribution %v, want 222 x exec1 / 23 x exec0", exec)
	}

	lens := c.ByID(0x0f)
	if lens == nil || lens.CName != "NewLensFlare_Demo" {
		t.Fatalf("ByID(0x0f) = %v, want NewLensFlare_Demo", lens)
	}
	if c.ByEffectID(0x0100000f) != lens {
		t.Fatal("ByEffectID(0x0100000f) did not resolve to the same entry")
	}
	if c.ByEffectID(0x0f) != nil {
		t.Fatal("ByEffectID accepted an untagged id")
	}
	if c.ByEffectID(0x0200000f) != nil {
		t.Fatal("ByEffectID accepted a foreign class tag")
	}

	// The d-index has holes; an editor that renumbers 0..N-1 corrupts these.
	holes := 0
	for _, e := range all {
		for i, p := range e.Params {
			if p.Name != fmt.Sprintf("d%d", i) {
				holes++
				break
			}
		}
	}
	t.Logf("effects=245 exec1=%d exec0=%d, %d effects have a non-contiguous d-index", exec[1], exec[0], holes)
}

// TestPacketSizeAgainstCorpus is the one that matters: the bake formula has to reproduce the
// size of every effect packet Konami shipped. A mismatch means the editor would emit a packet
// of the wrong length and desync the walker.
func TestPacketSizeAgainstCorpus(t *testing.T) {
	c := load(t)

	var total, match, unknown int
	type combo struct {
		id   int32
		exec int32
	}
	seen := map[combo]int{}
	bad := map[string]int{}
	execMismatch := map[int32]int{}

	eachEffectPacket(t, func(file string, p *sdt.Packet) {
		total++
		eff, ok := p.Effect()
		if !ok {
			bad[fmt.Sprintf("%s: short effect body", filepath.Base(file))]++
			return
		}
		e := c.ByEffectID(eff.EffectID)
		if e == nil {
			unknown++
			bad[fmt.Sprintf("unknown effect_id 0x%08x", eff.EffectID)]++
			return
		}
		seen[combo{e.ID, eff.ExecType}]++
		if int(eff.ExecType) != e.ExecType {
			execMismatch[e.ID]++
		}
		if got, want := 16+len(p.Body), e.PacketSize(); got != want {
			bad[fmt.Sprintf("0x%08x %s: shipped size %d, formula %d", eff.EffectID, e.CName, got, want)]++
			return
		}
		match++
	})

	ids := map[int32]bool{}
	for k := range seen {
		ids[k.id] = true
	}
	t.Logf("effect packets=%d  size match=%d (%.4f%%)  unknown ids=%d",
		total, match, 100*float64(match)/float64(total), unknown)
	t.Logf("distinct shipped (effect_id, exec_type) combos=%d over %d distinct ids", len(seen), len(ids))

	if len(execMismatch) != 0 {
		t.Errorf("packets whose exec_type disagrees with the catalog: %v", execMismatch)
	}
	if len(bad) != 0 {
		keys := make([]string, 0, len(bad))
		for k := range bad {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.Errorf("%s (x%d)", k, bad[k])
		}
	}
	if match != total {
		t.Fatalf("%d of %d packets failed the bake formula", total-match, total)
	}
}

// TestLensFlareEndToEnd decodes a real 0x0100000f packet: exec 1, Data{int d0; FVECTOR d1},
// packet 80 bytes, life at +32, d0 at +48, d1 at +64 (12 bytes of align pad before it).
func TestLensFlareEndToEnd(t *testing.T) {
	c := load(t)
	e := c.ByID(0x0f)
	if e.PacketSize() != 80 || e.PacketBodySize != 64 || e.DataStructSize != 32 || e.DataBaseOffset != 16 {
		t.Fatalf("geometry: size=%d body=%d data=%d base=%d", e.PacketSize(), e.PacketBodySize, e.DataStructSize, e.DataBaseOffset)
	}
	d0, d1 := e.Param("d0"), e.Param("d1")
	if d0 == nil || d1 == nil || d0.ByteOffset != 0 || d1.ByteOffset != 16 || d1.CType != TFVector {
		t.Fatalf("params: %+v %+v", d0, d1)
	}

	raw, err := os.ReadFile(filepath.Join(mcRoot(), `us\demo\_bp\p001_01_p01.sdt`))
	if err != nil {
		t.Skipf("no p001_01_p01.sdt: %v", err)
	}
	s, err := sdt.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}

	var body []byte
	for i := range s.Chunks {
		if s.Chunks[i].Type() != sdt.CDemo {
			continue
		}
		fr, err := sdt.DecodeFrame(s.Chunks[i].Payload)
		if err != nil {
			t.Fatal(err)
		}
		for j := range fr.Packets {
			p := &fr.Packets[j]
			if p.Kind() == sdt.PEffect && c.ForBody(p.Body) == e {
				body = p.Body
			}
		}
		if body != nil {
			break
		}
	}
	if body == nil {
		t.Fatal("no NewLensFlare_Demo packet in p001_01_p01.sdt")
	}

	if got := 16 + len(body); got != 80 {
		t.Fatalf("packet size %d, want 80", got)
	}
	life, ok := e.Life(body)
	if !ok || life != 612 {
		t.Fatalf("life = %d (%v), want 612", life, ok)
	}
	name, err := e.GetInt(body, d0, 0)
	if err != nil || uint32(name) != 0x00fe5a24 {
		t.Fatalf("d0 = 0x%08x (%v), want 0x00fe5a24", uint32(name), err)
	}
	var center [4]float64
	for i := range center {
		if center[i], err = e.GetFloat(body, d1, i); err != nil {
			t.Fatal(err)
		}
	}
	want := [4]float64{-10014.33, -8209.23, 70861.54, 0}
	for i := range want {
		if math.Abs(center[i]-want[i]) > 0.01 {
			t.Fatalf("d1 = %v, want ~%v", center, want)
		}
	}

	// The 12 bytes between d0 and d1 are FVECTOR alignment pad, and nothing may live there.
	if pad := body[offData+e.DataBaseOffset+4 : offData+e.DataBaseOffset+16]; !bytes.Equal(pad, make([]byte, 12)) {
		t.Errorf("align pad is not zero: % x", pad)
	}
	t.Logf("p001_01_p01 lens flare: life=%d frames  name=0x%08x  center=(%.2f, %.2f, %.2f, %.1f)",
		life, uint32(name), center[0], center[1], center[2], center[3])
}

// TestBodyRoundTrip: get every param of a real packet and set it straight back. If any
// accessor has the wrong offset, size or type, the body stops being byte-identical.
// char[128] is excluded on purpose -- SetString NUL-pads the tail (see TestStringTails).
func TestBodyRoundTrip(t *testing.T) {
	c := load(t)

	perEffect := map[int32]int{}
	var checked, params int

	eachEffectPacket(t, func(file string, p *sdt.Packet) {
		e := c.ForBody(p.Body)
		if e == nil || 16+len(p.Body) != e.PacketSize() {
			return
		}
		perEffect[e.ID]++
		checked++

		orig := append([]byte(nil), p.Body...)
		body := append([]byte(nil), p.Body...)
		if life, ok := e.Life(body); ok {
			if err := e.SetLife(body, life); err != nil {
				t.Fatalf("%s 0x%x: SetLife: %v", filepath.Base(file), e.ID, err)
			}
		}
		for i := range e.Params {
			pm := &e.Params[i]
			if pm.CType == TChar {
				continue
			}
			params++
			for comp := 0; comp < pm.Components(); comp++ {
				switch pm.CType {
				case TFloat, TFVector:
					v, err := e.GetFloat(body, pm, comp)
					if err != nil {
						t.Fatalf("0x%x %s: GetFloat: %v", e.ID, pm.Name, err)
					}
					// NaN payloads exist in retail data; float32(float64(x)) is exact either way.
					if err := e.SetFloat(body, pm, comp, v); err != nil {
						t.Fatalf("0x%x %s: SetFloat: %v", e.ID, pm.Name, err)
					}
				default:
					v, err := e.GetInt(body, pm, comp)
					if err != nil {
						t.Fatalf("0x%x %s: GetInt: %v", e.ID, pm.Name, err)
					}
					if err := e.SetInt(body, pm, comp, v); err != nil {
						t.Fatalf("0x%x %s: SetInt: %v", e.ID, pm.Name, err)
					}
				}
			}
		}
		if !bytes.Equal(body, orig) {
			t.Fatalf("%s: 0x%08x %s: body changed under get/set\n orig % x\n got  % x",
				filepath.Base(file), e.EffectID, e.CName, orig, body)
		}
	})
	t.Logf("round-tripped %d packets across %d effect ids (%d param reads), byte-identical",
		checked, len(perEffect), params)
}

// TestPadAndTailPreserved: Set* must never touch pad[2] (uninitialised retail garbage that has
// to survive a re-bake) nor the alignment tail.
func TestPadAndTailPreserved(t *testing.T) {
	c := load(t)
	e := c.ByID(0x0f)

	body := e.NewBody(120)
	binary.LittleEndian.PutUint32(body[0x08:], 0xdeadbeef) // stand in for the retail garbage
	binary.LittleEndian.PutUint32(body[0x0c:], 0xfeedface)

	if err := e.SetFloat(body, e.Param("d1"), 2, 1234.5); err != nil {
		t.Fatal(err)
	}
	if err := e.SetInt(body, e.Param("d0"), 0, 0x11223344); err != nil {
		t.Fatal(err)
	}
	if err := e.SetLife(body, 613); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(body[0x08:]); got != 0xdeadbeef {
		t.Errorf("pad[0] clobbered: 0x%08x", got)
	}
	if got := binary.LittleEndian.Uint32(body[0x0c:]); got != 0xfeedface {
		t.Errorf("pad[1] clobbered: 0x%08x", got)
	}
	if got := binary.LittleEndian.Uint32(body[0x14:]); got != 0 { // data[1], reserved
		t.Errorf("reserved word clobbered: 0x%08x", got)
	}
	v, err := e.GetFloat(body, e.Param("d1"), 2)
	if err != nil || v != 1234.5 {
		t.Fatalf("d1.z = %v (%v)", v, err)
	}

	// Exec 0 has no life word: writing one would land on the first Data field.
	amb := c.ByID(0x06)
	if _, ok := amb.Life(amb.NewBody(0)); ok {
		t.Error("exec 0 reported a life")
	}
	if err := amb.SetLife(amb.NewBody(0), 10); err == nil {
		t.Error("SetLife on an exec 0 effect should fail")
	}
}

// TestStringTails measures the one place a Set* is not byte-preserving: DM_DebugPrint's
// char[128]. SetString NUL-fills the whole array, so a stale tail in retail data is lost.
func TestStringTails(t *testing.T) {
	c := load(t)
	dbg := c.ByID(0xfff00)
	if dbg == nil || dbg.CName != "DM_DebugPrint" {
		t.Fatal("0xfff00 is not DM_DebugPrint")
	}
	p := dbg.Param("d0")
	if p.CType != TChar || p.ArrayLen != 128 {
		t.Fatalf("d0 = %+v, want char[128]", p)
	}

	var n, dirty int
	var sample string
	eachEffectPacket(t, func(file string, pk *sdt.Packet) {
		if c.ForBody(pk.Body) != dbg {
			return
		}
		n++
		s, err := dbg.GetString(pk.Body, p)
		if err != nil {
			t.Fatalf("GetString: %v", err)
		}
		if sample == "" {
			sample = s
		}
		body := append([]byte(nil), pk.Body...)
		if err := dbg.SetString(body, p, s); err != nil {
			t.Fatalf("SetString: %v", err)
		}
		if !bytes.Equal(body, pk.Body) {
			dirty++
		}
	})
	t.Logf("DM_DebugPrint packets=%d  bodies not byte-stable under GetString/SetString=%d  first string=%q", n, dirty, sample)

	// Whatever the retail tails look like, the string itself must survive.
	body := dbg.NewBody(0)
	if err := dbg.SetString(body, p, "hello world"); err != nil {
		t.Fatal(err)
	}
	got, err := dbg.GetString(body, p)
	if err != nil || got != "hello world" {
		t.Fatalf("GetString = %q (%v)", got, err)
	}
	if err := dbg.SetString(body, p, string(make([]byte, 128))); err == nil {
		t.Error("SetString accepted 128 chars into a char[128]")
	}
}

// TestNewBodySizes: a freshly authored packet must be exactly the size the game expects.
func TestNewBodySizes(t *testing.T) {
	c := load(t)
	for _, e := range c.All() {
		body := e.NewBody(42)
		if len(body) != e.PacketBodySize || (16+len(body))%16 != 0 {
			t.Fatalf("0x%x %s: NewBody is %d bytes, catalog says %d", e.ID, e.CName, len(body), e.PacketBodySize)
		}
		if got := int32(binary.LittleEndian.Uint32(body[0:])); got != e.EffectID {
			t.Fatalf("0x%x: NewBody effect_id = 0x%08x", e.ID, got)
		}
		if got := int(int32(binary.LittleEndian.Uint32(body[4:]))); got != e.ExecType {
			t.Fatalf("0x%x: NewBody exec_type = %d", e.ID, got)
		}
		if life, ok := e.Life(body); ok != (e.ExecType == 1) || (ok && life != 42) {
			t.Fatalf("0x%x: life = %d (%v)", e.ID, life, ok)
		}
	}
}
