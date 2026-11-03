package model

import (
	"bytes"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// The Konami source assets (32-bit PS2 structs) live under GameData/assets; the installed game
// ships the same models re-cooked for x64. Both must decode, so both get tested.
func srcPath(t *testing.T, kind, name string) string {
	t.Helper()
	p := filepath.Join(gameData(t), "assets", kind, "us", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("no %s", p)
	}
	return p
}

// The four models from the bug report, out of the PS2-format source tree -- the tree the old
// decoder died on with "unit 0 pack 0: null vert/norm block".
func TestDecodeSourceTreeKMS(t *testing.T) {
	for _, name := range []string{"amg_def.kms", "sol_onemesh_mt.kms", "rai_onemesh_mt.kms", "demo_w10a_wood.kms"} {
		m, err := LoadKMS(srcPath(t, "kms", name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if m.Tris() == 0 {
			t.Errorf("%s: decoded 0 triangles", name)
		}
		t.Logf("%-20s units=%2d verts=%5d tris=%5d parts=%3d  min=(%8.1f %8.1f %8.1f) max=(%8.1f %8.1f %8.1f)",
			name, m.Joints, m.Verts(), m.Tris(), len(m.Parts),
			m.Min[0], m.Min[1], m.Min[2], m.Max[0], m.Max[1], m.Max[2])
	}
}

// The same model from both trees must decode to the same geometry -- that is what proves the two
// struct layouts are the same data and not two guesses.
func TestBothLayoutsAgree(t *testing.T) {
	for _, tc := range []struct{ kind, name string }{
		{"kms", "amg_def.kms"},
		{"kms", "sol_def_sh_mt.kms"},
		{"kms", "24a.kms"},
		{"evm", "rai_def_mh_mt.evm"},
	} {
		src, err := LoadModel(srcPath(t, tc.kind, tc.name))
		if err != nil {
			t.Fatalf("%s (source): %v", tc.name, err)
		}
		cooked, err := LoadModel(filepath.Join(gameRoot(t), "assets", tc.kind, "us", tc.name))
		if err != nil {
			t.Fatalf("%s (installed): %v", tc.name, err)
		}
		if src.Verts() != cooked.Verts() || src.Tris() != cooked.Tris() || src.Joints != cooked.Joints {
			t.Errorf("%s: source %d/%d/%d != installed %d/%d/%d (verts/tris/joints)",
				tc.name, src.Verts(), src.Tris(), src.Joints, cooked.Verts(), cooked.Tris(), cooked.Joints)
			continue
		}
		for a := 0; a < 3; a++ {
			if math.Abs(float64(src.Min[a]-cooked.Min[a])) > 0.01 || math.Abs(float64(src.Max[a]-cooked.Max[a])) > 0.01 {
				t.Errorf("%s: bounds differ, %v..%v vs %v..%v", tc.name, src.Min, src.Max, cooked.Min, cooked.Max)
			}
		}
		t.Logf("%-20s identical from both trees: %d verts, %d tris, %d joints", tc.name, src.Verts(), src.Tris(), src.Joints)
	}
}

// THE scale check. A KMS unit is a joint and its vertices are unit-local, so a decoder that skips
// the rest transform collapses the model onto its root: Snake's collision hull comes out 838 tall
// instead of 1729, Raiden 660 instead of 1768. Characters are ~1750 units (1 unit ~ 1 mm); anything
// an order of magnitude off means the stride, the scale or the rest chain is wrong.
func TestCharacterHeight(t *testing.T) {
	for _, tc := range []struct{ kind, name string }{
		{"kms", "sol_bounding.kms"},  // Snake's hull -- the cleanest ruler in the game
		{"kms", "sol_def_sh_mt.kms"}, // Snake
		{"kms", "rai_def_sh_mt.kms"}, // Raiden
		{"evm", "sol_def_mh_mt.evm"}, // Snake, skinned (1.11.4, so also checks the ÷16)
		{"evm", "rai_def_mh_mt.evm"}, // Raiden, skinned
	} {
		m, err := LoadModel(srcPath(t, tc.kind, tc.name))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		h := m.Max[1] - m.Min[1]
		if h < 1400 || h > 2200 {
			t.Errorf("%s is %.0f units tall, want a human ~1750", tc.name, h)
		}
		t.Logf("%-20s %6.0f units tall (%s)", tc.name, h, tc.kind)
	}
}

// Every shipped model, from both trees, must decode. Failures are grouped so a regression names
// its own cause instead of drowning in 4,000 lines.
func TestCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus sweep")
	}
	for _, root := range []struct{ label, dir string }{
		{"source (PS2 structs)", filepath.Join(gameData(t), "assets")},
		{"installed (MC re-cook)", filepath.Join(gameRoot(t), "assets")},
	} {
		for _, ext := range []string{"kms", "evm"} {
			files, _ := filepath.Glob(filepath.Join(root.dir, ext, "*", "*."+ext))
			if len(files) == 0 {
				t.Errorf("%s: no .%s found", root.label, ext)
				continue
			}
			var ok, verts, tris, noTris, overflow int
			fails := map[string]int{}
			for _, f := range files {
				m, err := LoadModel(f)
				if err != nil {
					fails[failReason(err)]++
					continue
				}
				ok++
				verts += m.Verts()
				tris += m.Tris()
				overflow += m.Overflow
				if m.Tris() == 0 {
					noTris++
				}
				for a := 0; a < 3; a++ {
					if m.Max[a] < m.Min[a] || math.IsNaN(float64(m.Min[a])) {
						t.Errorf("%s: bad bounds %v..%v", filepath.Base(f), m.Min, m.Max)
					}
				}
			}
			t.Logf("%-24s .%s: %d/%d decoded (%.1f%%), %d verts, %d tris, %d empty, %d >4-bone verts",
				root.label, ext, ok, len(files), 100*float64(ok)/float64(len(files)), verts, tris, noTris, overflow)
			for r, n := range fails {
				t.Errorf("%s .%s: %d x %s", root.label, ext, n, r)
			}
		}
	}
}

// A random 200-file sample, exported all the way to glTF -- decoding is only half the job.
func TestSampleToGLTF(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(gameData(t), "assets", "kms", "*", "*.kms"))
	evm, _ := filepath.Glob(filepath.Join(gameData(t), "assets", "evm", "*", "*.evm"))
	files = append(files, evm...)
	sort.Strings(files)
	if len(files) < 200 {
		t.Fatalf("only %d models found", len(files))
	}
	rng := rand.New(rand.NewSource(20260714))
	rng.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })

	var ok, empty int
	fails := map[string]int{}
	for _, f := range files[:200] {
		m, err := LoadModel(f)
		if err != nil {
			fails[failReason(err)]++
			continue
		}
		var out bytes.Buffer
		if err := m.WriteGLTF(&out); err != nil {
			// A model with no triangles has nothing to export; that is a property of the file,
			// not a decode failure.
			if m.Tris() == 0 {
				empty++
				continue
			}
			fails["gltf: "+failReason(err)]++
			continue
		}
		checkGLTF(t, out.Bytes(), m.Skinned)
		ok++
	}
	t.Logf("200-file sample: %d exported to glTF, %d empty (0 tris), %d failed", ok, empty, len(fails))
	for r, n := range fails {
		t.Errorf("%d x %s", n, r)
	}
}

