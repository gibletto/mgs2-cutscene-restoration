// Package config holds the machine-specific paths. Nothing else in the tool is
// allowed to hardcode a path -- this is what makes the studio portable between
// a Windows box with the game on a Steam library drive and a Mac with a copied asset tree.
//
// Every path can be set in the config file (see Path). Empty fields fall back to an
// environment variable, then to auto-detection:
//
//	gameDir          $MGS2_DIR, then the usual Steam install spots
//	gameDataDir      $MGS2_GAMEDATA
//	toolsDir         $MGS2_TOOLS_DIR
//	dataDir          $MGS2CS_DATA_DIR, then ./data, <exe dir>/data, <module root>/data
//	catalogPath      effects.json in the data dir
//	putobjRegistry   $MGS2CS_PUTOBJ_REGISTRY, then putobj_registry.json next to the exe / in the working dir
//	outDir           ./out
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"mgs2-restoration/cutscene-studio/internal/datadir"
)

type Config struct {
	// GameDir is the Master Collection install (contains us/, eu/, jp/, assets/).
	GameDir string `json:"gameDir"`
	// GameDataDir is the Bluepoint GameData tree (manifests, flatlist, cooked assets).
	GameDataDir string `json:"gameDataDir"`
	// ToolsDir is Konami's tool tree (gclconv, gclk, streamtool, ...). Optional:
	// only the .gcx recompile path needs it.
	ToolsDir string `json:"toolsDir"`
	// DataDir holds the generated data (effects.json, stage/*.json, ...). Optional: the
	// default search is described in internal/datadir.
	DataDir string `json:"dataDir,omitempty"`
	// CatalogPath is the effect catalog, effects.json. Defaults to the one in the data dir.
	CatalogPath string `json:"catalogPath"`
	// PutobjRegistry is putobj_registry.json (the viewport's put-object ghosts). Optional;
	// empty means "look next to the executable, then in the working directory".
	PutobjRegistry string `json:"putobjRegistry,omitempty"`
	// OutDir is where a bake lands. Baking NEVER writes into the game -- deploying does,
	// and only when you ask. Keeps a bad bake one step away from a live game file.
	OutDir string `json:"outDir"`
}

// Problems lists what is missing, so the UI can tell the user exactly what to fix
// instead of failing somewhere deep in a bake.
func (c *Config) Problems() []string {
	var p []string
	check := func(label, path string, required bool, probe ...string) {
		if path == "" {
			if required {
				p = append(p, label+" is not set")
			}
			return
		}
		if _, err := os.Stat(path); err != nil {
			p = append(p, fmt.Sprintf("%s does not exist: %s", label, path))
			return
		}
		for _, pr := range probe {
			if _, err := os.Stat(filepath.Join(path, pr)); err != nil {
				p = append(p, fmt.Sprintf("%s looks wrong: %s is missing under %s", label, pr, path))
				return
			}
		}
	}
	check("gameDir", c.GameDir, true, filepath.Join("us", "demo2", "_bp"))
	check("gameDataDir", c.GameDataDir, false)
	check("toolsDir", c.ToolsDir, false)
	if c.CatalogPath != "" {
		if _, err := os.Stat(c.CatalogPath); err != nil {
			p = append(p, "catalogPath does not exist: "+c.CatalogPath)
		}
	}
	return p
}

func (c *Config) OK() bool { return len(c.Problems()) == 0 }

// DemoDirs are the places cutscenes live, in load-priority order. The update\ tree is
// the ASI-loader overload the game reads FIRST, so live override sdts sort ahead of stock.
func (c *Config) DemoDirs() []string {
	return []string{
		filepath.Join(c.GameDir, "update", "us", "demo2", "_bp"),
		filepath.Join(c.GameDir, "update", "us", "demo", "_bp"),
		filepath.Join(c.GameDir, "update", "jp", "demo2", "_bp"),
		filepath.Join(c.GameDir, "update", "jp", "demo", "_bp"),
		filepath.Join(c.GameDir, "us", "demo2", "_bp"),
		filepath.Join(c.GameDir, "us", "demo", "_bp"),
		filepath.Join(c.GameDir, "jp", "demo2", "_bp"),
		filepath.Join(c.GameDir, "jp", "demo", "_bp"),
	}
}

func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "mgs2-cutscene-studio", "config.json")
}

func Load() (*Config, error) {
	c := &Config{}
	b, err := os.ReadFile(Path())
	if err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, err
		}
	}
	c.applyDefaults()
	datadir.Set(c.DataDir)
	return c, nil
}

func (c *Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// applyDefaults guesses the usual install spots so a first run mostly just works.
func (c *Config) applyDefaults() {
	if c.OutDir == "" {
		c.OutDir = "out"
		if abs, err := filepath.Abs(c.OutDir); err == nil {
			c.OutDir = abs
		}
	}
	if c.DataDir == "" {
		c.DataDir = os.Getenv(datadir.EnvVar)
	}
	if c.CatalogPath == "" {
		datadir.Set(c.DataDir)
		c.CatalogPath = datadir.File("effects.json")
	}
	if c.PutobjRegistry == "" {
		c.PutobjRegistry = os.Getenv("MGS2CS_PUTOBJ_REGISTRY")
	}
	if c.ToolsDir == "" {
		c.ToolsDir = os.Getenv("MGS2_TOOLS_DIR")
	}
	if c.GameDataDir == "" {
		if p := os.Getenv("MGS2_GAMEDATA"); p != "" {
			if _, err := os.Stat(filepath.Join(p, "bp_flatlistassetmapping.txt")); err == nil {
				c.GameDataDir = p
			}
		}
	}
	if c.GameDir == "" {
		c.GameDir = os.Getenv("MGS2_DIR")
	}
	if c.GameDir != "" || runtime.GOOS != "windows" {
		return
	}
	for _, p := range steamCandidates() {
		if _, err := os.Stat(filepath.Join(p, "us", "demo2", "_bp")); err == nil {
			c.GameDir = p
			return
		}
	}
}

// steamCandidates is where a Steam install of the Master Collection usually lands: the
// default Steam library on C:, and a "SteamLibrary" folder at the root of any other drive.
func steamCandidates() []string {
	out := []string{`C:\Program Files (x86)\Steam\steamapps\common\MGS2`}
	for d := 'C'; d <= 'Z'; d++ {
		out = append(out, filepath.Join(string(d)+`:\`, "SteamLibrary", "steamapps", "common", "MGS2"))
	}
	return out
}
