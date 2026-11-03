package caption

import "fmt"

const (
	strCodeBits = 24
	strCodeMask = 1<<strCodeBits - 1
)

// StrCode is GV_StrCode (strcode.c:20-37): rotate left 5, add the byte, mask to 24 bits.
// It is the id space shared by a caption's Name, a MOTION packet's object_id, and every
// other script-visible object name.
//
// The engine hashes EUC-JP bytes. Go's stdlib has no EUC-JP encoder, so this hashes s's
// raw bytes -- identical for ASCII, which is every id an editor is likely to want, and
// wrong for CJK. StrCodeStrict refuses what it would get wrong.
func StrCode(s string) int32 {
	var id uint32
	for i := 0; i < len(s); i++ {
		id = id<<5 | id>>(strCodeBits-5)
		id = (id + uint32(s[i])) & strCodeMask
	}
	if id == 0 {
		return 1
	}
	return int32(id)
}

// StrCodeStrict is StrCode with the EUC-JP shortfall made loud.
func StrCodeStrict(s string) (int32, error) {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return 0, fmt.Errorf("caption: StrCode(%q): hashing non-ASCII needs an EUC-JP encoder", s)
		}
	}
	return StrCode(s), nil
}
