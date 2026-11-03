// mgs2cs is the MGS2 Cutscene Studio: a timeline editor for the game's .sdt cutscenes.
//
//	mgs2cs serve                    open the editor in a browser
//	mgs2cs ls                       list the cutscenes it can find
//	mgs2cs inspect <file.sdt>       dump a cutscene's structure
//	mgs2cs check <file.sdt>         run the validator (the idiom checklist)
//	mgs2cs roundtrip <dir>          prove the codec: import -> bake -> byte-compare
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mgs2-restoration/cutscene-studio/internal/catalog"
	"mgs2-restoration/cutscene-studio/internal/config"
	"mgs2-restoration/cutscene-studio/internal/datadir"
	"mgs2-restoration/cutscene-studio/internal/scene"
	"mgs2-restoration/cutscene-studio/internal/sdt"
	"mgs2-restoration/cutscene-studio/internal/server"
	"mgs2-restoration/cutscene-studio/internal/validate"
	webui "mgs2-restoration/cutscene-studio/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		die(err)
	}

	var cat *catalog.Catalog
	if cfg.CatalogPath != "" {
		// The validator's launcher table comes from the same file as the editor's catalog.
		validate.UseCatalogFile(cfg.CatalogPath)
		cat, err = catalog.Load(cfg.CatalogPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: no effect catalog (%v) -- effects will be opaque\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "warning: no effect catalog: effects.json not found in the data dir (%s); generate it with tools/cnf2json.py -- effects will be opaque\n", datadir.Dir())
	}

	switch os.Args[1] {
	case "serve":
		addr := "8733"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		ui, err := webui.FS()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v -- serving the API only\n", err)
			ui = nil
		}
		srv := server.New(cfg, cat, ui)
		for _, p := range cfg.Problems() {
			fmt.Fprintf(os.Stderr, "config: %s\n", p)
		}
		die(server.Listen(addr, srv.Handler()))

	case "ls":
		n := 0
		for _, dir := range cfg.DemoDirs() {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".sdt") {
					continue
				}
				p := filepath.Join(dir, e.Name())
				st, err := load(p)
				if err != nil {
					continue
				}
				frames := 0
				for i := range st.Chunks {
					if st.Chunks[i].Type() == sdt.CDemo {
						frames++
					}
				}
				if frames == 0 {
					continue
				}
				fmt.Printf("%-28s %5d frames  %6.1fs  %s\n", e.Name(), frames, float64(frames)/60, dir)
				n++
			}
		}
		fmt.Printf("\n%d cutscenes\n", n)

	case "inspect":
		need(3)
		inspect(os.Args[2], cat)

	case "check":
		need(3)
		st, err := load(os.Args[2])
		if err != nil {
			die(err)
		}
		f := validate.Stream(st)
		report(f)
		if validate.HasFatal(f) {
			os.Exit(1)
		}

	case "roundtrip":
		need(3)
		roundtrip(os.Args[2], cat)

	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mgs2cs -- MGS2 Cutscene Studio

  serve [port]        open the editor (default :8733)
  ls                  list cutscenes found in the configured game dir
  inspect <file.sdt>  dump structure: frames, cast, effects, camera
  check <file.sdt>    run the validator against the idiom checklist
  roundtrip <dir>     import -> bake -> byte-compare every .sdt in a dir
