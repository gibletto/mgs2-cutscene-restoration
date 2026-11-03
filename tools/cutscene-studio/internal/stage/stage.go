// Package stage puts the world back under the cutscene.
//
// A .sdt carries only the demo's own cast. The level it plays in -- map geometry,
// sea, sky, skyline -- comes from the STAGE, which is set up by Konami's scenario
// script. This package answers two questions the editor needs:
//
//	ForDemo("p080_13_p08.sdt") -> "d080p07"
//	Load("d080p07", ...)       -> the world geometry, placed, in world space
//
// The shipped scenario (assets/gcx/eu/_bp/scenerio_stage_<stage>.gcx) is COMPILED
// GCL bytecode. We do not decompile it. We parse the .gcl SOURCE it was compiled
// from (Konami's tree, mgs2x/scn), which is the same data without a VM in the way.
// The extracted result is written once to <data dir>/stage/*.json (see data.go) so the
// editor needs no source tree at runtime; regenerate with `go generate ./internal/stage`.
package stage

import (
	"fmt"
	"path/filepath"
	"strings"
)

//go:generate go run gen.go

// Placement is one piece of world geometry, in world space.
//
// Units are game units (1 unit ~= 1 mm). Rot is radians. The engine builds the
// matrix as T * Rz*Ry*Rx * S (putobj.c:100-103), so apply scale first, then the
// rotation in Z,Y,X order, then translate.
type Placement struct {
	Name    string     `json:"name"`            // model name, e.g. "w60a_1" -- also the manifest key
	Label   string     `json:"label,omitempty"` // human group, e.g. "Manhattan skyline"; "" if unknown
	CacheID int32      `json:"cacheId"`         // 24-bit, resolvable via internal/model's Index; 0 if unresolved
	Ext     string     `json:"ext"`             // "kms", "evm", ...
	Pos     [3]float32 `json:"pos"`             // world
	Rot     [3]float32 `json:"rot"`             // radians
	Scale   [3]float32 `json:"scale"`           // 1,1,1 if none
	LODs    []string   `json:"lods,omitempty"`  // extra -m models (lower detail); Name is the primary
	Visible bool       `json:"visible"`         // drawn when the stage finishes loading
	Created bool       `json:"created"`         // its proc actually runs at stage init
	LT2     string     `json:"lt2,omitempty"`   // lighting set it was placed under
	Flags   int32      `json:"flags,omitempty"` // putobj -f status bits
}

// Sky is what the engine draws behind everything.
//
// MGS2 has no cubemap and no procedural dome. "汎用空" (generic sky) is a flat base
// colour plus up to 4 scrolling cloud-texture layers plus up to 3 sky-wall MODELS
// (real .kms meshes, in the stage manifest). Params carries the raw numbers so the
// viewport can be honest about what it is drawing.
type Sky struct {
	Kind   string         `json:"kind"` // "generic_sky" (chara 汎用空), or "" if the stage sets none
	Params map[string]any `json:"params"`
}

// SkyWall is one sky-wall mesh: a real model with a vertex tint.
type SkyWall struct {
	Name    string   `json:"name"`
	CacheID int32    `json:"cacheId"`
	Ext     string   `json:"ext"`
	Tint    [3]int32 `json:"tint"` // 0-255 RGB applied to the mesh
}

// Sea is the "巨大海面" huge-sea plane: a flat, wave-animated, textured quad grid.
type Sea struct {
	Kind string     `json:"kind"` // "huge_sea" or "plant_sea"
	Pos  [3]float32 `json:"pos"`
	Size [2]float32 `json:"size"` // X and Z extent, centred on Pos
	Tex  []string   `json:"tex,omitempty"`
}

// Fog is the distance fog. The engine lerps to Color between Near and Far.
type Fog struct {
	Color [3]int32 `json:"color"`
	Near  float32  `json:"near"`
	Far   float32  `json:"far"`
}

// Toggle is a proc the stage exposes to the cutscene. The demo can fire it mid-shot
// to change the world -- d080p07's "Verrazano -> Manhattan" switch is one of these.
// Whether a given .sdt actually fires it is a property of the .sdt, not the stage.
type Toggle struct {
	Proc string `json:"proc"` // opaque (the source names these in Japanese)
	// ProcID is GV_StrCode of the proc's name, over its raw EUC-JP bytes. This is the value
	// a cutscene passes to DM_ExecProc (effect 0x010FFF01) to FIRE the toggle -- the engine
	// matches it against the demo's -p whitelist by value (demo_eft.c:347). No .gcx rebuild.
	ProcID int32    `json:"procId"`
	On     []string `json:"on"`  // placement names switched on
	Off    []string `json:"off"` // placement names switched off
}

// Asset is one entry of the stage's manifest: everything the stage has resident.
type Asset struct {
	Name    string `json:"name"`
	Ext     string `json:"ext"`
	CacheID int32  `json:"cacheId"`
	Path    string `json:"path"` // manifest column 1, e.g. "assets/kms/us/amg_def.kms"
}

