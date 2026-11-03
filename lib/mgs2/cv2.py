"""Read and write .cv2 vertex-animation keyframes.

Round-trips all 159 of the game's own files byte for byte.
"""
import struct, sys, pathlib

from .outputs import checked

CV2_ID = 1160797
RECORD = 0x60
HEADER = 16

# (attribute, count field, element width) in the order the record declares them
FIELDS = (('verts', 'n_verts', 16), ('verts_index', 'n_verts_index', 2),
          ('norms', 'n_norms', 16), ('norms_index', 'n_norms_index', 2),
          ('uvs', 'n_uvs', 16), ('uvs_index', 'n_uvs_index', 2))
OFFSETS = {'verts': (0x00, 0x08), 'verts_index': (0x04, 0x10),
           'norms': (0x18, 0x20), 'norms_index': (0x1c, 0x28),
           'uvs': (0x30, 0x38), 'uvs_index': (0x34, 0x40)}


class Key:
    __slots__ = ('verts', 'verts_index', 'norms', 'norms_index', 'uvs', 'uvs_index',
                 'usr', 'tail', 'at', 'declared')

    def __init__(self):
        for name, _, _ in FIELDS:
            setattr(self, name, [])
        self.usr, self.tail, self.at = (0, 0), bytes(8), {}
        # A record can name a count with a null pointer -- the flag keys claim 117
        # UVs and point nowhere. Nothing reads it, but it is preserved.
        self.declared = {}

    def count(self, name):
        return len(getattr(self, name)) or self.declared.get(name, 0)

    @property
    def n_verts(self):
        return len(self.verts)

    def __repr__(self):
        return (f'<Key {len(self.verts)} verts / {len(self.verts_index)} indices, '
                f'{len(self.norms)} norms / {len(self.norms_index)}>')


class CV2:
    def __init__(self, ident, flag, pad1, keys, raw=b''):
        self.id, self.flag, self.pad1, self.keys, self.raw = ident, flag, pad1, keys, raw


def read(path) -> CV2:
    data = pathlib.Path(path).read_bytes()
    ident, n_models, flag, pad1 = struct.unpack_from('<4i', data, 0)
    if ident != CV2_ID:
        raise ValueError(f'not a .cv2 (id {ident}, expected {CV2_ID})')

    keys = []
    for i in range(n_models):
        o = HEADER + i * RECORD
        k = Key()
        for name, _, width in FIELDS:
            n_off, p_off = OFFSETS[name]
            n = struct.unpack_from('<i', data, o + n_off)[0]
            p = struct.unpack_from('<Q', data, o + p_off)[0]
            k.at[name], k.declared[name] = p, n
            if not p or not n:
                continue
            if width == 16:
                setattr(k, name,
                        [struct.unpack_from('<4f', data, p + j * 16) for j in range(n)])
            else:
                setattr(k, name, list(struct.unpack_from(f'<{n}h', data, p)))
        k.usr = struct.unpack_from('<QQ', data, o + 0x48)
        k.tail = data[o + 0x58:o + RECORD]
        keys.append(k)

    return CV2(ident, flag, pad1, keys, data)


def _pack_block(items, width) -> bytes:
    if width == 16:
        return b''.join(struct.pack('<4f', *v) for v in items)
    return struct.pack(f'<{len(items)}h', *items)


def _unchanged_shape(cv2: CV2) -> bool:
    """True if every block can go back exactly where it came from."""
    if not cv2.raw:
        return False
    n = struct.unpack_from('<i', cv2.raw, 4)[0]
    if n != len(cv2.keys):
        return False
    for i, k in enumerate(cv2.keys):
        o = HEADER + i * RECORD
        for name, _, _ in FIELDS:
            n_off, p_off = OFFSETS[name]
            was = struct.unpack_from('<i', cv2.raw, o + n_off)[0]
            ptr = struct.unpack_from('<Q', cv2.raw, o + p_off)[0]
            now = k.count(name)
            if now != was or (len(getattr(k, name)) and not ptr) or k.at.get(name) != ptr:
                return False
    return True