`)
}

func need(n int) {
	if len(os.Args) < n {
		usage()
		os.Exit(2)
	}
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func load(path string) (*sdt.Stream, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) >= 4 && string(raw[:4]) == "LCGB" {
		return nil, fmt.Errorf("GCL bytecode, not a stream")
	}
	return sdt.Decode(raw)
}

func inspect(path string, cat *catalog.Catalog) {
	raw, err := os.ReadFile(path)
	die(err)
	sc, err := scene.Import(path, raw, cat)
	die(err)

	fmt.Printf("%s\n", path)
	fmt.Printf("  %d frames (%.1fs)   audio: %v", sc.Frames, float64(sc.Frames)/60, sc.HasAudio)
	if sc.HasAudio {
		fmt.Printf(" (%.1fs)", float64(sc.AudioFrames)/60)
		if sc.AudioFrames < sc.Frames {
			fmt.Printf("  <- demo outruns its audio by %.1fs (legal: wall-clock fallback)",
				float64(sc.Frames-sc.AudioFrames)/60)
		}
	}
	fmt.Println()

	fmt.Printf("\n  cast (%d)\n", len(sc.Objects))
	for _, o := range sc.Objects {
		vis := "hidden"
		for _, v := range o.Visibility {
			if v.Visible {
				vis = fmt.Sprintf("shown@%d", v.Frame)
				break
			}
		}
		fmt.Printf("    id=%-4d kms=0x%08x evm=0x%08x flag=%d joints=%-3d %s\n",
			o.ID, uint32(o.KMSCacheID), uint32(o.CacheID), o.Flag, o.NJoints, vis)
	}

	type row struct {
		label string
		id    int32
		exec  int
		n     int
	}
	byFX := map[int32]*row{}
	for _, e := range sc.Effects {
		r := byFX[e.EffectID]
		if r == nil {
			r = &row{label: e.Label, id: e.EffectID, exec: e.ExecType}
			byFX[e.EffectID] = r
		}
		r.n++
	}
	rows := make([]*row, 0, len(byFX))
	for _, r := range byFX {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })

	fmt.Printf("\n  effects (%d instances, %d distinct)\n", len(sc.Effects), len(rows))
	for _, r := range rows {
		fmt.Printf("    0x%08x exec=%d  x%-4d %s\n", uint32(r.id), r.exec, r.n, r.label)
	}

	if len(sc.Captions) > 0 {
		fmt.Printf("\n  captions (%d)\n", len(sc.Captions))
		for i, c := range sc.Captions {
			if i >= 5 {
				fmt.Printf("    ... and %d more\n", len(sc.Captions)-5)
				break
			}
			fmt.Printf("    [%5d..%5d] lang=%d %q\n", c.Start, c.End, c.Lang, trunc(c.Text, 60))
		}
	}

	c := sc.Camera
	fmt.Printf("\n  camera\n")
	fmt.Printf("    frame 0     pos=(%.0f %.0f %.0f) trg=(%.0f %.0f %.0f) screen=%.3f (hfov %.1f deg) roll=%.4f rad\n",
		c.PosX.Sample(0), c.PosY.Sample(0), c.PosZ.Sample(0),
		c.TrgX.Sample(0), c.TrgY.Sample(0), c.TrgZ.Sample(0),
		c.Screen.Sample(0), deg(sc.HFOV(0, 1.0)), c.Roll.Sample(0))
	last := sc.Frames - 1
	fmt.Printf("    frame %-5d pos=(%.0f %.0f %.0f) trg=(%.0f %.0f %.0f) screen=%.3f (hfov %.1f deg) roll=%.4f rad\n",
		last, c.PosX.Sample(last), c.PosY.Sample(last), c.PosZ.Sample(last),
		c.TrgX.Sample(last), c.TrgY.Sample(last), c.TrgZ.Sample(last),
		c.Screen.Sample(last), deg(sc.HFOV(last, 1.0)), c.Roll.Sample(last))
}

func deg(rad float64) float64 { return rad * 180 / 3.141592653589793 }

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func report(f []validate.Finding) {
	if len(f) == 0 {
		fmt.Println("clean -- no findings")
		return
	}
	for _, x := range f {
		sev := "info"
		switch x.Severity {
		case validate.Fatal:
			sev = "FATAL"
		case validate.Warn:
			sev = "warn"
		}
		loc := ""
		if x.Frame >= 0 {
			loc = fmt.Sprintf(" frame %d", x.Frame)
		}
		fmt.Printf("%-5s %-4s%s  %s\n", sev, x.Rule, loc, x.Msg)
		if x.Fix != "" {
			fmt.Printf("           fix: %s\n", x.Fix)
		}
	}
}

// roundtrip is the proof the codec is honest: import every cutscene into the editable
// model, bake it straight back with no edits, and require the bytes to match exactly.
func roundtrip(dir string, cat *catalog.Catalog) {
	entries, err := os.ReadDir(dir)
	die(err)
	ok, bad, skip := 0, 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.Contains(e.Name(), ".sdt") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sc, err := scene.Import(p, raw, cat)
		if err != nil {
			skip++
			continue
		}
		out, err := sc.Bake(cat)
		if err != nil {
			fmt.Printf("BAKE FAIL %-28s %v\n", e.Name(), err)
			bad++
			continue
		}
		if len(out) != len(raw) {
			fmt.Printf("DIFF      %-28s %d -> %d bytes\n", e.Name(), len(raw), len(out))
			bad++
			continue
		}
		for i := range out {
			if out[i] != raw[i] {
				fmt.Printf("DIFF      %-28s first at 0x%x\n", e.Name(), i)
				bad++
				goto next
			}
		}
		ok++
	next:
	}
	fmt.Printf("\n%d identical, %d differ, %d skipped\n", ok, bad, skip)
	if bad > 0 {
		os.Exit(1)
	}
}
