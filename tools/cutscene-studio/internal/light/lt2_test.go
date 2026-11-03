package light

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// The Bluepoint GameData tree. Override with MGS2_GAMEDATA. Tests skip without it.
func gameData(t *testing.T) string {
	t.Helper()
	d := testenv.GameDataDir()
	if _, err := os.Stat(filepath.Join(d, "assets", "lt2")); d == "" || err != nil {
		t.Skipf("no GameData lt2 tree at %q (set MGS2_GAMEDATA)", d)
	}
	return d
}

func allLT2(t *testing.T) []string {
	t.Helper()
	fs, err := filepath.Glob(filepath.Join(gameData(t), "assets", "lt2", "*", "*.lt2"))
	if err != nil || len(fs) == 0 {
		t.Fatalf("no .lt2 files: %v", err)
	}
	sort.Strings(fs)
	return fs
}

// Every shipped .lt2 must parse, and every value must be sane.
func TestParseAll(t *testing.T) {
	files := allLT2(t)
	kinds := map[Kind]int{}
	var totalLights, maxLights int
	var maxColor float32

	for _, f := range files {
		s, err := Load(f)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		n := 0
		for _, l := range s.Lights {
			kinds[l.Kind]++
			if l.Kind != Ambient && l.Kind != Parallel {
				n++
			}
			for _, c := range l.Color {
				if c < 0 || c > 2.0 || math.IsNaN(float64(c)) {
					t.Errorf("%s: colour %v out of range", filepath.Base(f), l.Color)
				}
				if c > maxColor {
					maxColor = c
				}
			}
			// A light at the origin or at 1e9 would mean the layout is wrong.
			for _, p := range l.Pos {
				if math.IsNaN(float64(p)) || math.Abs(float64(p)) > 1e6 {
					t.Errorf("%s: %s pos %v implausible", filepath.Base(f), l.Kind, l.Pos)
				}
			}
			// Blackpoint Range is a ramp slope, not a reach -- it is legitimately huge
			// (see Light.Range). Its reach is the bound box, which we check instead.
			lim := float32(1e6)
			if l.Kind == BlackPoint {
				lim = 4e6
				for i := 0; i < 3; i++ {
					if l.BoundMax[i] < l.BoundMin[i] || l.BoundMax[i]-l.BoundMin[i] > 5e5 {
						t.Errorf("%s: blackpoint bound %v..%v implausible", filepath.Base(f), l.BoundMin, l.BoundMax)
					}
				}
			}
			if l.Range < 0 || l.Range > lim {
				t.Errorf("%s: %s range %v implausible", filepath.Base(f), l.Kind, l.Range)
			}
			if l.Kind == Parallel {
				d := math.Sqrt(float64(l.Dir[0]*l.Dir[0] + l.Dir[1]*l.Dir[1] + l.Dir[2]*l.Dir[2]))
				if math.Abs(d-1) > 0.02 {
					t.Errorf("%s: parallel dir not unit (len %v)", filepath.Base(f), d)
				}
			}
		}
		totalLights += n
		if n > maxLights {
			maxLights = n
		}
	}
	t.Logf("parsed %d/%d files, %d lights (max %d in one file), peak colour %.2f",
		len(files), len(files), totalLights, maxLights, maxColor)
	t.Logf("kinds: %v", kinds)
	if maxColor <= 1.0 {
		t.Errorf("no colour exceeded 1.0 -- the 128==1.0 scale would be wrong")
	}
}

// force == (0.30r + 0.59g + 0.11b)/255 (fmt_lit.h, lt2_conv/convert.c:178). If our field
// offsets or colour scale were wrong this identity would not hold. It is the layout proof.
func TestForceMatchesColour(t *testing.T) {
	checked := 0
	for _, f := range allLT2(t) {
		s, err := Load(f)
		if err != nil {
			continue
		}
		for _, l := range s.Lights {
			if l.Kind != Point && l.Kind != Parallel {
				continue
			}
			// Color is raw/128, so raw = Color*128.
			r, g, b := l.Color[0]*128, l.Color[1]*128, l.Color[2]*128
			want := (0.30*r + 0.59*g + 0.11*b) / 255
			if math.Abs(float64(want-l.Intensity)) > 0.01 {
				t.Fatalf("%s: %s force %.4f but colour implies %.4f (layout wrong)",
					filepath.Base(f), l.Kind, l.Intensity, want)
			}
			checked++
		}
	}
	t.Logf("force identity holds for %d lights", checked)
}

