"""Read and write .sdt cutscene streams, byte for byte.

A cutscene is a stream of 16-byte-tagged chunks. Each DEMO chunk is one frame,
and holds a list of 16-byte-tagged packets: camera, objects, motion, effects,
commands, ended by a terminator. Reading then writing an untouched file gives
back the same bytes, so an edit changes only what it touches.

See docs/how-cutscenes-work.md for what the pieces are.
"""
from __future__ import annotations

import pathlib
import struct

from .outputs import checked

# chunk types (low 16 bits of the tag's type word; the high 16 are the channel)
SYSTEM, PCM, DEMO, LIP, CAPTION, END = 0x10, 1, 2, 3, 4, 0xF0

# packet kinds (low 16 bits of the packet type)
TERM_SEQ, TERM_FRAME, COMMAND, CAMERA, OBJECT, MOTION, EFFECT, MOTION_PACK = 0, 1, 2, 3, 4, 5, 6, 7

# Set on every packet kind except the camera, so a skipped frame still runs it.
# Kept as a signed int because packet types are read as signed.
NOSKIP = -0x80000000

# 0x01000000 | the effect's .cnf id
EFFECT_NAMESPACE = 0x01000000
EXEC_PROC = EFFECT_NAMESPACE | 0xFFF01          # DM_ExecProc: run a scenario proc by name hash
SEND_MESSAGE = EFFECT_NAMESPACE | 0x8000        # DM_SendEffectMessage: message a named actor


class Chunk:
    __slots__ = ('ctype', 'ch', 'time', 'option', 'payload')

    def __init__(self, ctype, ch, time, option, payload):
        self.ctype, self.ch, self.time, self.option, self.payload = ctype, ch, time, option, payload


class Packet:
    __slots__ = ('ptype', 'pid', 'reserved', 'body')

    def __init__(self, ptype, pid, reserved, body):
        self.ptype, self.pid, self.reserved, self.body = ptype, pid, reserved, body

    @property
    def kind(self) -> int:
        return self.ptype & 0xFFFF


# ---------------------------------------------------------------- container

def read_sdt(path) -> tuple[list[Chunk], bytes]:
    """All chunks up to and including END, and any trailing bytes (padding)."""
    buf = pathlib.Path(path).read_bytes()
    chunks, off = [], 0
    while off + 16 <= len(buf):
        raw_type, size, time, option = struct.unpack_from('<iiii', buf, off)
        if size < 16 or off + size > len(buf):
            break
        ctype = raw_type & 0xFFFF
        ch = (raw_type >> 16) & 0xFFFF
        chunks.append(Chunk(ctype, ch, time, option, buf[off + 16:off + size]))
        off += size
        if ctype == END:
            break
    return chunks, buf[off:]


def encode_sdt(chunks: list[Chunk], tail: bytes = b'') -> bytes:
    out = bytearray()
    for c in chunks:
        size = 16 + len(c.payload)
        assert size % 16 == 0, f'chunk not 16-byte aligned: {size}'
        out += struct.pack('<iiii', (c.ch << 16) | c.ctype, size, c.time, c.option)
        out += c.payload
    return bytes(out) + tail


def write_sdt(path, chunks: list[Chunk], tail: bytes = b'') -> int:
    data = encode_sdt(chunks, tail)
    checked(path).write_bytes(data)
    return len(data)


def demo_frames(chunks: list[Chunk]) -> list[Chunk]:
    return [c for c in chunks if c.ctype == DEMO]


# ---------------------------------------------------------------- packets

def read_packets(payload: bytes) -> tuple[list[Packet], bytes]:
    """The packets of one frame, stopping after its terminator."""
    pkts, off = [], 0
    while off + 16 <= len(payload):
        ptype, size, pid, reserved = struct.unpack_from('<iiii', payload, off)
        if size < 16 or off + size > len(payload):
            break
        pkts.append(Packet(ptype, pid, reserved, payload[off + 16:off + size]))
        off += size
        if (ptype & 0xFFFF) in (TERM_SEQ, TERM_FRAME):
            break
    return pkts, payload[off:]


def write_packets(pkts: list[Packet], tail: bytes = b'') -> bytes:
    out = bytearray()
    for p in pkts:
        size = 16 + len(p.body)
        assert size % 16 == 0, f'packet not 16-byte aligned: {size}'
        out += struct.pack('<iiii', p.ptype, size, p.pid, p.reserved)
        out += p.body
    return bytes(out) + tail


def free_id(pkts: list[Packet]) -> int:
    """An instance id no other packet in this frame is using.

    Retail issues fresh ids every frame and leaves gaps, so there is normally one
    spare inside the frame's own range.
    """
    used = {p.pid for p in pkts if p.pid}        # motion packs all carry a placeholder 0
    lo, hi = min(used), max(used)
    return next((i for i in range(lo + 1, hi) if i not in used), hi + 1)


def effect_packet(pid: int, effect_id: int, exec_type: int, data: bytes, life: int = 0) -> Packet:
    """A DEMO_EFFECT packet.

    exec_type 0 runs once with argv at data[0]. exec_type 1 makes a lifetime actor
    and puts a [life, 0, 0, 0] preamble before the data. Data is padded to 16.
    """
    data = data + bytes(-len(data) % 16)
    body = struct.pack('<4i', effect_id, exec_type, 0, 0)
    if exec_type == 1:
        body += struct.pack('<4i', life, 0, 0, 0)
    return Packet(NOSKIP | EFFECT, pid, 0, body + data)


def exec_proc_packet(pid: int, proc_hash: int) -> Packet:
    """DM_ExecProc: call a scenario proc by name hash (48 bytes)."""
    return effect_packet(pid, EXEC_PROC, 0, struct.pack('<4i', proc_hash, 0, 0, 0))


# ---------------------------------------------------------------- camera (kind 3)

def get_camera(pkt: Packet):
    """-> (no, screen, z_rotate, chanl, pos[3], trg[3]).

    `screen` is a zoom factor, not an FOV; z_rotate is a float in radians.
    """
    no, screen, z_rot, chanl = struct.unpack_from('<iffi', pkt.body, 0)
    pos = struct.unpack_from('<4f', pkt.body, 0x10)
    trg = struct.unpack_from('<4f', pkt.body, 0x20)
    return no, screen, z_rot, chanl, list(pos[:3]), list(trg[:3])


def set_camera(pkt: Packet, pos=None, trg=None, screen=None):
    body = bytearray(pkt.body)
    if screen is not None:
        struct.pack_into('<f', body, 4, screen)
    if pos is not None:
        struct.pack_into('<3f', body, 0x10, *pos)
    if trg is not None:
        struct.pack_into('<3f', body, 0x20, *trg)
    pkt.body = bytes(body)
