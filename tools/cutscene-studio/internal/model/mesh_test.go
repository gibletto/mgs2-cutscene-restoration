package model

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func evmPath(t *testing.T, name string) string {
	return filepath.Join(gameRoot(t), "assets", "evm", "us", name)
}

func kmsPath(t *testing.T, name string) string {
	return filepath.Join(gameRoot(t), "assets", "kms", "us", name)
}

func TestDecodeKnownModels(t *testing.T) {
	for _, tc := range []struct {
		file     string
		kind     string
		joints   int
		min, max [3]float32 // decoded vertex extents, tolerance 1 unit
	}{
		{evmPath(t, "ema_hair_mh.evm"), "evm", 20,
			[3]float32{-107.9, 20.0, -133.5}, [3]float32{95.0, 281.3, 103.0}},
		{evmPath(t, "fat_def_addhand_mh_mt.evm"), "evm", 92,
			[3]float32{-377, -1213, -279}, [3]float32{419, 820, 464}},
		{evmPath(t, "sol_snakearm_mh_mt.evm"), "evm", 33, // DG_EVMTYPE_LARGE: 1.15.0, no ÷16
			[3]float32{-186, 50, 34}, [3]float32{186, 2764, 227}},
		{kmsPath(t, "24a.kms"), "kms", 21,
			[3]float32{-9000, -1999, -11500}, [3]float32{9000, 2001, 11500}},
	} {
		m, err := LoadModel(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%-28s %s verts=%d tris=%d joints=%d min=%v max=%v",
			filepath.Base(tc.file), m.Kind, m.Verts(), m.Tris(), m.Joints, m.Min, m.Max)
		if m.Kind != tc.kind || m.Joints != tc.joints {
			t.Errorf("%s: kind=%s joints=%d, want %s/%d", tc.file, m.Kind, m.Joints, tc.kind, tc.joints)
		}
		if m.Tris() == 0 {
			t.Errorf("%s: no triangles", tc.file)
		}
		for a := 0; a < 3; a++ {
			if math.Abs(float64(m.Min[a]-tc.min[a])) > 1.5 || math.Abs(float64(m.Max[a]-tc.max[a])) > 1.5 {
				t.Errorf("%s: bounds %v..%v, want %v..%v", tc.file, m.Min, m.Max, tc.min, tc.max)
			}
		}
	}
}

// A character is ~1750 units tall and its head sits at rt_y ~ +632: sanity that the rig decodes.
func TestSkeletonRestChain(t *testing.T) {
	m, err := LoadEVM(evmPath(t, "fat_def_addhand_mh_mt.evm"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Skeleton[0].Parent != -1 {
		t.Errorf("joint 0 parent = %d, want -1", m.Skeleton[0].Parent)
	}
	worst := 0.0
	for i, j := range m.Skeleton {
		rest := j.Local
		if p := int(j.Parent); p >= 0 {
			for a := 0; a < 3; a++ {
				rest[a] += m.Skeleton[p].Rest[a]
			}
		}
		for a := 0; a < 3; a++ {
			if d := math.Abs(float64(rest[a] - j.Rest[a])); d > worst {
				worst = d
			}
		}
		if i == 12 {
			t.Logf("joint 12 (head) rest=%v", j.Rest)
		}
	}
	if worst > 0.01 {
		t.Errorf("parent-chain accumulation disagrees with rt_* by %f", worst)
	}
	t.Logf("92 joints, rest chain matches rt_* to %g", worst)
}

// Decode every shipped model. This is the claim that matters: the format is fully cracked.
func TestDecodeWholeCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus sweep")
	}
	root := gameRoot(t)
	for _, tc := range []struct{ dir, ext string }{
		{filepath.Join(root, "assets", "evm"), ".evm"},
		{filepath.Join(root, "assets", "kms"), ".kms"},
	} {
		files, err := filepath.Glob(filepath.Join(tc.dir, "*", "*"+tc.ext))
		if err != nil || len(files) == 0 {
			t.Fatalf("no %s under %s", tc.ext, tc.dir)
		}
		var ok, verts, tris, overflow, degenerate int
		var maxJoints int
		for _, f := range files {
			m, err := LoadModel(f)
			if err != nil {
				t.Errorf("%s: %v", filepath.Base(f), err)
				continue
			}
			if m.Tris() == 0 {
				degenerate++
			}
			ok++
			verts += m.Verts()
			tris += m.Tris()
			overflow += m.Overflow
			if m.Joints > maxJoints {
				maxJoints = m.Joints
			}
			for a := 0; a < 3; a++ {
				if m.Max[a] < m.Min[a] || math.IsNaN(float64(m.Min[a])) {
					t.Errorf("%s: bad bounds %v..%v", filepath.Base(f), m.Min, m.Max)
				}
			}
		}
		t.Logf("%s: %d/%d decoded, %d verts, %d tris, max joints=%d, no-tri models=%d, >4-weight verts=%d",
			tc.ext, ok, len(files), verts, tris, maxJoints, degenerate, overflow)
		if ok != len(files) {
			t.Errorf("%s: %d of %d failed", tc.ext, len(files)-ok, len(files))
		}
	}
}

func TestProxyBoxes(t *testing.T) {
	m, err := LoadEVM(evmPath(t, "fat_def_addhand_mh_mt.evm"))
	if err != nil {
		t.Fatal(err)
	}
	p := m.Proxy()
	boxes := p.Verts() / 8
	t.Logf("proxy: %d boxes, %d tris, bounds %v..%v (real mesh %v..%v)", boxes, p.Tris(), p.Min, p.Max, m.Min, m.Max)
	if boxes < 50 {
		t.Errorf("proxy has %d boxes, want one per skinned joint", boxes)
	}
	if p.Tris() != boxes*12 {
		t.Errorf("proxy: %d tris for %d boxes", p.Tris(), boxes)
	}
	// The per-joint AABBs are model-space, so the union must sit inside the mesh bounds (+slack).
	for a := 0; a < 3; a++ {
		if p.Min[a] < m.Min[a]-200 || p.Max[a] > m.Max[a]+200 {
			t.Errorf("proxy bounds %v..%v stray far outside mesh %v..%v", p.Min, p.Max, m.Min, m.Max)
		}
	}
}

// The EVM header "bbox" is a uniform radius, not an AABB -- proxies built from it are cube-shaped
// and hugely oversized. Prove it, so nobody re-introduces the bug.
func TestHeaderBoundsAreASphere(t *testing.T) {
	b, err := os.ReadFile(evmPath(t, "ema_hair_mh.evm"))
	if err != nil {
		t.Fatal(err)
	}
	r := &reader{b: b}
	lo, hi := r.vec3(8), r.vec3(0x14)
	if lo[0] != lo[1] || lo[1] != lo[2] || hi[0] != -lo[0] {
		t.Fatalf("header bounds %v..%v are not a uniform radius after all", lo, hi)
	}
	m, err := LoadEVM(evmPath(t, "ema_hair_mh.evm"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("header radius=%.1f but real extents are %v..%v", hi[0], m.Min, m.Max)
}