// Stage is a level, ready to draw.
type Stage struct {
	Name       string      `json:"name"`
	Placements []Placement `json:"placements"`
	Sky        *Sky        `json:"sky,omitempty"`
	Sea        *Sea        `json:"sea,omitempty"`
	Fog        *Fog        `json:"fog,omitempty"`
	Toggles    []Toggle    `json:"toggles,omitempty"`
	HZX        string      `json:"hzx,omitempty"` // collision set (not geometry we draw)
	LT2        string      `json:"lt2,omitempty"` // lighting set
	Assets     []Asset     `json:"assets,omitempty"`
	Demos      []string    `json:"demos,omitempty"` // cutscenes that play here

	// Partial is set when we have the manifest but no parsed scenario: the editor
	// knows WHAT the stage contains but not WHERE any of it goes.
	Partial bool `json:"partial"`
}

// Visible returns just the placements the stage draws at load.
func (s *Stage) VisiblePlacements() []Placement {
	var out []Placement
	for _, p := range s.Placements {
		if p.Created && p.Visible {
			out = append(out, p)
		}
	}
	return out
}

// ForDemo maps a cutscene to the stage it plays in.
//
// Method: Konami's per-stage scenario source (scn/_bp/scenerio_stage_<stage>.gcl)
// #includes the scenario(s) for that stage, and a scenario names the stream it plays
// with `chara demo デモ再生 -i t:<stream>`. Inverting that include graph gives an exact
// demo -> stage map. It is extracted into <data dir>/stage/demos.json.
//
// gameDir is unused today (the answer does not depend on the install), but is taken so
// callers do not have to change if a data-driven fallback is added.
func ForDemo(demoPath string, gameDir string) (string, error) {
	name := strings.ToLower(filepath.Base(demoPath))
	name = strings.TrimSuffix(name, ".sdt")
	d, err := demoTable()
	if err != nil {
		return "", err
	}
	if st, ok := d[name]; ok {
		return st, nil
	}
	return "", fmt.Errorf("stage: no stage known for demo %q", name)
}

// Demos lists every cutscene known to play in a stage.
func Demos(stageName string) []string {
	d, err := demoTable()
	if err != nil {
		return nil
	}
	var out []string
	for demo, st := range d {
		if st == stageName {
			out = append(out, demo)
		}
	}
	sortStrings(out)
	return out
}

// Stages lists every stage we have placement data for.
func Stages() []string {
	s, err := stageTable()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// Load assembles a stage: the parsed scenario's placements, resolved against the
// stage's manifest so every model carries the cache id internal/model can look up.
//
// gameDataDir (the Bluepoint GameData tree) or gameDir (the install) supplies
// <region>/stage/<stage>/manifest.txt; either works, gameDataDir wins. toolsDir is
// accepted for signature stability and unused -- we never invoke gclconv, because we
// read the .gcl source rather than decompiling the shipped .gcx.
//
// If we have no scenario for the stage but do have its manifest, Load still returns a
// Stage: Assets is filled, Placements is empty, Partial is true. The editor can then
// say "this stage contains these N models" instead of showing a bare grid.
func Load(stageName string, gameDir, gameDataDir, toolsDir string) (*Stage, error) {
	_ = toolsDir

	tbl, err := stageTable()
	if err != nil {
		return nil, err
	}
	st, haveScenario := tbl[stageName]
	if !haveScenario {
		st = &Stage{Name: stageName, Partial: true}
	} else {
		st = st.clone()
	}
	st.Name = stageName
	st.Demos = Demos(stageName)

	man, manErr := loadManifest(stageName, gameDataDir, gameDir)
	if manErr != nil && !haveScenario {
		return nil, fmt.Errorf("stage %q: no scenario data and no manifest: %w", stageName, manErr)
	}
	if manErr == nil {
		st.Assets = man.assets
		st.resolveIDs(man)
	}
	if !haveScenario {
		st.Partial = true
	}
	return st, nil
}

// resolveIDs upgrades the cache ids and extensions using the manifest, which is the same
// table the game binds against. Ids are already correct without it (cache id == StrCode of
// the name), but only the manifest knows which extension a model actually shipped as.
func (s *Stage) resolveIDs(m *manifest) {
	for i := range s.Placements {
		if id, ext, ok := m.lookup(s.Placements[i].Name); ok {
			s.Placements[i].CacheID, s.Placements[i].Ext = id, ext
		}
	}
	if s.Sky != nil {
		if walls, ok := s.Sky.Params["walls"].([]any); ok {
			for _, w := range walls {
				wm, ok := w.(map[string]any)
				if !ok {
					continue
				}
				n, _ := wm["name"].(string)
				if id, ext, ok := m.lookup(n); ok {
					wm["cacheId"], wm["ext"] = float64(id), ext
				}
			}
		}
	}
}

func (s *Stage) clone() *Stage {
	c := *s
	c.Placements = append([]Placement(nil), s.Placements...)
	if s.Sky != nil {
		sk := *s.Sky
		sk.Params = deepCopyMap(s.Sky.Params)
		c.Sky = &sk
	}
	if s.Sea != nil {
		v := *s.Sea
		c.Sea = &v
	}
	if s.Fog != nil {
		v := *s.Fog
		c.Fog = &v
	}
	return &c
}

func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case map[string]any:
			out[k] = deepCopyMap(t)
		case []any:
			cp := make([]any, len(t))
			for i, e := range t {
				if em, ok := e.(map[string]any); ok {
					cp[i] = deepCopyMap(em)
				} else {
					cp[i] = e
				}
			}
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
