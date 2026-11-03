// Package light decodes MGS2 stage lighting (.lt2).
//
// MGS2 stage lighting is RUNTIME, not baked. The .kms/.evm carry no vertex colours --
// Konami's own model header (fmt_kmd.h) annotates the rgbs field as an unused pointer to
// preshade / vertex-colour data. It is a runtime pointer that is NULL on disk. At room
// entry DG_MakePreshade (pshade.c:1068) mallocs a vertex-colour buffer and fills it by evaluating THIS file per vertex; with
// no light def it writes a flat 0x80808080 instead. So the .lt2 is the source of truth
// and a viewport can reproduce the game's lighting from it.
//
// Layout (little-endian; all offsets from file start). From include/fmt_lit.h:
//
//	LIT_DEF header                                        32 bytes @ 0x00
//	  0x00 dir      FVECTOR  global parallel light, vw = force
//	  0x10 color    CVECTOR  parallel colour (r,g,b,pad)
//	  0x14 ambient  CVECTOR  global ambient
//	  0x18 n_lit_group  int32
//	  0x1c pad          int32
//	LIT_GRP x n_lit_group                                 48 bytes each @ 0x20
//	  0x00 bound_max FVECTOR
//	  0x10 bound_min FVECTOR
//	  0x20 n_lights  int32
//	  0x24 type      int32   one of POINT/SPOT/LINE/BLACKPOINT, plus flag bits
//	  0x28 lit       int32   FILE OFFSET to this group's light array (a void* on PS2;
//	                         lt2_conv/convert.c writes it as an offset and the loader
//	                         relocates it in place -- loader.c:577)
//	  0x2c pad       int32
//	then each group's light array, at its own lit offset:
//	  LIT_POINT       32 bytes  point(vw=force) color r_range e_range flag
//	  LIT_SPOT        80 bytes  bmax bmin point(vw=force) dir(vw=range/2) color umbra penumbra flag
//	  LIT_LINE        80 bytes  bmax bmin point(vw=force) dir(vw=length) color r_range pad flag
//	  LIT_BLACKPOINT  64 bytes  bmax bmin point r_range flag pad[2]
//
// COLOUR SCALE: raw bytes, 0..255, where 128 == 1.0 (the GS convention). The engine sums
// light colours straight into a vertex colour -- color = ambient + parallel*N.L + sum(point) --
// and packs it with BP_Vec4_ToColor, which clamps to 255 (bp_vector.h:580). The neutral
// "no light" colour it writes is 0x80 = 128. So 128 is full brightness and the shipped data
// really does go above it: 14327 of 31752 colour bytes across all 121 files exceed 128.
// We normalise by 128, so Color may legitimately reach ~1.99. Dividing by 255 instead
// halves every light and is exactly why unlit interiors read as black.
//
// The separate `force` field IS /255 -- (0.30r + 0.59g + 0.11b)/255, per fmt_lit.h and
// lt2_conv/convert.c:178. But force is only a SELECTION weight (which 2 lights are
// strongest for a character); the background preshade never multiplies by it. It lands
// in Intensity for completeness.
//
// NORMALS: the engine dots the vertex normal with the vector pointing FROM the light TOWARD
// the surface -- delta = vert_pos - light_pos, i = max(0, dot(N, normalize(delta)))
// (pshade.c:284). That only lights a floor from a lamp above it if the stored normals point
// INWARD. They do: MGS2 .kms normals are negated, and 111 of 121 stages have a parallel
// dir.vy < 0 (light travelling downward). Dir below is the engine's raw TRAVEL direction.
// A viewport using standard outward normals must negate: L_toward_light = -Dir.
package light

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type Kind string

const (
	Ambient  Kind = "ambient"
	Parallel Kind = "parallel"
	Point    Kind = "point"
	Spot     Kind = "spot"

	// Beyond the four asked for, because the shipped data is full of them and dropping
	// them would be a lie by omission: 3798 blackpoints and 1246 line lights across
	// GameData, 36% of all lights. Both are real and both are evaluated at runtime.
	Line       Kind = "line"
	BlackPoint Kind = "blackpoint"
)

// Light attribute flags (fmt_lit.h). A light only lights the map if BGOnly is set;
// CharaOnly lights are for characters. Filtering on this matters -- a viewport that
// lights geometry with chara-only lights gets a different scene than the game.
const (
	FlagCharaOnly = 0x0100
	FlagBGOnly    = 0x0200
	FlagShadow    = 0x0400
	FlagDisable   = 0x8000
)

