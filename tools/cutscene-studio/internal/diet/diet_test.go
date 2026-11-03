package diet

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/sdt"
	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// mcRoot is the Master Collection install; override with MGS2_DIR.
func mcRoot() string { return testenv.GameDir() }

func corpus(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{`us\demo2\_bp`, `us\demo\_bp`, `jp\demo2\_bp`, `jp\demo\_bp`} {
		m, _ := filepath.Glob(filepath.Join(mcRoot(), dir, "*.sdt"))
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Skipf("no demo corpus under %s (set MGS2_DIR)", mcRoot())
	}
	return files
}

// The two byte ranges of a MOTION_PACK body that the format leaves undefined and diet_de2 never
// writes, so retail ships stale FrameBuffer memory in them (see TestJunkIsStaleFrameBuffer):
//
//	body[0x08:0x10]  DEMO_MOTION_PACK.pad[2] -- fmt_demo.h declares it, conv.c:672-676 skips it
//	body[end:]       the 16-byte-align padding after the last info's diet stream
//
// EncodePack zeroes both, as a baker should. Everything else it must
// reproduce bit-for-bit.
func isJunk(off, end int) bool { return (off >= 8 && off < 16) || off >= end }

// eachPack walks every MOTION_PACK body of one demo, in stream order -- the only order in which
// a stateful codec means anything.
func eachPack(t *testing.T, file string, fn func(body []byte)) {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s, err := sdt.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Chunks {
		c := &s.Chunks[i]
		if c.Type() != sdt.CDemo {
			continue
		}
		fr, err := sdt.DecodeFrame(c.Payload)
		if err != nil {
			t.Fatal(err)
		}
		for j := range fr.Packets {
			if p := &fr.Packets[j]; p.Kind() == sdt.PMotionPack {
				fn(p.Body)
			}
		}
	}
}

// dataEnd is where the last info's diet stream stops, i.e. the start of the align padding.
func dataEnd(t *testing.T, body []byte) int {
	t.Helper()
	links, _, err := parseLinks(body)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDecoder()
	end := 0
	for _, l := range links {
		_, e, err := d.decodeInfo(body, l)
		if err != nil {
			t.Fatal(err)
		}
		if e > end {
			end = e
		}
	}
	return end
}

