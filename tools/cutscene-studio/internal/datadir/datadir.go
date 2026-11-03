// Package datadir locates the studio's generated data: the effect catalog, the stage
// tables and the corpus statistics. None of it is committed -- it is derived from the
// game's files and from the original source tree, so each user regenerates it locally
// (see README.md, "Setup"). This package only answers "where is it?".
//
// Lookup order for a data file, first hit wins:
//
//  1. the directory passed to Set (the config file's "dataDir")
//  2. $MGS2CS_DATA_DIR
//  3. ./data                      (running from the studio root)
//  4. <dir of the executable>/data
//  5. <module root>/data          (found by walking up from the working dir to go.mod;
//     this is what makes `go test ./...` find it from any package directory)
package datadir

import (
	"os"
	"path/filepath"
	"sync"
)

// EnvVar overrides the data directory.
const EnvVar = "MGS2CS_DATA_DIR"

var (
	mu       sync.RWMutex
	override string
)

// Set pins the data directory (e.g. from the config file). "" clears it.
func Set(dir string) {
	mu.Lock()
	override = dir
	mu.Unlock()
}

// Candidates lists the directories searched, in priority order. Duplicates and empty
// entries are dropped.
func Candidates() []string {
	mu.RLock()
	o := override
	mu.RUnlock()

	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	add(o)
	add(os.Getenv(EnvVar))
	add("data")
	if e, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(e), "data"))
	}
	if root := moduleRoot(); root != "" {
		add(filepath.Join(root, "data"))
	}
	return out
}

// File returns the path of a data file (name may contain a subdirectory, e.g.
// "stage/stages.json"), or "" if no candidate directory has it.
func File(name string) string {
	for _, d := range Candidates() {
		p := filepath.Join(d, filepath.FromSlash(name))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Dir is the data directory in use: the first candidate that exists, or the first
// candidate at all (where generated data is expected to go) if none does.
func Dir() string {
	c := Candidates()
	for _, d := range c {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	if len(c) > 0 {
		return c[0]
	}
	return "data"
}

// Default is where generators should write when not told otherwise: the Set/env
// override if any, else <module root>/data, else ./data.
func Default() string {
	mu.RLock()
	o := override
	mu.RUnlock()
	for _, p := range []string{o, os.Getenv(EnvVar)} {
		if p != "" {
			return p
		}
	}
	if root := moduleRoot(); root != "" {
		return filepath.Join(root, "data")
	}
	return "data"
}

// moduleRoot walks up from the working directory to the nearest go.mod.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