// Group type bits (fmt_lit.h).
const (
	typePoint      = 0x0001
	typeSpot       = 0x0002
	typeLine       = 0x0004
	typeBlackPoint = 0x0010
	typeBaseMask   = typePoint | typeSpot | typeLine | typeBlackPoint
)

type Light struct {
	Kind      Kind
	Color     [3]float32 // normalised: raw byte / 128. 1.0 = full, >1.0 = intentional overbright.
	Intensity float32    // the file's `force` = (0.30r+0.59g+0.11b)/255; a selection weight, not a scale.
	Pos       [3]float32 // world, game units (point/spot/line start/blackpoint centre)
	Dir       [3]float32 // normalised TRAVEL direction (parallel/spot/line). Negate for "toward light".
	Range     float32    // distance at which the light reaches zero. 0 if none.
	ConeDeg   float32    // spot outer half-angle from the axis, degrees. 0 if n/a.
	// NOTE Range on a BlackPoint is NOT a reach -- it is the slope of the darkening ramp,
	// and the reach is BoundMin/BoundMax. Artists exploit this: r_range of 500k-1M with a
	// tight AABB makes dist/Range ~ 0.03 everywhere inside the box, i.e. a "black out this
	// volume" primitive. w42a and w44a use 21 of them between them.

	// Extras the format carries and a correct viewport needs.
	InnerConeDeg float32    // spot umbra half-angle; between this and ConeDeg the light ramps off
	Length       float32    // line light: length of the segment along Dir
	Flags        int32      // LIT_FLAG_* -- test against FlagBGOnly / FlagCharaOnly
	Group        int        // which LIT_GRP it came from
	BoundMin     [3]float32 // reach AABB. Points: Pos +/- e_range. Others: the record's own bound.
	BoundMax     [3]float32
}

// LitsBG reports whether this light affects map geometry.
func (l Light) LitsBG() bool { return l.Flags&FlagBGOnly != 0 }

// LitsChara reports whether this light affects characters.
func (l Light) LitsChara() bool { return l.Flags&FlagCharaOnly != 0 }

// Enabled reports whether the light starts switched on.
func (l Light) Enabled() bool { return l.Flags&FlagDisable == 0 }

// Falloff is the engine's distance attenuation (pshade.c:284). Linear to zero at Range,
// with a 1.5x boost at the centre -- NOT inverse-square. At Range/2 it is 0.75, which is
// what fmt_lit.h means when it describes r_range as the reference-brightness distance.
// Blackpoint inverts: it scales the accumulated colour by dist/Range, going fully black at 0.
func (l Light) Falloff(dist float32) float32 {
	if l.Range <= 0 {
		return 0
	}
	if l.Kind == BlackPoint {
		if dist >= l.Range {
			return 1
		}
		return dist / l.Range
	}
	if dist >= l.Range {
		return 0
	}
	return 1.5 * (l.Range - dist) / l.Range
}

type Set struct {
	Name   string
	Lights []Light
	Raw    map[string]any
}

// BGLights returns the enabled lights that actually light map geometry, plus the
// global ambient and parallel (which always apply).
func (s *Set) BGLights() []Light {
	var out []Light
	for _, l := range s.Lights {
		if !l.Enabled() {
			continue
		}
		if l.Kind == Ambient || l.Kind == Parallel || l.LitsBG() {
			out = append(out, l)
		}
	}
	return out
}

// Load reads one .lt2 file.
func Load(path string) (*Set, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	s, err := Decode(b, name)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return s, nil
}

const (
	hdrSize = 32
	grpSize = 48
)

var litSize = map[int32]int{typePoint: 32, typeSpot: 80, typeLine: 80, typeBlackPoint: 64}

