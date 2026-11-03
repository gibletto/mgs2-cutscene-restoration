"""Decompress and repack MGS2 lzsh streams.

Written for interoperability: behaviour was matched against the game's own
decoder (`system/libutl/utl_lzh.c` in the source tree) so the game reads what
this writes. The packer only emits literals, which the format permits.

LZSS over a 16 KB window, Huffman coded, with the code lengths themselves gamma
coded. The .anm vertex animations are packed with it. A file starts with the
decompressed size as a little-endian int, then the bitstream.

    lzsh.py <file.anm> [out.bin]
"""
import struct, sys, pathlib

from .outputs import checked

THRESHOLD = 3
F = 256 + 2
DICBIT = 14
DICSIZ = 1 << DICBIT
DICMASK = DICSIZ - 1
D256 = 256

CHILD_N = F - THRESHOLD + 1 + D256      # 512 literal/length symbols
PARENT_N = 16                           # match-distance buckets
HEAD_N = 16                             # code lengths for the table above
HUFNIL = -1


class Bits:
    """MSB-first bit reader, byte at a time, exactly as the C does it."""

    def __init__(self, data: bytes, at: int = 0):
        self.data, self.at = data, at
        self.buf = 0
        self.count = 0

    def bit(self) -> int:
        self.count -= 1
        if self.count >= 0:
            return (self.buf >> self.count) & 1
        self.count = 7
        self.buf = self.data[self.at]
        self.at += 1
        return (self.buf >> 7) & 1

    def bits(self, n: int) -> int:
        x = 0
        while n > self.count:
            n -= self.count
            x |= (self.buf & ((1 << self.count) - 1)) << n
            self.buf = self.data[self.at]
            self.at += 1
            self.count = 8
        self.count -= n
        return x | ((self.buf >> self.count) & ((1 << n) - 1))

    def gamma(self) -> int:
        i = 0
        while not self.bit():
            i += 1
        code = 1
        while i > 0:
            i -= 1
            code = (code << 1) + self.bit()
        return code


class Huff:
    def __init__(self, tableno: int):
        self.tableno = tableno
        self.bits = [0] * tableno                  # code length per symbol
        self.code = [0] * tableno
        self.child0 = [HUFNIL] * (tableno * 2)
        self.child1 = [HUFNIL] * (tableno * 2)

    def make_code(self, len_count):
        start = [0] * 18
        for i in range(1, 17):
            start[i + 1] = (start[i] + len_count[i]) << 1
        for i in range(18):
            for j in range(self.tableno):
                if self.bits[j] == i:
                    self.code[j] = start[i]
                    start[i] += 1

    def restore_tree(self) -> int:
        """Rebuild the decode tree; returns its root. Symbols are the leaves below `tableno`."""
        root = self.tableno
        free = self.tableno + 1
        self.child0[root] = self.child1[root] = HUFNIL
        self.child0[free] = self.child1[free] = HUFNIL

        for i in range(self.tableno):
            at = self.tableno
            n = self.bits[i]
            for j in range(n):
                one = self.code[i] & (1 << (n - j - 1))
                child = self.child1 if one else self.child0
                if j + 1 == n:
                    child[at] = i
                    break
                if child[at] != HUFNIL:
                    at = child[at]
                else:
                    child[at] = free
                    self.child0[free] = self.child1[free] = HUFNIL
                    at = free
                    free += 1
        return root

    def decode(self, bits: Bits, root: int) -> int:
        node = root
        while True:
            node = self.child1[node] if bits.bit() else self.child0[node]
            if node < self.tableno:
                return node


def _read_lengths(bits: Bits, huff: Huff, len_count):
    """Code lengths as gamma-coded xor deltas -- the form used for h_head and h_ptr."""
    pc = 0
    for i in range(huff.tableno):
        pc = (bits.gamma() ^ pc) if bits.bit() else pc
        if pc > 16:
            raise ValueError('code length over 16')
        huff.bits[i] = pc
        len_count[pc] += 1


def _input_counts(bits: Bits, head: Huff, char: Huff, ptr: Huff):
    """Read the three code-length tables that open every block."""
    len_count = [0] * 17
    _read_lengths(bits, head, len_count)
    head.make_code(len_count)
    root = head.restore_tree()

    # the literal/length lengths come as runs: alternately "decoded" and "all zero"
    xorbuf = [0] * char.tableno
    flag = bits.bit()
    i = 0
    while i < char.tableno:
        n = bits.gamma()
        if flag:
            for j in range(n):
                xorbuf[i + j] = head.decode(bits, root)
        flag ^= 1
        i += n

    len_count = [0] * 17
    pc = 0
    for i in range(char.tableno):
        pc = xorbuf[i] ^ pc
        if pc > 16:
            raise ValueError('code length over 16')
        char.bits[i] = pc
        len_count[pc] += 1
    char.make_code(len_count)

    len_count = [0] * 17
    _read_lengths(bits, ptr, len_count)
    ptr.make_code(len_count)


