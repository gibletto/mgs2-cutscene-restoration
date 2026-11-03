// Package server exposes the studio over localhost and serves the embedded web UI.
package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"mgs2-restoration/cutscene-studio/internal/audio"
	"mgs2-restoration/cutscene-studio/internal/catalog"
	"mgs2-restoration/cutscene-studio/internal/config"
	"mgs2-restoration/cutscene-studio/internal/scene"
	"mgs2-restoration/cutscene-studio/internal/sdt"
	"mgs2-restoration/cutscene-studio/internal/validate"
)

type Server struct {
	mu     sync.RWMutex
	cfg    *config.Config
	cat    *catalog.Catalog
	open   map[string]*scene.Scene // path -> the session's working copy
	motion map[string]*scene.Scene // path -> the same demo, imported WITH joints (heavy)
	uiFS   fs.FS
}

func New(cfg *config.Config, cat *catalog.Catalog, uiFS fs.FS) *Server {
	return &Server{cfg: cfg, cat: cat, open: map[string]*scene.Scene{}, uiFS: uiFS}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/demos", s.handleDemos)
	mux.HandleFunc("/api/scene", s.handleScene)
	mux.HandleFunc("/api/catalog", s.handleCatalog)
	mux.HandleFunc("/api/limits", s.handleLimits)
	mux.HandleFunc("/api/gltf", s.handleGLTF)
	mux.HandleFunc("/api/stage", s.handleStage)
	mux.HandleFunc("/api/lights", s.handleLights)
	mux.HandleFunc("/api/putobjs", s.handlePutobjs)
	mux.HandleFunc("/api/motion", s.handleMotion)
	mux.HandleFunc("/api/stage/toggle", s.handleStageToggle)
	mux.HandleFunc("/api/stage/place", s.handlePlace)
	mux.HandleFunc("/api/extend", s.handleExtend)
	mux.HandleFunc("/api/bake", s.handleBake)
	mux.HandleFunc("/api/deploy", s.handleDeploy)
	mux.HandleFunc("/api/validate", s.handleValidate)

	if s.uiFS != nil {
		mux.Handle("/", spaHandler{fs: s.uiFS})
	}
	return mux
}

// spaHandler serves the built UI and falls back to index.html so client routing works.
type spaHandler struct{ fs fs.FS }

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if _, err := fs.Stat(h.fs, p); err != nil {
		p = "index.html"
	}
	if p == "index.html" {
		// hashed assets are immutable, but a cached index.html pins a browser tab
		// to a STALE bundle across server rebuilds -- never cache it
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, h.fs, p)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	// no heuristic caching: ghost tracks/registry change every bake, and a stale
	// cached copy shows a world the scene no longer describes
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r.Method == http.MethodPost {
		var c config.Config
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			fail(w, 400, err)
			return
		}
		if c.CatalogPath == "" {
			c.CatalogPath = s.cfg.CatalogPath
		}
		// Not edited by the UI: keep whatever the config file / environment supplied.
		if c.DataDir == "" {
			c.DataDir = s.cfg.DataDir
		}
		if c.PutobjRegistry == "" {
			c.PutobjRegistry = s.cfg.PutobjRegistry
		}
		if err := c.Save(); err != nil {
			fail(w, 500, err)
			return
		}
		s.cfg = &c
	}
	writeJSON(w, map[string]any{
		"gameDir":     s.cfg.GameDir,
		"gameDataDir": s.cfg.GameDataDir,
		"toolsDir":    s.cfg.ToolsDir,
		"catalogPath": s.cfg.CatalogPath,
		"dataDir":     s.cfg.DataDir,
		"outDir":      s.cfg.OutDir,
		"ok":          s.cfg.OK(),
		"problems":    s.cfg.Problems(),
	})
}

type demoInfo struct {
	Name        string  `json:"name"`
	Region      string  `json:"region"`
	Path        string  `json:"path"`
	Frames      int     `json:"frames"`
	DurationSec float64 `json:"durationSec"`
	HasAudio    bool    `json:"hasAudio"`
	AudioSec    float64 `json:"audioSec"`
	SizeBytes   int64   `json:"sizeBytes"`
}

func (s *Server) handleDemos(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	var out []demoInfo
	for _, dir := range cfg.DemoDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.Contains(e.Name(), ".sdt") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			info, err := summarise(p)
			if err != nil {
				continue // not a cutscene (vox, codec bytecode, movie); quietly skip
			}
			// us/ and jp/ ship the same basenames, so qualify them or the list doubles up.
			info.Region = regionOf(dir)
			out = append(out, *info)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Region != out[j].Region {
			return out[i].Region < out[j].Region
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, out)
}