def write(path, cv2: CV2):
    """Write the file back.

    If nothing changed shape, the original bytes are patched in place, so an
    untouched read/write is byte for byte identical and an edit disturbs nothing
    but the numbers it changed. Otherwise the file is laid out afresh, in the
    order Konami's own files use.
    """
    if _unchanged_shape(cv2):
        out = bytearray(cv2.raw)
        for i, k in enumerate(cv2.keys):
            for name, _, width in FIELDS:
                items = getattr(k, name)
                if items:
                    block = _pack_block(items, width)
                    at = k.at[name]
                    out[at:at + len(block)] = block
            o = HEADER + i * RECORD
            struct.pack_into('<QQ', out, o + 0x48, *k.usr)
            out[o + 0x58:o + RECORD] = k.tail
        struct.pack_into('<4i', out, 0, cv2.id, len(cv2.keys), cv2.flag, cv2.pad1)
        checked(path).write_bytes(bytes(out))
        return len(out)

    n = len(cv2.keys)
    out = bytearray(struct.pack('<4i', cv2.id, n, cv2.flag, cv2.pad1))
    out += bytes(n * RECORD)

    # Konami groups by field across all keys, not by key: all verts, then all
    # normals, then the index arrays.
    for name, _, width in FIELDS:
        for i, k in enumerate(cv2.keys):
            items = getattr(k, name)
            n_off, p_off = OFFSETS[name]
            o = HEADER + i * RECORD
            struct.pack_into('<i', out, o + n_off, k.count(name))
            struct.pack_into('<Q', out, o + p_off, len(out) if items else 0)
            out += _pack_block(items, width)

    for i, k in enumerate(cv2.keys):
        o = HEADER + i * RECORD
        struct.pack_into('<QQ', out, o + 0x48, *k.usr)
        out[o + 0x58:o + RECORD] = k.tail

    checked(path).write_bytes(bytes(out))
    return len(out)


def check_for_vanime(cv2: CV2):
    """The rules put_vanime.c and SimpleVertexAnimation impose. Raises if broken."""
    n0 = len(cv2.keys[0].verts)
    for i, k in enumerate(cv2.keys):
        if len(k.verts) != n0:
            raise SystemExit(f'key {i} has {len(k.verts)} vertices, key 0 has {n0} -- '
                             'put_vanime.c refuses to load this')
        if len(k.norms) < len(cv2.keys[0].norms):
            raise SystemExit(f'key {i} has {len(k.norms)} normals, fewer than key 0\'s '
                             f'{len(cv2.keys[0].norms)} -- the blend would read past the end')


def describe(path):
    cv2 = read(path)
    print(f'{pathlib.Path(path).name}: {len(cv2.keys)} keys, flag {cv2.flag}')
    for i, k in enumerate(cv2.keys):
        print(f'  [{i}] {k!r}')
        if k.verts:
            for axis, j in (('x', 0), ('y', 1), ('z', 2)):
                vals = [v[j] for v in k.verts]
                print(f'       {axis} {min(vals):9.1f} .. {max(vals):9.1f}', end='')
            print()


def roundtrip(root):
    import tempfile
    files = sorted(pathlib.Path(root).rglob('*.cv2'))
    tmp = pathlib.Path(tempfile.mkdtemp()) / 'rt.cv2'
    ok, bad = 0, []
    for f in files:
        original = f.read_bytes()
        try:
            write(tmp, read(f))
            if tmp.read_bytes() == original:
                ok += 1
            else:
                bad.append((f.name, 'differs'))
        except Exception as e:
            bad.append((f.name, str(e)))
    for name, why in bad:
        print(f'  {why:10} {name}')
    print(f'{ok}/{len(files)} byte-identical after a read/write round trip')
    return not bad


if __name__ == '__main__':
    if len(sys.argv) > 2 and sys.argv[1] == '--roundtrip':
        sys.exit(0 if roundtrip(sys.argv[2]) else 1)
    elif len(sys.argv) > 1:
        describe(sys.argv[1])
    else:
        raise SystemExit(__doc__)