// TestDecodeCorpus decodes every MOTION_PACK in every shipped demo and asserts the
// read-side invariants. The load-bearing one: walking an info's diet blocks must land exactly
// on the next info's data start. A codec that mistook the run count for mask bits, or that got
// a block's size wrong, would desync and blow this up on the first packet.
func TestDecodeCorpus(t *testing.T) {
	files := corpus(t)

	var packs, infos, blocks, runBlocks, joints int64
	var normSum float64
	var normIn2pct int64
	minNorm, maxNorm := math.Inf(1), math.Inf(-1)
	words := map[uint16]int64{}
	type track struct {
		file  string
		obj   int32
		joint int
	}
	outliers := map[track]int64{}

	for _, f := range files {
		name := filepath.Base(f)
		d := NewDecoder()
		eachPack(t, f, func(body []byte) {
			packs++
			links, dataOffset, err := parseLinks(body)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if want := int32(packetHeader + packHeader + infoSize*len(links)); dataOffset != want {
				t.Fatalf("%s: data_offset %d, want %d", name, dataOffset, want)
			}
			if len(body)%16 != 0 {
				t.Fatalf("%s: body is %d bytes, not 16-aligned", name, len(body))
			}

			for i, l := range links {
				infos++
				if l.flag != FlagRetail {
					t.Fatalf("%s: info %d flag %#04x, want 0x17", name, i, l.flag)
				}
				if l.frame != 1 || l.startJoint != 0 {
					t.Fatalf("%s: info %d frame=%d start_joint=%d", name, i, l.frame, l.startJoint)
				}
				if l.nJoints < 1 || l.nJoints > 123 {
					t.Fatalf("%s: info %d n_joints=%d", name, i, l.nJoints)
				}
				if l.offset%2 != 0 {
					t.Fatalf("%s: info %d offset %#x is odd", name, i, l.offset)
				}

				// Census of the block stream, walked independently of the decoder so the two
				// have to agree on where it ends.
				p := int(l.offset) - packetHeader + 16 + 8 // past the pos + rot preamble
				for j := 0; j < int(l.nJoints); {
					hdr := le.Uint16(body[p:])
					words[hdr]++
					blocks++
					run := int(hdr & runMask)
					if run < 1 {
						t.Fatalf("%s: info %d: 0-count block at joint %d", name, i, j)
					}
					if hdr&0x8000 != 0 {
						t.Fatalf("%s: info %d: block %#04x sets bit 15", name, i, hdr)
					}
					if run > 1 {
						runBlocks++
					}
					p += 2 + 2*popcount(^hdr&omitAll)
					j += run
				}

				om, end, err := d.decodeInfo(body, l)
				if err != nil {
					t.Fatalf("%s: info %d: %v", name, i, err)
				}
				if end != p {
					t.Fatalf("%s: info %d: decoder stopped at %d, census says %d", name, i, end, p)
				}
				want := len(body)
				if i+1 < len(links) {
					want = int(links[i+1].offset) - packetHeader
				}
				if end > want {
					t.Fatalf("%s: info %d OVERRAN: ended at %d, next data starts at %d", name, i, end, want)
				}
				if i+1 < len(links) && end != want {
					t.Fatalf("%s: info %d: ended at %d but next data starts at %d (gap)", name, i, end, want)
				}

				for jn, jt := range om.Joints {
					joints++
					q := jt.Quat
					n := math.Sqrt(float64(q[0])*float64(q[0]) + float64(q[1])*float64(q[1]) +
						float64(q[2])*float64(q[2]) + float64(q[3])*float64(q[3]))
					normSum += n
					minNorm, maxNorm = math.Min(minNorm, n), math.Max(maxNorm, n)
					if math.Abs(n-1) <= 0.02 {
						normIn2pct++
					} else {
						outliers[track{name, om.ObjectID, jn}]++
					}
				}
			}
		})
	}

	t.Logf("%d files, %d packs, %d infos, %d blocks (%d with run>1), %d joints",
		len(files), packs, infos, blocks, runBlocks, joints)
	t.Logf("pre-normalise |q|: mean %.6f, min %.6f, max %.6f", normSum/float64(joints), minNorm, maxNorm)
	t.Logf("within 2%% of unit: %d/%d (%.5f%%)", normIn2pct, joints,
		100*float64(normIn2pct)/float64(joints))
	t.Logf("literal block word 0x7F00: %d (never occurs); 0x7001: %d, 0x7F01: %d, 0x7E01: %d",
		words[0x7F00], words[0x7001], words[0x7F01], words[0x7E01])
	if words[0x7F00] != 0 {
		t.Errorf("literal 0x7F00 occurs %d times", words[0x7F00])
	}

	// A wrong retention model does not fail gracefully: reconstructing an omitted component from
	// the wrong source smears every partially-masked joint of every rig. The spec measured the
	// rival "previous joint in the same packet" model at |q| mean 0.9461 / 54.6% near unit. So the
	// non-unit joints are the assertion that matters -- they must stay a rounding error, and they
	// must be CONFINED, not spread across rigs.
	for k, n := range outliers {
		t.Logf("non-unit track: %s object %d joint %d, %d frames", k.file, k.obj, k.joint, n)
	}
	if len(outliers) > 1 {
		t.Errorf("non-unit quats on %d distinct tracks -- that smells like a desync, not sloppy data",
			len(outliers))
	}
	if frac := float64(normIn2pct) / float64(joints); frac < 0.9999 {
		t.Errorf("only %.5f%% of decoded quats are near unit", 100*frac)
	}
}

// TestReencodeCorpus is the acid test. Decode every shipped MOTION_PACK, feed the same values
// straight back into the Encoder, compare bytes. Every byte the format defines must match.
func TestReencodeCorpus(t *testing.T) {
	files := corpus(t)

	var packs, exactFull, exactDefined int64
	var junkBytes, junkNonZero int64

	for _, f := range files {
		name := filepath.Base(f)
		dec, enc := NewDecoder(), NewEncoder()
		eachPack(t, f, func(body []byte) {
			packs++
			objs, err := dec.DecodePack(body)
			if err != nil {
				t.Fatalf("%s: decode: %v", name, err)
			}
			got, err := enc.EncodePack(objs)
			if err != nil {
				t.Fatalf("%s: encode: %v", name, err)
			}
			if len(got) != len(body) {
				t.Fatalf("%s: encoded %d bytes, original is %d", name, len(got), len(body))
			}

			end := dataEnd(t, body)
			full, defined := true, true
			for k := range body {
				if isJunk(k, end) {
					junkBytes++
					if body[k] != 0 {
						junkNonZero++
					}
				}
				if got[k] == body[k] {
					continue
				}
				full = false
				if !isJunk(k, end) {
					if defined {
						t.Errorf("%s: mismatch at body offset %#x: got %#02x want %#02x",
							name, k, got[k], body[k])
					}
					defined = false
				}
			}
			if full {
				exactFull++
			}
			if defined {
				exactDefined++
			}
		})
	}

	t.Logf("%d packs: byte-exact over every DEFINED byte: %d (%.4f%%)", packs, exactDefined,
		100*float64(exactDefined)/float64(packs))
	t.Logf("%d packs: byte-exact over the WHOLE body (defined + junk): %d (%.4f%%)", packs, exactFull,
		100*float64(exactFull)/float64(packs))
	t.Logf("undefined bytes (pad[2] + align padding): %d, of which %d (%.2f%%) are non-zero in retail",
		junkBytes, junkNonZero, 100*float64(junkNonZero)/float64(junkBytes))
	if exactDefined != packs {
		t.Errorf("%d of %d packs did not re-encode byte-exactly", packs-exactDefined, packs)
	}
}

