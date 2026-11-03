package stage

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// manifest is <region>/stage/<stage>/manifest.txt -- the list of everything a stage has
// resident, and the table the game itself binds cache ids against. Lines look like:
//
//	assets/kms/us/amg_def.kms,us/stage/d080p07/cache/00a13626.kms,cache/00a13626.kms
//
// col1 = the real file, col3 = the cache key the .sdt and the scenario refer to.
type manifest struct {
	assets []Asset
	byName map[string][]Asset
}

func (m *manifest) lookup(name string) (int32, string, bool) {
	// a model can exist as .kms and .evm; the mesh loader prefers kms, then evm, then zms.
	for _, ext := range []string{"kms", "evm", "zms", "cmdl"} {
		for _, a := range m.byName[strings.ToLower(name)] {
			if a.Ext == ext {
				return a.CacheID, a.Ext, true
			}
		}
	}
	if a := m.byName[strings.ToLower(name)]; len(a) > 0 {
		return a[0].CacheID, a[0].Ext, true
	}
	return 0, "", false
}

// loadManifest finds a stage's manifest. Western installs run as region "eu" for every
// region-scoped path (fshd.c), so eu/ is probed first; us/ and jp/ follow for safety.
func loadManifest(stage string, dirs ...string) (*manifest, error) {
	var tried []string
	for _, root := range dirs {
		if root == "" {
			continue
		}
		for _, region := range []string{"eu", "us", "jp"} {
			p := filepath.Join(root, region, "stage", stage, "manifest.txt")
			tried = append(tried, p)
			if _, err := os.Stat(p); err != nil {
				continue
			}
			return parseManifest(p)
		}
	}
	if len(tried) == 0 {
		return nil, fmt.Errorf("stage %q: no gameDir or gameDataDir given", stage)
	}
	return nil, fmt.Errorf("stage %q: manifest not found (tried %d paths, e.g. %s)", stage, len(tried), tried[0])
}

func parseManifest(path string) (*manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := &manifest{byName: map[string][]Asset{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		cols := strings.Split(strings.TrimSpace(sc.Text()), ",")
		if len(cols) < 3 {
			continue
		}
		src, cache := strings.TrimSpace(cols[0]), strings.TrimSpace(cols[2])

		// col3: "cache/00a13626.kms"
		base := path7Base(cache)
		dot := strings.LastIndexByte(base, '.')
		if dot < 0 {
			continue
		}
		id, err := strconv.ParseUint(base[:dot], 16, 32)
		if err != nil {
			continue
		}
		ext := strings.ToLower(base[dot+1:])

		// col1: "assets/kms/us/amg_def.kms" -> name "amg_def"
		nb := path7Base(src)
		if d := strings.LastIndexByte(nb, '.'); d >= 0 {
			nb = nb[:d]
		}
		a := Asset{Name: nb, Ext: ext, CacheID: int32(id & 0xFFFFFF), Path: src}
		m.assets = append(m.assets, a)
		key := strings.ToLower(nb)
		m.byName[key] = append(m.byName[key], a)
	}
	return m, sc.Err()
}

func path7Base(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}
