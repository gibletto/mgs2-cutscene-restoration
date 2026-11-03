package model

import (
	"bufio"
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// How a model gets its pixels.
//
// A packet names a texture with tex_id[0]; the model header names the .tri archive that texture
// is supposed to live in (KMS +0x0C, EVM +0x24 -- the "texture group"). That pair is exactly the
// key the runtime binds on: libdg/text.c:370 walks a loaded .tri's DG_TEX list and calls
// BP_BindMGSTexture(..., tex->tri_id, tex->tex_id, ...), which builds the path
// "<triId:08x>/<texId:08x>.ctxr" (BP_BaseRenderer.cpp:1265).
//
// Bluepoint flattened those virtual paths into a pool, and BP_FlatlistTextureMapping.txt is the
// rename table -- same trick as bp_flatlistassetmapping.txt, one level deeper:
//
//	us/face/f00a/cache/0001f7ba/00316d7c.tga   otc_glasses.bmp
//	                  ^tri id   ^tex id        ^pool file
//
// The pool file lands at textures/flatlist/_win/<target>.ctxr. (0001f7ba really is a .tri:
// bp_flatlistassetmapping.txt maps us/face/f00a/cache/0001f7ba.tri -> tri/us/zzz.tri.)
//
// So we never open a .tri at all. The .tri is the PS2 swizzled/palettised archive; the flatlist
// already tells us which cooked PC texture each (tri, tex) pair became, and that is the file the
// game loads. Reading the .tri would mean unswizzling PS2 CLUT formats for the same pixels.

type texKey struct{ tri, tex uint32 }

// TextureIndex is BP_FlatlistTextureMapping.txt, resolved.
type TextureIndex struct {
	root  string            // the tree holding textures/flatlist/_win
	exact map[texKey]string // (tri, tex) -> pool target
	byTex map[uint32]string // tex -> pool target, only where every candidate is the same source image

	mu    sync.Mutex
	baked map[string]baked // encoded once, reused across models
	cost  int
}

// baked is a pool texture already turned into the bytes the glTF wants.
type baked struct {
	png   []byte
	alpha alphaKind
	w, h  int
}

// pngBudget caps the shared PNG cache. A model's own textures are deduped regardless; this only
// bounds what we keep for the NEXT model.
const pngBudget = 128 << 20

type vote struct {
	target string
	n      int
}

// LoadTextureIndex parses GameData/BP_FlatlistTextureMapping.txt (523,573 lines).
func LoadTextureIndex(gameDataDir string) (*TextureIndex, error) {
	name := filepath.Join(gameDataDir, "BP_FlatlistTextureMapping.txt")
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	exact := make(map[texKey][]vote, 1<<16)
	byTex := make(map[uint32][]vote, 1<<14)
	intern := make(map[string]string, 1<<14)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for line := 0; sc.Scan(); line++ {
		text := strings.TrimRight(sc.Text(), "\r")
		if text == "" {
			continue
		}
		virt, target, ok := strings.Cut(text, " ")
		if !ok {
			if line == 0 && strings.HasPrefix(text, "flatmap") {
				continue
			}
			return nil, fmt.Errorf("%s:%d: not a mapping line: %q", name, line+1, text)
		}
		k, region, ok, err := parseTexMapping(virt)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, line+1, err)
		}
		if !ok {
			continue // a title-screen image, not a model texture: 48 lines, no tri/tex pair
		}
		if v, ok := intern[target]; ok {
			target = v
		} else {
			intern[target] = target
		}
		// us wins any tie: 99% of what the editor shows is the us build, and eu/jp mostly
		// re-point the same pixels.
		w := 1
		if region == "us" {
			w = 1000
		}
		exact[k] = addVote(exact[k], target, w)
		byTex[k.tex] = addVote(byTex[k.tex], target, w)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	ix := &TextureIndex{
		root:  gameDataDir,
		exact: make(map[texKey]string, len(exact)),
		byTex: make(map[uint32]string, len(byTex)),
		baked: map[string]baked{},
	}
	for k, vs := range exact {
		ix.exact[k] = winner(vs)
	}
	// The tex-only fallback is a guess, so only take it where there is nothing to guess: every
	// candidate must be the same source image, differing only by Bluepoint's dedup suffix
	// (w25a_tank00.bmp vs w25a_tank00.bmp_02f63b5a...). 19 of 14,934 tex ids fail that and are
	// dropped -- e.g. 002c98c9 is both vr_wall_01.bmp and vr2_wall_textless.bmp, and picking
	// wrong there paints the wrong wall.
	for tex, vs := range byTex {
		if sameSource(vs) {
			ix.byTex[tex] = winner(vs)
		}
	}
	return ix, nil
}

