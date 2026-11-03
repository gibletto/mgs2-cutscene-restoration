package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Engine limits: the ranges Konami's authoring schema does NOT declare but the engine
// enforces (or, worse, fails to enforce).
//
// demoeffect.cnf gives a type and a default and nothing else, so effects.json reports
// min/max as null for almost everything. That gap is not academic: NewBlur_Demo's
// "intensity" is a PS2 GS alpha where 128 == 1.0, and setting it to 130 composites the
// previous frame over the new one at full opacity, every frame -- the picture freezes
// while the audio plays on. The value is only clamped if the effect is sent a ramp
// message, which shipped cutscenes never do.
//
// So these limits are read out of the engine source, and they carry the thing that
// actually matters: whether the engine clamps for you, or lets the raw value through.

// Units names the scale a parameter is really in, which is often not what the label says.
type Units string

const (
	UnitGSAlpha   Units = "gs-alpha"   // 0..128, where 128 == 1.0. NOT 0..255.
	UnitRGB255    Units = "rgb-255"    // 0..255 per component
	UnitAngle4096 Units = "angle-4096" // 4096 == 360 degrees
	UnitRadians   Units = "radians"
	UnitDegrees   Units = "degrees"
	UnitFrames    Units = "frames"
	UnitScalar    Units = "scalar"
	UnitIndex     Units = "index"
	UnitMM        Units = "mm"
)

// Limit is one engine-derived range for one effect parameter.
type Limit struct {
	EffectID int32    `json:"effectId"`
	Param    string   `json:"param"` // the catalog's d-index, e.g. "d1"
	Member   string   `json:"member"`
	Min      *float64 `json:"min"`
	Max      *float64 `json:"max"`
	Units    Units    `json:"units"`
	// EngineClamps is the field that decides how loud the UI should be. True: the engine
	// clamps, so an out-of-range value is merely ignored. False: the raw value reaches the
	// renderer or the logic, and the user gets a broken cutscene with no error.
	EngineClamps bool   `json:"engineClamps"`
	OutOfRange   string `json:"outOfRange"` // plain English: what actually goes wrong
	Source       string `json:"source"`     // file:line in the Konami tree
}

// LoadLimits merges engine limits into an already-loaded catalog. Missing file is not an
// error -- the catalog just has fewer known ranges.
func (c *Catalog) LoadLimits(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var limits []Limit
	if err := json.Unmarshal(raw, &limits); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}

	n := 0
	for i := range limits {
		l := &limits[i]
		e := c.ByEffectID(l.EffectID)
		if e == nil {
			continue
		}
		for j := range e.Params {
			p := &e.Params[j]
			if p.Name != l.Param {
				continue
			}
			if l.Min != nil {
				p.Min = l.Min
			}
			if l.Max != nil {
				p.Max = l.Max
			}
			p.Units = l.Units
			p.EngineClamps = l.EngineClamps
			p.OutOfRange = l.OutOfRange
			p.LimitSource = l.Source
			n++
		}
	}
	return n, nil
}

// limitsPathFor guesses effect_limits.json sitting next to effects.json.
func limitsPathFor(catalogPath string) string {
	return filepath.Join(filepath.Dir(catalogPath), "effect_limits.json")
}

// InRange reports whether v is within the param's engine limits.
func (p *Param) InRange(v float64) bool {
	if p.Min != nil && v < *p.Min {
		return false
	}
	if p.Max != nil && v > *p.Max {
		return false
	}
	return true
}