// The whole caller path the viewport actually uses: a DEMO_OBJECT cache id in, a glTF file out.
func TestCacheIDToGLTF(t *testing.T) {
	ix := loadIndex(t)
	ix.SetAssetRoot(gameData(t))
	cache := &Cache{Dir: t.TempDir()}

	for _, tc := range []struct {
		id  int32
		ext string
	}{
		{0x00a13626, "kms"}, // amg_def -- Arsenal Gear
		{0x00a3f5b0, "kms"}, // sol_onemesh_mt
	} {
		hits := ix.Resolve(tc.id, tc.ext)
		if len(hits) == 0 {
			t.Fatalf("cache id 0x%06x: no asset", tc.id)
		}
		var out string
		for _, a := range hits {
			if !ix.Exists(a) {
				continue
			}
			p, err := cache.GLTF(ix.FilePath(a))
			if err != nil {
				t.Fatalf("%s: %v", a.Path, err)
			}
			out = p
			break
		}
		if out == "" {
			t.Fatalf("cache id 0x%06x: nothing on disk", tc.id)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		checkGLTF(t, raw, true)
		fi, _ := os.Stat(out)
		t.Logf("0x%06x -> %s -> %s (%d bytes)", tc.id, hits[0].Path, baseName(out), fi.Size())
	}
}

// Strip the file name so failures group by cause.
func failReason(err error) string {
	s := err.Error()
	if i := bytes.IndexByte([]byte(s), ':'); i >= 0 {
		if j := bytes.LastIndexByte([]byte(s[:i]), ' '); j >= 0 {
			return s[:j] + s[i:]
		}
	}
	return s
}
