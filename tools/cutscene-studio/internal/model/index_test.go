package model

import (
	"os"
	"testing"

	"mgs2-restoration/cutscene-studio/internal/testenv"
)

// The corpus. GameData (the flatlist) and the installed game usually live in different places.
func gameData(t *testing.T) string {
	t.Helper()
	dir := testenv.GameDataDir()
	if _, err := os.Stat(dir); dir == "" || err != nil {
		t.Skipf("no GameData at %q (set MGS2_GAMEDATA)", dir)
	}
	return dir
}

func gameRoot(t *testing.T) string {
	t.Helper()
	dir := envOr("MGS2_ROOT", testenv.GameDir())
	if _, err := os.Stat(dir); dir == "" || err != nil {
		t.Skipf("no game install at %q (set MGS2_DIR)", dir)
	}
	return dir
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadIndex(t *testing.T) *Index {
	t.Helper()
	ix, err := LoadIndex(gameData(t))
	if err != nil {
		t.Fatal(err)
	}
	ix.SetAssetRoot(gameRoot(t))
	return ix
}

func TestLoadIndex(t *testing.T) {
	ix := loadIndex(t)
	assets := ix.Assets()
	t.Logf("keys=%d distinct targets=%d exts=%v", ix.Keys(), len(assets), ix.Exts())
	t.Logf("evm targets=%d kms targets=%d", len(ix.AssetsByExt("evm")), len(ix.AssetsByExt("kms")))

	if ix.Keys() != 170478 {
		t.Errorf("keys = %d, want 170478", ix.Keys())
	}
	ids := map[int32]bool{}
	for _, a := range assets {
		ids[a.CacheID] = true
	}
	if len(ids) != 3994 {
		t.Errorf("distinct cache ids = %d, want 3994", len(ids))
	}
}

func TestStrCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int32
	}{
		{"ema_hair_mh", 0xce9e72},
		{"scenerio", 0x180720},
		{"kck_mrot", 0x2bc7f0},
		{"cit_male_all_def", 0x07596a},
		{"transcache", 0x573de0},
		{"rai_def", 0xc13a4e},
		{"demo_dummy", 0x64c6aa},
	} {
		if got := StrCode(tc.name); got != tc.want {
			t.Errorf("StrCode(%q) = %#x, want %#x", tc.name, got, tc.want)
		}
	}
}

func TestSplitCacheID(t *testing.T) {
	for _, tc := range []struct {
		field int32
		id    int32
		spec  byte
	}{
		{0x04541e89, 0x541e89, 'e'}, // Raiden's body, as a DEMO_OBJECT.cache_id
		{0x0a413aa8, 0x413aa8, 'k'}, // ...and his kms_cache_id sibling
		{0x00ce9e72, 0xce9e72, 'a'}, // a bare flatlist id: spec byte 0 reads as 'a'
	} {
		id, spec := SplitCacheID(tc.field)
		if id != tc.id || spec != tc.spec {
			t.Errorf("SplitCacheID(%#x) = %#x,%q want %#x,%q", tc.field, id, spec, tc.id, tc.spec)
		}
	}
	if got := MakeCacheID(0x541e89, "evm"); got != 0x04541e89 {
		t.Errorf("MakeCacheID = %#x, want 0x04541e89", got)
	}
}

// The headline trap: one cache id, many meshes. Resolve must surface all of them.
func TestResolveForked(t *testing.T) {
	ix := loadIndex(t)
	for _, tc := range []struct {
		name  string
		field int32
		ext   string
		want  int
	}{
		{"raiden body", 0x04541e89, "", 7},
		{"snake body", 0x0455aaa5, "", 8},
		{"snake kms", 0x0a413aa8, "", 7},
		{"dr_free", 0x0a24eeef, "", 9},
		{"ema_hair_mh", 0x00ce9e72, "evm", 2},
	} {
		got := ix.Resolve(tc.field, tc.ext)
		paths := make([]string, len(got))
		for i, a := range got {
			paths[i] = a.Path
		}
		t.Logf("%-12s %#x -> %d targets: %v", tc.name, tc.field, len(got), paths)
		if len(got) != tc.want {
			t.Errorf("%s: %d targets, want %d", tc.name, len(got), tc.want)
		}
		for _, a := range got {
			if _, err := os.Stat(ix.FilePath(a)); err != nil {
				t.Errorf("%s: %s does not exist on disk", tc.name, ix.FilePath(a))
			}
		}
	}
}

