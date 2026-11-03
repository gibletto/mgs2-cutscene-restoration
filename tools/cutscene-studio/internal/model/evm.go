package model

import (
	"fmt"
	"math"
	"os"
)

// EVM is the skinned character mesh: a joint skeleton plus packets of <=32 vertices, each packet
// carrying its own 8-entry matrix palette. Unlike KMS, EVM vertices are already in model space --
// the skinning does the placing -- so there is no rest transform to apply here.

// LoadEVM decodes a skinned model: geometry, the bind-pose skeleton, and 4-bone skin weights.
func LoadEVM(path string) (*Mesh, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeEVM(b, path)
}

func decodeEVM(b []byte, src string) (*Mesh, error) {
	r := &reader{b: b}
	nJoints := int(r.u32(4)) // n_x_models: 186 of 366 files disagree with n_models, and this is the right one
	if r.err != nil {
		return nil, fmt.Errorf("evm %s: %w", src, r.err)
	}
	l, err := detectEVM(r, nJoints)
	if err != nil {
		return nil, fmt.Errorf("evm %s: %w", src, err)
	}
	typ := r.u32(l.typ)
	nPacks := int(r.u32(l.nPacks))
	packOff := r.off(l.packet, l.ptr)
	if r.err != nil {
		return nil, fmt.Errorf("evm %s: %w", src, r.err)
	}

	m := &Mesh{Kind: "evm", Source: src, Skinned: true, Joints: nJoints, TexGroup: r.u32(evmTexGroup)}
	for j := 0; j < nJoints; j++ {
		o := l.def + evmSkelSize*j
		m.Skeleton = append(m.Skeleton, Joint{
			Parent:    r.i32(o + 4),
			Local:     r.vec3(o + 8),
			Rest:      r.vec3(o + 0x14), // stored, and it agrees with the parent chain -- a free format check
			Min:       r.vec3(o + 0x20),
			Max:       r.vec3(o + 0x30),
			HasBounds: true,
		})
	}
	if r.err != nil {
		return nil, fmt.Errorf("evm %s: %w", src, r.err)
	}

	scale := float32(1) / 16 // 1.11.4
	if typ&evmTypeLarge != 0 {
		scale = 1 // DG_EVMTYPE_LARGE: 1.15.0
	}
	for p := 0; p < nPacks; p++ {
		po := packOff + l.pack*p
		flag := r.u32(po)
		tex := r.u32(po + 4)
		n := int(r.u16(po + l.nVerts))
		nMats := int(r.u32(po + l.nMats))
		if r.err != nil {
			return nil, fmt.Errorf("evm %s: pack %d: %w", src, p, r.err)
		}
		if n == 0 {
			continue
		}
		if n > 32 || nMats > 8 {
			return nil, fmt.Errorf("evm %s: pack %d: n_verts=%d n_mats=%d out of spec", src, p, n, nMats)
		}
		matID := r.bytesAt(po+l.matID, 8)
		verts := r.shorts(r.off(po+l.verts, l.ptr), 4*n)
		norms := r.shorts(r.off(po+l.norms, l.ptr), 4*n)
		uvs := r.shorts(r.off(po+l.uv0, l.ptr), 4*n)
		weights := r.bytesAt(r.off(po+l.weight, l.ptr), 8*n)
		if r.err != nil {
			return nil, fmt.Errorf("evm %s: pack %d: %w", src, p, r.err)
		}
		if verts == nil || norms == nil || matID == nil {
			return nil, fmt.Errorf("evm %s: pack %d: null vert/norm/palette block", src, p)
		}
		part := m.appendStrip(evmPack(n, scale, verts, norms, uvs))
		m.appendSkin(n, nMats, matID, weights, nJoints)
		part.Unit, part.Joint, part.Flag, part.Tex = p, -1, flag, tex // skinning is per-vertex, so no single joint
		m.Parts = append(m.Parts, part)
	}
	m.bounds()
	if r.err != nil {
		return nil, fmt.Errorf("evm %s: %w", src, r.err)
	}
	return m, nil
}

// EVM vertices are 1.11.4 (÷16) unless the model is DG_EVMTYPE_LARGE, and the kick flag lives in
// the VERTEX's w -- the mirror image of KMS, where it lives in the normal's w. UVs are 4 shorts
// per vertex here, 2 in KMS.
func evmPack(n int, scale float32, verts, norms, uvs []int16) pack {
	p := pack{n: n, pos: make([]float32, 3*n), nrm: make([]float32, 3*n), uv: make([]float32, 2*n), kick: make([]bool, n)}
	for v := 0; v < n; v++ {
		p.pos[3*v] = float32(verts[4*v]) * scale
		p.pos[3*v+1] = float32(verts[4*v+1]) * scale
		p.pos[3*v+2] = float32(verts[4*v+2]) * scale
		setNormal(p.nrm[3*v:], norms[4*v], norms[4*v+1], norms[4*v+2])
		p.kick[v] = uint16(verts[4*v+3])&drawingKick != 0
		if uvs != nil {
			p.uv[2*v] = float32(uvs[4*v]) / 4096
			p.uv[2*v+1] = float32(uvs[4*v+1]) / 4096
		}
	}
	return p
}

// Skin weights: 8 bytes/vertex = 4 weights (÷128) then 4 byte-offsets into the packet's palette
// (offset/4 = slot, mat_id[slot] = the global joint). The loop runs n_mats, not 4.
func (m *Mesh) appendSkin(n, nMats int, matID, weights []byte, nJoints int) {
	for v := 0; v < n; v++ {
		bones := [4]uint16{}
		w := [4]float32{}
		out := 0
		if weights != nil {
			for i := 0; i < nMats; i++ {
				jw := float32(weights[8*v+i]) / 128
				if jw <= 0 {
					continue
				}
				slot := int(weights[8*v+4+i]) / 4
				if slot >= len(matID) {
					continue
				}
				j := int(matID[slot])
				if j >= nJoints {
					continue
				}
				if out == 4 {
					m.Overflow++
					break
				}
				bones[out], w[out] = uint16(j), jw
				out++
			}
		}
		if out == 0 {
			w[0] = 1 // the runtime's default: fully bound to joint 0
		}
		m.Bones = append(m.Bones, bones[:]...)
		m.Weights = append(m.Weights, w[:]...)
	}
}

func setNormal(dst []float32, x, y, z int16) {
	nx, ny, nz := float32(x)/4096, float32(y)/4096, float32(z)/4096
	if d := float32(math.Sqrt(float64(nx*nx + ny*ny + nz*nz))); d > 1e-6 {
		nx, ny, nz = nx/d, ny/d, nz/d
	} else {
		ny = 1
	}
	dst[0], dst[1], dst[2] = nx, ny, nz
}
