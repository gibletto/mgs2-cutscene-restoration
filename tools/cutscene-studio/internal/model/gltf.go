package model

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

// glTF 2.0 is JSON plus one binary blob, so the whole exporter is stdlib. MGS2 is right-handed
// Y-up in metres-ish units and so is glTF -- no axis flip, no scale.

const (
	gltfFloat  = 5126
	gltfUShort = 5123
	gltfUInt   = 5125

	targetArray = 34962
	targetIndex = 34963
)

type gltfDoc struct {
	Asset       gltfAsset      `json:"asset"`
	Scene       int            `json:"scene"`
	Scenes      []gltfScene    `json:"scenes"`
	Nodes       []gltfNode     `json:"nodes"`
	Meshes      []gltfMesh     `json:"meshes"`
	Skins       []gltfSkin     `json:"skins,omitempty"`
	Materials   []gltfMaterial `json:"materials,omitempty"`
	Textures    []gltfTexture  `json:"textures,omitempty"`
	Images      []gltfImage    `json:"images,omitempty"`
	Samplers    []gltfSampler  `json:"samplers,omitempty"`
	Accessors   []gltfAccessor `json:"accessors"`
	BufferViews []gltfView     `json:"bufferViews"`
	Buffers     []gltfBuffer   `json:"buffers"`
}

type gltfAsset struct {
	Version   string `json:"version"`
	Generator string `json:"generator"`
}

type gltfScene struct {
	Nodes  []int       `json:"nodes"`
	Extras *gltfExtras `json:"extras,omitempty"`
}

type gltfExtras struct {
	// Origin is DG_DEF.tx/ty/tz. Shipped as extras rather than baked into a node, because
	// the engine applies it ONLY to ONEPIECE (rigid) objects -- baking it would misplace
	// the 31 jointed demo-object models that carry a nonzero one.
	Origin [3]float32 `json:"origin"`
}

type gltfNode struct {
	Name        string      `json:"name,omitempty"`
	Mesh        *int        `json:"mesh,omitempty"`
	Skin        *int        `json:"skin,omitempty"`
	Children    []int       `json:"children,omitempty"`
	Translation *[3]float32 `json:"translation,omitempty"`
}

type gltfMesh struct {
	Name       string          `json:"name,omitempty"`
	Primitives []gltfPrimitive `json:"primitives"`
}

type gltfPrimitive struct {
	Attributes map[string]int `json:"attributes"`
	Indices    int            `json:"indices"`
	Material   *int           `json:"material,omitempty"`
	Mode       int            `json:"mode"`
}

type gltfSkin struct {
	InverseBindMatrices int   `json:"inverseBindMatrices"`
	Skeleton            int   `json:"skeleton"`
	Joints              []int `json:"joints"`
}

type gltfMaterial struct {
	Name       string   `json:"name"`
	PBR        gltfPBR  `json:"pbrMetallicRoughness"`
	DoubleSide bool     `json:"doubleSided"`
	AlphaMode  string   `json:"alphaMode,omitempty"`
	AlphaCut   *float32 `json:"alphaCutoff,omitempty"`
}

type gltfPBR struct {
	BaseColor    [4]float32  `json:"baseColorFactor"`
	BaseColorTex *gltfTexRef `json:"baseColorTexture,omitempty"`
	Metallic     float32     `json:"metallicFactor"`
	Roughness    float32     `json:"roughnessFactor"`
}

type gltfTexRef struct {
	Index    int `json:"index"`
	TexCoord int `json:"texCoord"`
}

type gltfTexture struct {
	Sampler int `json:"sampler"`
	Source  int `json:"source"`
}

type gltfImage struct {
	Name       string `json:"name,omitempty"`
	MimeType   string `json:"mimeType"`
	BufferView int    `json:"bufferView"`
}

type gltfSampler struct {
	MagFilter int `json:"magFilter"`
	MinFilter int `json:"minFilter"`
	WrapS     int `json:"wrapS"`
	WrapT     int `json:"wrapT"`
}

type gltfAccessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float32 `json:"min,omitempty"`
	Max           []float32 `json:"max,omitempty"`
}

type gltfView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
	Target     int `json:"target,omitempty"`
}

type gltfBuffer struct {
	ByteLength int    `json:"byteLength"`
	URI        string `json:"uri"`
}

