package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"mgs2-restoration/cutscene-studio/internal/scene"
)

// Putobj ghosts: the editor-side view of the mesg-driven put-object world. The registry
// (putobj_registry.json, emitted by the restoration's own registry generator, not part of
// this tool) declares every putobj the scenario creates; this endpoint evaluates the
// scene's DM_SendEffectMessage stream against it so the viewport can play the objects
// back per frame.
//
// The registry is found at the config's "putobjRegistry" path (or $MGS2CS_PUTOBJ_REGISTRY)
// when set, otherwise next to the executable, otherwise in the working directory. Without
// one this endpoint 404s and the viewport simply shows no ghosts.
//
// putobj.c commands: 0=OFF 1=ON 2=TURN(rx,ry,rz in 4096ths) 3=MOVE(x,y,z) 4=PRESHADE 5=KILL.
// MOVE and TURN are absolute hard-sets, so playback is a step-hold walk of the events.

const mesgEffectID = 0x01008000 // DM_SendEffectMessage

type putobjEvent struct {
	Frame int      `json:"frame"`
	Cmd   int      `json:"cmd"`
	Args  [3]int32 `json:"args"`
}

type putobjEntry struct {
	Name         string    `json:"name"`
	Code         int32     `json:"code"`
	Model        string    `json:"model"`
	ModelCacheID int32     `json:"modelCacheId"`
	P            []float64 `json:"p"`
	R            []float64 `json:"r"`
	S            []float64 `json:"s"`
	Hidden       bool      `json:"hidden"`
}

type putobjRegistry struct {
	Putobjs []putobjEntry `json:"putobjs"`
}

func (s *Server) handlePutobjs(w http.ResponseWriter, r *http.Request) {
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

	s.mu.RLock()
	explicit := s.cfg.PutobjRegistry
	s.mu.RUnlock()

	var raw []byte
	if explicit != "" {
		raw, err = os.ReadFile(explicit)
		if err != nil {
			fail(w, 404, fmt.Errorf("putobjRegistry %s: %w", explicit, err))
			return
		}
	} else {
		exe, _ := os.Executable()
		regPath := filepath.Join(filepath.Dir(exe), "putobj_registry.json")
		raw, err = os.ReadFile(regPath)
		if err != nil {
			// dev runs from the studio root
			raw, err = os.ReadFile("putobj_registry.json")
			if err != nil {
				fail(w, 404, fmt.Errorf("putobj_registry.json not found (set putobjRegistry in the config or $MGS2CS_PUTOBJ_REGISTRY): %w", err))
				return
			}
		}
	}
	var reg putobjRegistry
	if err := json.Unmarshal(raw, &reg); err != nil {
		fail(w, 500, fmt.Errorf("putobj_registry.json: %w", err))
		return
	}
	known := make(map[int32]bool, len(reg.Putobjs))
	for _, e := range reg.Putobjs {
		known[e.Code] = true
	}

	// Walk every mesg instance. The importer merges same-packet-id messages into ONE
	// instance whose sparse channels carry a value on each firing frame -- so an
	// instance is a STREAM of events, not one: every frame where d0 is hot is a send.
	tracks := make(map[int32][]putobjEvent)
	for _, inst := range sc.Effects {
		if inst.EffectID != mesgEffectID || inst.Deleted {
			continue
		}
		d0 := inst.Params["d0"]
		if d0 == nil {
			continue
		}
		emit := func(f int) {
			code := int32(sampleParam(inst, "d0", f))
			if code == 0 || !known[code] {
				return
			}
			ev := putobjEvent{Frame: f, Cmd: int(sampleParam(inst, "d9", f))}
			for i := 0; i < 3; i++ {
				ev.Args[i] = int32(sampleParam(inst, fmt.Sprintf("d%d", i+1), f))
			}
			tracks[code] = append(tracks[code], ev)
		}
		if len(d0.Baked) > 0 {
			end := inst.End
			if end > len(d0.Baked) {
				end = len(d0.Baked)
			}
			for f := inst.Start; f < end; f++ {
				if f >= 0 && f < len(d0.Baked) && d0.Baked[f] != 0 {
					emit(f)
				}
			}
		} else {
			emit(inst.Start)
		}
	}
	for _, evs := range tracks {
		sort.Slice(evs, func(a, b int) bool { return evs[a].Frame < evs[b].Frame })
	}

	writeJSON(w, map[string]any{
		"putobjs": reg.Putobjs,
		"tracks":  tracks,
	})
}

func sampleParam(inst *scene.EffectInstance, key string, frame int) float64 {
	ch, ok := inst.Params[key]
	if !ok || ch == nil {
		return 0
	}
	return ch.Sample(frame)
}
