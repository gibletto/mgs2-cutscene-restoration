package model

import (
	"encoding/binary"
	"fmt"
	"image"
)

// CTXR is Bluepoint's cooked texture -- the one the shipped PC game actually loads
// (BP_BaseRenderer.cpp:1265 builds "<triId>/<texId>.ctxr" and hands it to the resource cache).
// The whole format is in Win32CTexture.cpp:37-225 (CBaseTexture::Create), and it is friendly:
// a big-endian header padded to 128 bytes, then the mip chain, each level prefixed by its
// byte count and padded up to a 32-byte boundary.
//
// All 15,292 shipped .ctxr are version 7, kType_Normal, kFormat_A8R8G8B8 -- i.e. UNCOMPRESSED
// 32-bit, no BCn anywhere. Walking the mip chain lands exactly on EOF for 15,292/15,292 files,
// which pins every field in the header at once.
//
// kFormat_A8R8G8B8 maps to D3DFMT_A8R8G8B8 (Win32CTexture.cpp:465), a little-endian 0xAARRGGBB
// DWORD -- so the bytes on disk are B,G,R,A.

const (
	ctxrMagic  = 0x54585452 // 'TXTR'
	ctxrHeader = 128        // kFixedHeaderSize
	ctxrRGBA   = 0          // kFormat_A8R8G8B8
	ctxr2D     = 0          // kType_Normal
)

// Texture is one decoded .ctxr, mip 0 only.
type Texture struct {
	Name  string
	Image *image.NRGBA
	Alpha alphaKind
}

type alphaKind int

const (
	alphaOpaque alphaKind = iota
	alphaMask             // hair, grates, foliage: alpha is effectively on/off
	alphaBlend            // glass, smoke: real gradients
)

// decodeCTXR reads mip 0 and converts it to straight RGBA.
//
// The alpha needs rescaling. These textures keep the PS2 convention where the GS treats 0x80 as
// fully opaque (SCE_GS_SET_ALPHA(...,128) all over libdg/text.c), and the bytes were carried
// across unchanged: 228.8M of the shipped alpha bytes are exactly 128 and 10,172 of the 15,292
// files never exceed it. Emitting those raw would render the entire game 50% transparent.
func decodeCTXR(b []byte, name string) (*Texture, error) {
	if len(b) < ctxrHeader {
		return nil, fmt.Errorf("ctxr %s: %d bytes, shorter than the header", name, len(b))
	}
	be := binary.BigEndian
	if be.Uint32(b) != ctxrMagic {
		return nil, fmt.Errorf("ctxr %s: bad magic %08x", name, be.Uint32(b))
	}
	if v := be.Uint32(b[4:]); v > 7 {
		return nil, fmt.Errorf("ctxr %s: version %d > 7", name, v)
	}
	w, h := int(be.Uint16(b[8:])), int(be.Uint16(b[10:]))
	format := be.Uint32(b[14:])
	typ := be.Uint32(b[0x22:])
	if format != ctxrRGBA {
		return nil, fmt.Errorf("ctxr %s: format %d, only A8R8G8B8 is handled", name, format)
	}
	if typ != ctxr2D {
		return nil, fmt.Errorf("ctxr %s: type %d, only 2D is handled", name, typ)
	}
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 {
		return nil, fmt.Errorf("ctxr %s: implausible size %dx%d", name, w, h)
	}
	n := int(be.Uint32(b[ctxrHeader:])) // mip 0's byte count
	src := ctxrHeader + 4
	if n != 4*w*h || src+n > len(b) {
		return nil, fmt.Errorf("ctxr %s: mip 0 is %d bytes, expected %d for %dx%d", name, n, 4*w*h, w, h)
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	soft, clear := 0, 0
	for i := 0; i < w*h; i++ {
		p := b[src+4*i:]
		a := int(p[3]) * 255 / 128
		if a > 255 {
			a = 255 // the 0x81+ overbright values clamp; nothing is more than opaque here
		}
		d := img.Pix[4*i:]
		d[0], d[1], d[2], d[3] = p[2], p[1], p[0], uint8(a) // BGRA -> RGBA
		switch {
		case a == 0:
			clear++
		case a < 240:
			soft++
		}
	}

	t := &Texture{Name: name, Image: img}
	switch {
	case soft == 0 && clear == 0:
		t.Alpha = alphaOpaque
	case soft*20 < w*h: // <5% partially-transparent texels: it is a cutout, not a gradient
		t.Alpha = alphaMask
	default:
		t.Alpha = alphaBlend
	}
	return t, nil
}
