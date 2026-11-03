package model

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// gltfRev bumps whenever the exporter's output changes, so a stale cache from an older build is
// not served forever. 2 = textured.
const gltfRev = 5 // 5: normals flipped outward (MGS2 stores them inward)

// Cache turns .kms/.evm into .gltf on disk, once. The viewport asks for a URL, not a decode.
type Cache struct {
	Dir      string
	Proxy    bool // export per-joint proxy boxes instead of real geometry
	NoTextur bool // export flat grey, skipping texture resolution entirely
	Origin   bool // bake DG_DEF.tx/ty/tz into the vertices and drop the skin (map areas)

	once sync.Once
	tex  *TextureIndex
}

// textures finds the flatlists from the model's own path -- assets/ and textures/ are siblings --
// so the viewport gets textures without the caller having to know they exist.
func (c *Cache) textures(modelPath string) *TextureIndex {
	if c.NoTextur {
		return nil
	}
	c.once.Do(func() {
		root := FindGameData(modelPath)
		if root == "" {
			return
		}
		c.tex, _ = LoadTextureIndex(root) // no textures is a downgrade, not a failure
	})
	return c.tex
}

// SetTextures pins the texture index explicitly, instead of discovering it from a model path.
func (c *Cache) SetTextures(ix *TextureIndex) {
	c.once.Do(func() { c.tex = ix })
}

// GLTF returns the path to the converted model, converting it if the cache is cold or stale.
func (c *Cache) GLTF(modelPath string) (string, error) {
	fi, err := os.Stat(modelPath)
	if err != nil {
		return "", err
	}
	out := filepath.Join(c.Dir, c.name(modelPath, fi))
	if st, err := os.Stat(out); err == nil && st.Size() > 0 {
		return out, nil
	}
	m, err := LoadModel(modelPath)
	if err != nil {
		return "", err
	}
	if c.Origin {
		m.BakeOrigin()
	}
	ix := c.textures(modelPath)
	if c.Proxy {
		m, ix = m.Proxy(), nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(c.Dir, ".gltf-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := m.WriteGLTFWith(tmp, ix); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		return "", err
	}
	return out, nil
}

// Stamp the source path, size and mtime into the name, so a patched game invalidates itself.
func (c *Cache) name(modelPath string, fi os.FileInfo) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%v|%v|%v|%d", modelPath, fi.Size(), fi.ModTime().UnixNano(), c.Proxy, c.NoTextur, c.Origin, gltfRev)))
	base := strings.TrimSuffix(filepath.Base(modelPath), filepath.Ext(modelPath))
	return fmt.Sprintf("%s-%s.gltf", base, hex.EncodeToString(h[:6]))
}
