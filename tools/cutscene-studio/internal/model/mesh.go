package model

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// Joint is one node of a model's hierarchy: an EVM skeleton joint, or a KMS unit (which is the
// same shape minus the skinning). The bind pose is translation-only -- no rotations are stored --
// so Rest is just the parent chain summed, and the inverse bind matrix is a pure -Rest translate.
type Joint struct {
	Parent    int32
	Local     [3]float32 // offset from parent
	Rest      [3]float32 // offset from root (EVM stores it; KMS we accumulate)
	Min, Max  [3]float32 // model-space AABB
	HasBounds bool
}

// Part is one packet: a contiguous slice of the index buffer, the unit it came from, the joint it
// hangs off and its texture id. This is the per-part/per-joint index a poser or a material pass
// needs; the vertices themselves are already bound via Bones/Weights.
type Part struct {
	Unit  int    // KMS unit / EVM packet index
	Joint int    // KMS: the unit's joint. EVM: -1, skinning is per-vertex
	Flag  uint32 // DG_PACKFLAG_*; TEX0 (0x8) clear means the packet really has no texture
	Tex   uint32 // tex_id[0]
	First int    // offset into Indices
	Count int    // number of indices
	Base  int    // offset into Positions, in vertices
	Verts int
}

// Textured reports whether the packet names a texture at all. 1,369 of the 233,695 shipped
// packets do not, and those are meant to be flat.
func (p Part) Textured() bool { return p.Flag&packTex0 != 0 && p.Tex != 0 }

// Mesh is a decoded model, flattened to one buffer: every packet's vertices concatenated,
// strips already expanded to triangles. Positions are MGS2 world units (~1 unit = 1 mm, a
// character is ~1750 tall), right-handed Y-up -- which is glTF's convention, so no axis flip.
//
// Both formats come out skinned, and both come out in the REST POSE: the joint hierarchy and the
// per-vertex weights are exported as-is, but nothing here reads an MTN or poses anything.
type Mesh struct {
	Positions []float32 // 3 per vertex
	Normals   []float32 // 3 per vertex
	UVs       []float32 // 2 per vertex (uvs[0] only)
	Indices   []uint32
	Bones     []uint16  // 4 per vertex -- global joint indices
	Weights   []float32 // 4 per vertex
	Parts     []Part
	Min, Max  [3]float32
	Joints    int // len(Skeleton)
	Skeleton  []Joint
	Kind      string // "kms", "evm", or "proxy"
	Source    string

	// ApplyOrigin bakes Origin into the vertex positions and drops the skin. Map areas are
	// ONEPIECE -- their joints are never posed -- and a three.js SkinnedMesh cancels its own
	// world matrix (bindMode "attached"), so translating the node does nothing. Baking it is
	// both correct and unambiguous.
	// Origin is DG_DEF.tx/ty/tz (KMS2_DEF +0x34), the model's "reference display position".
	// The engine adds it to the root ONLY for DG_FLAG_ONEPIECE (rigid) objects
	// (DG_SetObjsMatrix). It is zero for anything the script places with -p or the demo
	// drives by motion -- and it is how a MAP AREA gets its world position: the room mesh
	// w11a0 is modelled around the origin and carries (-11751, -48375, 20250).
	Origin         [3]float32
	Skinned        bool
	normalsFlipped bool   // MGS2 stores normals inward; flip once on export
	Overflow       int    // vertices with >4 non-zero weights; the extras are dropped (kms2obj overruns here)
	TexGroup       uint32 // the .tri this model's tex ids are keyed against (see texture.go)
}

// LoadModel dispatches on the file extension.
func LoadModel(path string) (*Mesh, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".kms":
		return LoadKMS(path)
	case ".evm":
		return LoadEVM(path)
	}
	return nil, fmt.Errorf("model: %s: not a .kms or .evm", filepath.Base(path))
}

func (m *Mesh) Verts() int { return len(m.Positions) / 3 }
func (m *Mesh) Tris() int  { return len(m.Indices) / 3 }