type gltfBuild struct {
	doc  gltfDoc
	buf  bytes.Buffer
	flat *int           // the shared untextured material, made on demand
	mats map[string]int // pool target -> material index
}

func (g *gltfBuild) view(data []byte, target int) int {
	for g.buf.Len()%4 != 0 {
		g.buf.WriteByte(0)
	}
	v := gltfView{ByteOffset: g.buf.Len(), ByteLength: len(data), Target: target}
	g.buf.Write(data)
	g.doc.BufferViews = append(g.doc.BufferViews, v)
	return len(g.doc.BufferViews) - 1
}

func (g *gltfBuild) accessor(a gltfAccessor) int {
	g.doc.Accessors = append(g.doc.Accessors, a)
	return len(g.doc.Accessors) - 1
}

func floats(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func uints(v []uint32) []byte {
	b := make([]byte, 4*len(v))
	for i, u := range v {
		binary.LittleEndian.PutUint32(b[4*i:], u)
	}
	return b
}

func ushorts(v []uint16) []byte {
	b := make([]byte, 2*len(v))
	for i, u := range v {
		binary.LittleEndian.PutUint16(b[2*i:], u)
	}
	return b
}

// WriteGLTF emits the mesh untextured -- one flat double-sided material.
func (m *Mesh) WriteGLTF(w io.Writer) error { return m.WriteGLTFWith(w, nil) }

// WriteGLTFWith emits the mesh as a self-contained glTF 2.0 file (buffer inlined as a data URI),
// which three.js GLTFLoader eats directly. Skinned models come out as a real SkinnedMesh: one node
// per EVM joint, bind pose translation-only, joint indices unchanged -- so DEMO_MOTION quaternions
// can drive skeleton.bones[start_joint+i] with no remapping.
//
// With a TextureIndex, each packet's tex_id[0] is resolved to a cooked .ctxr, decoded, and embedded
// as a PNG; the mesh splits into one primitive per texture. All the primitives share one set of
// attribute accessors, so the skin is untouched by the split. Packets whose texture will not
// resolve fall back to the flat material -- an untextured part beats a wrong one.
func (m *Mesh) WriteGLTFWith(w io.Writer, ix *TextureIndex) error {
	if len(m.Positions) == 0 || len(m.Indices) == 0 {
		return errors.New("model: nothing to export (empty mesh)")
	}
	// MGS2 stores normals pointing INWARD: the preshader dots against the vector from the
	// light TOWARD the surface (pshade.c:284), i.e. -N. Every standard renderer wants the
	// outward normal, and with the stored one you light the ceiling and leave the floor
	// black -- which is exactly what it does. Flip once, here, on the way out.
	m.flipNormals()
	if len(m.Normals) != len(m.Positions) {
		return fmt.Errorf("model: %d positions vs %d normals", m.Verts(), len(m.Normals)/3)
	}
	g := &gltfBuild{}
	g.doc.Asset = gltfAsset{Version: "2.0", Generator: "mgs2-cutscene-studio/model"}

	attrs := map[string]int{}
	attrs["POSITION"] = g.accessor(gltfAccessor{
		BufferView:    g.view(floats(m.Positions), targetArray),
		ComponentType: gltfFloat, Count: m.Verts(), Type: "VEC3",
		Min: m.Min[:], Max: m.Max[:],
	})
	attrs["NORMAL"] = g.accessor(gltfAccessor{
		BufferView:    g.view(floats(m.Normals), targetArray),
		ComponentType: gltfFloat, Count: m.Verts(), Type: "VEC3",
	})
	if len(m.UVs) == 2*m.Verts() {
		attrs["TEXCOORD_0"] = g.accessor(gltfAccessor{
			BufferView:    g.view(floats(m.UVs), targetArray),
			ComponentType: gltfFloat, Count: m.Verts(), Type: "VEC2",
		})
	}

	skinned := m.Skinned && len(m.Skeleton) > 0 && len(m.Bones) == 4*m.Verts() && len(m.Weights) == 4*m.Verts()
	if skinned {
		attrs["JOINTS_0"] = g.accessor(gltfAccessor{
			BufferView:    g.view(ushorts(m.Bones), targetArray),
			ComponentType: gltfUShort, Count: m.Verts(), Type: "VEC4",
		})
		attrs["WEIGHTS_0"] = g.accessor(gltfAccessor{
			BufferView:    g.view(floats(normalizedWeights(m.Weights)), targetArray),
			ComponentType: gltfFloat, Count: m.Verts(), Type: "VEC4",
		})
	}

	g.doc.Meshes = []gltfMesh{{Name: m.Kind, Primitives: g.primitives(m, ix, attrs)}}

	// node 0 is the mesh; joints follow, so joint j is node j+1.
	meshIdx := 0
	root := gltfNode{Name: "model", Mesh: &meshIdx}
	g.doc.Nodes = []gltfNode{root}
	sceneRoots := []int{0}

	if skinned {
		joints := make([]int, len(m.Skeleton))
		for j, s := range m.Skeleton {
			t := s.Local
			g.doc.Nodes = append(g.doc.Nodes, gltfNode{
				Name:        fmt.Sprintf("joint%d", j),
				Translation: &t,
			})
			joints[j] = j + 1
		}
		skelRoot := -1
		for j, s := range m.Skeleton {
			if p := int(s.Parent); p >= 0 && p < len(m.Skeleton) {
				g.doc.Nodes[p+1].Children = append(g.doc.Nodes[p+1].Children, j+1)
			} else if skelRoot < 0 {
				skelRoot = j + 1
			}
		}
		if skelRoot < 0 {
			return errors.New("model: skeleton has no root joint")
		}
		ibm := g.accessor(gltfAccessor{
			BufferView:    g.view(floats(inverseBind(m.Skeleton)), 0),
			ComponentType: gltfFloat, Count: len(m.Skeleton), Type: "MAT4",
		})
		skin := 0
		g.doc.Nodes[0].Skin = &skin
		g.doc.Skins = []gltfSkin{{InverseBindMatrices: ibm, Skeleton: skelRoot, Joints: joints}}
		sceneRoots = append(sceneRoots, skelRoot)
	}

	g.doc.Scene = 0
	g.doc.Scenes = []gltfScene{{Nodes: sceneRoots, Extras: &gltfExtras{Origin: m.Origin}}}
	g.doc.Buffers = []gltfBuffer{{
		ByteLength: g.buf.Len(),
		URI:        "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString(g.buf.Bytes()),
	}}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(&g.doc)
}

const (
	gltfLinear      = 9729
	gltfLinearMip   = 9987
	gltfRepeat      = 10497 // MGS2 UVs run outside [0,1] wherever a surface tiles
	alphaCutoffHalf = float32(0.5)
)

// primitives groups the packets by resolved texture and emits one primitive each. The vertex
// attributes are shared, so only the index buffer is split -- which is what keeps JOINTS_0/WEIGHTS_0
// valid across every primitive.
func (g *gltfBuild) primitives(m *Mesh, ix *TextureIndex, attrs map[string]int) []gltfPrimitive {
	// A proxy has no packets; and with no index there is nothing to look up.
	if len(m.Parts) == 0 || ix == nil {
		return []gltfPrimitive{{Attributes: attrs, Indices: g.indexOf(m.Indices), Material: g.flatMaterial(), Mode: 4}}
	}

	type group struct {
		target string // "" = flat
		idx    []uint32
	}
	var groups []*group
	at := map[string]*group{}
	get := func(target string) *group {
		if gr, ok := at[target]; ok {
			return gr
		}
		gr := &group{target: target}
		at[target] = gr
		groups = append(groups, gr)
		return gr
	}

	for _, p := range m.Parts {
		if p.Count == 0 {
			continue
		}
		target := ""
		if p.Textured() {
			if t, _, ok := ix.Resolve(m.TexGroup, p.Tex); ok {
				target = t
			}
		}
		gr := get(target)
		gr.idx = append(gr.idx, m.Indices[p.First:p.First+p.Count]...)
	}

	var out []gltfPrimitive
	for _, gr := range groups {
		if len(gr.idx) == 0 {
			continue
		}
		var mat *int
		if gr.target != "" {
			// A texture that will not decode falls back to flat rather than to a guess.
			if tm, err := g.texMaterial(ix, gr.target); err == nil {
				mat = tm
			}
		}
		if mat == nil {
			mat = g.flatMaterial()
		}
		out = append(out, gltfPrimitive{Attributes: attrs, Indices: g.indexOf(gr.idx), Material: mat, Mode: 4})
	}
	if len(out) == 0 {
		out = append(out, gltfPrimitive{Attributes: attrs, Indices: g.indexOf(m.Indices), Material: g.flatMaterial(), Mode: 4})
	}
	return out
}

func (g *gltfBuild) indexOf(idx []uint32) int {
	return g.accessor(gltfAccessor{
		BufferView:    g.view(uints(idx), targetIndex),
		ComponentType: gltfUInt, Count: len(idx), Type: "SCALAR",
	})
}

func (g *gltfBuild) flatMaterial() *int {
	if g.flat != nil {
		return g.flat
	}
	g.doc.Materials = append(g.doc.Materials, gltfMaterial{
		Name:       "mgs2_flat",
		PBR:        gltfPBR{BaseColor: [4]float32{0.8, 0.8, 0.82, 1}, Metallic: 0, Roughness: 0.9},
		DoubleSide: true,
	})
	i := len(g.doc.Materials) - 1
	g.flat = &i
	return g.flat
}

// texMaterial decodes the pool texture once per glTF and hands back its material index.
//
// MGS2 is a PS2 game: double-sided is the norm (DG_TYPE_SINGLESIDE is the exception), and the
// alpha mode comes from the texels because the packet does not carry a blend state we can read.
func (g *gltfBuild) texMaterial(ix *TextureIndex, target string) (*int, error) {
	if i, ok := g.mats[target]; ok {
		return &i, nil
	}
	b, err := ix.Bake(target)
	if err != nil {
		return nil, err
	}
	view := g.view(b.png, 0)
	g.doc.Images = append(g.doc.Images, gltfImage{Name: target, MimeType: "image/png", BufferView: view})
	if len(g.doc.Samplers) == 0 {
		g.doc.Samplers = append(g.doc.Samplers, gltfSampler{
			MagFilter: gltfLinear, MinFilter: gltfLinearMip, WrapS: gltfRepeat, WrapT: gltfRepeat,
		})
	}
	g.doc.Textures = append(g.doc.Textures, gltfTexture{Sampler: 0, Source: len(g.doc.Images) - 1})

	mat := gltfMaterial{
		Name: target,
		PBR: gltfPBR{
			BaseColor:    [4]float32{1, 1, 1, 1},
			BaseColorTex: &gltfTexRef{Index: len(g.doc.Textures) - 1},
			Metallic:     0, Roughness: 0.9,
		},
		DoubleSide: true,
		AlphaMode:  "OPAQUE",
	}
	switch b.alpha {
	case alphaMask:
		cut := alphaCutoffHalf
		mat.AlphaMode, mat.AlphaCut = "MASK", &cut
	case alphaBlend:
		mat.AlphaMode = "BLEND"
	}
	g.doc.Materials = append(g.doc.Materials, mat)
	i := len(g.doc.Materials) - 1
	if g.mats == nil {
		g.mats = map[string]int{}
	}
	g.mats[target] = i
	return &i, nil
}

// glTF wants the four weights to sum to 1; the 0.1.7 fixed-point ones don't always, quite.
func normalizedWeights(w []float32) []float32 {
	out := make([]float32, len(w))
	copy(out, w)
	for i := 0; i+3 < len(out); i += 4 {
		s := out[i] + out[i+1] + out[i+2] + out[i+3]
		if s <= 0 {
			out[i] = 1
			continue
		}
		for k := 0; k < 4; k++ {
			out[i+k] /= s
		}
	}
	return out
}

// The bind pose is translation-only, so the inverse bind matrix is just -Rest. (Rest is stored in
// the file AND recomputable from the parent chain; the two agree, which is a free format check.)
func inverseBind(sk []Joint) []float32 {
	out := make([]float32, 0, 16*len(sk))
	for _, j := range sk {
		out = append(out,
			1, 0, 0, 0,
			0, 1, 0, 0,
			0, 0, 1, 0,
			-j.Rest[0], -j.Rest[1], -j.Rest[2], 1)
	}
	return out
}

// flipNormals negates the stored normals. Idempotent per export: it works on a copy of the
// slice so re-exporting the same in-memory Mesh does not flip it back.
func (m *Mesh) flipNormals() {
	if len(m.Normals) == 0 || m.normalsFlipped {
		return
	}
	n := make([]float32, len(m.Normals))
	for i, v := range m.Normals {
		n[i] = -v
	}
	m.Normals = n
	m.normalsFlipped = true
}
