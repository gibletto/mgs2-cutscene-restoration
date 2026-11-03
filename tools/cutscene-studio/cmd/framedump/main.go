// framedump: per-frame camera/object/effect dump for a frame range of a .sdt.
// usage: framedump <file.sdt> <from> <to> [stride]
package main

import (
	"fmt"
	"os"
	"strconv"

	"mgs2-restoration/cutscene-studio/internal/catalog"
	"mgs2-restoration/cutscene-studio/internal/config"
	"mgs2-restoration/cutscene-studio/internal/scene"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: framedump <file.sdt> <from> <to> [stride]")
		os.Exit(2)
	}
	path := os.Args[1]
	from, _ := strconv.Atoi(os.Args[2])
	to, _ := strconv.Atoi(os.Args[3])
	stride := 10
	if len(os.Args) > 4 {
		stride, _ = strconv.Atoi(os.Args[4])
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var cat *catalog.Catalog
	if cfg.CatalogPath != "" {
		cat, _ = catalog.Load(cfg.CatalogPath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sc, err := scene.Import(path, raw, cat)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("frames=%d\n", sc.Frames)
	if to >= sc.Frames {
		to = sc.Frames - 1
	}
	for f := from; f <= to; f += stride {
		c := sc.Camera
		fmt.Printf("f%-5d cam p(%8.0f %6.0f %8.0f) t(%8.0f %6.0f %8.0f) scr %.3f",
			f, c.PosX.Sample(f), c.PosY.Sample(f), c.PosZ.Sample(f),
			c.TrgX.Sample(f), c.TrgY.Sample(f), c.TrgZ.Sample(f), c.Screen.Sample(f))
		for _, o := range sc.Objects {
			if o.ID == 8 {
				fmt.Printf(" | id8 p(%8.0f %6.0f %8.0f) ry %5.0f",
					o.PosX.Sample(f), o.PosY.Sample(f), o.PosZ.Sample(f), o.RotY.Sample(f))
			}
		}
		fmt.Println()
	}
	fmt.Println("effects overlapping range:")
	for _, e := range sc.Effects {
		if e.End >= from && e.Start <= to {
			fmt.Printf("  [%4d..%4d] 0x%08x exec=%d %s\n", e.Start, e.End, e.EffectID, e.ExecType, e.Label)
		}
	}
}