// Size is the AABB extent, and Center its middle -- what a proxy box needs.
func (m *Mesh) Size() [3]float32 {
	return [3]float32{m.Max[0] - m.Min[0], m.Max[1] - m.Min[1], m.Max[2] - m.Min[2]}
}

func (m *Mesh) Center() [3]float32 {
	return [3]float32{(m.Min[0] + m.Max[0]) / 2, (m.Min[1] + m.Max[1]) / 2, (m.Min[2] + m.Max[2]) / 2}
}

// Proxy builds the fallback: one box per joint/unit AABB, or a single box round the whole model if
// the file has no per-part bounds. Articulated and correctly pivoted, so it beats a blob even when
// we never touch a vertex.
func (m *Mesh) Proxy() *Mesh {
	p := &Mesh{Kind: "proxy", Source: m.Source, Joints: m.Joints, Skeleton: m.Skeleton}
	boxes := 0
	for _, j := range m.Skeleton {
		if j.HasBounds && !degenerate(j.Min, j.Max) {
			p.addBox(j.Min, j.Max)
			boxes++
		}
	}
	if boxes == 0 {
		p.addBox(m.Min, m.Max)
	}
	p.bounds()
	return p
}

func degenerate(lo, hi [3]float32) bool {
	for i := 0; i < 3; i++ {
		if hi[i] <= lo[i] {
			return true
		}
	}
	return false
}

var boxFaces = [12][3]int{
	{0, 2, 1}, {1, 2, 3}, {4, 5, 6}, {5, 7, 6}, {0, 1, 4}, {1, 5, 4},
	{2, 6, 3}, {3, 6, 7}, {0, 4, 2}, {2, 4, 6}, {1, 3, 5}, {3, 7, 5},
}

func (m *Mesh) addBox(lo, hi [3]float32) {
	base := uint32(m.Verts())
	for c := 0; c < 8; c++ {
		x, y, z := lo[0], lo[1], lo[2]
		if c&4 != 0 {
			x = hi[0]
		}
		if c&2 != 0 {
			y = hi[1]
		}
		if c&1 != 0 {
			z = hi[2]
		}
		m.Positions = append(m.Positions, x, y, z)
		m.Normals = append(m.Normals, 0, 1, 0)
		m.UVs = append(m.UVs, 0, 0)
	}
	for _, f := range boxFaces {
		m.Indices = append(m.Indices, base+uint32(f[0]), base+uint32(f[1]), base+uint32(f[2]))
	}
}

