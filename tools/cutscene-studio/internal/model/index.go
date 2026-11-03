// Package model reads MGS2 geometry for the editor viewport: the Bluepoint asset index
// (cache_id -> real file) and the Konami runtime mesh formats (.kms rigid, .evm skinned),
// exported as glTF 2.0 for three.js.
package model

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Asset is one resolved target: a cache id, under one extension, pointing at one real file.
// The same file can back several ids (Bluepoint content-deduped its pool), and one id can
// have several files (the _stage_XXXX forks), so identity is the (CacheID, Ext, Path) triple.
type Asset struct {
	Name    string // target basename, no extension
	CacheID int32  // 24-bit, spec byte already stripped
	Ext     string // "evm", "kms", ...
	Stage   string // representative stage/face name; see Index.KeysFor for the full set
	Path    string // flatlist target, relative to <gameroot>/assets -- e.g. "evm/us/rai_def_mh_mt.evm"
}

// Key is the flatlist's real lookup key. Nothing less than the full 6-tuple is safe:
// on cache id alone, ~45% of the ids a demo references have more than one target.
type Key struct {
	Region    string // us / eu / jp
	Container string // stage / face
	Name      string // stage id (a16a) or face id (f04a)
	Dir       string // cache / resident
	CacheID   int32
	Ext       string
}

type cid struct {
	id  int32
	ext string
}

type entry struct {
	asset Asset
	keys  []Key
}

// Index is bp_flatlistassetmapping.txt, both ways round.
type Index struct {
	entries    []entry
	byKey      map[Key]int
	byID       map[cid][]int
	extForSpec map[byte]string
	assetRoot  string
}

// LoadIndex parses GameData/bp_flatlistassetmapping.txt. If the game tree sits next to
// GameData it also picks up the asset root, so FilePath works without further setup.
func LoadIndex(gameDataDir string) (*Index, error) {
	name := filepath.Join(gameDataDir, "bp_flatlistassetmapping.txt")
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ix := &Index{
		byKey:      make(map[Key]int, 1<<18),
		byID:       make(map[cid][]int, 1<<13),
		extForSpec: make(map[byte]string, 16),
	}
	slot := make(map[Asset]int, 1<<13)
	intern := make(map[string]string, 512)
	keep := func(s string) string {
		if v, ok := intern[s]; ok {
			return v
		}
		intern[s] = s
		return s
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for line := 0; sc.Scan(); line++ {
		text := strings.TrimRight(sc.Text(), "\r")
		if text == "" {
			continue
		}
		virt, target, ok := strings.Cut(text, " ")
		if !ok {
			if line == 0 && strings.HasPrefix(text, "assetmap") {
				continue
			}
			return nil, fmt.Errorf("%s:%d: not a mapping line: %q", name, line+1, text)
		}
		k, a, err := parseMapping(virt, target)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, line+1, err)
		}
		k.Region, k.Container, k.Name, k.Dir, k.Ext = keep(k.Region), keep(k.Container), keep(k.Name), keep(k.Dir), keep(k.Ext)
		a.Ext = k.Ext

		i, ok := slot[a]
		if !ok {
			i = len(ix.entries)
			slot[a] = i
			ix.entries = append(ix.entries, entry{asset: a})
			c := cid{a.CacheID, a.Ext}
			ix.byID[c] = append(ix.byID[c], i)
		}
		ix.entries[i].keys = append(ix.entries[i].keys, k)
		if prev, dup := ix.byKey[k]; dup && prev != i {
			return nil, fmt.Errorf("%s:%d: key %v maps to two targets", name, line+1, k)
		}
		ix.byKey[k] = i
		ix.extForSpec[k.Ext[0]] = k.Ext
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for i := range ix.entries {
		ix.entries[i].asset.Stage = pickStage(ix.entries[i].keys)
	}
	if root := filepath.Dir(gameDataDir); dirExists(filepath.Join(root, "assets")) {
		ix.assetRoot = root
	}
	return ix, nil
}

// virtual: <region>/<container>/<name>/<dir>/<cache_id24:08x>.<ext>   target: <ext>/<region>/<base>.<ext>
func parseMapping(virt, target string) (Key, Asset, error) {
	var k Key
	var a Asset
	p := strings.Split(virt, "/")
	if len(p) != 5 {
		return k, a, fmt.Errorf("bad virtual path %q", virt)
	}
	file := p[4]
	dot := strings.LastIndexByte(file, '.')
	if dot < 0 {
		return k, a, fmt.Errorf("bad virtual path %q", virt)
	}
	id, err := strconv.ParseUint(file[:dot], 16, 32)
	if err != nil {
		return k, a, fmt.Errorf("bad cache id in %q", virt)
	}
	if id>>24 != 0 {
		return k, a, fmt.Errorf("virtual path %q carries a spec byte", virt)
	}
	k = Key{Region: p[0], Container: p[1], Name: p[2], Dir: p[3], CacheID: int32(id), Ext: file[dot+1:]}
	a = Asset{Name: strings.TrimSuffix(path.Base(target), path.Ext(target)), CacheID: k.CacheID, Path: target}
	return k, a, nil
}

