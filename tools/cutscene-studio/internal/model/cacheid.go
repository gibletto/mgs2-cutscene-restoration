package model

// A DEMO_OBJECT cache_id is (spec << 24) | root_id, where spec = ext[0] - 'a' and root_id is a
// 24-bit GV_StrCode. Flatlist paths carry the bare 24 bits, so the field MUST be split before
// any lookup: keying the raw 32-bit value against a path-derived index misses 100% of the time.

const specMask = 0xFFFFFF

// SplitCacheID returns the 24-bit id and the extension's first letter ('e' = evm, 'k' = kms).
// A zero spec byte (a bare id, e.g. one taken from a flatlist path) yields spec 0.
func SplitCacheID(field int32) (id int32, spec byte) {
	id = field & specMask
	if s := byte(uint32(field) >> 24); s < 26 {
		spec = 'a' + s
	}
	return id, spec
}

// MakeCacheID is GV_CacheID: fold the extension's first letter back into the top byte.
func MakeCacheID(id int32, ext string) int32 {
	if ext == "" {
		return id & specMask
	}
	spec := int32(normalizeExt(ext)[0] - 'a')
	return (spec << 24) | (id & specMask)
}

// StrCode is GV_StrCode: a 24-bit rotate-left-5 + add over the name's bytes, with 0 remapped to 1.
// Konami hashed EUC-JP bytes; for ASCII names (every model name) that is the same thing.
// Use this to MINT an id for a new asset -- never to recover an existing one, since only ~64% of
// shipped ids actually hash from their filename (lowercasing and content dedup broke the rest).
func StrCode(s string) int32 { return StrCodeBytes([]byte(s)) }

func StrCodeBytes(b []byte) int32 {
	var h uint32
	for _, c := range b {
		h = ((h << 5) | (h >> 19)) & specMask
		h = (h + uint32(c)) & specMask
	}
	if h == 0 {
		return 1
	}
	return int32(h)
}
