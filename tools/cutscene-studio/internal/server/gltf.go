package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"mgs2-restoration/cutscene-studio/internal/model"
)

// The model index is big (170k keys) and only needed once the viewport asks for geometry,
// so it loads on first use rather than at boot.
var (
	idxOnce sync.Once
	idx     *model.Index
	idxErr  error
)

func (s *Server) index() (*model.Index, error) {
	s.mu.RLock()
	dir := s.cfg.GameDataDir
	s.mu.RUnlock()
	if dir == "" {
		return nil, fmt.Errorf("gameDataDir is not set: point it at the GameData tree to see real models")
	}
	idxOnce.Do(func() {
		idx, idxErr = model.LoadIndex(dir)
		if idxErr == nil {
			idx.SetAssetRoot(dir)
		}
	})
	return idx, idxErr
}

// handleGLTF converts a model to glTF for the viewport, keyed by its 24-bit cache id.
//
// A demo object carries TWO ids: cache_id (EVM, the skinned model) and kms_cache_id (the
// mesh). cache_id is 0 for 79% of shipped objects -- a rigid, jointless prop is KMS-only
// and perfectly legal -- so the caller must be able to ask for either.
func (s *Server) handleGLTF(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	raw := q.Get("cacheId")
	if raw == "" {
		fail(w, 400, fmt.Errorf("cacheId is required"))
		return
	}
	v, err := strconv.ParseInt(raw, 0, 64)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if v == 0 {
		fail(w, 404, fmt.Errorf("cache id 0: this object has no model of that kind"))
		return
	}

	ix, err := s.index()
	if err != nil {
		fail(w, 503, err)
		return
	}

	id, spec := model.SplitCacheID(int32(v))
	ext := q.Get("ext")
	if ext == "" {
		ext = extForSpec(spec)
	}

	// Only .kms and .evm carry geometry. A model and its texture archive share a name and
	// therefore a StrCode, so a caller can hand us ext="tri" for a model that plainly exists
	// -- and that DOES happen (the stage parser does it for w16a0). Resolving the .tri and
	// then failing to decode it as a mesh is not an answer; ignore any non-mesh extension.
	tryExt := []string{"kms", "evm"}
	if ext == "evm" {
		tryExt = []string{"evm", "kms"}
	}
	var hits []model.Asset
	for _, e := range tryExt {
		hits = append(hits, ix.Resolve(id, e)...)
	}
	if len(hits) == 0 {
		fail(w, 404, fmt.Errorf("no kms/evm mesh for cache id 0x%06x", id))
		return
	}
	// The real flatlist key is a 6-tuple, so one id can name several files; take the first
	// that actually exists on disk rather than the first that merely indexes.
	for _, a := range hits {
		if !ix.Exists(a) {
			continue
		}
		// origin=1: bake DG_DEF.tx/ty/tz into the vertices. Map areas need it (that IS their
		// world position); posable objects must NOT have it, and the engine agrees -- it only
		// adds the term for ONEPIECE.
		c := s.cache()
		if q.Get("origin") == "1" {
			c = s.originCache()
		}
		gltf, err := c.GLTF(ix.FilePath(a))
		if err != nil {
			continue
		}
		w.Header().Set("Content-Type", "model/gltf+json")
		// Revalidate, do not blind-cache. A converted model changes whenever the decoder
		// improves (textures, skins), and a max-age here silently pins the browser to the
		// old geometry with no way to tell. ServeFile's Last-Modified still gives us 304s.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, gltf)
		return
	}
	fail(w, 404, fmt.Errorf("cache id 0x%06x resolves to %d asset(s), none decodable", id, len(hits)))
}

// extForSpec: SplitCacheID already hands back the extension's first LETTER, not the raw
// spec byte. Adding 'a' to it again silently falls through to "kms" -- which works by luck
// for KMS objects and 404s every EVM, i.e. every skinned character.
func extForSpec(spec byte) string {
	switch spec {
	case 'e':
		return "evm"
	case 'k':
		return "kms"
	}
	return "kms"
}

var (
	cacheOnce sync.Once
	meshCache *model.Cache
)

var (
	originOnce  sync.Once
	originCache *model.Cache
)

func (s *Server) originCache() *model.Cache {
	originOnce.Do(func() {
		originCache = &model.Cache{Dir: s.cache().Dir, Origin: true}
	})
	return originCache
}

func (s *Server) cache() *model.Cache {
	cacheOnce.Do(func() {
		dir, err := os.UserCacheDir()
		if err != nil {
			dir = os.TempDir()
		}
		dir = filepath.Join(dir, "mgs2-cutscene-studio", "gltf")
		_ = os.MkdirAll(dir, 0o755)
		meshCache = &model.Cache{Dir: dir}
	})
	return meshCache
}
