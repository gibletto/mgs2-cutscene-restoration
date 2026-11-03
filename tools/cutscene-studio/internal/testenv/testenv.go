// Package testenv tells the tests where the user's own inputs are. Nothing it points at is
// part of this repository: the game install, the Bluepoint GameData tree and the generated
// data dir all come from the user's machine. A test that needs one of them and cannot find
// it skips, naming the variable that would supply it.
//
//	MGS2_DIR          the Master Collection install (default: the config file's gameDir,
//	                  which itself falls back to the usual Steam install spots)
//	MGS2_GAMEDATA     the Bluepoint GameData tree (default: the config file's gameDataDir)
//	MGS2CS_DATA_DIR   the generated data dir (default: see internal/datadir)
package testenv

import (
	"os"
	"sync"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/config"
	"mgs2-restoration/cutscene-studio/internal/datadir"
)

var (
	once sync.Once
	cfg  *config.Config
)

func loadConfig() *config.Config {
	once.Do(func() {
		c, err := config.Load()
		if err != nil {
			c = &config.Config{}
		}
		cfg = c
	})
	return cfg
}

// GameDir is the game install, or "" when none is configured or detected.
func GameDir() string {
	if v := os.Getenv("MGS2_DIR"); v != "" {
		return v
	}
	return loadConfig().GameDir
}

// GameDataDir is the Bluepoint GameData tree, or "" when none is configured.
func GameDataDir() string {
	if v := os.Getenv("MGS2_GAMEDATA"); v != "" {
		return v
	}
	return loadConfig().GameDataDir
}

// DataFile returns the path of a generated data file (e.g. "effects.json",
// "stage/stages.json"), skipping the test when it has not been generated.
func DataFile(t testing.TB, name string) string {
	t.Helper()
	loadConfig() // honours the config file's dataDir
	p := datadir.File(name)
	if p == "" {
		t.Skipf("%s not found in the data dir (generate it, see README \"Setup\", or set %s)", name, datadir.EnvVar)
	}
	return p
}