// regionOf turns .../us/demo2/_bp into "us/demo2".
func regionOf(dir string) string {
	dir = filepath.ToSlash(dir)
	parts := strings.Split(strings.TrimSuffix(dir, "/_bp"), "/")
	if len(parts) < 2 {
		return dir
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// summarise reads just enough to list a demo without building a whole scene.
func summarise(path string) (*demoInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) >= 4 && string(raw[:4]) == "LCGB" {
		return nil, fmt.Errorf("GCL bytecode")
	}
	st, err := sdt.Decode(raw)
	if err != nil {
		return nil, err
	}
	frames := 0
	for i := range st.Chunks {
		if st.Chunks[i].Type() == sdt.CDemo {
			frames++
		}
	}
	if frames == 0 {
		return nil, fmt.Errorf("no demo chunks")
	}
	d := &demoInfo{
		Name:        filepath.Base(path),
		Path:        path,
		Frames:      frames,
		DurationSec: float64(frames) / 60,
		SizeBytes:   int64(len(raw)),
		HasAudio:    audio.HasAudio(st),
	}
	if d.HasAudio {
		if af, err := audio.DurationFrames(st); err == nil {
			d.AudioSec = float64(af) / 60
		}
	}
	return d, nil
}

func (s *Server) handleScene(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		path := r.URL.Query().Get("path")
		if path == "" {
			fail(w, 400, fmt.Errorf("path is required"))
			return
		}
		sc, err := s.load(path)
		if err != nil {
			fail(w, 500, err)
			return
		}
		writeJSON(w, sc)

	case http.MethodPost:
		var in scene.Scene
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, err)
			return
		}
		s.mu.Lock()
		cur := s.open[canon(in.SourcePath)]
		s.mu.Unlock()
		if cur == nil {
			fail(w, 400, fmt.Errorf("scene %s is not open", in.SourcePath))
			return
		}
		// The base stream and the decoded packet tables never leave the server, so
		// merge the client's edits onto the copy that still knows Konami's bytes.
		merge(cur, &in)
		writeJSON(w, map[string]any{"ok": true, "dirty": cur.Dirty()})

	default:
		fail(w, 405, fmt.Errorf("method not allowed"))
	}
}

func merge(dst, src *scene.Scene) {
	dst.Frames = src.Frames
	dst.Camera = src.Camera
	dst.Captions = src.Captions
	dst.Markers = src.Markers
	dst.CastDirty = src.CastDirty
	dst.MotionDirty = src.MotionDirty
	dst.CaptionsDirty = src.CaptionsDirty

	byUID := map[string]*scene.EffectInstance{}
	for _, e := range dst.Effects {
		byUID[e.UID] = e
	}
	var merged []*scene.EffectInstance
	for _, in := range src.Effects {
		if old, ok := byUID[in.UID]; ok {
			old.Start, old.End, old.Label = in.Start, in.End, in.Label
			old.Params, old.Dirty, old.Deleted = in.Params, in.Dirty, in.Deleted
			old.Continue = in.Continue
			merged = append(merged, old)
			continue
		}
		in.Added = true
		if in.Raw == nil {
			in.Raw = map[int][]byte{}
		}
		merged = append(merged, in)
	}
	dst.Effects = merged

	byID := map[int32]*scene.ObjectTrack{}
	for _, o := range dst.Objects {
		byID[o.ID] = o
	}
	for _, in := range src.Objects {
		if old, ok := byID[in.ID]; ok {
			old.Label, old.Visibility = in.Label, in.Visibility
			old.PosX, old.PosY, old.PosZ = in.PosX, in.PosY, in.PosZ
			old.RotX, old.RotY, old.RotZ = in.RotX, in.RotY, in.RotZ
		}
	}
}

// canon collapses slash direction and case so one file cannot become two sessions --
// the UI sends backslash paths, curl sends forward slashes, and Windows treats them the
// same while a map key does not.
func canon(p string) string {
	return strings.ToLower(filepath.Clean(p))
}

