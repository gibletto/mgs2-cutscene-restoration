package stage

// StrCode is Konami's name hash (libgv/strcode.c): a 24-bit rotate-left-5-and-add.
//
// A stage asset's cache id IS the StrCode of its name -- verified against every entry of
// d080p07's manifest (amg_def -> 0xa13626, w60a_1 -> 0x799102, ...). So the scenario, which
// names models as text, and the .sdt, which names them by cache id, are the same namespace,
// and we can resolve a placement without any manifest at all.
func StrCode(s string) int32 {
	const bits = 24
	const mask = 1<<bits - 1
	id := uint32(0)
	for i := 0; i < len(s); i++ {
		id = (id<<5 | id>>(bits-5)) & mask
		id = (id + uint32(s[i])) & mask
	}
	if id == 0 {
		id = 1
	}
	return int32(id)
}