func TestFind(t *testing.T) {
	gd := gameData(t)
	for _, name := range []string{"w11", "w62a_scale10_am700", "w11.lt2"} {
		p, err := Find(name, gd)
		if err != nil {
			t.Fatalf("Find(%q): %v", name, err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("Find(%q) -> %s: %v", name, p, err)
		}
	}
	if _, err := Find("no_such_lt2", gd); err == nil {
		t.Error("expected an error for a missing lt2")
	}
}

// d001p02 is the Tanker hold -- the interior that currently renders black. Its lights are
// the fix. d080p07 is Manhattan/harbour.
func TestStages(t *testing.T) {
	gd := gameData(t)
	for _, tc := range []struct {
		stage, lt2 string
		// the map areas sit in this ballpark; lights must land near the geometry
		xzMax, yMin, yMax float32
	}{
		{"d001p02", "w11", 40000, -60000, 5000},
		{"d080p07", "w62a_scale10_am700", 40000, -5000, 30000},
	} {
		p, err := Find(tc.lt2, gd)
		if err != nil {
			t.Fatalf("%s: %v", tc.stage, err)
		}
		s, err := Load(p)
		if err != nil {
			t.Fatalf("%s: %v", tc.stage, err)
		}

		kinds := map[Kind]int{}
		var bg int
		var minP, maxP [3]float32
		first := true
		for _, l := range s.Lights {
			kinds[l.Kind]++
			if l.LitsBG() {
				bg++
			}
			if l.Kind == Ambient || l.Kind == Parallel {
				continue
			}
			for i := 0; i < 3; i++ {
				if first || l.Pos[i] < minP[i] {
					minP[i] = l.Pos[i]
				}
				if first || l.Pos[i] > maxP[i] {
					maxP[i] = l.Pos[i]
				}
			}
			first = false
		}
		if len(s.Lights) < 3 {
			t.Fatalf("%s: only %d lights", tc.stage, len(s.Lights))
		}
		t.Logf("%s (%s): %v, %d BG-lighting", tc.stage, tc.lt2, kinds, bg)
		t.Logf("  ambient=%v parallel=%v raw=%v", s.Lights[0].Color, s.Lights[1].Color, s.Raw["parallelRaw"])
		t.Logf("  pos x[%.0f..%.0f] y[%.0f..%.0f] z[%.0f..%.0f]",
			minP[0], maxP[0], minP[1], maxP[1], minP[2], maxP[2])

		for i, axis := range []int{0, 2} {
			if math.Abs(float64(minP[axis])) > float64(tc.xzMax) || math.Abs(float64(maxP[axis])) > float64(tc.xzMax) {
				t.Errorf("%s: axis %d out of stage bounds [%.0f..%.0f]", tc.stage, i, minP[axis], maxP[axis])
			}
		}
		if minP[1] < tc.yMin || maxP[1] > tc.yMax {
			t.Errorf("%s: y [%.0f..%.0f] outside expected [%.0f..%.0f]",
				tc.stage, minP[1], maxP[1], tc.yMin, tc.yMax)
		}
		if bg == 0 {
			t.Errorf("%s: no BG lights -- the map would render black", tc.stage)
		}
	}
}

// The falloff is linear with a 1.5x centre boost, not inverse-square. r_range (== Range/2)
// is where brightness is 0.75, per fmt_lit.h.
func TestFalloff(t *testing.T) {
	l := Light{Kind: Point, Range: 1000}
	for _, tc := range []struct{ d, want float32 }{
		{0, 1.5}, {500, 0.75}, {1000, 0}, {2000, 0},
	} {
		if got := l.Falloff(tc.d); math.Abs(float64(got-tc.want)) > 1e-4 {
			t.Errorf("Falloff(%v) = %v, want %v", tc.d, got, tc.want)
		}
	}
	// Blackpoint runs the other way: fully black at the centre.
	b := Light{Kind: BlackPoint, Range: 1000}
	if got := b.Falloff(0); got != 0 {
		t.Errorf("blackpoint Falloff(0) = %v, want 0", got)
	}
	if got := b.Falloff(1000); got != 1 {
		t.Errorf("blackpoint Falloff(Range) = %v, want 1", got)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}, "x"); err == nil {
		t.Error("expected an error for a short file")
	}
	b := make([]byte, 64)
	for i := range b {
		b[i] = 0xff
	}
	if _, err := Decode(b, "x"); err == nil {
		t.Error("expected an error for a bogus n_lit_group")
	}
}
