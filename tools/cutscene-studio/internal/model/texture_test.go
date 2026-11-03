package model

import (
	"bytes"

	"encoding/binary"
	"encoding/json"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func texIndex(t *testing.T) *TextureIndex {
	t.Helper()
	ix, err := LoadTextureIndex(gameData(t))
	if err != nil {
		t.Skipf("no texture flatlist: %v", err)
	}
	return ix
}

// The crash demo's cast. Every packet's tex_id[0] must land on a real .ctxr.
func TestTextureCast(t *testing.T) {
	ix := texIndex(t)
	for _, name := range []string{"amg_def.kms", "sol_onemesh_mt.kms", "rai_onemesh_mt.kms", "demo_w10a_wood.kms"} {
		m, err := LoadKMS(srcPath(t, "kms", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ids := map[uint32]bool{}
		for _, p := range m.Parts {
			if p.Textured() {
				ids[p.Tex] = true
			}
		}
		resolved, exactN := 0, 0
		var dims []string
		for _, id := range sortedIDs(ids) {
			target, exact, ok := ix.Resolve(m.TexGroup, id)
			if !ok {
				t.Errorf("%s: tex %08x unresolved", name, id)
				continue
			}
			b, err := ix.Bake(target)
			if err != nil {
				t.Errorf("%s: tex %08x -> %s: %v", name, id, target, err)
				continue
			}
			resolved++
			if exact {
				exactN++
			}
			if len(dims) < 6 {
				dims = append(dims, target)
			}
			// 4x4 flat-colour swatches are real assets (sol_body_color.bmp), so only the floor
			// of a legal PNG is worth asserting.
			if b.w <= 0 || b.h <= 0 || len(b.png) < 60 {
				t.Errorf("%s: %s decoded to %dx%d, %d B of png", name, target, b.w, b.h, len(b.png))
			}
		}
		if resolved != len(ids) {
			t.Errorf("%s: %d/%d texture ids resolved", name, resolved, len(ids))
		}
		t.Logf("%-22s tri=%08x parts=%3d  texIDs=%3d resolved=%3d (exact %d, fallback %d)  e.g. %s",
			name, m.TexGroup, len(m.Parts), len(ids), resolved, exactN, resolved-exactN, strings.Join(dims, " "))
	}
}

func sortedIDs(m map[uint32]bool) []uint32 {
	out := make([]uint32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Corpus sweep: how much of the shipped geometry actually gets its pixels?
func TestTextureCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus sweep")
	}
	ix := texIndex(t)
	root := gameData(t)

	var files []string
	for _, ext := range []string{"kms", "evm"} {
		ents, err := os.ReadDir(filepath.Join(root, "assets", ext, "us"))
		if err != nil {
			t.Skipf("no assets/%s/us", ext)
		}
		for _, e := range ents {
			if strings.HasSuffix(e.Name(), "."+ext) {
				files = append(files, filepath.Join(root, "assets", ext, "us", e.Name()))
			}
		}
	}
	sort.Strings(files)
	rand.New(rand.NewSource(7)).Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
	if len(files) > 400 {
		files = files[:400]
	}

	var packs, flat, exact, fallback, miss, decodeFail int
	models, full := 0, 0
	why := map[string]int{}
	for _, f := range files {
		m, err := LoadModel(f)
		if err != nil {
			why["model decode: "+err.Error()]++
			continue
		}
		models++
		ok := true
		for _, p := range m.Parts {
			packs++
			if !p.Textured() {
				flat++
				continue
			}
			target, ex, hit := ix.Resolve(m.TexGroup, p.Tex)
			if !hit {
				miss++
				ok = false
				why["tex id has no unambiguous pool target"]++
				continue
			}
			if _, err := ix.Bake(target); err != nil {
				decodeFail++
				ok = false
				why["ctxr decode: "+err.Error()]++
				continue
			}
			if ex {
				exact++
			} else {
				fallback++
			}
		}
		if ok {
			full++
		}
	}
	textured := exact + fallback + miss + decodeFail
	t.Logf("models=%d fully-textured=%d (%.1f%%)", models, full, 100*float64(full)/float64(models))
	t.Logf("packets=%d  no TEX0 (flat by design)=%d  textured=%d", packs, flat, textured)
	t.Logf("  exact (tri,tex): %d (%.2f%%)", exact, 100*float64(exact)/float64(textured))
	t.Logf("  tex-id fallback: %d (%.2f%%)", fallback, 100*float64(fallback)/float64(textured))
	t.Logf("  unresolved: %d   decode failures: %d", miss, decodeFail)
	for k, v := range why {
		t.Logf("  reason: %-50s %d", k, v)
	}
	if decodeFail > 0 {
		t.Errorf("%d packets resolved to a .ctxr that would not decode", decodeFail)
	}
	if r := float64(exact+fallback) / float64(textured); r < 0.99 {
		t.Errorf("texture resolution rate %.2f%% is below 99%%", 100*r)
	}
}

// The whole point: a textured glTF must still parse, still skin, and every material's
// baseColorTexture must reach a real PNG.
func TestTexturedGLTF(t *testing.T) {
	ix := texIndex(t)
	for _, tc := range []struct {
		path string
		skin bool
	}{
		{srcPath(t, "kms", "amg_def.kms"), true},
		{srcPath(t, "kms", "sol_onemesh_mt.kms"), true},
		{srcPath(t, "evm", "rai_def_mh_mt.evm"), true},
	} {
		name := filepath.Base(tc.path)
		m, err := LoadModel(tc.path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var buf bytes.Buffer
		if err := m.WriteGLTFWith(&buf, ix); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		doc := checkGLTF(t, buf.Bytes(), tc.skin)
		blob := gltfBlob(t, doc)

		prims := doc["meshes"].([]any)[0].(map[string]any)["primitives"].([]any)
		mats, _ := doc["materials"].([]any)
		texs, _ := doc["textures"].([]any)
		imgs, _ := doc["images"].([]any)
		views := doc["bufferViews"].([]any)

		if len(prims) < 2 {
			t.Errorf("%s: %d primitives -- the mesh did not split by material", name, len(prims))
		}
		// every primitive keeps the skin attributes
		for i, p := range prims {
			a := p.(map[string]any)["attributes"].(map[string]any)
			for _, want := range []string{"POSITION", "NORMAL", "TEXCOORD_0", "JOINTS_0", "WEIGHTS_0"} {
				if _, ok := a[want]; !ok {
					t.Errorf("%s: primitive %d lost %s", name, i, want)
				}
			}
		}
		withTex := 0
		for i, mm := range mats {
			pbr := mm.(map[string]any)["pbrMetallicRoughness"].(map[string]any)
			ref, ok := pbr["baseColorTexture"].(map[string]any)
			if !ok {
				continue
			}
			withTex++
			ti := int(ref["index"].(float64))
			if ti < 0 || ti >= len(texs) {
				t.Fatalf("%s: material %d points at texture %d of %d", name, i, ti, len(texs))
			}
			src := int(texs[ti].(map[string]any)["source"].(float64))
			if src < 0 || src >= len(imgs) {
				t.Fatalf("%s: texture %d points at image %d of %d", name, ti, src, len(imgs))
			}
			img := imgs[src].(map[string]any)
			if img["mimeType"] != "image/png" {
				t.Errorf("%s: image %d mimeType %v", name, src, img["mimeType"])
			}
			v := views[int(img["bufferView"].(float64))].(map[string]any)
			off, n := int(v["byteOffset"].(float64)), int(v["byteLength"].(float64))
			if n < 60 || off+n > len(blob) {
				t.Fatalf("%s: image %d is %d bytes at %d (buffer is %d)", name, src, n, off, len(blob))
			}
			// and it really is a PNG we can read back
			pix, err := png.Decode(bytes.NewReader(blob[off : off+n]))
			if err != nil {
				t.Fatalf("%s: image %d does not decode: %v", name, src, err)
			}
			if pix.Bounds().Dx() == 0 || pix.Bounds().Dy() == 0 {
				t.Errorf("%s: image %d is empty", name, src)
			}
			if am := mm.(map[string]any)["alphaMode"]; am == "MASK" {
				if _, ok := mm.(map[string]any)["alphaCutoff"]; !ok {
					t.Errorf("%s: material %d is MASK with no alphaCutoff", name, i)
				}
			}
		}
		if withTex == 0 {
			t.Errorf("%s: no material carries a baseColorTexture", name)
		}
		t.Logf("%-22s prims=%2d materials=%2d textures=%2d images=%2d  gltf=%d KB",
			name, len(prims), len(mats), len(texs), len(imgs), buf.Len()/1024)
	}
}

// Are the pixels REAL, or did we misread the byte order?
//
// The pool ships the cooked .ctxr next to the .tga it was cooked from, and a .ctxr's mip 0 is that
// TGA's pixel block verbatim. So decode both and compare: if our RGBA matches the TGA channel for
// channel it cannot be a byte-order mistake, and the coloured samples discriminate BGRA from RGBA
// (the greys would match either way).
func TestCTXRAgainstSourceTGA(t *testing.T) {
	root := gameData(t)
	for _, name := range []string{"000062b9", "otc_glasses.bmp", "B_DECK.bmp"} {
		raw, err := os.ReadFile(filepath.Join(root, "textures", "flatlist", "_win", name+".ctxr"))
		if err != nil {
			t.Skipf("no %s.ctxr", name)
		}
		tex, err := decodeCTXR(raw, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		tb, err := os.ReadFile(filepath.Join(root, "textures", "flatlist", name+".tga"))
		if err != nil {
			t.Skipf("no %s.tga", name)
		}
		tw, th, tpx, err := readTGA(tb)
		if err != nil {
			t.Skipf("%s.tga: %v", name, err)
		}
		w, h := tex.Image.Bounds().Dx(), tex.Image.Bounds().Dy()
		if w != tw || h != th {
			t.Fatalf("%s: ctxr %dx%d vs tga %dx%d", name, w, h, tw, th)
		}
		bad, swapped := 0, 0
		for i := 0; i < w*h; i++ {
			c, s := tex.Image.Pix[4*i:], tpx[4*i:] // tga is B,G,R,A
			if c[0] != s[2] || c[1] != s[1] || c[2] != s[0] {
				bad++
			}
			if c[0] == s[0] && c[1] == s[1] && c[2] == s[2] && s[0] != s[2] {
				swapped++
			}
		}
		if bad != 0 {
			t.Errorf("%s: %d/%d texels disagree with the source TGA", name, bad, w*h)
		}
		t.Logf("%-18s %3dx%-3d  RGB matches the source TGA on %d/%d texels (channel-swapped reading would match %d)",
			name, w, h, w*h-bad, w*h, swapped)
	}
}

// Every texture a real model pulls in, checked against the .tga it was cooked from. This is the
// strong form of "are the pixels real": a channel swap, a row flip, a stride slip or an off-by-one
// header would all break the comparison, and it covers whatever the model happens to use rather
// than a hand-picked file.
//
// It also asserts the images are not flat: MGS2 does ship 4x4 colour swatches, but a whole model's
// worth of textures coming out uniform would mean we were reading padding.
func TestCTXRModelTexturesAreReal(t *testing.T) {
	root := gameData(t)
	ix := texIndex(t)
	for _, name := range []string{"demo_w10a_wood.kms", "sol_onemesh_mt.kms"} {
		m, err := LoadKMS(srcPath(t, "kms", name))
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		checked, matched, textured := 0, 0, 0
		for _, p := range m.Parts {
			if !p.Textured() {
				continue
			}
			target, _, ok := ix.Resolve(m.TexGroup, p.Tex)
			if !ok || seen[target] {
				continue
			}
			seen[target] = true

			raw, err := os.ReadFile(filepath.Join(root, "textures", "flatlist", "_win", target+".ctxr"))
			if err != nil {
				t.Fatal(err)
			}
			tex, err := decodeCTXR(raw, target)
			if err != nil {
				t.Fatalf("%s: %v", target, err)
			}
			w, h := tex.Image.Bounds().Dx(), tex.Image.Bounds().Dy()
			checked++

			distinct := map[uint32]bool{}
			for i := 0; i < w*h; i++ {
				distinct[binary.LittleEndian.Uint32(tex.Image.Pix[4*i:][:4])] = true
			}
			if len(distinct) > 1 {
				textured++
			}

			tb, err := os.ReadFile(filepath.Join(root, "textures", "flatlist", target+".tga"))
			if err != nil {
				continue
			}
			tw, th, tpx, err := readTGA(tb)
			if err != nil || tw != w || th != h {
				continue
			}
			bad := 0
			for i := 0; i < w*h; i++ {
				c, s := tex.Image.Pix[4*i:], tpx[4*i:]
				if c[0] != s[2] || c[1] != s[1] || c[2] != s[0] {
					bad++
				}
			}
			if bad != 0 {
				t.Errorf("%s / %s: %d/%d texels disagree with the source TGA", name, target, bad, w*h)
			}
			matched++
		}
		if checked == 0 {
			t.Fatalf("%s: no textures resolved", name)
		}
		if textured*4 < checked {
			t.Errorf("%s: only %d of %d textures have more than one colour", name, textured, checked)
		}
		t.Logf("%-22s %2d textures decoded, %2d verified byte-for-byte against their source TGA, %2d non-uniform",
			name, checked, matched, textured)
	}
}

// Writes one decoded texture out so a human can look at it.
func TestDumpTexturePNG(t *testing.T) {
	out := os.Getenv("MGS2_TEX_DUMP")
	if out == "" {
		t.Skip("set MGS2_TEX_DUMP=<dir> to dump decoded textures")
	}
	ix := texIndex(t)
	m, err := LoadKMS(srcPath(t, "kms", "sol_onemesh_mt.kms"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range m.Parts {
		if !p.Textured() {
			continue
		}
		target, _, ok := ix.Resolve(m.TexGroup, p.Tex)
		if !ok || seen[target] {
			continue
		}
		seen[target] = true
		b, err := ix.Bake(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, target+".png"), b.png, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s.png  %dx%d", target, b.w, b.h)
	}
}

// A minimal uncompressed-truecolor TGA reader, only for the ground-truth comparison above.
func readTGA(b []byte) (w, h int, px []byte, err error) {
	if len(b) < 18 || b[2] != 2 || b[16] != 32 {
		return 0, 0, nil, errNotTGA
	}
	w = int(binary.LittleEndian.Uint16(b[12:]))
	h = int(binary.LittleEndian.Uint16(b[14:]))
	off := 18 + int(b[0])
	if off+4*w*h > len(b) {
		return 0, 0, nil, errNotTGA
	}
	px = b[off : off+4*w*h]
	if b[17]&0x20 == 0 { // bottom-up: the .ctxr is top-down, like D3D and like PNG
		flip := make([]byte, len(px))
		row := 4 * w
		for y := 0; y < h; y++ {
			copy(flip[y*row:], px[(h-1-y)*row:(h-1-y)*row+row])
		}
		px = flip
	}
	return w, h, px, nil
}

var errNotTGA = errTGA("not a 32-bit uncompressed TGA")

type errTGA string

func (e errTGA) Error() string { return string(e) }

// The alpha rescale is load-bearing: the PS2 GS calls 0x80 opaque, so a raw copy would render the
// whole game half-transparent.
func TestCTXRAlphaRescale(t *testing.T) {
	root := gameData(t)
	raw, err := os.ReadFile(filepath.Join(root, "textures", "flatlist", "_win", "otc_glasses.bmp.ctxr"))
	if err != nil {
		t.Skip("no otc_glasses.bmp.ctxr")
	}
	if raw[128+4+3] != 0x80 {
		t.Fatalf("expected a raw alpha of 0x80, got 0x%02x", raw[128+4+3])
	}
	tex, err := decodeCTXR(raw, "otc_glasses")
	if err != nil {
		t.Fatal(err)
	}
	if got := tex.Image.Pix[3]; got != 255 {
		t.Errorf("raw alpha 0x80 decoded to %d, want 255 (GS treats 128 as fully opaque)", got)
	}
	if tex.Alpha != alphaOpaque {
		t.Errorf("an all-0x80 texture came out as alpha mode %d, want OPAQUE", tex.Alpha)
	}
}

// Hair and grates must not come out as opaque blocks. Konami named the alpha-carrying textures
// _alp / _alp_ovl, so the naming is a free oracle: those must never classify OPAQUE, and the
// ordinary ones mostly should.
func TestAlphaModes(t *testing.T) {
	ix := texIndex(t)
	m, err := LoadKMS(srcPath(t, "kms", "rai_onemesh_mt.kms"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	count := map[alphaKind]int{}
	wrong := 0
	for _, p := range m.Parts {
		if !p.Textured() {
			continue
		}
		target, _, ok := ix.Resolve(m.TexGroup, p.Tex)
		if !ok || seen[target] {
			continue
		}
		seen[target] = true
		raw, err := os.ReadFile(filepath.Join(gameData(t), "textures", "flatlist", "_win", target+".ctxr"))
		if err != nil {
			t.Fatal(err)
		}
		tex, err := decodeCTXR(raw, target)
		if err != nil {
			t.Fatal(err)
		}
		count[tex.Alpha]++
		if strings.Contains(target, "_alp") && tex.Alpha == alphaOpaque {
			t.Errorf("%s carries alpha in its name but decoded OPAQUE -- it would render as a block", target)
			wrong++
		}
		if strings.Contains(target, "hair") {
			t.Logf("  %-32s -> %s", target, []string{"OPAQUE", "MASK", "BLEND"}[tex.Alpha])
		}
	}
	t.Logf("rai_onemesh_mt.kms: OPAQUE=%d MASK=%d BLEND=%d (%d misclassified)",
		count[alphaOpaque], count[alphaMask], count[alphaBlend], wrong)
}

// Serving a model twice must not re-decode it, and the cached file must be byte-identical.
func TestCacheTexturesOnce(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir}
	src := srcPath(t, "kms", "demo_w10a_wood.kms")
	a, err := c.GLTF(src)
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.GLTF(src)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("second call produced a different file: %s vs %s", a, b)
	}
	second, _ := os.ReadFile(b)
	if !bytes.Equal(first, second) {
		t.Error("cached glTF changed between calls")
	}
	var doc map[string]any
	if err := json.Unmarshal(first, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc["images"].([]any)) == 0 {
		t.Error("the cache served a model with no textures")
	}
	t.Logf("cached %s: %d KB, %d images", filepath.Base(a), len(first)/1024, len(doc["images"].([]any)))
}