// TestJunkIsStaleFrameBuffer accounts for the entire re-encode residual. diet_de2 mallocs a 1 MB
// FrameBuffer, memsets it ONCE (conv.c:573-574), then rewrites it from offset 0 every frame and
// writes out only FrameBufferSize bytes. It never writes DEMO_MOTION_PACK.pad[2] and never writes
// the 16-align padding -- so both hold whatever the PREVIOUS demo frame left at that address.
//
// If that is true, every undefined byte equals the previous DEMO chunk payload's byte at the same
// offset. It is, exactly, which is why a re-bake can't reproduce those bytes and needn't try.
func TestJunkIsStaleFrameBuffer(t *testing.T) {
	files := corpus(t)
	var junk, stale int64

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s, err := sdt.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		frameBuf := make([]byte, 1<<20)
		for i := range s.Chunks {
			c := &s.Chunks[i]
			if c.Type() != sdt.CDemo {
				continue
			}
			fr, err := sdt.DecodeFrame(c.Payload)
			if err != nil {
				t.Fatal(err)
			}
			at := 0
			for j := range fr.Packets {
				p := &fr.Packets[j]
				if p.Kind() == sdt.PMotionPack {
					end := dataEnd(t, p.Body)
					for k := range p.Body {
						if !isJunk(k, end) {
							continue
						}
						junk++
						if p.Body[k] == frameBuf[at+16+k] {
							stale++
						}
					}
				}
				at += 16 + len(p.Body)
			}
			copy(frameBuf, c.Payload)
		}
	}
	t.Logf("%d undefined bytes; %d (%.4f%%) are the previous demo frame's byte at the same offset",
		junk, stale, 100*float64(stale)/float64(junk))
	if stale != junk {
		t.Errorf("%d undefined bytes are not explained by stale FrameBuffer memory", junk-stale)
	}
}

// TestSemanticRoundTrip: a re-encode must not change one thing the game sees.
// decode(encode(decode(x))) == decode(x) on every value of every joint of every object.
func TestSemanticRoundTrip(t *testing.T) {
	files := corpus(t)
	var packs, objs, joints int64

	for _, f := range files {
		name := filepath.Base(f)
		d1, enc, d2 := NewDecoder(), NewEncoder(), NewDecoder()
		eachPack(t, f, func(body []byte) {
			packs++
			a, err := d1.DecodePack(body)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			re, err := enc.EncodePack(a)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			b, err := d2.DecodePack(re)
			if err != nil {
				t.Fatalf("%s: re-decode: %v", name, err)
			}
			if len(a) != len(b) {
				t.Fatalf("%s: %d objects in, %d out", name, len(a), len(b))
			}
			for i := range a {
				objs++
				x, y := a[i], b[i]
				if x.ObjectID != y.ObjectID || x.Flag != y.Flag || x.StartJoint != y.StartJoint ||
					x.Rot != y.Rot || len(x.Joints) != len(y.Joints) {
					t.Fatalf("%s: object %d: header differs", name, x.ObjectID)
				}
				// Retail ships NaN in some objects' pos, so compare bits, not values.
				for c := range x.Pos {
					if math.Float32bits(x.Pos[c]) != math.Float32bits(y.Pos[c]) {
						t.Fatalf("%s: object %d: pos %v != %v", name, x.ObjectID, x.Pos, y.Pos)
					}
				}
				for j := range x.Joints {
					joints++
					if !sameJoint(x.Joints[j], y.Joints[j]) {
						t.Fatalf("%s: object %d joint %d: %v != %v", name, x.ObjectID, j,
							x.Joints[j], y.Joints[j])
					}
				}
			}
		})
	}
	t.Logf("decode(encode(decode(x))) == decode(x) on %d packs, %d objects, %d joints", packs, objs, joints)
}

func sameJoint(a, b Joint) bool {
	for c := range a.Quat {
		if math.Float32bits(a.Quat[c]) != math.Float32bits(b.Quat[c]) {
			return false
		}
	}
	for c := range a.Trans {
		if math.Float32bits(a.Trans[c]) != math.Float32bits(b.Trans[c]) {
			return false
		}
	}
	return true
}

func popcount(v uint16) int {
	n := 0
	for ; v != 0; v &= v - 1 {
		n++
	}
	return n
}

