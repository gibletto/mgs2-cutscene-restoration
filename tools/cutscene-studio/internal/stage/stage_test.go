package stage

import (
	"os"
	"strings"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// The d080p07 numbers below are ground truth twice over: they are what Konami's
// P080_13_p07b.w51a.gcl says, and they are the exact strings the Manhattan reposition
// script searches for when it moves the city ("-p 0,0,0", "-size 400000 400000",
// "-pos 60000,0,-200000"). If this test drifts, the parse broke.

var (
	testGame     = testenv.GameDir()
	testGameData = testenv.GameDataDir()
)

func haveGame(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(testGame); testGame == "" || err != nil {
		t.Skip("game install not present (set MGS2_DIR)")
	}
}

// haveTables skips when the generated stage tables are absent from the data dir.
func haveTables(t *testing.T) {
	t.Helper()
	testenv.DataFile(t, stagesFile)
	testenv.DataFile(t, demosFile)
}

func TestForDemo(t *testing.T) {
	haveTables(t)
	for demo, want := range map[string]string{
		"p080_13_p08":                  "d080p07", // the Manhattan crash -- NOT d080p08
		"us/demo2/_bp/p080_13_p08.sdt": "d080p07",
		"p080_13_p07":                  "d080p06",
		"p080_14_p08":                  "d080p08",
		"p001_01_p01":                  "d001p01",
		"p082_01_p01":                  "d082p01",
	} {
		got, err := ForDemo(demo, "")
		if err != nil {
			t.Errorf("ForDemo(%q): %v", demo, err)
			continue
		}
		if got != want {
			t.Errorf("ForDemo(%q) = %q, want %q", demo, got, want)
		}
	}
	if _, err := ForDemo("no_such_demo", ""); err == nil {
		t.Error("expected an error for an unknown demo")
	}
}

func TestLoadD080p07(t *testing.T) {
	haveGame(t)
	haveTables(t)
	s, err := Load("d080p07", testGame, testGameData, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Partial {
		t.Error("d080p07 should have full scenario data")
	}
	if len(s.Placements) < 17 {
		t.Errorf("got %d placements, want >= 17", len(s.Placements))
	}

	by := map[string]Placement{}
	for _, p := range s.Placements {
		by[p.Name] = p
	}

	// The Manhattan skyline: authored at the origin, scaled 10x, and switched OFF at load.
	// That "created but off" state is the restoration finding -- Konami built the city and
	// never turned it on.
	for _, n := range []string{"w60a_1", "w60a_2", "w60a_3", "w60a_4", "w60a_5", "w60a_6", "w60a_7", "w60a_8"} {
		p, ok := by[n]
		if !ok {
			t.Fatalf("%s missing", n)
		}
		if p.Pos != [3]float32{0, 0, 0} {
			t.Errorf("%s pos = %v, want origin", n, p.Pos)
		}
		if p.Scale != [3]float32{10, 10, 10} {
			t.Errorf("%s scale = %v, want 10 (-s 1000 / 100)", n, p.Scale)
		}
		if !p.Created {
			t.Errorf("%s should be created at stage init", n)
		}
		if p.Visible {
			t.Errorf("%s should be switched OFF at stage init", n)
		}
		if p.CacheID == 0 {
			t.Errorf("%s did not resolve to a cache id", n)
		}
	}
	if got := by["w60a_1"].CacheID; got != 0x799102 {
		t.Errorf("w60a_1 cache id = %06x, want 799102", got)
	}

	// The wrecked land is in a proc stage init never calls: present, but never created.
	w, ok := by["vw60a_9"]
	if !ok {
		t.Fatal("vw60a_9 (wrecked Manhattan) missing")
	}
	if w.Created || w.Visible {
		t.Errorf("vw60a_9 created=%v visible=%v, want false/false", w.Created, w.Visible)
	}

	// The Verrazano bridge is what the camera actually flies past: ON at load.
	b, ok := by["w10a_1_hi_reso"]
	if !ok {
		t.Fatal("w10a_1_hi_reso missing")
	}
	if !b.Created || !b.Visible {
		t.Error("the bridge should be on at stage init")
	}
	if by["w10a_8"].Scale != [3]float32{1, 1, 1} { // -s 100 -> 1.0, the odd one out
		t.Errorf("w10a_8 scale = %v, want 1", by["w10a_8"].Scale)
	}

	// Sea plane.
	if s.Sea == nil {
		t.Fatal("no sea")
	}
	if s.Sea.Pos != [3]float32{60000, 0, -200000} {
		t.Errorf("sea pos = %v, want (60000,0,-200000)", s.Sea.Pos)
	}
	if s.Sea.Size != [2]float32{400000, 400000} {
		t.Errorf("sea size = %v, want 400000x400000", s.Sea.Size)
	}

	// Sky: base colour + two sky-wall meshes, no cloud layers.
	if s.Sky == nil || s.Sky.Kind != "generic_sky" {
		t.Fatal("no generic sky")
	}
	walls, _ := s.Sky.Params["walls"].([]any)
	if len(walls) != 2 {
		t.Fatalf("got %d sky walls, want 2", len(walls))
	}
	w0 := walls[0].(map[string]any)
	if w0["name"] != "w60a_sky_wall1" {
		t.Errorf("sky wall 0 = %v, want w60a_sky_wall1", w0["name"])
	}
	if id, _ := w0["cacheId"].(float64); int32(id) != 0xea2e47 {
		t.Errorf("sky wall 0 cache id = %x, want ea2e47", int32(id))
	}

	if s.Fog == nil || s.Fog.Far != 290000 {
		t.Errorf("fog = %+v, want far 290000", s.Fog)
	}
	if s.HZX != "d00a3" || s.LT2 != "w62a_scale10_am700" {
		t.Errorf("hzx=%q lt2=%q", s.HZX, s.LT2)
	}

	// The mid-shot world switch the cutscene can fire.
	if len(s.Toggles) == 0 {
		t.Error("expected the Verrazano->Manhattan toggle")
	}
	if len(s.Assets) < 50 {
		t.Errorf("got %d manifest assets, want >= 50", len(s.Assets))
	}
}

// A stage asset's cache id is the StrCode of its name. These pairs are lifted straight
// out of d080p07's manifest.
func TestStrCodeIsTheCacheID(t *testing.T) {
	for name, want := range map[string]int32{
		"amg_def":            0xa13626,
		"w60a_1":             0x799102,
		"w10a_5":             0x299106,
		"vw60a_9":            0x79ae8a,
		"w10a_1_hi_reso":     0x2fdf38,
		"w60a_sky_wall1":     0xea2e47,
		"sol_onemesh_mt":     0xa3f5b0,
		"d00a3":              0x58cc59,
		"w62a_scale10_am700": 0x4271af,
	} {
		if got := StrCode(name); got != want {
			t.Errorf("StrCode(%q) = %06x, want %06x", name, got, want)
		}
	}
}

// Line splicing is where this parser is easiest to get wrong, and both mistakes were real:
// a commented-out argument leaves a blank line mid-statement (drop the splice and you lose
// the rest), while a directive must END one (swallow the "#else" and its whole branch dies).
func TestJoinContinuations(t *testing.T) {
	src := stripComments(strings.Join([]string{
		`chara demo x \`,
		`//	-i t:dead \`,
		`	-i t:alive \`,
		`	-p proc1`,
		`#if FOO`,
		`chara a b \`,
		`	-m m1 \`,
		`#else`,
		`chara c d`,
		`#endif`,
	}, "\n"))
	got := joinContinuations(src)

	var joined, directives []string
	for _, l := range got {
		if s := strings.TrimSpace(l); strings.HasPrefix(s, "#") {
			directives = append(directives, s)
		} else if s != "" {
			joined = append(joined, s)
		}
	}
	if len(joined) == 0 || !strings.Contains(joined[0], "-i t:alive") || !strings.Contains(joined[0], "-p proc1") {
		t.Errorf("splice across a commented-out arg failed: %q", joined)
	}
	if strings.Contains(joined[0], "dead") {
		t.Error("a commented-out argument leaked into the statement")
	}
	want := []string{"#if FOO", "#else", "#endif"}
	if len(directives) != 3 || directives[0] != want[0] || directives[1] != want[1] || directives[2] != want[2] {
		t.Errorf("directives = %q, want %q (a splice must not eat one)", directives, want)
	}
}

// A stage with a manifest but no parsed scenario must still be useful.
func TestLoadPartial(t *testing.T) {
	haveGame(t)
	s, err := Load("d00t", testGame, testGameData, "")
	if err != nil {
		t.Skipf("d00t not available: %v", err)
	}
	if len(s.Assets) == 0 {
		t.Error("a loaded stage should always list its manifest assets")
	}
}
