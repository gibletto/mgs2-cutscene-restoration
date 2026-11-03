package model

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

// Structural validation: does the JSON parse, does the buffer's declared byteLength match the bytes
// actually encoded, and does every accessor fit inside its bufferView.
func checkGLTF(t *testing.T, raw []byte, wantSkin bool) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("glTF is not valid JSON: %v", err)
	}
	if doc["asset"].(map[string]any)["version"] != "2.0" {
		t.Error("asset.version != 2.0")
	}
	bufs := doc["buffers"].([]any)
	buf := bufs[0].(map[string]any)
	uri := buf["uri"].(string)
	const prefix = "data:application/octet-stream;base64,"
	if !strings.HasPrefix(uri, prefix) {
		t.Fatal("buffer is not an inline data URI")
	}
	blob, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, prefix))
	if err != nil {
		t.Fatalf("buffer base64: %v", err)
	}
	if got, want := len(blob), int(buf["byteLength"].(float64)); got != want {
		t.Errorf("buffer byteLength = %d, decoded %d bytes", want, got)
	}

	sizes := map[string]int{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT4": 16}
	comps := map[int]int{gltfFloat: 4, gltfUShort: 2, gltfUInt: 4}
	views := doc["bufferViews"].([]any)
	for i, a := range doc["accessors"].([]any) {
		acc := a.(map[string]any)
		v := views[int(acc["bufferView"].(float64))].(map[string]any)
		off, length := int(v["byteOffset"].(float64)), int(v["byteLength"].(float64))
		need := int(acc["count"].(float64)) * sizes[acc["type"].(string)] * comps[int(acc["componentType"].(float64))]
		if need != length {
			t.Errorf("accessor %d: needs %d bytes, view has %d", i, need, length)
		}
		if off+length > len(blob) {
			t.Errorf("accessor %d: view runs past the buffer", i)
		}
		if off%4 != 0 {
			t.Errorf("accessor %d: view offset %d is not 4-byte aligned", i, off)
		}
	}
	_, hasSkin := doc["skins"]
	if hasSkin != wantSkin {
		t.Errorf("skins present = %v, want %v", hasSkin, wantSkin)
	}
	return doc
}

func TestWriteGLTF(t *testing.T) {
	for _, tc := range []struct {
		file  string
		proxy bool
		skin  bool
	}{
		{evmPath(t, "rai_def_mh_mt.evm"), false, true},
		{evmPath(t, "sna_def_mh.evm"), false, true},
		{kmsPath(t, "24a.kms"), false, true},           // KMS units are bones too, so it skins as well
		{evmPath(t, "rai_def_mh_mt.evm"), true, false}, // the proxy fallback
	} {
		m, err := LoadModel(tc.file)
		if err != nil {
			t.Fatal(err)
		}
		if tc.proxy {
			m = m.Proxy()
		}
		var out bytes.Buffer
		if err := m.WriteGLTF(&out); err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		doc := checkGLTF(t, out.Bytes(), tc.skin)
		label := m.Kind
		if tc.proxy {
			label = "proxy"
		}
		t.Logf("%-24s %-5s verts=%5d tris=%5d gltf=%6d bytes nodes=%d",
			baseName(tc.file), label, m.Verts(), m.Tris(), out.Len(), len(doc["nodes"].([]any)))

		if tc.skin {
			skin := doc["skins"].([]any)[0].(map[string]any)
			joints := skin["joints"].([]any)
			if len(joints) != m.Joints {
				t.Errorf("skin has %d joints, mesh has %d", len(joints), m.Joints)
			}
			// every JOINTS_0 index must address a real joint
			prim := doc["meshes"].([]any)[0].(map[string]any)["primitives"].([]any)[0].(map[string]any)
			if _, ok := prim["attributes"].(map[string]any)["JOINTS_0"]; !ok {
				t.Error("skinned mesh has no JOINTS_0")
			}
			for _, b := range m.Bones {
				if int(b) >= m.Joints {
					t.Fatalf("bone index %d >= %d joints", b, m.Joints)
				}
			}
		}
	}
}