func (m *Mesh) bounds() {
	if len(m.Positions) == 0 {
		m.Min, m.Max = [3]float32{}, [3]float32{}
		return
	}
	m.Min = [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	m.Max = [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for i := 0; i < len(m.Positions); i += 3 {
		for a := 0; a < 3; a++ {
			v := m.Positions[i+a]
			if v < m.Min[a] {
				m.Min[a] = v
			}
			if v > m.Max[a] {
				m.Max[a] = v
			}
		}
	}
}

// pack is one decoded packet's vertex block, ready to be strip-expanded.
type pack struct {
	n    int
	pos  []float32 // 3n, scaled
	nrm  []float32 // 3n, normalized
	uv   []float32 // 2n
	kick []bool    // n; true = strip break at this vertex
}

// appendStrip expands a triangle strip with breaks and returns the range it wrote. The winding is
// NOT the strip parity -- it is resolved per triangle against the stored vertex normal
// (kms2obj main.cpp:586-651 / 858-926).
func (m *Mesh) appendStrip(p pack) Part {
	base := uint32(m.Verts())
	first := len(m.Indices)
	m.Positions = append(m.Positions, p.pos...)
	m.Normals = append(m.Normals, p.nrm...)
	m.UVs = append(m.UVs, p.uv...)

	i1, i2, i3 := 0, 1, 2
	clk := true
	for v := 2; v < p.n; v++ {
		if !p.kick[v] {
			e1 := sub3(p.pos, i2, i1)
			e2 := sub3(p.pos, i3, i1)
			nc := cross3(e1, e2)
			n := [3]float32{p.nrm[3*v], p.nrm[3*v+1], p.nrm[3*v+2]}
			a, b, c := uint32(i1), uint32(i2), uint32(i3)
			if dot3(n, nc) <= 0 {
				b, c = c, b
			}
			m.Indices = append(m.Indices, base+a, base+b, base+c)
		}
		if clk {
			i1 = i3
		} else {
			i2 = i3
		}
		clk = !clk
		i3++
	}
	return Part{First: first, Count: len(m.Indices) - first, Base: int(base), Verts: p.n}
}

func sub3(p []float32, a, b int) [3]float32 {
	return [3]float32{p[3*a] - p[3*b], p[3*a+1] - p[3*b+1], p[3*a+2] - p[3*b+2]}
}

func cross3(a, b [3]float32) [3]float32 {
	return [3]float32{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func dot3(a, b [3]float32) float32 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

// reader is a bounds-checked view of the whole file: every "pointer" in a .kms/.evm is a 32-bit
// byte offset from the file base, fixed up at load by adding the base address. 0 means NULL.
type reader struct {
	b   []byte
	err error
}

func (r *reader) fail(format string, a ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, a...)
	}
}

func (r *reader) have(off, n int) bool {
	if off < 0 || n < 0 || off+n > len(r.b) {
		r.fail("offset 0x%x+%d out of range (file is %d bytes)", off, n, len(r.b))
		return false
	}
	return true
}

func (r *reader) u32(off int) uint32 {
	if !r.have(off, 4) {
		return 0
	}
	return binary.LittleEndian.Uint32(r.b[off:])
}

func (r *reader) u16(off int) uint16 {
	if !r.have(off, 2) {
		return 0
	}
	return binary.LittleEndian.Uint16(r.b[off:])
}

func (r *reader) i32(off int) int32 { return int32(r.u32(off)) }

// off reads a stored offset -- 4 bytes on PS2, 8 on the MC re-cook. 0 is NULL.
func (r *reader) off(at, width int) int {
	if width == 8 {
		if !r.have(at, 8) {
			return 0
		}
		v := binary.LittleEndian.Uint64(r.b[at:])
		if v > uint64(len(r.b)) {
			r.fail("offset 0x%x out of range (file is %d bytes)", v, len(r.b))
			return 0
		}
		return int(v)
	}
	return int(r.u32(at))
}

func (r *reader) f32(off int) float32 { return math.Float32frombits(r.u32(off)) }

func (r *reader) vec3(off int) [3]float32 {
	return [3]float32{r.f32(off), r.f32(off + 4), r.f32(off + 8)}
}

// shorts reads n int16s, or nil if the offset is NULL.
func (r *reader) shorts(off, n int) []int16 {
	if off == 0 {
		return nil
	}
	if !r.have(off, 2*n) {
		return nil
	}
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(r.b[off+2*i:]))
	}
	return out
}

func (r *reader) bytesAt(off, n int) []byte {
	if off == 0 || !r.have(off, n) {
		return nil
	}
	return r.b[off : off+n]
}

// BakeOrigin folds Origin into the vertex positions and un-skins the mesh.
//
// Map areas are ONEPIECE: the engine never poses their joints, it just adds DG_DEF.tx/ty/tz
// to the root. Exporting them as a SkinnedMesh and translating the node does NOT work --
// three.js cancels a skinned mesh's own world matrix (bindMode "attached") -- so bake the
// offset into the geometry and drop the skin. Callers that DO pose a model must not use this.
func (m *Mesh) BakeOrigin() {
	o := m.Origin
	if o == [3]float32{} && !m.Skinned {
		return
	}
	for i := 0; i+2 < len(m.Positions); i += 3 {
		m.Positions[i] += o[0]
		m.Positions[i+1] += o[1]
		m.Positions[i+2] += o[2]
	}
	for a := 0; a < 3; a++ {
		m.Min[a] += o[a]
		m.Max[a] += o[a]
	}
	m.Skinned = false
	m.Bones = nil
	m.Weights = nil
	m.Skeleton = nil
	m.Joints = 0
	m.Origin = [3]float32{}
}