func pickStage(keys []Key) string {
	best := keys[0]
	for _, k := range keys[1:] {
		if less(k, best) {
			best = k
		}
	}
	return best.Name
}

// us before eu/jp, stage before face, cache before resident, then name -- purely so a target's
// representative key (and any ranking built on it) is stable.
func less(a, b Key) bool {
	if ra, rb := regionRank(a.Region), regionRank(b.Region); ra != rb {
		return ra < rb
	}
	if a.Container != b.Container {
		return a.Container < b.Container
	}
	if a.Dir != b.Dir {
		return a.Dir < b.Dir
	}
	return a.Name < b.Name
}

func usKeys(keys []Key) int {
	n := 0
	for _, k := range keys {
		if k.Region == "us" {
			n++
		}
	}
	return n
}

func regionRank(r string) int {
	switch r {
	case "us":
		return 0
	case "eu":
		return 1
	}
	return 2
}

// Lookup is the only correct resolve: the full 6-tuple, exactly as the flatlist keys it.
func (ix *Index) Lookup(k Key) (Asset, bool) {
	i, ok := ix.byKey[k]
	if !ok {
		return Asset{}, false
	}
	return ix.entries[i].asset, true
}

// Resolve is the lossy one: a cache id and an extension, with no scene context. It returns
// every target that id can mean, ranked (most-referenced US target first) -- so the caller can
// show the ambiguity instead of silently picking the wrong Raiden. Prefer Lookup or ResolveStage.
//
// cacheID may be a raw DEMO_OBJECT field (spec byte in the top byte) or a bare 24-bit id.
// An empty ext is derived from the spec byte, which is the field's only ext carrier.
func (ix *Index) Resolve(cacheID int32, ext string) []Asset {
	id, spec := SplitCacheID(cacheID)
	ext = normalizeExt(ext)
	if ext == "" {
		ext = ix.extForSpec[spec]
	}
	slots := ix.byID[cid{id, ext}]
	if len(slots) == 0 {
		return nil
	}
	ranked := append([]int(nil), slots...)
	sort.Slice(ranked, func(i, j int) bool {
		ei, ej := &ix.entries[ranked[i]], &ix.entries[ranked[j]]
		if ui, uj := usKeys(ei.keys), usKeys(ej.keys); ui != uj {
			return ui > uj
		}
		if len(ei.keys) != len(ej.keys) {
			return len(ei.keys) > len(ej.keys)
		}
		return ei.asset.Path < ej.asset.Path
	})
	out := make([]Asset, len(ranked))
	for i, s := range ranked {
		out[i] = ix.entries[s].asset
	}
	return out
}

// ResolveStage narrows Resolve with the scene's stage (or face) id -- usually down to one.
// Region and dir stay open, because a demo tells us neither.
func (ix *Index) ResolveStage(stage string, cacheID int32, ext string) []Asset {
	var out []Asset
	for _, a := range ix.Resolve(cacheID, ext) {
		for _, k := range ix.KeysFor(a) {
			if k.Name == stage {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// KeysFor gives every virtual path that lands on this asset.
func (ix *Index) KeysFor(a Asset) []Key {
	for _, s := range ix.byID[cid{a.CacheID, a.Ext}] {
		if ix.entries[s].asset.Path == a.Path {
			return ix.entries[s].keys
		}
	}
	return nil
}

// Assets returns every distinct target, sorted, for enumeration in the editor's picker.
func (ix *Index) Assets() []Asset {
	out := make([]Asset, len(ix.entries))
	for i := range ix.entries {
		out[i] = ix.entries[i].asset
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ext != out[j].Ext {
			return out[i].Ext < out[j].Ext
		}
		if out[i].CacheID != out[j].CacheID {
			return out[i].CacheID < out[j].CacheID
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// AssetsByExt returns the distinct targets for one extension.
func (ix *Index) AssetsByExt(ext string) []Asset {
	ext = normalizeExt(ext)
	var out []Asset
	for _, a := range ix.Assets() {
		if a.Ext == ext {
			out = append(out, a)
		}
	}
	return out
}

// Keys is the number of flatlist lines the index absorbed.
func (ix *Index) Keys() int { return len(ix.byKey) }

// Exts lists the extensions seen, sorted.
func (ix *Index) Exts() []string {
	seen := map[string]bool{}
	for _, e := range ix.entries {
		seen[e.asset.Ext] = true
	}
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// SetAssetRoot points the index at the installed game (the directory holding assets/).
func (ix *Index) SetAssetRoot(dir string) { ix.assetRoot = dir }

func (ix *Index) AssetRoot() string { return ix.assetRoot }

// FilePath turns a target into a real on-disk path. Empty if no asset root is set.
func (ix *Index) FilePath(a Asset) string {
	if ix.assetRoot == "" {
		return ""
	}
	return filepath.Join(ix.assetRoot, "assets", filepath.FromSlash(a.Path))
}

// Exists reports whether the target is actually on disk. Two flatlist targets are not:
// kms/us/{cit_male_all_def,cti_female}.kms (the d082p01 citizens) ship only under eu/.
func (ix *Index) Exists(a Asset) bool {
	p := ix.FilePath(a)
	if p == "" {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func normalizeExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}