// Decode parses .lt2 bytes. The shipped PC/PS2 files are little-endian; the HD loader
// endian-swaps (loader.c:577) because it also ran on big-endian consoles, so we sniff.
func Decode(b []byte, name string) (*Set, error) {
	if len(b) < hdrSize {
		return nil, fmt.Errorf("short file: %d bytes", len(b))
	}
	bo, err := byteOrder(b)
	if err != nil {
		return nil, err
	}

	s := &Set{Name: name, Raw: map[string]any{}}
	r := reader{b: b, bo: bo}

	dir := r.vec4(0)
	pr, pg, pb := b[16], b[17], b[18]
	ar, ag, ab := b[20], b[21], b[22]
	nGroups := int(r.i32(0x18))

	// Ambient applies everywhere with no direction; the engine adds it as the base term.
	s.Lights = append(s.Lights, Light{
		Kind:  Ambient,
		Color: [3]float32{norm(ar), norm(ag), norm(ab)},
		Group: -1,
	})
	// The global parallel is bounded by +/-FLT_MAX in code, i.e. unbounded (pshade.c:252).
	s.Lights = append(s.Lights, Light{
		Kind:      Parallel,
		Color:     [3]float32{norm(pr), norm(pg), norm(pb)},
		Intensity: dir[3],
		Dir:       [3]float32{dir[0], dir[1], dir[2]},
		Group:     -1,
	})

	if nGroups < 0 || nGroups > 4096 {
		return nil, fmt.Errorf("implausible n_lit_group %d", nGroups)
	}
	if hdrSize+nGroups*grpSize > len(b) {
		return nil, fmt.Errorf("%d groups overrun a %d-byte file", nGroups, len(b))
	}

	var groups []any
	var oddTypeBits []string
	for g := 0; g < nGroups; g++ {
		o := hdrSize + g*grpSize
		bmax, bmin := r.vec4(o), r.vec4(o+0x10)
		n := int(r.i32(o + 0x20))
		ty := r.i32(o + 0x24)
		off := int(r.i32(o + 0x28))

		base := ty & typeBaseMask
		sz, ok := litSize[base]
		if !ok {
			return nil, fmt.Errorf("group %d: bad type 0x%04x (no single base bit)", g, uint32(ty))
		}
		if n < 0 || off < 0 || off+n*sz > len(b) {
			return nil, fmt.Errorf("group %d: %d lights at 0x%x overruns a %d-byte file", g, n, off, len(b))
		}
		if extra := ty &^ typeBaseMask; extra != 0 {
			oddTypeBits = append(oddTypeBits, fmt.Sprintf("grp%d:0x%04x", g, uint32(extra)))
		}
		groups = append(groups, map[string]any{
			"index": g, "type": fmt.Sprintf("0x%04x", uint32(ty)),
			"nLights": n, "offset": off,
			"boundMin": xyz(bmin), "boundMax": xyz(bmax),
		})

		for i := 0; i < n; i++ {
			s.Lights = append(s.Lights, r.light(off+i*sz, base, g, bmin, bmax))
		}
	}

	s.Raw["byteOrder"] = boName(bo)
	s.Raw["fileSize"] = len(b)
	s.Raw["nLitGroups"] = nGroups
	s.Raw["colorScale"] = "raw bytes 0..255, 128 == 1.0 (GS); Color = raw/128"
	s.Raw["ambientRaw"] = []int{int(ar), int(ag), int(ab)}
	s.Raw["parallelRaw"] = []int{int(pr), int(pg), int(pb)}
	s.Raw["parallelForce"] = dir[3]
	s.Raw["groups"] = groups
	// LIT_TYPE_DYNAMIC/DISABLE/CHANGE would show up here. No shipped file sets any.
	if len(oddTypeBits) > 0 {
		s.Raw["groupTypeExtraBits"] = oddTypeBits
	}
	return s, nil
}

func (r reader) light(o int, base int32, g int, gmin, gmax [4]float32) Light {
	l := Light{Group: g}
	switch base {
	case typePoint:
		p := r.vec4(o)
		rr := r.f32(o + 0x14)
		er := r.f32(o + 0x18)
		l.Kind = Point
		l.Pos = [3]float32{p[0], p[1], p[2]}
		l.Intensity = p[3]
		l.Color = r.color(o + 0x10)
		l.Range = rr * 2 // brightness hits zero at r_range*2; r_range itself is the 0.75 point
		l.Flags = r.i32(o + 0x1c)
		// e_range is the engine's AABB cull, not a radius (pshade.c:284).
		l.BoundMin = [3]float32{p[0] - er, p[1] - er, p[2] - er}
		l.BoundMax = [3]float32{p[0] + er, p[1] + er, p[2] + er}

	case typeSpot:
		bmax, bmin := r.vec4(o), r.vec4(o+0x10)
		p, d := r.vec4(o+0x20), r.vec4(o+0x30)
		l.Kind = Spot
		l.Pos = [3]float32{p[0], p[1], p[2]}
		l.Intensity = p[3]
		l.Dir = [3]float32{d[0], d[1], d[2]}
		l.Range = d[3] * 2 // dir.vw is half the max reach
		l.Color = r.color(o + 0x40)
		l.InnerConeDeg = acosDeg(r.f32(o + 0x44)) // umbra: cosine of the bright core
		l.ConeDeg = acosDeg(r.f32(o + 0x48))      // penumbra: cosine of the outer edge
		l.Flags = r.i32(o + 0x4c)
		l.BoundMin, l.BoundMax = xyz(bmin), xyz(bmax)

	case typeLine:
		// A segment light. The engine cheats: it finds the closest point on the segment
		// and treats it as a point light (fmt_lit.h). Pos is the START.
		bmax, bmin := r.vec4(o), r.vec4(o+0x10)
		p, d := r.vec4(o+0x20), r.vec4(o+0x30)
		l.Kind = Line
		l.Pos = [3]float32{p[0], p[1], p[2]}
		l.Intensity = p[3]
		l.Dir = [3]float32{d[0], d[1], d[2]}
		l.Length = d[3]
		l.Color = r.color(o + 0x40)
		l.Range = r.f32(o+0x44) * 2
		l.Flags = r.i32(o + 0x4c)
		l.BoundMin, l.BoundMax = xyz(bmin), xyz(bmax)

	case typeBlackPoint:
		// A NEGATIVE light: it darkens whatever the other lights accumulated. Colour is
		// unused. Only the first one that contains a vertex applies (pshade.c:464).
		bmax, bmin := r.vec4(o), r.vec4(o+0x10)
		p := r.vec4(o + 0x20)
		l.Kind = BlackPoint
		l.Pos = [3]float32{p[0], p[1], p[2]}
		l.Range = r.f32(o+0x30) * 2
		l.Flags = r.i32(o + 0x34)
		l.BoundMin, l.BoundMax = xyz(bmin), xyz(bmax)
	}
	return l
}

