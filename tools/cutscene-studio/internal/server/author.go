package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"mgs2-restoration/cutscene-studio/internal/model"
	"mgs2-restoration/cutscene-studio/internal/scene"
	"mgs2-restoration/cutscene-studio/internal/stage"
)

// DM_ExecProc: the one packet that lets a cutscene reach back into its stage's script.
const (
	fxExecProc = int32(0x010FFF01)
)

type toggleReq struct {
	Path   string `json:"path"`
	ProcID int32  `json:"procId"`
	Frame  int    `json:"frame"`
}

// handleStageToggle fires one of the stage's own toggle procs from the cutscene, by adding a
// DM_ExecProc effect packet.
//
// This needs NO .gcx rebuild. The engine matches the id by value against the demo's 64-entry
// -p whitelist (demo_eft.c:347), and d080p07's whitelist already contains the Verrazano→
// Manhattan switch -- Konami wired it up, scheduled it, and commented the call out.
func (s *Server) handleStageToggle(w http.ResponseWriter, r *http.Request) {
	var req toggleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	sc, err := s.load(req.Path)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if req.Frame < 0 || req.Frame >= sc.Frames {
		fail(w, 400, fmt.Errorf("frame %d is outside 0..%d", req.Frame, sc.Frames-1))
		return
	}
	if !s.procAllowed(req.Path, req.ProcID) {
		fail(w, 400, fmt.Errorf("proc %d is not in this demo's -p whitelist: the engine would silently ignore it", req.ProcID))
		return
	}

	v := float64(req.ProcID)
	inst := &scene.EffectInstance{
		UID:      fmt.Sprintf("exec_%d_%d", req.ProcID, req.Frame),
		EffectID: fxExecProc,
		ExecType: 0, // <no_birth>: fire and forget, one frame
		Start:    req.Frame,
		End:      req.Frame + 1,
		Label:    fmt.Sprintf("DM_ExecProc %d", req.ProcID),
		Params: map[string]*scene.Channel{
			"d0": {Keys: []scene.Key{{Frame: req.Frame, Value: v, Interp: scene.Hold}}, Dirty: true},
		},
		Raw:   map[int][]byte{},
		Added: true,
		Dirty: true,
	}

	s.mu.Lock()
	sc.Effects = append(sc.Effects, inst)
	s.mu.Unlock()

	writeJSON(w, map[string]any{"ok": true, "uid": inst.UID, "frame": req.Frame})
}

// procAllowed checks the demo's -p whitelist. A proc that is not on it is not an error the
// game reports -- DM_ExecProc simply finds no match and does nothing -- so catch it here.
func (s *Server) procAllowed(demoPath string, procID int32) bool {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	name, err := stage.ForDemo(demoPath, cfg.GameDir)
	if err != nil {
		return false
	}
	st, err := stage.Load(name, cfg.GameDir, cfg.GameDataDir, cfg.ToolsDir)
	if err != nil {
		return false
	}
	for _, t := range st.Toggles {
		if t.ProcID == procID {
			return true
		}
	}
	return false
}

type extendReq struct {
	Path   string `json:"path"`
	Frames int    `json:"frames"`
}

// handleExtend lengthens a demo by continuation. The original frames are untouched; the new
// tail continues whatever was moving (the ship, the wake), holds the camera as a placeholder,
// and carries the per-frame effects on.
func (s *Server) handleExtend(w http.ResponseWriter, r *http.Request) {
	var req extendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	sc, err := s.load(req.Path)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if !sc.JointsHeld {
		fail(w, 400, fmt.Errorf("this demo is too large to hold its motion in memory, so it cannot be extended"))
		return
	}
	if req.Frames <= sc.Frames {
		fail(w, 400, fmt.Errorf("new length %d must be greater than the current %d", req.Frames, sc.Frames))
		return
	}
	s.mu.Lock()
	old := sc.Frames
	sc.Extend(req.Frames)
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "from": old, "to": sc.Frames, "audioEnds": sc.AudioFrames})
}

type placeReq struct {
	Path    string     `json:"path"`
	CacheID int32      `json:"cacheId"` // 24-bit, from a stage placement
	Name    string     `json:"name"`
	Pos     [3]float32 `json:"pos"`
	Rot     [3]int16   `json:"rot"`   // 12-bit fixed: 4096 == 360 deg
	Scale   [3]float32 `json:"scale"` // viewport preview only; putobj -s / 100 (10 == the city). 0 == 1
}

// handlePlace turns a stage model into a MOVABLE demo object.
//
// The stage's own placements are frozen at whatever -p the .gcx baked in (Manhattan and the
// bridge are both at the origin, which is why they overlap). But a model that is already
// RESIDENT in the stage can be re-declared as a DEMO_OBJECT, and then MOTION_PACK gives it a
// full per-frame transform -- from the .sdt alone, no cooking, no .gcx rebuild.
func (s *Server) handlePlace(w http.ResponseWriter, r *http.Request) {
	var req placeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	sc, err := s.load(req.Path)
	if err != nil {
		fail(w, 500, err)
		return
	}
	if !sc.JointsHeld {
		fail(w, 400, fmt.Errorf("this demo is too large to hold its motion in memory, so the cast cannot be edited"))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Object ids are load-bearing: stable, unique, never reused.
	next := int32(2)
	for _, o := range sc.Objects {
		if o.ID >= next {
			next = o.ID + 1
		}
	}
	if len(sc.Objects) >= 255 {
		fail(w, 400, fmt.Errorf("DM_MAX_OBJECT is 256 and this demo already has %d", len(sc.Objects)))
		return
	}

	n := sc.Frames
	ot := &scene.ObjectTrack{
		ID: next,
		// flag 0 = rigid, jointless (DG_FLAG_ONEPIECE). Placement is pos+rot, full stop --
		// and it is the 79% case in retail.
		Flag:       0,
		KMSCacheID: model.MakeCacheID(req.CacheID, "kms"),
		CacheID:    0,
		Name:       stage.StrCode(req.Name),
		Label:      req.Name,
		NJoints:    1, // the decoder still dereferences a block; 1 no-op joint is the minimum
		MotionOK:   true,
		Added:      true,
		Visibility: []scene.Visibility{{Frame: 0, Visible: true}},
		Joints:     make([][]scene.Joint, n),
	}
	for _, ch := range []*scene.Channel{&ot.PosX, &ot.PosY, &ot.PosZ, &ot.RotX, &ot.RotY, &ot.RotZ} {
		ch.Baked = make([]float64, n)
	}
	for f := 0; f < n; f++ {
		ot.PosX.Baked[f] = float64(req.Pos[0])
		ot.PosY.Baked[f] = float64(req.Pos[1])
		ot.PosZ.Baked[f] = float64(req.Pos[2])
		ot.RotX.Baked[f] = float64(req.Rot[0])
		ot.RotY.Baked[f] = float64(req.Rot[1])
		ot.RotZ.Baked[f] = float64(req.Rot[2])
		ot.Joints[f] = []scene.Joint{{Quat: [4]float32{0, 0, 0, 1}}} // identity
	}
	ot.PreviewScale = req.Scale
	for i := range ot.PreviewScale {
		if ot.PreviewScale[i] == 0 {
			ot.PreviewScale[i] = 1
		}
	}

	sc.Objects = append(sc.Objects, ot)
	sc.CastDirty = true
	sc.MotionDirty = true // a new object means the whole MOTION_PACK is re-encoded

	writeJSON(w, map[string]any{"ok": true, "id": ot.ID, "name": ot.Label, "objects": len(sc.Objects)})
}
