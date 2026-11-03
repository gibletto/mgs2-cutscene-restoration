package stage

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"mgs2-restoration/cutscene-studio/internal/datadir"
)

// The stage geometry is static data extracted once from the original scenario source
// (mgs2x/scn) by gen.go, so the editor needs no source tree at runtime. It is NOT
// shipped with the tool: generate it into the data dir with
//
//	go run ./internal/stage/gen.go -scn <source tree>/mgs2x/scn -game <game dir>
//
// which writes <data dir>/stage/stages.json and <data dir>/stage/demos.json. Without
// those files the tables are empty: ForDemo reports "no stage known" for every demo and
// the viewport shows no world, but nothing else changes.
const (
	stagesFile = "stage/stages.json"
	demosFile  = "stage/demos.json"
)

var (
	once      sync.Once
	stagesMap map[string]*Stage
	demosMap  map[string]string
	loadErr   error
)

// readTable returns the file's bytes, or an empty JSON object when it was never generated.
func readTable(name string) ([]byte, error) {
	p := datadir.File(name)
	if p == "" {
		return []byte("{}"), nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func loadTables() {
	once.Do(func() {
		stagesJSON, err := readTable(stagesFile)
		if err != nil {
			loadErr = err
			return
		}
		demosJSON, err := readTable(demosFile)
		if err != nil {
			loadErr = err
			return
		}
		if err := json.Unmarshal(stagesJSON, &stagesMap); err != nil {
			loadErr = fmt.Errorf("%s: %w", stagesFile, err)
			return
		}
		if err := json.Unmarshal(demosJSON, &demosMap); err != nil {
			loadErr = fmt.Errorf("%s: %w", demosFile, err)
		}
	})
}

// HaveTables reports whether the generated stage tables were found in the data dir.
func HaveTables() bool {
	return datadir.File(stagesFile) != "" && datadir.File(demosFile) != ""
}

func stageTable() (map[string]*Stage, error) {
	loadTables()
	return stagesMap, loadErr
}

func demoTable() (map[string]string, error) {
	loadTables()
	return demosMap, loadErr
}
