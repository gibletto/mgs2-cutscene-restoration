package stage

import "strings"

// labelStage puts English names on the geometry we can recognise. The scenario names its
// objects in Japanese and we deliberately never decode EUC-JP, so this is a curated map
// keyed off the model name instead. Unknown geometry gets no label -- that is fine.
func labelStage(s *Stage) {
	for i := range s.Placements {
		s.Placements[i].Label = labelFor(s.Placements[i].Name)
	}
}

func labelFor(model string) string {
	m := strings.ToLower(model)
	switch {
	case m == "vw60a_9":
		return "Manhattan land (wrecked)"
	case m == "w60a_7":
		return "Manhattan land (intact)"
	case m == "w60a_amg":
		return "Arsenal Gear (stage copy)"
	case strings.HasPrefix(m, "w60a_sky_wall"), strings.HasSuffix(m, "_sky_wall1"),
		strings.HasSuffix(m, "_sky_wall2"), strings.HasSuffix(m, "_sky_wall3"):
		return "sky wall"
	case m == "w00_sky":
		return "sky"
	case strings.HasPrefix(m, "w60a_"):
		return "Manhattan skyline"
	case strings.HasPrefix(m, "w10a_"):
		return "Verrazano-Narrows bridge / land"
	}
	return ""
}