def decompress(data: bytes) -> bytes:
    """`data` is the whole file, size header and all."""
    size = struct.unpack_from('<i', data, 0)[0]
    bits = Bits(data, 4)
    head, char, ptr = Huff(HEAD_N), Huff(CHILD_N), Huff(PARENT_N)

    text = bytearray(DICSIZ)
    out = bytearray()
    pos = 0

    while True:
        count = bits.bits(8) | (bits.bits(8) << 8)
        if not count:
            break
        _input_counts(bits, head, char, ptr)
        c_root = char.restore_tree()
        p_root = ptr.restore_tree()

        for _ in range(count):
            node = char.decode(bits, c_root)
            if node < D256:
                text[pos] = node
                out.append(node)
                pos = (pos + 1) & DICMASK
            else:
                i = ptr.decode(bits, p_root)
                if i:
                    i = (1 << (i - 1)) + bits.bits(i - 1)
                i = (pos - i - 1) & DICMASK
                for _ in range(node - D256 + THRESHOLD):
                    b = text[i]
                    out.append(b)
                    text[pos] = b
                    pos = (pos + 1) & DICMASK
                    i = (i + 1) & DICMASK

    if len(out) != size:
        raise ValueError(f'decompressed {len(out)} bytes, header says {size}')
    return bytes(out)


# ---------------------------------------------------------------- encoding

class BitWriter:
    """MSB-first, mirroring Bits above."""

    def __init__(self):
        self.out = bytearray()
        self.buf = 0
        self.n = 0

    def bit(self, b: int):
        self.buf = (self.buf << 1) | (b & 1)
        self.n += 1
        if self.n == 8:
            self.out.append(self.buf)
            self.buf, self.n = 0, 0

    def bits(self, value: int, count: int):
        for i in range(count - 1, -1, -1):
            self.bit((value >> i) & 1)

    def gamma(self, n: int):
        """Elias gamma, the shape get_gamma reads: zeros, a one, then the low bits."""
        width = n.bit_length() - 1
        for _ in range(width):
            self.bit(0)
        self.bit(1)
        self.bits(n & ((1 << width) - 1), width)

    def flush(self) -> bytes:
        while self.n:
            self.bit(0)
        return bytes(self.out)


def _lengths(w: BitWriter, lengths):
    """Code lengths as the gamma/xor deltas _read_lengths expects."""
    pc = 0
    for n in lengths:
        if n == pc:
            w.bit(0)
        else:
            w.bit(1)
            w.gamma(n ^ pc)
        pc = n


def _tokenise(data: bytes):
    """Greedy LZSS: the most recent occurrence of the next three bytes, extended."""
    tokens, table, pos, n = [], {}, 0, len(data)
    while pos < n:
        length = 0
        if pos + THRESHOLD <= n:
            key = data[pos:pos + THRESHOLD]
            at = table.get(key)
            if at is not None and 0 < pos - at <= DICSIZ:
                limit = min(F, n - pos)
                length = THRESHOLD
                while length < limit and data[at + length] == data[pos + length]:
                    length += 1
        if length >= THRESHOLD:
            tokens.append((length + D256 - THRESHOLD, pos - at))
            step = length
        else:
            tokens.append((data[pos], 0))
            step = 1
        for k in range(pos, min(pos + step, n - THRESHOLD + 1)):
            table[data[k:k + THRESHOLD]] = k
        pos += step
    return tokens


def compress(data: bytes) -> bytes:
    """Produce a stream the game's decoder accepts.

    Every symbol is given a fixed-width code -- nine bits for the literal/length
    table, four for the distance table -- which makes the canonical assignment come
    out as code == symbol, so a symbol is written by writing its own value. That
    costs about an eighth over an ideal Huffman code and saves having to build one;
    the matches are where the size actually goes, and a frame identical to the one
    before collapses to a handful of bytes.
    """
    w = BitWriter()
    tokens = _tokenise(data)

    BLOCK = 0xFFFF
    for start in range(0, len(tokens), BLOCK):
        block = tokens[start:start + BLOCK]
        w.bits(len(block) & 0xFF, 8)
        w.bits((len(block) >> 8) & 0xFF, 8)

        # h_head: two values to spell out, 9 for the literal/length lengths and 4
        # for the distance ones, each on its own single-bit code
        head = [0] * HEAD_N
        head[9] = head[4] = 1
        _lengths(w, head)

        # h_char: nine bits for all 512 symbols. As xor deltas that is a single 9
        # followed by 511 zeroes, so one coded run of one and one zero run of 511.
        w.bit(1)
        w.gamma(1)
        w.bit(1)                      # h_head's code for 9 -- see _head_code below
        w.gamma(CHILD_N - 1)

        # h_ptr: four bits for all 16
        _lengths(w, [4] * PARENT_N)

        for symbol, distance in block:
            w.bits(symbol, 9)
            if symbol >= D256:
                offset = distance - 1
                width = offset.bit_length()
                w.bits(width, 4)
                if width:
                    w.bits(offset - (1 << (width - 1)), width - 1)

    w.bits(0, 8)
    w.bits(0, 8)
    return struct.pack('<i', len(data)) + w.flush()


if __name__ == '__main__':
    if len(sys.argv) < 2:
        raise SystemExit(__doc__)
    src = pathlib.Path(sys.argv[1])
    out = decompress(src.read_bytes())
    print(f'{src.name}: {src.stat().st_size} -> {len(out)} bytes')
    if len(sys.argv) > 2:
        checked(sys.argv[2]).write_bytes(out)
        print(f'-> {sys.argv[2]}')
