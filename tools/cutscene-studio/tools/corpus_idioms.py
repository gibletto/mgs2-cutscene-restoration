#!/usr/bin/env python3
"""corpus_idioms.py -- second-pass measurements for the tricky invariants.
Answers the questions corpus_scan.py raised: packet-id rule, MOTION_PACK data_offset rule,
chunk type hi-16 bits, chunk interleave order, PCM/SYSTEM option fields, per-demo table.

Writes the per-demo table to <studio>/data/demos.json (gitignored: it is measured from the
game's files).

Usage:
  python3 corpus_idioms.py --root <game dir> [--out <path>]   # --root defaults to $MGS2_DIR
"""
import argparse, os, struct, sys, json
from collections import Counter, defaultdict

_STUDIO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_ap = argparse.ArgumentParser()
_ap.add_argument("--root", default=os.environ.get("MGS2_DIR"),
                 help="game install (default $MGS2_DIR)")
_ap.add_argument("--out", default=os.path.join(os.environ.get("MGS2CS_DATA_DIR") or
                                               os.path.join(_STUDIO, "data"), "demos.json"))
_args = _ap.parse_args()
if not _args.root:
    _ap.error("need --root <game dir> (or set MGS2_DIR)")
ROOT = _args.root
CH = {0: "FREE", 1: "PCM", 2: "DEMO", 3: "LIP", 4: "CAPTION", 5: "MOTION",
      6: "CODEC_CAP", 0x0E: "MPEG2", 0x0F: "IPU", 0x10: "SYSTEM", 0x20: "MOVIE", 0xF0: "END"}
PK = {0: "TERM_SEQ", 1: "TERM_FRAME", 2: "COMMAND", 3: "FRAME", 4: "OBJECT",
      5: "MOTION", 6: "EFFECT", 7: "MOTION_PACK"}
u32 = lambda v: v & 0xFFFFFFFF


def demos():
    for dp, _, fns in os.walk(ROOT):
        for fn in sorted(fns):
            if fn.endswith(".sdt"):
                yield os.path.join(dp, fn)


hi_by_ctype = defaultdict(Counter)
opt_by_ctype = defaultdict(Counter)
id_by_kind = defaultdict(Counter)          # kind -> "counter"/"stable"
dataoff_rule = Counter()
wrap_evidence = Counter()
obj_flag_by_pid = Counter()
first_chunk = Counter()
order_viol = []
per_demo = []
mp_off_align = Counter()
eff_id_reuse = Counter()
term_pairs = Counter()
demo_between_pcm = Counter()

