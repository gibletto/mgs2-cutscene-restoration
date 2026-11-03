package scene

import (
	"os"
	"path/filepath"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/catalog"
	"mgs2-restoration/cutscene-studio/internal/testenv"
)

func testDemo(t *testing.T) (string, []byte, *catalog.Catalog) {
	t.Helper()
	root := testenv.GameDir()
	// The pristine backup deploy leaves next to a replaced file (set MGS2_DIR).
	p := filepath.Join(root, `us\demo2\_bp\p080_13_p08.sdt.orig`)
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("no corpus: %v", err)
	}
	cat, err := catalog.Load(testenv.DataFile(t, "effects.json"))
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return p, raw, cat
}

func diffRuns(a, b []byte) (n int, first int) {
	first = -1
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if first < 0 {
				first = i
			}
			n++
		}
	}
	return n, first
}

// TestCleanBakeIsIdentical is the easy half: an untouched demo bakes back to Konami's bytes.
func TestCleanBakeIsIdentical(t *testing.T) {
	p, raw, cat := testDemo(t)
	sc, err := Import(p, raw, cat)
	if err != nil {
		t.Fatal(err)
	}
	out, err := sc.Bake(cat)
	if err != nil {
		t.Fatal(err)
	}
	if n, at := diffRuns(raw, out); n != 0 || len(out) != len(raw) {
		t.Fatalf("clean bake differs: %d bytes, first at 0x%x (sizes %d vs %d)", n, at, len(raw), len(out))
	}
}

// TestDirtyBakeTouchesNothingElse is the half that actually matters, and the one the
// original round-trip test missed: a clean scene short-circuits straight to the base
// bytes, so it never exercised the rebuild. Here we force the rebuild path WITHOUT
// changing any value -- so the output must still be byte-identical.
//
// This is what caught three real bugs: caption times being round-tripped through frames
// (208 ticks -> 205), terminator packet _id counters being zeroed, and effect params
// being addressed by the wrong channel name.
func TestDirtyBakeTouchesNothingElse(t *testing.T) {
	p, raw, cat := testDemo(t)
	sc, err := Import(p, raw, cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.Effects) == 0 {
		t.Fatal("no effects to dirty")
	}
	// Force the full rebuild without altering a single authored value.
	sc.Effects[0].Dirty = true
	if !sc.Dirty() {
		t.Fatal("scene should be dirty")
	}

	out, err := sc.Bake(cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(raw) {
		t.Fatalf("size changed: %d -> %d", len(raw), len(out))
	}
	if n, at := diffRuns(raw, out); n != 0 {
		t.Fatalf("a rebuild with no value change altered %d bytes (first at 0x%x); "+
			"the passthrough is leaking", n, at)
	}
}

// TestEffectParamEdit proves an edited parameter actually reaches the bytes, and that it
// is the ONLY thing that moves. A value typed into a field is recorded as a single key;
// it must hold across the whole channel, not fall back to the imported data.
func TestEffectParamEdit(t *testing.T) {
	p, raw, cat := testDemo(t)
	sc, err := Import(p, raw, cat)
	if err != nil {
		t.Fatal(err)
	}

	// Pick an exec-1 effect with at least one scalar int/float param.
	var inst *EffectInstance
	var param *catalog.Param
	for _, e := range sc.Effects {
		def := cat.ByEffectID(e.EffectID)
		if def == nil || def.ExecType != 1 {
			continue
		}
		for i := range def.Params {
			pr := &def.Params[i]
			if pr.CType == "int" || pr.CType == "float" {
				inst, param = e, pr
				break
			}
		}
		if inst != nil {
			break
		}
	}
	if inst == nil {
		t.Skip("no exec-1 effect with a scalar param")
	}

	before, err := cat.ByEffectID(inst.EffectID).GetFloat(inst.Raw[inst.Start], param, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := before + 70

	inst.Params[param.Name] = &Channel{
		Keys:  []Key{{Frame: 0, Value: want, Interp: Hold}}, // one key = a value, held everywhere
		Dirty: true,
	}
	inst.Dirty = true

	out, err := sc.Bake(cat)
	if err != nil {
		t.Fatal(err)
	}

	// Re-import the baked file and read the value back out of the actual bytes.
	sc2, err := Import("baked", out, cat)
	if err != nil {
		t.Fatal(err)
	}
	var got float64
	found := false
	for _, e := range sc2.Effects {
		if e.EffectID != inst.EffectID || e.Start != inst.Start {
			continue
		}
		got, err = cat.ByEffectID(e.EffectID).GetFloat(e.Raw[e.Start], param, 0)
		if err != nil {
			t.Fatal(err)
		}
		found = true
		break
	}
	if !found {
		t.Fatal("the edited effect vanished from the bake")
	}
	if got != want {
		t.Fatalf("param %s (%s): baked %v, want %v (was %v) -- the edit did not reach the bytes",
			param.Name, param.LabelEN, got, want, before)
	}

	// And nothing else moved: the only diff should be inside that one packet.
	n, at := diffRuns(raw, out)
	if n == 0 {
		t.Fatal("nothing changed at all")
	}
	if n > 8 { // one int/float, plus whatever the value quantises to
		t.Fatalf("edit rippled: %d bytes changed (first at 0x%x); expected only the one param", n, at)
	}
	t.Logf("edited %s: %v -> %v; %d bytes changed at 0x%x", param.Name, before, got, n, at)
}
