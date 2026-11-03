package model

import "fmt"

// MGS2 geometry ships in two struct layouts, and both are on disk right now:
//
//	PS2 -- Konami's own structs (tools/libMDU/fmt_kms.h, MDU_LoadKms2). 32-bit pointers.
//	MC  -- Bluepoint's x64 re-cook, what the Master Collection installs. 64-bit pointers.
//
// The field ORDER is identical; the structs just grew around their pointers, so only the strides
// and the pointer width change. Every stored "pointer" is a byte offset from the file base in both.
// The texture-group id stays put in BOTH layouts -- KMS +0x0C, EVM +0x24, no slide: those offsets
// hold an id that is in the texture flatlist for 3,789/3,789 KMS and 366/366 EVM, and the
// would-be-slid ones hold nothing for 0.
//
// Getting this wrong is the classic failure: read an MC file with PS2 strides and pack 0's uvs[1]
// lands where verts should be, so the first thing you see is a NULL vertex block.

const (
	kmsDefSize  = 0x40 // KMS2_DEF -- same in both
	kmsOrigin   = 0x34 // DG_DEF.tx/ty/tz -- the model's reference display position
	kmsMdlPacks = 0x30 // KMS2_MDL.packs, same offset in both; only the width differs
	kmsTexGroup = 0x0C // KMS2_DEF.texture
	evmTexGroup = 0x24 // EVM_DEF.texture

	evmSkelSize = 0x40 // EVM_SKEL -- 64 B in both (the variant with the per-joint AABB)

	kmsFormatMask = 0x00FFFFFF
	kmsModelNorm  = 13112177 // MGS_MODEL_NORM
	kmsMultiTex   = 11686819 // MGS_MODEL_MULTITEX

	packEnvelope = 0x0001 // DG_PACKFLAG_ENVELOPE -- vert.w blends this unit against its parent
	packTex0     = 0x0008 // DG_PACKFLAG_TEX0 -- tex_id[0] is live
	drawingKick  = 0x8000 // strip break
	evmTypeLarge = 0x0001 // verts are 1.15.0 instead of 1.11.4
)

// kmsLayout is everything that differs between the two KMS struct layouts.
type kmsLayout struct {
	name  string
	ptr   int // stored pointer width
	mdl   int // sizeof KMS2_MDL
	pack  int // sizeof KMS2_MDLPACK
	verts int // KMS2_MDLPACK.verts
	norms int // .norms
	uv0   int // .uvs[0]
}

var (
	kmsPS2 = kmsLayout{name: "ps2", ptr: 4, mdl: 0x40, pack: 0x30, verts: 0x14, norms: 0x18, uv0: 0x1C}
	kmsMC  = kmsLayout{name: "mc", ptr: 8, mdl: 0x50, pack: 0x60, verts: 0x20, norms: 0x28, uv0: 0x30}
)

// evmLayout is the same idea for the skinned format.
type evmLayout struct {
	name   string
	ptr    int
	def    int // sizeof EVM_DEF (where the skeleton starts)
	pack   int // sizeof EVM_PACK
	typ    int // EVM_DEF.type
	nPacks int // .n_packs
	packet int // .packet
	nVerts int // EVM_PACK.n_verts
	nMats  int // .n_mats
	matID  int // .mat_id[8]
	verts  int // .verts
	norms  int // .norms
	uv0    int // .uvs[0]
	weight int // .weight
}

var (
	evmPS2 = evmLayout{name: "ps2", ptr: 4, def: 0x30, pack: 0x40,
		typ: 0x20, nPacks: 0x28, packet: 0x2C,
		nVerts: 0x10, nMats: 0x14, matID: 0x18, verts: 0x20, norms: 0x24, uv0: 0x28, weight: 0x34}
	evmMC = evmLayout{name: "mc", ptr: 8, def: 0x40, pack: 0x70,
		typ: 0x28, nPacks: 0x2C, packet: 0x30,
		nVerts: 0x20, nMats: 0x24, matID: 0x28, verts: 0x30, norms: 0x38, uv0: 0x40, weight: 0x58}
)

// Both formats lay the packet array down immediately after the model array, which pins the stride
// exactly: it holds for 3,922/3,922 PS2 files and 3,920/3,920 MC files, and never for the wrong
// layout. That single invariant is the whole format check.
func detectKMS(r *reader, nUnits int) (kmsLayout, error) {
	for _, l := range []kmsLayout{kmsPS2, kmsMC} {
		if kmsPinned(r, l, nUnits) {
			return l, nil
		}
	}
	// No unit carries geometry, so nothing pins the stride; fall back to the model array
	// exactly filling the file.
	for _, l := range []kmsLayout{kmsPS2, kmsMC} {
		if kmsDefSize+l.mdl*nUnits == len(r.b) {
			return l, nil
		}
	}
	return kmsLayout{}, fmt.Errorf("neither the PS2 nor the MC struct layout fits %d units in %d bytes", nUnits, len(r.b))
}

func kmsPinned(r *reader, l kmsLayout, nUnits int) bool {
	end := kmsDefSize + l.mdl*nUnits
	if nUnits <= 0 || end > len(r.b) {
		return false
	}
	for u := 0; u < nUnits; u++ {
		o := kmsDefSize + l.mdl*u
		if peek32(r.b, o+4) == 0 { // n_packs
			continue
		}
		return peekOff(r.b, o+kmsMdlPacks, l.ptr) == end
	}
	return false
}

func detectEVM(r *reader, nJoints int) (evmLayout, error) {
	for _, l := range []evmLayout{evmPS2, evmMC} {
		if want := l.def + evmSkelSize*nJoints; want <= len(r.b) && peekOff(r.b, l.packet, l.ptr) == want {
			return l, nil
		}
	}
	return evmLayout{}, fmt.Errorf("neither the PS2 nor the MC struct layout fits %d joints in %d bytes", nJoints, len(r.b))
}

// peek* read without disturbing the reader's error state -- detection probes wrong layouts on
// purpose, so an out-of-range read there is an answer, not a failure.
func peek32(b []byte, off int) uint32 {
	if off < 0 || off+4 > len(b) {
		return 0
	}
	return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
}

func peekOff(b []byte, off, width int) int {
	if width == 8 {
		if off < 0 || off+8 > len(b) || peek32(b, off+4) != 0 {
			return -1
		}
	}
	if off < 0 || off+4 > len(b) {
		return -1
	}
	return int(peek32(b, off))
}