for path in demos():
    buf = open(path, "rb").read()
    rel = os.path.relpath(path, ROOT).replace("\\", "/")
    if buf[:4] == b"LCGB":
        continue
    chunks, off, n = [], 0, len(buf)
    while off + 16 <= n:
        raw, size, t, opt = struct.unpack_from("<iiii", buf, off)
        if size < 16 or off + size > n:
            break
        chunks.append((u32(raw), size, t, opt, off))
        off += size
        if (u32(raw) & 0xFFFF) == 0xF0:
            break
    if not chunks:
        continue
    for raw, size, t, opt, o in chunks:
        ct, hi = raw & 0xFFFF, (raw >> 16) & 0xFFFF
        hi_by_ctype[CH.get(ct, hex(ct))][hi] += 1
        opt_by_ctype[CH.get(ct, hex(ct))][opt] += 1
    first_chunk[CH.get(chunks[0][0] & 0xFFFF, "?")] += 1

    dchunks = [c for c in chunks if (c[0] & 0xFFFF) == 2]
    if not dchunks:
        continue

    # ---- per-demo packet walk
    ids_running = []          # ids of non-OBJECT, non-MOTIONPACK packets, in order
    obj_ids = {}
    kinds_all = Counter()
    eff_ids = Counter()
    last_two = []
    for raw, size, t, opt, o in dchunks:
        p, end = o + 16, o + size
        while p + 16 <= end:
            prt, ps, pid, rsv = struct.unpack_from("<iiii", buf, p)
            k = u32(prt) & 0xFFFF
            if ps < 16 or p + ps > end:
                break
            kinds_all[k] += 1
            if k == 4:
                cache, flag, name, kms = struct.unpack_from("<4i", buf, p + 16)
                obj_ids[pid] = flag
                obj_flag_by_pid[(pid, flag)] += 1
            elif k == 7:
                nl, doff = struct.unpack_from("<2i", buf, p + 16)
                dataoff_rule["MATCH 32+12n" if doff == 32 + 12 * nl else f"OTHER({doff}-{32+12*nl})"] += 1
                for i in range(nl):
                    oo = p + 32 + i * 12
                    if oo + 12 > p + ps:
                        break
                    oid, fl, sj, nj, moff, mfr = struct.unpack_from("<iHBBHH", buf, oo)
                    mp_off_align[moff % 16] += 1
            else:
                ids_running.append(pid)
                if k == 6:
                    eid = struct.unpack_from("<i", buf, p + 16)[0]
                    eff_ids[pid] += 1
            p += ps
            if k in (0, 1):
                if k == 0:
                    last_two.append("SEQ")
                else:
                    last_two.append("FRM")
                break
        # last frame may continue past TERM_SEQ
        if p < end:
            prt2, ps2, pid2, _ = struct.unpack_from("<iiii", buf, p)
            term_pairs[(u32(prt2) & 0xFFFF, ps2)] += 1

    # id monotonic / wrap check on the running-counter kinds
    regress = [(a, b) for a, b in zip(ids_running, ids_running[1:]) if b < a]
    for a, b in regress:
        wrap_evidence["wrap@65535" if a > 65000 and b < 1000 else f"other({a}->{b})"] += 1
    for pid, c in eff_ids.items():
        if c > 1:
            eff_id_reuse[c] += 1

    times = [c[2] for c in dchunks]
    per_demo.append({
        "file": rel, "frames": len(dchunks), "time_last": times[-1],
        "dur_s": round((times[-1] + 5) / 300.0, 2),
        "pcm": sum(c[1] - 16 for c in chunks if (c[0] & 0xFFFF) == 1),
        "cap": sum(1 for c in chunks if (c[0] & 0xFFFF) == 4),
        "objs": len(obj_ids), "obj_id_range": [min(obj_ids), max(obj_ids)] if obj_ids else None,
        "max_running_id": max(ids_running) if ids_running else 0,
        "id_regressions": len(regress),
        "eff": kinds_all.get(6, 0), "cmd": kinds_all.get(2, 0), "mp": kinds_all.get(7, 0),
        "bytes": len(buf),
    })

print("=== chunk raw-type HIGH 16 bits, per ctype (channel field?) ===")
for k, v in hi_by_ctype.items():
    print(f"  {k:10s} {dict(v.most_common(8))}")
print("\n=== chunk option field, per ctype ===")
for k, v in opt_by_ctype.items():
    print(f"  {k:10s} distinct={len(v):5d} top={dict(v.most_common(6))}")
print("\n=== first chunk of every stream ===", dict(first_chunk))
print("\n=== MOTION_PACK data_offset vs 32+12*n_lists ===", dict(dataoff_rule.most_common(5)))
print("=== MOTION_PACK per-list offset % 16 ===", dict(mp_off_align))
print("\n=== packet-id regressions: wrap or not? ===", dict(wrap_evidence.most_common(5)))
print("=== EFFECT header.id reused within a demo (count -> n) ===", dict(eff_id_reuse.most_common(6)))
print("\n=== what follows TERM_SEQ in the last frame ===",
      {f"kind={k} size={s}": v for (k, s), v in term_pairs.items()})
print("\n=== OBJECT flag values by (pid,flag) -- top ===",
      dict(Counter({f"flag={f}": c for (p, f), c in obj_flag_by_pid.items()}).most_common(8)))

per_demo.sort(key=lambda d: -d["frames"])
print(f"\n=== PER-DEMO TABLE ({len(per_demo)} demos) ===")
print(f"{'file':52s} {'frames':>6s} {'dur_s':>7s} {'pcm':>9s} {'cap':>4s} {'objs':>4s} "
      f"{'eff':>6s} {'cmd':>5s} {'maxid':>6s} {'bytes':>9s}")
for d in per_demo:
    print(f"{d['file'][:52]:52s} {d['frames']:6d} {d['dur_s']:7.1f} {d['pcm']:9d} {d['cap']:4d} "
          f"{d['objs']:4d} {d['eff']:6d} {d['cmd']:5d} {d['max_running_id']:6d} {d['bytes']:9d}")

os.makedirs(os.path.dirname(os.path.abspath(_args.out)), exist_ok=True)
json.dump(per_demo, open(_args.out, "w"), indent=1)
f = [d["frames"] for d in per_demo]
print(f"\nframes: min={min(f)} max={max(f)} mean={sum(f)/len(f):.0f}")
