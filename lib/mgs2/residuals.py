"""Learn and re-apply the differences between our rebuild and the shipped scenario.

Four things differ, none of them ours: stale symbol values in the source tree's
.lst files, uninitialised heap garbage in the font field, a few short
region-variant table values, and the text blob (which gclk emits in the engine's
own character codes rather than UTF-8).

All four are learned by compiling unmodified and diffing, which doubles as the
proof the toolchain is faithful: learn refuses unless it reproduces the shipped
file exactly.

    residuals.py learn <unmodified build> <shipped> <scn dir> <out.json>
    residuals.py apply <build> <residuals.json> <out>
"""
import json, pathlib, re, struct, sys

from .outputs import checked

U32 = struct.Struct('<I')
SYMBOL = re.compile(rb'^\s*0x([0-9A-Fa-f]{8})\s+(\S+)')


def block_header(data: bytes) -> tuple[int, tuple[int, int, int, int, int]]:
    """Offset of the five-word block header, and its words."""
    p = 8                                      # past 'BGCL' and the variable version
    while True:
        a, b = U32.unpack_from(data, p)[0], U32.unpack_from(data, p + 4)[0]
        p += 8
        if a == 0 and b == 0:                  # the proc table ends on a null pair
            break
    return p, struct.unpack_from('<5I', data, p)


def bp_strings(data: bytes) -> tuple[int, int]:
    """Offset and size of the BPStringData blob (count, offsets, UTF-8 text)."""
    p, (_blocksize, resourcep, _stringp, _fontp, bpstringdatap) = block_header(data)
    assert bpstringdatap == 20, f'not a final-form scenario ({bpstringdatap=})'
    return p + bpstringdatap, resourcep - bpstringdatap


def swap_bp_strings(data: bytes, blob: bytes) -> bytes:
    """Swap in the shipped text blob and shift everything after it along.

    Our edit never adds a string and the table indexing it comes out identical,
    so Konami's blob drops straight in -- no need to reimplement CharacterMap.
    """
    off, size = bp_strings(data)
    if size == len(blob):
        return data[:off] + blob + data[off + size:]

    p, (blocksize, resourcep, stringp, fontp, bpstringdatap) = block_header(data)
    delta = len(blob) - size
    d = bytearray(data[:off] + blob + data[off + size:])
    struct.pack_into('<5I', d, p, blocksize + delta, resourcep + delta,
                     stringp + delta, fontp + delta, bpstringdatap)

    # strip the existing 16-byte pad before re-padding, or the file overshoots
    pad = 0
    while pad < 15 and d and d[-1 - pad] == 0:
        pad += 1
    del d[len(d) - pad:]
    while len(d) % 16:
        d.append(0)
    return bytes(d)


def font_block(data: bytes) -> tuple[int, int]:
    """Offset and size of the font data. It moves when the proc table changes."""
    p, (blocksize, _r, _s, fontp, _bp) = block_header(data)
    return p + fontp, blocksize - fontp


def symbol_tables(scn: pathlib.Path) -> list[pathlib.Path]:
    """The tables gclconv was given first, then any others.

    Several hold the same symbols, so order decides which name gets reported.
    """
    named = []
    for args in scn.glob('_bp_gclc_args_*.txt'):
        words = args.read_text(encoding='ascii', errors='replace').split()
        named += [w for f, w in zip(words, words[1:]) if f == '-t']
    ordered = [scn / n for n in dict.fromkeys(named) if (scn / n).exists()]
    return ordered + [p for p in sorted(scn.glob('*.lst')) if p not in ordered]


def symbol_values(scn: pathlib.Path) -> dict[int, str]:
    """Every `0xVALUE name` entry in the source tree's symbol tables."""
    out = {}
    for lst in symbol_tables(scn):
        for line in lst.read_bytes().split(b'\n'):
            m = SYMBOL.match(line)
            if m:
                out.setdefault(int(m.group(1), 16),
                               f'{lst.name}:{m.group(2).decode("ascii", "replace")}')
    return out


def apply_patch(data: bytes, patch: dict) -> bytes:
    if patch.get('bpstrings'):
        data = swap_bp_strings(data, bytes.fromhex(patch['bpstrings']))
    d = bytearray(data)

    if patch.get('font'):
        off, size = font_block(d)
        want = bytes.fromhex(patch['font'])
        if size != len(want):
            raise SystemExit(f'font block is {size} bytes, residual has {len(want)}')
        if bytes(d[off:off + size]) != bytes(size):
            raise SystemExit(f'font block at 0x{off:x} is not zeroed; refusing to overwrite it')
        d[off:off + size] = want

    for sub in patch.get('subs', []):
        frm, to = U32.pack(sub['from']), U32.pack(sub['to'])
        if d.count(frm) != sub['count']:
            raise SystemExit(f'{sub["symbol"]}: expected {sub["count"]} use(s) of '
                             f'0x{sub["from"]:08x}, found {d.count(frm)}')
        i = 0
        while (j := d.find(frm, i)) >= 0:
            d[j:j + 4] = to
            i = j + 4

    for run in patch.get('runs', []):
        before, after = bytes.fromhex(run['before']), bytes.fromhex(run['after'])
        frm, to = bytes.fromhex(run['from']), bytes.fromhex(run['to'])
        pattern = before + frm + after
        n = d.count(pattern)
        if n != 1:
            raise SystemExit(f'region value {run["from"]} -> {run["to"]}: its context '
                             f'matches {n} times, not once -- refusing to guess')
        j = d.find(pattern) + len(before)
        d[j:j + len(frm)] = to

    return bytes(d)