// Find resolves an lt2 name ("w62a_scale10_am700") to a file. Both the Bluepoint GameData
// tree and the Steam install lay these out the same way: assets/lt2/<region>/<name>.lt2.
func Find(name, gameDataDir string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty lt2 name")
	}
	if gameDataDir == "" {
		return "", fmt.Errorf("lt2 %q: no gameDataDir given", name)
	}
	base := strings.TrimSuffix(name, ".lt2") + ".lt2"

	// Western installs run as region "eu" for region-scoped paths, but lt2s are split
	// across eu/ and us/ arbitrarily (d001p02's is only in us/), so probe both.
	var tried []string
	for _, region := range []string{"eu", "us", "jp"} {
		p := filepath.Join(gameDataDir, "assets", "lt2", region, base)
		tried = append(tried, p)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	// Case-insensitive fallback.
	root := filepath.Join(gameDataDir, "assets", "lt2")
	for _, region := range []string{"eu", "us", "jp"} {
		ents, err := os.ReadDir(filepath.Join(root, region))
		if err != nil {
			continue
		}
		for _, e := range ents {
			if strings.EqualFold(e.Name(), base) {
				return filepath.Join(root, region, e.Name()), nil
			}
		}
	}
	return "", fmt.Errorf("lt2 %q not found (tried %d paths, e.g. %s)", name, len(tried), tried[0])
}

// --- reading ---

type reader struct {
	b  []byte
	bo binary.ByteOrder
}

func (r reader) i32(o int) int32   { return int32(r.bo.Uint32(r.b[o:])) }
func (r reader) f32(o int) float32 { return math.Float32frombits(r.bo.Uint32(r.b[o:])) }

func (r reader) vec4(o int) [4]float32 {
	return [4]float32{r.f32(o), r.f32(o + 4), r.f32(o + 8), r.f32(o + 12)}
}

// CVECTOR is r,g,b,pad bytes -- byte order never applies.
func (r reader) color(o int) [3]float32 {
	return [3]float32{norm(r.b[o]), norm(r.b[o+1]), norm(r.b[o+2])}
}

// 128 == 1.0. See the package doc: this is the GS convention and the whole point.
func norm(c byte) float32 { return float32(c) / 128 }

func xyz(v [4]float32) [3]float32 { return [3]float32{v[0], v[1], v[2]} }

func acosDeg(cos float32) float32 {
	c := float64(cos)
	if c > 1 {
		c = 1
	} else if c < -1 {
		c = -1
	}
	return float32(math.Acos(c) * 180 / math.Pi)
}

// byteOrder sniffs on n_lit_group, which is small and positive in every real file.
func byteOrder(b []byte) (binary.ByteOrder, error) {
	if n := binary.LittleEndian.Uint32(b[0x18:]); n <= 4096 {
		return binary.LittleEndian, nil
	}
	if n := binary.BigEndian.Uint32(b[0x18:]); n <= 4096 {
		return binary.BigEndian, nil
	}
	return nil, fmt.Errorf("not an .lt2: n_lit_group implausible in both byte orders")
}

func boName(bo binary.ByteOrder) string {
	if bo == binary.BigEndian {
		return "big"
	}
	return "little"
}