func (s *Server) load(path string) (*scene.Scene, error) {
	path = filepath.Clean(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc, ok := s.open[canon(path)]; ok {
		return sc, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc, err := scene.ImportWith(path, raw, s.cat, scene.Opts{Joints: true})
	if err != nil {
		return nil, err
	}
	s.open[canon(path)] = sc
	return sc, nil
}

// handleCatalog serves effects.json verbatim. The Go structs are a reader's view of that
// file; re-marshalling them would invent a second, subtly different shape for the UI to
// track. One source of truth.
func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	p := s.cfg.CatalogPath
	s.mu.RUnlock()
	if p == "" {
		writeJSON(w, []any{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, p)
}

// handleLimits serves the engine-derived parameter ranges that Konami's schema omits.
// Kept separate from the catalog so each file stays the single source of its own truth.
func (s *Server) handleLimits(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cat := s.cfg.CatalogPath
	s.mu.RUnlock()
	if cat == "" {
		writeJSON(w, []any{})
		return
	}
	p := filepath.Join(filepath.Dir(cat), "effect_limits.json")
	if _, err := os.Stat(p); err != nil {
		writeJSON(w, []any{})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	http.ServeFile(w, r, p)
}

type bakeReq struct {
	Path  string `json:"path"`
	Force bool   `json:"force"` // write even with fatal findings
}

// handleBake writes the baked demo to the staging dir. It NEVER touches the game --
// deploy does that, and only when explicitly asked.
func (s *Server) handleBake(w http.ResponseWriter, r *http.Request) {
	var req bakeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	s.mu.RLock()
	sc := s.open[canon(req.Path)]
	cat := s.cat
	outDir := s.cfg.OutDir
	s.mu.RUnlock()
	if sc == nil {
		fail(w, 400, fmt.Errorf("scene %s is not open", req.Path))
		return
	}

	out, err := sc.Bake(cat)
	if err != nil {
		fail(w, 500, err)
		return
	}

	st, err := sdt.Decode(out)
	if err != nil {
		fail(w, 500, err)
		return
	}
	// Byte-level idioms, plus the authoring check the byte validator cannot do:
	// a value that is structurally legal but outside what the engine can survive.
	findings := append(sc.CheckParams(cat), validate.Stream(st)...)
	fatal := validate.HasFatal(findings)

	resp := map[string]any{
		"findings":      findings,
		"bytesWritten":  len(out),
		"fatal":         fatal,
		"byteIdentical": !sc.Dirty(),
		"ok":            false,
	}
	if fatal && !req.Force {
		writeJSON(w, resp)
		return
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(w, 500, err)
		return
	}
	// Same basename as the original: the game strcmps it, and a name without ".sdt"
	// is a null-deref. So staging keeps the name and changes the folder.
	dst := filepath.Join(outDir, filepath.Base(req.Path))
	if err := os.WriteFile(dst, out, 0o644); err != nil {
		fail(w, 500, err)
		return
	}
	resp["ok"] = true
	resp["outPath"] = dst
	writeJSON(w, resp)
}

type deployReq struct {
	Path  string `json:"path"` // the game file to replace
	Force bool   `json:"force"`
}

// handleDeploy copies a staged bake over the game's file. This is the only call in the
// whole tool that writes into the install, and it refuses a demo the validator rejects.
func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req deployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	s.mu.RLock()
	outDir := s.cfg.OutDir
	s.mu.RUnlock()

	src := filepath.Join(outDir, filepath.Base(req.Path))
	baked, err := os.ReadFile(src)
	if err != nil {
		fail(w, 400, fmt.Errorf("nothing staged for %s -- bake first", filepath.Base(req.Path)))
		return
	}

	// Re-validate the actual bytes about to land in the game, not the ones we baked.
	st, err := sdt.Decode(baked)
	if err != nil {
		fail(w, 500, err)
		return
	}
	findings := validate.Stream(st)
	if validate.HasFatal(findings) && !req.Force {
		writeJSON(w, map[string]any{"ok": false, "findings": findings, "fatal": true})
		return
	}

	backup, err := backupOnce(req.Path)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if err := os.WriteFile(req.Path, baked, 0o644); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"deployedTo": req.Path,
		"backup":     backup,
		"bytes":      len(baked),
		"findings":   findings,
	})
}

// backupOnce snapshots the pristine file the first time we ever overwrite it, and never
// again -- so the .orig always holds Konami's bytes, not your last bake.
func backupOnce(path string) (string, error) {
	orig := path + ".orig"
	if _, err := os.Stat(orig); err == nil {
		return orig, nil // already protected; do NOT overwrite it
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil // nothing there to protect
	}
	return orig, os.WriteFile(orig, b, 0o644)
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	raw, err := os.ReadFile(path)
	if err != nil {
		fail(w, 400, err)
		return
	}
	st, err := sdt.Decode(raw)
	if err != nil {
		fail(w, 400, err)
		return
	}
	f := validate.Stream(st)
	writeJSON(w, map[string]any{"findings": f, "fatal": validate.HasFatal(f)})
}

// Listen starts the server and reports the URL to open.
func Listen(addr string, h http.Handler) error {
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(addr, ":")); err == nil {
		addr = "127.0.0.1" + addr
	}
	log.Printf("MGS2 Cutscene Studio -> http://%s", addr)
	return http.ListenAndServe(addr, h)
}
