package server

import (
	"fmt"
	"net/http"

	"mgs2-restoration/cutscene-studio/internal/light"
	"mgs2-restoration/cutscene-studio/internal/stage"
)

// handleLights returns the stage's real lighting rig.
//
// MGS2's stage lighting is RUNTIME, not baked: the .kms carries no vertex colours (the field
// is a NULL runtime pointer), and the engine mallocs the colour buffer at room entry and
// fills it from the .lt2 (map.c:256 -> pshade.c:1068). So the .lt2 IS the lighting, and a
// viewport can reproduce it.
//
// Only BG lights are returned. A light lights map geometry only if LIT_FLAG_BGONLY is set --
// CHARAONLY lights are for characters and go through a different, brighter path.
func (s *Server) handleLights(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		fail(w, 400, fmt.Errorf("path is required"))
		return
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	name, err := stage.ForDemo(path, cfg.GameDir)
	if err != nil {
		writeJSON(w, map[string]any{"lights": []any{}, "unknown": true})
		return
	}
	st, err := stage.Load(name, cfg.GameDir, cfg.GameDataDir, cfg.ToolsDir)
	if err != nil {
		fail(w, 500, err)
		return
	}

	// Every placement records the lighting set it was placed under; in practice a stage uses
	// one. Take the first non-empty.
	lt2 := ""
	for _, p := range st.Placements {
		if p.LT2 != "" {
			lt2 = p.LT2
			break
		}
	}
	if lt2 == "" {
		writeJSON(w, map[string]any{"stage": name, "lights": []any{}, "reason": "the stage names no .lt2"})
		return
	}

	f, err := light.Find(lt2, cfg.GameDataDir)
	if err != nil {
		writeJSON(w, map[string]any{"stage": name, "lt2": lt2, "lights": []any{}, "reason": err.Error()})
		return
	}
	set, err := light.Load(f)
	if err != nil {
		fail(w, 500, err)
		return
	}

	// Send BOTH paths. Characters are lit by CHARAONLY lights on a separate, brighter path
	// (light.c:686), so a BG-only list would leave the cast unlit by the very lamps standing
	// next to them.
	out := make([]map[string]any, 0, len(set.Lights))
	for _, l := range set.Lights {
		if !l.Enabled() {
			continue
		}
		global := l.Kind == light.Ambient || l.Kind == light.Parallel
		if !global && !l.LitsBG() && !l.LitsChara() {
			continue
		}
		out = append(out, map[string]any{
			"kind":   string(l.Kind),
			"color":  l.Color,
			"pos":    l.Pos,
			"dir":    l.Dir,
			"range":  l.Range,
			"cone":   l.ConeDeg,
			"inner":  l.InnerConeDeg,
			"length": l.Length,
			"force":  l.Intensity, // `force`: a selection weight for BG, a real scale for characters
			"bg":     global || l.LitsBG(),
			"chara":  global || l.LitsChara(),
			"min":    l.BoundMin,
			"max":    l.BoundMax,
		})
	}
	writeJSON(w, map[string]any{"stage": name, "lt2": lt2, "lights": out})
}
