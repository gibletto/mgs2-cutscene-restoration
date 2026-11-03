"""GV_StrCode: the engine's 24-bit name hash.

Every named thing the engine looks up at runtime -- actors, procs, models, cache
entries -- is keyed by this hash of its name. Rotate the accumulator left by five
within 24 bits, add the next byte, repeat; zero is reserved, so it becomes 1.

Scenario names are Japanese and are hashed as EUC-JP bytes, the encoding of the
script source; asset names are plain ASCII.
"""
from __future__ import annotations

BITS = 24
MASK = (1 << BITS) - 1


def strcode(name: str | bytes, encoding: str = 'euc-jp') -> int:
    data = name.encode(encoding) if isinstance(name, str) else name
    v = 0
    for c in data:
        v = ((v << 5) | (v >> (BITS - 5))) & MASK
        v = (v + c) & MASK
    return v or 1


def cache_id(name: str) -> str:
    """The eight-hex-digit form used as a file name in the stage cache lists."""
    return f'{strcode(name, "ascii"):08x}'


if __name__ == '__main__':
    import sys
    for arg in sys.argv[1:]:
        print(f'{arg}\t0x{strcode(arg):06x}\t{strcode(arg)}')