// Walk the node tree we just wrote and confirm world(joint) == -inverseBind(joint) for every joint.
// Since every bind transform is a pure translation, that makes the bind-pose skin matrix exactly
// identity -- i.e. an unposed SkinnedMesh in three.js lands on the decoded vertices, not somewhere
// else. This catches a mis-wired parent chain or a stale inverse-bind buffer, which nothing else does.
func TestGLTFBindPoseIsIdentity(t *testing.T) {
	m, err := LoadEVM(evmPath(t, "fat_def_addhand_mh_mt.evm"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := m.WriteGLTF(&out); err != nil {
		t.Fatal(err)
	}
	doc := checkGLTF(t, out.Bytes(), true)
	blob := gltfBlob(t, doc)

	nodes := doc["nodes"].([]any)
	parent := make([]int, len(nodes))
	for i := range parent {
		parent[i] = -1
	}
	trans := make([][3]float32, len(nodes))
	for i, n := range nodes {
		node := n.(map[string]any)
		if tr, ok := node["translation"].([]any); ok {
			for a := 0; a < 3; a++ {
				trans[i][a] = float32(tr[a].(float64))
			}
		}
		kids, _ := node["children"].([]any)
		for _, c := range kids {
			parent[int(c.(float64))] = i
		}
	}

	skin := doc["skins"].([]any)[0].(map[string]any)
	joints := skin["joints"].([]any)
	ibmView := viewOf(t, doc, int(skin["inverseBindMatrices"].(float64)))

	worst := 0.0
	for j, jn := range joints {
		n := int(jn.(float64))
		var world [3]float32
		for k := n; k >= 0; k = parent[k] {
			for a := 0; a < 3; a++ {
				world[a] += trans[k][a]
			}
		}
		// the IBM's translation row sits at floats 12..14 of each mat4
		for a := 0; a < 3; a++ {
			ibm := f32At(blob, ibmView+64*j+4*(12+a))
			if d := math.Abs(float64(world[a] + ibm)); d > worst {
				worst = d
			}
		}
	}
	if worst > 0.01 {
		t.Fatalf("bind pose is not identity: world(joint) + inverseBind off by %g", worst)
	}
	t.Logf("%d joints: world(joint) == -inverseBind(joint) to %g -- bind pose is identity", len(joints), worst)
}

func gltfBlob(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	uri := doc["buffers"].([]any)[0].(map[string]any)["uri"].(string)
	blob, err := base64.StdEncoding.DecodeString(uri[strings.IndexByte(uri, ',')+1:])
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

func viewOf(t *testing.T, doc map[string]any, accessor int) int {
	t.Helper()
	acc := doc["accessors"].([]any)[accessor].(map[string]any)
	v := doc["bufferViews"].([]any)[int(acc["bufferView"].(float64))].(map[string]any)
	return int(v["byteOffset"].(float64))
}

func f32At(b []byte, off int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b[off:]))
}

func TestWriteGLTFWeightsNormalized(t *testing.T) {
	m, err := LoadEVM(evmPath(t, "fat_def_addhand_mh_mt.evm"))
	if err != nil {
		t.Fatal(err)
	}
	w := normalizedWeights(m.Weights)
	worst := float32(0)
	for i := 0; i+3 < len(w); i += 4 {
		d := w[i] + w[i+1] + w[i+2] + w[i+3] - 1
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst = d
		}
	}
	if worst > 1e-5 {
		t.Errorf("weights sum off by %g", worst)
	}
	t.Logf("%d skinned verts, weight sums within %g of 1.0, %d verts had >4 bones", m.Verts(), worst, m.Overflow)
}

func TestCache(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir}
	src := evmPath(t, "rai_def_mh_mt.evm")

	first, err := c.GLTF(src)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.GLTF(src)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("cache missed: %s then %s", first, second)
	}
	fi2, _ := os.Stat(second)
	if !fi.ModTime().Equal(fi2.ModTime()) {
		t.Error("cache rewrote an already-converted model")
	}
	raw, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	checkGLTF(t, raw, true)
	t.Logf("cached %s (%d bytes)", baseName(first), fi.Size())
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}