// virtual: <region>/<container>/<name>/<dir>/<tri_id:08x>/<tex_id:08x>.tga
//
// 48 of the 523,573 lines are 4-part instead (us/stage/n_title/transfarring_icon_lock.tga) -- title
// UI art that the runtime binds by image id, not through a .tri (BP_BindMGSImage, ".img.ctxr").
// Those carry no tri/tex pair, so no model can name them: skip, don't fail.
func parseTexMapping(virt string) (texKey, string, bool, error) {
	var k texKey
	p := strings.Split(virt, "/")
	if len(p) == 4 && strings.HasSuffix(p[3], ".tga") {
		return k, "", false, nil
	}
	if len(p) != 6 || !strings.HasSuffix(p[5], ".tga") {
		return k, "", false, fmt.Errorf("bad virtual path %q", virt)
	}
	tri, err := strconv.ParseUint(p[4], 16, 32)
	if err != nil {
		return k, "", false, fmt.Errorf("bad tri id in %q", virt)
	}
	tex, err := strconv.ParseUint(strings.TrimSuffix(p[5], ".tga"), 16, 32)
	if err != nil {
		return k, "", false, fmt.Errorf("bad tex id in %q", virt)
	}
	return texKey{uint32(tri), uint32(tex)}, p[0], true, nil
}

func addVote(vs []vote, target string, w int) []vote {
	for i := range vs {
		if vs[i].target == target {
			vs[i].n += w
			return vs
		}
	}
	return append(vs, vote{target, w})
}

func winner(vs []vote) string {
	best := vs[0]
	for _, v := range vs[1:] {
		if v.n > best.n || (v.n == best.n && v.target < best.target) {
			best = v
		}
	}
	return best.target
}

func sameSource(vs []vote) bool {
	for _, v := range vs[1:] {
		if dedupBase(v.target) != dedupBase(vs[0].target) {
			return false
		}
	}
	return true
}

// Bluepoint appends _<md5 in 32 lowercase hex> when the same source name cooked to different bytes.
func dedupBase(t string) string {
	i := strings.LastIndexByte(t, '_')
	if i < 0 || len(t)-i-1 != 32 {
		return t
	}
	for _, c := range t[i+1:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return t
		}
	}
	return t[:i]
}

// Resolve maps a packet's (texture group, tex_id[0]) to a pool target. exact is false when we had
// to fall back to the tex id alone, which happens when the packet's texture lives in some other
// .tri that was resident at the same time (3.4% of packets).
func (ix *TextureIndex) Resolve(tri, tex uint32) (target string, exact, ok bool) {
	if t, hit := ix.exact[texKey{tri, tex}]; hit {
		return t, true, true
	}
	if t, hit := ix.byTex[tex]; hit {
		return t, false, true
	}
	return "", false, false
}

func (ix *TextureIndex) ctxrPath(target string) string {
	return filepath.Join(ix.root, "textures", "flatlist", "_win", target+".ctxr")
}

// Bake decodes a pool texture and re-encodes it as the PNG the glTF embeds, memoized.
func (ix *TextureIndex) Bake(target string) (baked, error) {
	ix.mu.Lock()
	if b, ok := ix.baked[target]; ok {
		ix.mu.Unlock()
		return b, nil
	}
	ix.mu.Unlock()

	raw, err := os.ReadFile(ix.ctxrPath(target))
	if err != nil {
		return baked{}, err
	}
	t, err := decodeCTXR(raw, target)
	if err != nil {
		return baked{}, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, t.Image); err != nil {
		return baked{}, err
	}
	r := t.Image.Bounds()
	b := baked{png: buf.Bytes(), alpha: t.Alpha, w: r.Dx(), h: r.Dy()}

	ix.mu.Lock()
	if ix.baked == nil {
		ix.baked = map[string]baked{}
	}
	if ix.cost+len(b.png) <= pngBudget {
		ix.baked[target] = b
		ix.cost += len(b.png)
	}
	ix.mu.Unlock()
	return b, nil
}

// FindGameData walks up from a model file to the tree that holds the flatlists -- assets/ and
// textures/ are siblings under it, so the model path alone is enough to find the textures.
func FindGameData(modelPath string) string {
	dir := filepath.Dir(modelPath)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "BP_FlatlistTextureMapping.txt")); err == nil {
			return dir
		}
		up := filepath.Dir(dir)
		if up == dir {
			break
		}
		dir = up
	}
	return ""
}
