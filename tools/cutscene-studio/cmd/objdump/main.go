// objdump: per-frame position of one object in a .sdt.
// usage: objdump <file.sdt> <object id> <from> <to> [stride]
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
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: objdump <file.sdt> <object id> <from> <to> [stride]")
		os.Exit(2)
	}
	path := os.Args[1]
	idArg, _ := strconv.Atoi(os.Args[2])
	id := int32(idArg)
	from, _ := strconv.Atoi(os.Args[3])
	to, _ := strconv.Atoi(os.Args[4])
	stride := 1
	if len(os.Args) > 5 {
		stride, _ = strconv.Atoi(os.Args[5])
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
	if to >= sc.Frames {
		to = sc.Frames - 1
	}
	for _, o := range sc.Objects {
		if o.ID != id {
			continue
		}
		fmt.Printf("# object %d, frames %d..%d\n", id, from, to)
		for f := from; f <= to; f += stride {
			fmt.Printf("%d %.1f %.1f %.1f %.1f\n", f,
				o.PosX.Sample(f), o.PosY.Sample(f), o.PosZ.Sample(f), o.RotY.Sample(f))
		}
		return
	}
	fmt.Fprintf(os.Stderr, "no object with id %d (the file has %d)\n", id, len(sc.Objects))
	os.Exit(1)
}
