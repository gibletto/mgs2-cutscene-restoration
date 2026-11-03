"""Compare two scenarios and describe the difference in the scenario's own terms.

A byte diff of a recompiled `.gcx` is useless -- adding one call shifts every
offset after it. This reads both files into their sections instead and reports
what actually changed: which procs exist, which strings the scenario references,
and how much bytecode each section gained.

    python gcxdiff.py <shipped.gcx> <built.gcx>
"""
import struct, sys

U32 = struct.Struct('<I')


def parse(data: bytes) -> dict:
    assert data[0:4] == U32.pack(0x4247434C), 'not a final-form scenario'

    p = 8
    procs = []
    while True:
        strid, off = U32.unpack_from(data, p)[0], U32.unpack_from(data, p + 4)[0]
        p += 8
        if strid == 0 and off == 0:
            break
        procs.append((strid, off))
    H = p

    blocksize, resourcep, stringp, fontp, bpstringdatap = struct.unpack_from('<5I', data, H)
    res = [U32.unpack_from(data, H + resourcep + i * 4)[0]
           for i in range((stringp - resourcep) // 4)]
    strtab = data[H + stringp:H + fontp]

    strings = []
    for r in res:
        if r & 0x80000000:
            o = r & 0x7FFFFFFF
            strings.append(strtab[o:strtab.index(b'\0', o)])

    b = H + blocksize
    bodies = []
    for _ in range(2):
        n = U32.unpack_from(data, b)[0]
        bodies.append(n)
        b += 4 + n

    return {'procs': procs, 'resources': res, 'strings': strings,
            'proc_body': bodies[0], 'data_body': bodies[1],
            'strtab': len(strtab), 'size': len(data)}


def show(name: str, a, b):
    if a != b:
        print(f'  {name:14} {a} -> {b}  ({b - a:+d})')


def main(shipped_path, built_path):
    a, b = parse(open(shipped_path, 'rb').read()), parse(open(built_path, 'rb').read())

    print('sections:')
    show('file', a['size'], b['size'])
    show('procs', len(a['procs']), len(b['procs']))
    show('resources', len(a['resources']), len(b['resources']))
    show('string table', a['strtab'], b['strtab'])
    show('proc body', a['proc_body'], b['proc_body'])
    show('data body', a['data_body'], b['data_body'])

    pa, pb = {s for s, _ in a['procs']}, {s for s, _ in b['procs']}
    for tag, ids in (('proc added', pb - pa), ('proc removed', pa - pb)):
        for s in sorted(ids):
            print(f'  {tag}: strcode 0x{s:06x}')

    sa, sb = a['strings'], b['strings']
    for tag, items in (('string added', [s for s in sb if s not in sa]),
                       ('string removed', [s for s in sa if s not in sb])):
        for s in items:
            print(f'  {tag}: {s.decode("euc-jp", "replace")}')

    moved = sum(1 for (sa_, oa), (sb_, ob) in zip(a['procs'], b['procs'])
                if sa_ == sb_ and oa != ob)
    if moved:
        print(f'  {moved} of {len(a["procs"])} procs shifted (expected: an edit moves '
              'everything after it)')

    if a == b:
        print('  identical')


if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2])