// The 6-tuple is the only safe key: same id + ext + region, different container -> different mesh.
func TestLookupFullKey(t *testing.T) {
	ix := loadIndex(t)
	for _, tc := range []struct {
		key  Key
		want string
	}{
		{Key{"us", "face", "f04a", "cache", 0xce9e72, "evm"}, "evm/us/ema_hair_mh.evm"},
		{Key{"us", "stage", "a16a", "cache", 0xce9e72, "evm"}, "evm/us/ema_hair_mh_stage_a16a.evm"},
		{Key{"us", "stage", "a45a", "cache", 0x413aa8, "kms"}, "kms/us/sna_def_stage_a45a.kms"},
	} {
		a, ok := ix.Lookup(tc.key)
		if !ok {
			t.Fatalf("Lookup(%v) missed", tc.key)
		}
		if a.Path != tc.want {
			t.Errorf("Lookup(%v) = %s, want %s", tc.key, a.Path, tc.want)
		}
	}
	if _, ok := ix.Lookup(Key{"us", "stage", "a16a", "cache", 0x04ce9e72, "evm"}); ok {
		t.Error("Lookup accepted a raw 32-bit field with the spec byte still on it")
	}
}

// How wrong is a cache-id-only index? Measure it over the whole US pool.
func TestForkRate(t *testing.T) {
	ix := loadIndex(t)
	seen := map[cid]int{}
	for _, a := range ix.Assets() {
		us := false
		for _, k := range ix.KeysFor(a) {
			if k.Region == "us" {
				us = true
				break
			}
		}
		if us {
			seen[cid{a.CacheID, a.Ext}]++
		}
	}
	forked, evmForked, evmTotal := 0, 0, 0
	for c, n := range seen {
		if n > 1 {
			forked++
		}
		if c.ext == "evm" {
			evmTotal++
			if n > 1 {
				evmForked++
			}
		}
	}
	t.Logf("us (id,ext) pairs=%d forked=%d (%.1f%%); evm pairs=%d forked=%d (%.1f%%)",
		len(seen), forked, 100*float64(forked)/float64(len(seen)),
		evmTotal, evmForked, 100*float64(evmForked)/float64(evmTotal))
	if forked == 0 {
		t.Fatal("no forks found -- the index is not keyed on the full tuple")
	}
}

// Every mesh the index can name must actually be on disk, or the viewport will show holes.
func TestMeshTargetsExist(t *testing.T) {
	ix := loadIndex(t)
	for _, ext := range []string{"evm", "kms"} {
		assets := ix.AssetsByExt(ext)
		var missing []string
		for _, a := range assets {
			if _, err := os.Stat(ix.FilePath(a)); err != nil {
				missing = append(missing, a.Path)
			}
		}
		t.Logf("%s: %d targets, %d missing on disk: %v", ext, len(assets), len(missing), missing)
		for _, p := range missing {
			if !danglingUS[p] {
				t.Errorf("%s: %s is named by the flatlist but not shipped", ext, p)
			}
		}
	}
	// The two dangling ones are the d082p01 citizens: the us/ row points at a file the MC never
	// shipped, while the identical eu/ row resolves fine. Prefer the region that exists.
	for _, a := range ix.Resolve(0x0a07596a, "") {
		t.Logf("cit_male_all_def -> %s exists=%v", a.Path, ix.Exists(a))
	}
}

var danglingUS = map[string]bool{
	"kms/us/cit_male_all_def.kms": true,
	"kms/us/cti_female.kms":       true,
}

func TestResolveStage(t *testing.T) {
	ix := loadIndex(t)
	for _, tc := range []struct{ stage, want string }{
		{"d010p01", "evm/us/rai_def_mh_mt_stage_d010p01.evm"}, // a stage that forked Raiden
		{"a45a", "evm/us/rai_def_mh_mt.evm"},                  // a stage that did not
	} {
		got := ix.ResolveStage(tc.stage, 0x04541e89, "")
		if len(got) != 1 {
			t.Fatalf("raiden in %s: %d targets, want 1", tc.stage, len(got))
		}
		if got[0].Path != tc.want {
			t.Errorf("raiden in %s -> %s, want %s", tc.stage, got[0].Path, tc.want)
		}
		t.Logf("raiden in %s -> %s", tc.stage, got[0].Path)
	}
}
