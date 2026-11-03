package server

import (
	"fmt"
	"net/http"

	"mgs2-restoration/cutscene-studio/internal/stage"
)

// handleStage returns the world a cutscene plays in: map geometry with real transforms,
// the sky, the sea, the fog.
//
// The .sdt carries only its own cast -- the level comes from the stage's scenario script.
// The demo->stage map is baked in (the naming convention does NOT work: p080_13_p08 plays
// in d080p07, not d080p08, because a stage name is capped at 7 characters and the trailing
// 'b' of d080p07b was dropped).
func (s *Server) handleStage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		fail(w, 400, fmt.Errorf("path is required"))
		return
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	name := r.URL.Query().Get("stage")
	if name == "" {
		var err error
		name, err = stage.ForDemo(path, cfg.GameDir)
		if err != nil {
			// 131 of 133 demos map; the rest are Snake Tales streams missing from the tree.
			writeJSON(w, map[string]any{"stage": "", "unknown": true, "reason": err.Error()})
			return
		}
	}

	st, err := stage.Load(name, cfg.GameDir, cfg.GameDataDir, cfg.ToolsDir)
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, st)
}
