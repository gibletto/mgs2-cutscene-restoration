package sdt

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// mcRoot is the Master Collection install; override with MGS2_DIR.
func mcRoot() string { return testenv.GameDir() }

func corpus(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{`us\demo2\_bp`, `us\demo\_bp`, `jp\demo2\_bp`, `jp\demo\_bp`} {
		matches, _ := filepath.Glob(filepath.Join(mcRoot(), dir, "*.sdt*"))
		files = append(files, matches...)
	}
	if len(files) == 0 {
		t.Skipf("no demo corpus under %s (set MGS2_DIR)", mcRoot())
	}
	return files
}

// TestGoldenRoundTrip is the load-bearing test: every shipped demo must decode and
// re-encode to identical bytes, at both the chunk and the packet level. If this
// passes for the whole corpus, the codec has not silently dropped anything, and an
// unedited cutscene will always bake back to Konami's exact file.
func TestGoldenRoundTrip(t *testing.T) {
	files := corpus(t)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			s, err := Decode(raw)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}

			// Packet level: re-encoding each DEMO chunk's frame must reproduce its payload.
			for i := range s.Chunks {
				c := &s.Chunks[i]
				if c.Type() != CDemo {
					continue
				}
				fr, err := DecodeFrame(c.Payload)
				if err != nil {
					t.Fatalf("chunk %d: decode frame: %v", i, err)
				}
				got, err := fr.Encode()
				if err != nil {
					t.Fatalf("chunk %d: encode frame: %v", i, err)
				}
				if !bytes.Equal(got, c.Payload) {
					t.Fatalf("chunk %d: frame round-trip differs (%d packets, got %d bytes want %d)",
						i, len(fr.Packets), len(got), len(c.Payload))
				}
			}

			// Chunk level: the whole file.
			out, err := s.Encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if !bytes.Equal(out, raw) {
				t.Fatalf("file round-trip differs: got %d bytes, want %d", len(out), len(raw))
			}
		})
	}
	t.Logf("round-tripped %d files byte-for-byte", len(files))
}
