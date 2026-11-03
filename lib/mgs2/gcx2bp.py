"""Convert a raw `gclk` scenario into the Master Collection's final 'BGCL' form.

`gclk` writes the scenario format the PS2 game used. The HD releases ship a
wrapped form: a 'BGCL' magic, a five-word header (an extra pointer), and a
string-data block. Bluepoint produced it with their `stringtableextract` tool;
this does the same conversion for MGS2 without needing that tool, its .NET
dependencies or its GameData tree. The text block it writes is replaced by the
shipped one afterwards (see residuals.py), so it does not need to reproduce
the tool's text encoding.

raw gclk layout:
    [0]  variable table date : u32
    [4]  proc table          : (strcode, offset) pairs, ended by (0, 0)
    [H]  header              : blocksize, resourcep, stringp, fontp   (16 bytes)
         resource table, string table, font data
    [H+blocksize] proc size u32 + proc body, then data size u32 + data body

BGCL layout:
    'BGCL' u32 (on disk the bytes read "LCGB")
    variable version u32
    proc table, unchanged, with its (0, 0) terminator
    header       : blocksize, resourcep, stringp, fontp, bpstringdatap (20 bytes)
    BPStringData : count u32, one offset per resource, then the strings
    resource table / string table / font data
    proc body section, verbatim
    zero pad to 16 bytes

    gcx2bp.py <raw.gcx> <out.gcx>
"""
import struct, sys

from .outputs import checked

U32 = struct.Struct('<I')


def convert(raw: bytes, font_override: bytes | None = None) -> bytes:
    var_ver = raw[0:4]

    # --- locate end of proc table: pairs of u32 terminated by (0,0)
    p = 4
    while True:
        a, b = U32.unpack_from(raw, p), U32.unpack_from(raw, p + 4)
        p += 8
        if a[0] == 0 and b[0] == 0:
            break
    proc_table = raw[4:p]
    H = p

    blocksize, resourcep, stringp, fontp = struct.unpack_from('<4I', raw, H)
    assert resourcep == 16, f'unexpected raw header (resourcep={resourcep})'

    r_size = stringp - resourcep
    s_size = fontp - stringp
    f_size = blocksize - fontp

    res_tbl = raw[H + resourcep: H + resourcep + r_size]
    str_tbl = raw[H + stringp:   H + stringp + s_size]
    fnt_dat = raw[H + fontp:     H + fontp + f_size]
    if font_override is not None:
        assert len(font_override) == f_size
        fnt_dat = font_override

    proc_body_section = raw[H + blocksize:]          # proc size+body, data size+body

    # --- BPStringData: only "string" type resources (bit31 set) get an offset
    num_strings = r_size // 4
    offsets = [U32.unpack_from(res_tbl, i * 4)[0] for i in range(num_strings)]

    bp = bytearray()
    bp += U32.pack(num_strings)
    # placeholder offsets, patched once string bytes are laid out
    off_slot = {}
    for i, o in enumerate(offsets):
        if o & 0x80000000:
            off_slot[i] = len(bp)
            bp += U32.pack(0)
        else:
            bp += U32.pack(0xFFFFFFFF)

    for i, o in enumerate(offsets):
        if o & 0x80000000:
            so = o & 0x7FFFFFFF
            end = str_tbl.index(b'\0', so)
            s = str_tbl[so:end]                      # already the shipped text
            U32.pack_into(bp, off_slot[i], len(bp))  # offset relative to BPStringData
            bp += s + b'\0'

    bp = bytes(bp)

    # --- new 5-int header
    hdr_size = 20
    bpstringdatap = hdr_size
    resourcep2 = hdr_size + len(bp)
    stringp2 = resourcep2 + r_size
    fontp2 = stringp2 + s_size
    blocksize2 = fontp2 + f_size

    out = bytearray()
    out += struct.pack('<I', 0x4247434C)             # 'BGCL' -> "LCGB" on disk
    out += var_ver
    out += proc_table
    out += struct.pack('<5I', blocksize2, resourcep2, stringp2, fontp2, bpstringdatap)
    out += bp + res_tbl + str_tbl + fnt_dat
    out += proc_body_section

    if len(out) % 16:                                # 16-byte align
        out += b'\0' * (16 - len(out) % 16)
    return bytes(out)


if __name__ == '__main__':
    src, dst = sys.argv[1], sys.argv[2]
    font = None
    if len(sys.argv) > 3:                            # --font=RRGGBBAA hex, to bit-match a shipped file
        font = bytes.fromhex(sys.argv[3])
    data = open(src, 'rb').read()
    out = convert(data, font)
    checked(dst).write_bytes(out)
    print(f'{src} ({len(data)}) -> {dst} ({len(out)})')
