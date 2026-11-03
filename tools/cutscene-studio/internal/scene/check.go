package scene

import (
	"fmt"

	"mgs2-restoration/cutscene-studio/internal/catalog"
	"mgs2-restoration/cutscene-studio/internal/validate"
)

// CheckParams flags authored effect values that fall outside the range the ENGINE can
// survive -- a range Konami's schema does not declare, so nothing else would catch it.
//
// The motivating case: NewBlur_Demo's intensity is a GS alpha (128 == 1.0). Set it to 130
// and the blur composites the previous frame over the new one at full opacity, forever:
// the picture freezes while the audio plays on. Nothing errors. Nothing logs. You just get
// a broken cutscene. So when the engine does NOT clamp, this is Fatal, not a warning.
func (s *Scene) CheckParams(cat *catalog.Catalog) []validate.Finding {
	if cat == nil {
		return nil
	}
	var out []validate.Finding

	for _, inst := range s.Effects {
		if inst.Deleted {
			continue
		}
		def := cat.ByEffectID(inst.EffectID)
		if def == nil {
			continue
		}
		for i := range def.Params {
			p := &def.Params[i]
			if p.Min == nil && p.Max == nil {
				continue // no engine range known for this one
			}
			for c := 0; c < components(p); c++ {
				ch := inst.Params[paramChannel(p, c)]
				if ch == nil {
					continue
				}
				frame, v, bad := worstSample(ch, inst, p)
				if !bad {
					continue
				}
				sev := validate.Warn
				fix := fmt.Sprintf("keep it within %s", rangeText(p))
				if !p.EngineClamps {
					// The raw value reaches the renderer. This is the dangerous kind.
					sev = validate.Fatal
					fix = fmt.Sprintf("clamp to %s -- the engine does NOT clamp this, so the raw value reaches the renderer", rangeText(p))
				}
				msg := fmt.Sprintf("%s.%s (%s) = %g, outside the engine range %s",
					def.CName, p.Name, labelOf(p), v, rangeText(p))
				if p.OutOfRange != "" {
					msg += " -- " + p.OutOfRange
				}
				out = append(out, validate.Finding{
					Rule:     "E1",
					Severity: sev,
					Msg:      msg,
					Chunk:    -1,
					Frame:    frame,
					Packet:   -1,
					Fix:      fix,
				})
			}
		}
	}
	return out
}

// worstSample returns the first out-of-range sample over the instance's span. Effect
// params are baked per frame, so a channel can be legal at frame 0 and illegal at 900.
func worstSample(ch *Channel, inst *EffectInstance, p *catalog.Param) (frame int, v float64, bad bool) {
	end := inst.End
	if inst.ExecType == 1 {
		end = inst.Start + 1 // born once; only its birth value is ever read
	}
	for f := inst.Start; f < end; f++ {
		got := ch.Sample(f)
		if !p.InRange(got) {
			return f, got, true
		}
	}
	return -1, 0, false
}

func rangeText(p *catalog.Param) string {
	unit := ""
	if p.Units != "" {
		unit = " " + string(p.Units)
	}
	switch {
	case p.Min != nil && p.Max != nil:
		return fmt.Sprintf("%g..%g%s", *p.Min, *p.Max, unit)
	case p.Max != nil:
		return fmt.Sprintf("<= %g%s", *p.Max, unit)
	case p.Min != nil:
		return fmt.Sprintf(">= %g%s", *p.Min, unit)
	}
	return "unbounded"
}

func labelOf(p *catalog.Param) string {
	if p.LabelEN != "" {
		return p.LabelEN
	}
	if p.Member != "" {
		return p.Member
	}
	return p.LabelJA
}
