"""The soundtrack: one audio stream grafted into the cutscene's PCM channel.

The extended cutscene's audio is a single foleyed mix -- music and sound effects
together -- in audio/arsenal-crash.wav. It goes in the way the game carries
audio:

  prepare_wav()   resample to 48 kHz stereo, a 20 ms fade at the end so the stream
                  never stops on a click, peak-normalise to 0.97
  encode_wma()    Media Foundation's WMA8 encoder at 192 kbit/s (wma8_encode.ps1):
                  the only encoder found whose 8192-byte superframes the engine plays
  pack_stream()   AMWX header + seek table + packets, the header and table taken from
                  the donor and resized to the packet count
  graft()         the stream replaces the cutscene's PCM, framed like the donor's

A cutscene's audio is one WMA2 stream (48 kHz stereo, 192 kbit/s, 8192-byte packets)
carried in PCM chunks, the first of which holds only the 32-byte AMWX header and the
seek table. The demo clock follows the audio, so the stream has to run the full
length of the extended cutscene -- a starved stream freezes the clock and the
closing white screen never ends.

The donor is t13a1d, the tanker-intro cutscene, whose score is the same track and
builds to the crescendo. Without a soundtrack of your own, graft() lays t13a1d's own
score in as it is (music only, no foley); --stock-audio does that.
"""
import pathlib
import struct
import subprocess
from math import gcd

from mgs2.outputs import checked
from mgs2.sdt import DEMO, END, PCM, read_sdt, write_sdt

PACKET = 8192
RATE = 48000
MAX_S = 75.0          # a safety cap; the seek table sizes itself to the audio
ENCODER = pathlib.Path(__file__).with_name('wma8_encode.ps1')


def prepare_wav(src, dst):
    """48 kHz stereo 16-bit, faded out over 20 ms, peak at most 0.97."""
    import numpy as np                       # only this step needs numpy/scipy/soundfile
    import soundfile as sf
    from scipy.signal import resample_poly
    d, sr = sf.read(str(src), dtype='float32')
    if d.ndim == 1:
        d = np.stack([d, d], -1)
    g = gcd(RATE, sr)
    x = resample_poly(d, RATE // g, sr // g, axis=0)[:int(MAX_S * RATE)]
    fl = int(0.020 * RATE)
    x[-fl:] *= np.linspace(1.0, 0.0, fl)[:, None]
    pk = float(np.abs(x).max())
    if pk > 0.97:
        x *= 0.97 / pk
    sf.write(str(checked(dst)), x, RATE, subtype='PCM_16')
    print(f'  prepared: {len(x) / RATE:.2f} s from {sr} Hz, peak in {pk:.3f}')


def encode_wma(wav, wma):
    subprocess.run(['powershell.exe', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File',
                    str(ENCODER), str(wav), str(checked(wma))], check=True, capture_output=True)


def wma_packets(wma) -> list[bytes]:
    import av                                # PyAV: reads the packets out of the ASF file
    with av.open(str(wma)) as c:
        return [bytes(p) for p in c.demux(c.streams.audio[0]) if p.size]


def _donor_header(donor_sdt):
    chunks, _ = read_sdt(donor_sdt)
    p0 = next(c for c in chunks if c.ctype == PCM).payload
    freq, ba = struct.unpack_from('<i', p0, 12)[0], struct.unpack_from('<i', p0, 24)[0]
    assert (freq, ba) == (48000, PACKET), (freq, ba)
    return p0


def pack_stream(packets: list[bytes], donor_sdt) -> bytes:
    """AMWX header + seek table + stream, from WMA packets, using the donor's
    header and table entries resized to the packet count."""
    p0 = _donor_header(donor_sdt)
    npk = len(packets)
    ndon = struct.unpack_from('<I', p0, 48)[0] >> 16
    if npk > ndon:
        raise SystemExit(f'the stream is longer than the donor table ({npk} packets > {ndon})')
    stream = b''.join(packets)
    tvals = list(struct.unpack('<%dI' % (5 + ndon + 1), p0[32:32 + 4 * (5 + ndon + 1)]))
    tb = bytearray(struct.pack('<%dI' % (5 + npk + 1),
                               *(tvals[:5] + tvals[5:5 + npk] + [tvals[5 + npk - 1] & 0xFFFF])))
    tb += b'\0' * ((-len(tb)) % 16)
    struct.pack_into('<I', tb, 16, npk << 16)
    hdr = bytearray(p0[:32])
    struct.pack_into('<I', hdr, 16, len(stream))
    return bytes(hdr) + bytes(tb) + stream


def graft(demo_sdt, donor_sdt, dst, stream: bytes | None = None, offset_ticks: int = 0):
    """Write `demo_sdt` to `dst` with its PCM replaced by the donor's track
    (or by `stream`, framed like the donor's). 300 ticks = 1 s; a positive
    offset starts the track later."""
    src_chunks, _ = read_sdt(donor_sdt)
    track = [c for c in src_chunks if c.ctype == PCM]

    if stream is not None:
        # chunk 0 must be EXACTLY the header and seek table: the engine starts the
        # stream at chunk 1, so packet bytes in chunk 0 are lost and the
        # bit-reservoir stream desyncs (stutter, then silence)
        npk = struct.unpack_from('<I', stream, 32 + 16)[0] >> 16
        c0len = 32 + ((4 * (5 + npk + 1) + 15) & ~15)
        assert len(stream) == c0len + npk * PACKET, (len(stream), c0len, npk)
        off, kept = c0len, []
        track[0].payload = stream[:c0len]
        kept.append(track[0])
        for c in track[1:]:
            piece = stream[off:off + PACKET]
            if not piece:
                break          # past the stream's end: those chunks are dropped
            c.payload = piece
            off += PACKET
            kept.append(c)
        track = kept
        print(f'  soundtrack: {len(stream)} bytes, {npk} packets, {len(track)} chunks')
    else:
        print(f'  soundtrack: the donor\'s own score, {len(track)} chunks')

    dst_chunks, tail = read_sdt(demo_sdt)
    graft = []
    for c in track:
        t = c.time + offset_ticks
        if t < 0:
            continue
        c.time = t
        graft.append(c)

    # strip the original PCM, then interleave: each PCM chunk goes in front of the
    # first DEMO chunk at or after its time (the original files' own layout)
    body = [c for c in dst_chunks if c.ctype not in (PCM, END)]
    end = next(c for c in dst_chunks if c.ctype == END)
    out, gi = [], 0
    for c in body:
        while gi < len(graft) and c.ctype == DEMO and graft[gi].time <= c.time:
            out.append(graft[gi])
            gi += 1
        out.append(c)
    out.extend(graft[gi:])
    out.append(end)
    n = write_sdt(dst, out, tail)
    print(f'  grafted {len(graft)} chunks, wrote {n} bytes')
