package model

import (
	"fmt"
	"os"
)

// KMS is the rigid mesh: a tree of units (KMS2_MDL), each a bag of triangle-strip packets.
//
// A unit is a JOINT, not a mesh group. Its vertices are stored in the unit's own local space and
// the unit's tx/ty/tz places it under its parent -- so a decoder that dumps the raw shorts gets a
// model collapsed onto its own root. Snake comes out 871 units tall that way instead of 1956, and
// Raiden 660 instead of 1768. kms2obj's OBJ path has exactly this bug; its MDL path doesn't,
// because it tags each submesh with unitIdx and lets the cooker apply the transform (main.cpp:658).
// We apply it here, so Positions are model-space.
//
// Stage geometry has zero unit translations throughout, which is why the bug hides so well.

// LoadKMS decodes a rigid model: positions, normals, uvs, triangles, a unit skeleton, and the
// 2-bone envelope weights that let the units be posed.
func LoadKMS(path string) (*Mesh, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeKMS(b, path)
}

func decodeKMS(b []byte, src string) (*Mesh, error) {
	r := &reader{b: b}
	format := r.u32(0) & kmsFormatMask
	nUnits := int(r.u32(8)) // n_x_models -- includes extension units; n_models undercounts
	if r.err != nil {
		return nil, fmt.Errorf("kms %s: %w", src, r.err)
	}
	if format != kmsModelNorm && format != kmsMultiTex {
		return nil, fmt.Errorf("kms %s: unknown data_format %d", src, format)
	}
	l, err := detectKMS(r, nUnits)
	if err != nil {
		return nil, fmt.Errorf("kms %s: %w", src, err)
	}

	m := &Mesh{Kind: "kms", Source: src, Skinned: true, TexGroup: r.u32(kmsTexGroup)}
	m.Origin = r.vec3(kmsOrigin)

	// The whole skeleton has to resolve before a single vertex can be placed.
	for u := 0; u < nUnits; u++ {
		o := kmsDefSize + l.mdl*u
		m.Skeleton = append(m.Skeleton, Joint{
			Parent:    r.i32(o + 0x2C),
			Local:     r.vec3(o + 0x20),
			Min:       r.vec3(o + 0x08),
			Max:       r.vec3(o + 0x14),
			HasBounds: true,
		})
	}
	if r.err != nil {
		return nil, fmt.Errorf("kms %s: %w", src, r.err)
	}
	resolveRest(m.Skeleton)
	// The per-unit AABB is unit-local too (it matches the raw shorts exactly, every unit, every
	// file). Lift it to model space so Joint.Min/Max means the same thing it does for EVM.
	for i := range m.Skeleton {
		j := &m.Skeleton[i]
		for a := 0; a < 3; a++ {
			j.Min[a] += j.Rest[a]
			j.Max[a] += j.Rest[a]
		}
	}

	for u := 0; u < nUnits; u++ {
		o := kmsDefSize + l.mdl*u
		nPacks := int(r.u32(o + 4))
		packs := r.off(o+kmsMdlPacks, l.ptr)
		parent := int(m.Skeleton[u].Parent)
		if parent < 0 || parent >= nUnits {
			parent = u // a root unit blends against itself, which is just "rigid"
		}
		for p := 0; p < nPacks; p++ {
			po := packs + l.pack*p
			flag := r.u32(po)
			n := int(r.u16(po + 4))
			tex := r.u32(po + 8)
			if r.err != nil {
				return nil, fmt.Errorf("kms %s: unit %d pack %d: %w", src, u, p, r.err)
			}
			if n == 0 {
				continue
			}
			verts := r.shorts(r.off(po+l.verts, l.ptr), 4*n)
			norms := r.shorts(r.off(po+l.norms, l.ptr), 4*n)
			uvs := r.shorts(r.off(po+l.uv0, l.ptr), 2*n)
			if r.err != nil {
				return nil, fmt.Errorf("kms %s: unit %d pack %d: %w", src, u, p, r.err)
			}
			if verts == nil || norms == nil {
				return nil, fmt.Errorf("kms %s: unit %d pack %d: null vert/norm block", src, u, p)
			}
			part := m.appendStrip(kmsPack(n, m.Skeleton[u].Rest, verts, norms, uvs))
			m.appendUnitSkin(n, u, parent, flag&packEnvelope != 0, verts)
			part.Unit, part.Joint, part.Flag, part.Tex = u, u, flag, tex
			m.Parts = append(m.Parts, part)
		}
	}
	m.Joints = len(m.Skeleton)
	m.bounds()
	if r.err != nil {
		return nil, fmt.Errorf("kms %s: %w", src, r.err)
	}
	return m, nil
}

// KMS vertices are 1.15.0 raw shorts in unit-local space; rest lifts them to model space. The kick
// flag lives in the NORMAL's w -- that is the one thing that differs from EVM, where it is in the
// vertex's w, and the easiest way to get this wrong.
func kmsPack(n int, rest [3]float32, verts, norms, uvs []int16) pack {
	p := pack{n: n, pos: make([]float32, 3*n), nrm: make([]float32, 3*n), uv: make([]float32, 2*n), kick: make([]bool, n)}
	for v := 0; v < n; v++ {
		p.pos[3*v] = float32(verts[4*v]) + rest[0]
		p.pos[3*v+1] = float32(verts[4*v+1]) + rest[1]
		p.pos[3*v+2] = float32(verts[4*v+2]) + rest[2]
		setNormal(p.nrm[3*v:], norms[4*v], norms[4*v+1], norms[4*v+2])
		p.kick[v] = uint16(norms[4*v+3])&drawingKick != 0
		if uvs != nil {
			p.uv[2*v] = float32(uvs[2*v]) / 4096
			p.uv[2*v+1] = float32(uvs[2*v+1]) / 4096
		}
	}
	return p
}

// A KMS unit IS a bone. Rigid packets ride it whole; an envelope packet (DG_PACKFLAG_ENVELOPE)
// blends each vertex between the unit and its parent by vert.w, 1.3.12 -- 1.0 is all this unit,
// 0.0 is all the parent (fmt_kms.h). Real weights out of the file, not invented ones.
func (m *Mesh) appendUnitSkin(n, unit, parent int, envelope bool, verts []int16) {
	for v := 0; v < n; v++ {
		w := float32(1)
		if envelope {
			w = float32(verts[4*v+3]) / 4096
			if w < 0 {
				w = 0
			} else if w > 1 {
				w = 1
			}
		}
		m.Bones = append(m.Bones, uint16(unit), uint16(parent), 0, 0)
		m.Weights = append(m.Weights, w, 1-w, 0, 0)
	}
}

// Rest is the parent chain summed. Units are usually stored parent-first but nothing in the format
// promises it, so resolve on demand, with a guard: a cycle would otherwise hang the decoder.
func resolveRest(sk []Joint) {
	const (
		todo = iota
		busy
		done
	)
	state := make([]byte, len(sk))
	var resolve func(i int) [3]float32
	resolve = func(i int) [3]float32 {
		if state[i] == done {
			return sk[i].Rest
		}
		r := sk[i].Local
		if p := int(sk[i].Parent); state[i] != busy && p >= 0 && p < len(sk) && p != i {
			state[i] = busy
			pr := resolve(p)
			for a := 0; a < 3; a++ {
				r[a] += pr[a]
			}
		}
		sk[i].Rest = r
		state[i] = done
		return r
	}
	for i := range sk {
		resolve(i)
	}
}