func TestMotionFloat(t *testing.T) {
	cases := []struct {
		mf uint16
		f  float32
	}{
		{0x0000, 0},
		{0x8000, float32(math.Copysign(0, -1))},
		{0x2000, 1.0},         // e = 8, the bias
		{0x0400, 0.0078125},   // min normal, 2^-7
		{0x7fff, 16769024.0},  // max magnitude
		{0xffff, -16769024.0}, //
	}
	for _, c := range cases {
		if got := MFtoF(c.mf); math.Float32bits(got) != math.Float32bits(c.f) {
			t.Errorf("MFtoF(%#04x) = %v, want %v", c.mf, got, c.f)
		}
		if got := FtoMF(c.f); got != c.mf {
			t.Errorf("FtoMF(%v) = %#04x, want %#04x", c.f, got, c.mf)
		}
	}
	if got := FtoMF(0.001); got != 0x0000 { // below 2^-7: flushes, keeping the sign
		t.Errorf("FtoMF(0.001) = %#04x, want 0x0000", got)
	}
	if got := FtoMF(-0.001); got != 0x8000 {
		t.Errorf("FtoMF(-0.001) = %#04x, want 0x8000", got)
	}
	if got := FtoMF(1e30); got != 0x7fff { // above 2^24: saturates
		t.Errorf("FtoMF(1e30) = %#04x, want 0x7fff", got)
	}
	// FtoMF truncates. 1.9990234375 is representable; anything under the next step must fall back.
	if got, want := FtoMF(1.99999), uint16(0x23ff); got != want {
		t.Errorf("FtoMF(1.99999) = %#04x, want %#04x (truncation, not rounding)", got, want)
	}
	for v := 0; v <= 0xFFFF; v++ {
		if got := FtoMF(MFtoF(uint16(v))); got != uint16(v) {
			t.Fatalf("FtoMF(MFtoF(%#04x)) = %#04x", v, got)
		}
	}
}

// The re-encode can only be byte-exact because dequantise/quantise is an exact identity across
// the whole s16 range. If it were off by one ULP anywhere, that value would re-quantise low and
// flip a mask bit, and the stream would diverge from that joint onward.
func TestQuatQuantIsExactlyInvertible(t *testing.T) {
	for v := -32768; v <= 32767; v++ {
		if got := quantQuat(dequantQuat(int16(v))); got != int32(v) {
			t.Fatalf("quantQuat(dequantQuat(%d)) = %d", v, got)
		}
	}
}

// A still character costs 2 bytes: one all-masked block whose run count is the whole rig.
func TestStillRigFoldsToOneBlock(t *testing.T) {
	joints := make([]Joint, 60)
	for i := range joints {
		joints[i] = Joint{Quat: [4]float32{0, 0, 0, 1}}
	}
	o := ObjectMotion{ObjectID: 7, Flag: FlagRetail, Pos: [3]float32{1, 2, 3}, Joints: joints}
	body, err := NewEncoder().EncodePack([]ObjectMotion{o})
	if err != nil {
		t.Fatal(err)
	}
	blk := le.Uint16(body[packHeader+infoSize+16+8:])
	if want := omitAll | 60; blk != want {
		t.Fatalf("block %#04x, want %#04x", blk, want)
	}
	objs, err := NewDecoder().DecodePack(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || len(objs[0].Joints) != 60 ||
		objs[0].Joints[59].Quat != [4]float32{0, 0, 0, 1} || objs[0].Pos != [3]float32{1, 2, 3} {
		t.Fatalf("decoded %+v", objs)
	}
}

// The largest legal rig is 255 joints (n_joints is a u8), and a still one folds into a single
// block whose run count is exactly 255 -- the boundary conv.c's `*last_flag += 1` would carry
// past, into the mask bits. Anything bigger is rejected outright.
func TestMaxRigIsOneRunOf255(t *testing.T) {
	joints := make([]Joint, 255)
	for i := range joints {
		joints[i] = Joint{Quat: [4]float32{0, 0, 0, 1}}
	}
	o := ObjectMotion{ObjectID: 1, Flag: FlagRetail, Joints: joints}
	body, err := NewEncoder().EncodePack([]ObjectMotion{o})
	if err != nil {
		t.Fatal(err)
	}
	at := packHeader + infoSize + 16 + 8
	if got, want := le.Uint16(body[at:]), omitAll|255; got != want {
		t.Fatalf("block %#04x, want %#04x", got, want)
	}
	objs, err := NewDecoder().DecodePack(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs[0].Joints) != 255 {
		t.Fatalf("decoded %d joints", len(objs[0].Joints))
	}

	o.Joints = make([]Joint, 256)
	if _, err := NewEncoder().EncodePack([]ObjectMotion{o}); err == nil {
		t.Fatal("256 joints should not encode")
	}
}
