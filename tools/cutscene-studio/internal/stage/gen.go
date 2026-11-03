//go:build ignore

// gen.go rebuilds the stage tables (stages.json and demos.json) from Konami's scenario
// source. They land in <data dir>/stage/, which is gitignored: the output is derived from
// the original source tree and is never committed.
//
//	go run ./internal/stage/gen.go -scn <source tree>/mgs2x/scn -game <game dir>
//
// or, with MGS2_SOURCE_DIR (the source tree root, the directory holding mgs2x/) and
// MGS2_DIR (the game install) set in the environment:
//
//	go generate ./internal/stage
//
// The .gcl tree is EUC-JP; nothing here decodes it (see gcl.go).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mgs2-restoration/cutscene-studio/internal/datadir"
	"mgs2-restoration/cutscene-studio/internal/stage"
)

var includeRE = regexp.MustCompile(`#include\s+"([^"]+)"`)

func main() {
	defScn := ""
	if src := os.Getenv("MGS2_SOURCE_DIR"); src != "" {
		defScn = filepath.Join(src, "mgs2x", "scn")
	}
	scn := flag.String("scn", defScn, "Konami mgs2x/scn directory (default $MGS2_SOURCE_DIR/mgs2x/scn)")
	game := flag.String("game", os.Getenv("MGS2_DIR"), "game install, to list shipped .sdt (default $MGS2_DIR)")
	out := flag.String("out", filepath.Join(datadir.Default(), "stage"), "output directory (default <data dir>/stage)")
	flag.Parse()
	if *scn == "" || *game == "" {
		fatal("need -scn <source tree>/mgs2x/scn and -game <game dir> (or set MGS2_SOURCE_DIR and MGS2_DIR)")
	}

	// every stream the game actually ships -- the filter for "is this token a cutscene?"
	shipped := map[string]bool{}
	for _, sub := range []string{"us/demo/_bp", "us/demo2/_bp", "jp/demo/_bp", "jp/demo2/_bp"} {
		fs, _ := filepath.Glob(filepath.Join(*game, sub, "*.sdt"))
		for _, f := range fs {
			shipped[strings.ToLower(strings.TrimSuffix(filepath.Base(f), ".sdt"))] = true
		}
	}
	fmt.Printf("shipped streams: %d\n", len(shipped))

	gcls, err := filepath.Glob(filepath.Join(*scn, "_bp", "scenerio_stage_*.gcl"))
	must(err)
	if len(gcls) == 0 {
		fatal("no scenerio_stage_*.gcl under " + filepath.Join(*scn, "_bp"))
	}

	stages := map[string]*stage.Stage{}
	cand := map[string][]string{} // demo -> candidate stages
	direct := map[string][]string{}

	for _, g := range gcls {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(g), "scenerio_stage_"), ".gcl")

		st, err := stage.ParseStage(*scn, name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", name, err)
			continue
		}
		if len(st.Placements) > 0 || st.Sky != nil || st.Sea != nil || st.Fog != nil {
			st.Demos = nil // filled at Load time from the demo table
			stages[name] = st
		}

		demos, err := stage.ScanDemos(*scn, name)
		if err != nil {
			continue
		}
		raw, _ := os.ReadFile(g)
		var incs []string
		for _, m := range includeRE.FindAllStringSubmatch(string(raw), -1) {
			incs = append(incs, strings.ToLower(filepath.Base(m[1])))
		}
		for _, d := range demos {
			if !shipped[d] {
				continue
			}
			cand[d] = append(cand[d], name)
			for _, in := range incs {
				if strings.HasPrefix(in, d+".") { // scenerio_stage_d080p07.gcl -> P080_13_p07b.w51a.gcl
					direct[d] = append(direct[d], name)
				}
			}
		}
	}

	demos := map[string]string{}
	var ambig int
	for d, cs := range cand {
		pick := choose(d, cs, direct[d])
		if len(uniq(cs)) > 1 {
			ambig++
		}
		demos[d] = pick
	}

	must(os.MkdirAll(*out, 0o755))
	writeJSON(filepath.Join(*out, "stages.json"), stages)
	writeJSON(filepath.Join(*out, "demos.json"), demos)

	fmt.Printf("stages with world data: %d / %d\n", len(stages), len(gcls))
	fmt.Printf("demos mapped: %d (%d needed tie-breaking)\n", len(demos), ambig)
	var unmapped []string
	for s := range shipped {
		if _, ok := demos[s]; !ok {
			unmapped = append(unmapped, s)
		}
	}
	sort.Strings(unmapped)
	fmt.Printf("shipped streams with NO stage: %d %v\n", len(unmapped), unmapped)
}

// choose breaks a tie when several stages name the same stream.
//
//  1. a stage whose scenerio_stage_*.gcl #includes the scenario file named after the demo
//     is the stage that owns it;
//  2. otherwise prefer a dedicated demo stage ("d...") over the game stage that also
//     references the cutscene (p080_01_p01 is named by both d080p01 and w46a).
func choose(demo string, cands, directHits []string) string {
	if d := uniq(directHits); len(d) == 1 {
		return d[0]
	} else if len(d) > 1 {
		cands = d
	}
	c := uniq(cands)
	sort.Slice(c, func(i, j int) bool {
		di, dj := strings.HasPrefix(c[i], "d"), strings.HasPrefix(c[j], "d")
		if di != dj {
			return di
		}
		return c[i] < c[j]
	})
	return c[0]
}

func uniq(s []string) []string {
	m := map[string]bool{}
	var out []string
	for _, v := range s {
		if !m[v] {
			m[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", " ")
	must(err)
	must(os.WriteFile(path, append(b, '\n'), 0o644))
	fmt.Printf("wrote %s (%d bytes)\n", path, len(b))
}

func must(err error) {
	if err != nil {
		fatal(err.Error())
	}
}
func fatal(m string) { fmt.Fprintln(os.Stderr, "gen: "+m); os.Exit(1) }