def context_run(work: bytes, shipped: bytes, a: int, b: int) -> dict:
    """Key a differing run on the bytes either side, widening until it is unique."""
    for pad in range(8, 41, 4):
        before, after = bytes(work[max(0, a - pad):a]), bytes(work[b:b + pad])
        if work.count(before + bytes(work[a:b]) + after) == 1:
            return {'from': bytes(work[a:b]).hex(), 'to': bytes(shipped[a:b]).hex(),
                    'before': before.hex(), 'after': after.hex(), 'at': a}
    raise SystemExit(f'cannot key the run at 0x{a:x} uniquely on its surroundings')


def learn(unmod: bytes, shipped: bytes, scn: pathlib.Path) -> dict:
    patch = {'font': None, 'subs': [], 'runs': [], 'bpstrings': None}

    # the text blob first: it is the one residual that changes the file's length
    off, size = bp_strings(unmod)
    s_off, s_size = bp_strings(shipped)
    if unmod[off:off + size] != shipped[s_off:s_off + s_size]:
        patch['bpstrings'] = shipped[s_off:s_off + s_size].hex()
        unmod = swap_bp_strings(unmod, shipped[s_off:s_off + s_size])

    if len(unmod) != len(shipped):
        raise SystemExit(f'build is {len(unmod)} bytes, shipped is {len(shipped)} -- '
                         'that is a real difference, not a residual')

    off, size = font_block(unmod)
    if unmod[off:off + size] != shipped[off:off + size]:
        if unmod[off:off + size] != bytes(size):
            raise SystemExit(f'font block at 0x{off:x} differs but is not zeroed')
        patch['font'] = shipped[off:off + size].hex()

    symbols = symbol_values(scn)
    work = bytearray(apply_patch(unmod, dict(patch, bpstrings=None)))
    seen = set()

    for i in range(len(work)):
        if work[i] == shipped[i]:
            continue
        # the value is not aligned in the file, so try every window covering the byte
        for w in range(max(0, i - 3), i + 1):
            mine, theirs = U32.unpack_from(work, w)[0], U32.unpack_from(shipped, w)[0]
            if mine == theirs or mine not in symbols or mine in seen:
                continue
            sub = {'from': mine, 'to': theirs, 'symbol': symbols[mine],
                   'count': work.count(U32.pack(mine))}
            patch['subs'].append(sub)
            seen.add(mine)
            work = bytearray(apply_patch(bytes(work), {'subs': [sub]}))
            break
        else:
            # not a symbol: take the whole differing run and key it on its context
            j = i
            while j < len(work) and work[j] != shipped[j]:
                j += 1
            run = context_run(bytes(work), shipped, i, j)
            patch['runs'].append(run)
            work = bytearray(apply_patch(bytes(work), {'runs': [run]}))

    if bytes(work) != shipped:
        raise SystemExit('learned residuals do not reproduce the shipped file')
    return patch


def main(argv):
    if argv[1] == 'learn':
        build, shipped, scn, out = argv[2:6]
        patch = learn(pathlib.Path(build).read_bytes(),
                      pathlib.Path(shipped).read_bytes(),
                      pathlib.Path(scn))
        checked(out).write_text(json.dumps(patch, indent=2) + '\n')
        print(f'residuals: font {"patched" if patch["font"] else "already matched"}, '
              f'{len(patch["subs"])} stale symbol(s), {len(patch["runs"])} region value(s)'
              + (f', {len(patch["bpstrings"]) // 2} B of text' if patch['bpstrings'] else ''))
        for s in patch['subs']:
            print(f'  {s["symbol"]}: 0x{s["from"]:08x} -> 0x{s["to"]:08x} ({s["count"]}x)')
        for r in patch['runs']:
            print(f'  region value @0x{r["at"]:x}: {r["from"]} -> {r["to"]} '
                  f'(keyed on {len(r["before"]) // 2} bytes either side)')
        print(f'-> {out}  (unmodified build reproduces the shipped scenario exactly)')

    elif argv[1] == 'apply':
        build, res, out = argv[2:5]
        patch = json.loads(pathlib.Path(res).read_text())
        checked(out).write_bytes(apply_patch(pathlib.Path(build).read_bytes(), patch))
        print(f'{build} -> {out}: font + {len(patch["subs"])} symbol + '
              f'{len(patch.get("runs", []))} region substitution(s)')

    else:
        raise SystemExit(__doc__)


if __name__ == '__main__':
    main(sys.argv)
