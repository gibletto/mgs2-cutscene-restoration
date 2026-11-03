"""Insert packets into a cutscene at chosen frames.

The commonest edit a restoration makes to a cutscene is a call into its stage's
scenario: effect 0xFFF01, DM_ExecProc, runs one of the procs the scenario
registered on the demo, by name hash. That is how a cutscene switches restored
objects on and off. The same path can insert any other effect packet whole.

A proc can only be called from a cutscene if it is in the `-p` list on the
scenario's `chara demo` line (at most 16). Adding the proc to the scenario is not
enough, which is why `scenario_procs()` checks the compiled scenario first.
"""
from __future__ import annotations

import pathlib
import struct
from typing import Callable

from . import sdt
from .strcode import strcode

BGCL = 0x4247434C


def scenario_procs(path) -> set[int]:
    """The name hashes of every proc in a compiled (final-form) scenario."""
    data = pathlib.Path(path).read_bytes()
    assert data[0:4] == struct.pack('<I', BGCL), 'not a final-form scenario'
    procs, p = set(), 8
    while True:
        strid, off = struct.unpack_from('<II', data, p)
        p += 8
        if strid == 0 and off == 0:
            return procs
        procs.add(strid)


def add_calls(src, dst, calls: list[tuple[int, str]], scenario=None,
              specials: dict[str, Callable[[], bytes]] | None = None):
    """Write `src` to `dst` with packets added.

    calls: (frame, name). A plain name is a scenario proc; `@key` inserts the
    effect packet body built by `specials[key]()`. Each goes last in its frame,
    just before the terminator.
    """
    specials = specials or {}
    known = scenario_procs(scenario) if scenario else None
    chunks, tail = sdt.read_sdt(src)
    frames = sdt.demo_frames(chunks)

    wanted: dict[int, list[str]] = {}
    for frame, name in calls:
        if not 0 <= frame < len(frames):
            raise SystemExit(f'frame {frame} is outside the demo (0..{len(frames) - 1})')
        wanted.setdefault(frame, []).append(name)

    for frame, names in sorted(wanted.items()):
        chunk = frames[frame]
        pkts, ptail = sdt.read_packets(chunk.payload)
        if pkts[-1].kind not in (sdt.TERM_SEQ, sdt.TERM_FRAME):
            raise SystemExit(f'frame {frame} does not end on a terminator')

        for name in names:
            if name.startswith('@'):
                body = specials[name[1:]]()
                pkt = sdt.Packet(sdt.NOSKIP | sdt.EFFECT, sdt.free_id(pkts), 0, body)
                what = f'{name[1:]} effect'
            else:
                code = strcode(name)
                if known is not None and code not in known:
                    raise SystemExit(f'the scenario has no proc {name} (0x{code:06x}) -- '
                                     'a call to it would do nothing')
                pkt = sdt.exec_proc_packet(sdt.free_id(pkts), code)
                what = f'call {name} (0x{code:06x})'
            pkts.insert(len(pkts) - 1, pkt)
            print(f'  frame {frame:5d}: {what}, {16 + len(pkt.body)} bytes, '
                  f'instance id {pkt.pid}')

        chunk.payload = sdt.write_packets(pkts, ptail)

    before = pathlib.Path(src).stat().st_size
    size = sdt.write_sdt(dst, chunks, tail)
    print(f'  -> {dst} ({size} bytes, {size - before:+d})')

    # a demo that will not re-read is a demo that will not play
    again, _ = sdt.read_sdt(dst)
    if len(again) != len(chunks):
        raise SystemExit('the rewritten demo does not parse back to the same chunks')
