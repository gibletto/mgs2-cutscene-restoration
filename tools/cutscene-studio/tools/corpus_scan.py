#!/usr/bin/env python3
"""
corpus_scan.py -- MEASURE the shipped MGS2 .sdt corpus. No theory, only what is on disk.

Parses every .sdt under the game root, classifies streams by chunk-ctype set, and builds
histograms of demo packets / effects / commands / motion, plus an IDIOM audit (the
structural invariants a naive .sdt writer would violate and thereby crash the game).

FORMAT GROUND TRUTH (all offsets little-endian; citations are into the Konami source):
  container chunk tag : mgs2x/source/system/libfs/stream.h:14-21   STREAM_TAG {type,size,time,option} 4x i32
  chunk ctype enum    : mgs2x/source/system/libfs/stream.h:32-56   CHANK_TYPE_*
  demo packet header  : mgs2x/source/include/fmt_demo.h:69-74      DEMO_PACKET {type,size,id,reserved} 4x i32
  packet type enum    : mgs2x/source/include/fmt_demo.h:81-100     + NOSKIP/ENDIAN_SWAPPED/TYPE_MASK
  payload structs     : mgs2x/source/include/fmt_demo.h:105-215
  effect dispatch     : mgs2x/source/user/mode/demo/eft_act.c:62-108
                        exec_type 0 -> exec(header.id, &data[0])       params start at data[0]
                        exec_type 1 -> exec(header.id, &data[4])       data[0]=life(frames),
                                                                       data[1..3]=reserved,
                                                                       params start at data[4]
  effect_id encoding  : demo_eft.c:146-164 compares literals 0x0100XXXX; the low 24 bits are the
                        effect number that names the source file (effect/<hex>_<Name>.c) and the
                        <id:0x...> field in effect/demoeffect.cnf.

Outputs (into --out-dir, default <studio>/data, gitignored -- measured from the game):
  corpus.json           the corpus statistics
  corpus_effects.json   per-effect observed usage (id -> name/flags/observed exec types).
                        NOT effects.json: that name is the editor's catalog (tools/cnf2json.py).

Usage:
  python3 corpus_scan.py --root <game dir> --effect-src <source tree>/mgs2x/source/user/mode/demo/effect
      --root defaults to $MGS2_DIR, --effect-src to $MGS2_SOURCE_DIR/mgs2x/source/user/mode/demo/effect
  python3 corpus_scan.py --include-backups     # also .sdt.orig/.prev/.bak
"""
import argparse, json, os, re, struct, sys
from collections import Counter, defaultdict

# ---------------------------------------------------------------- constants
CHANK = {
    0x00: "FREE", 0x01: "PCM", 0x02: "DEMO", 0x03: "LIP", 0x04: "CAPTION",
    0x05: "MOTION", 0x06: "CODEC_CAP", 0x0C: "CRI_MPEG", 0x0D: "CRI_IPIC",
    0x0E: "MPEG2_VIDEO", 0x0F: "IPU", 0x10: "SYSTEM", 0x20: "MOVIE",
    0x80: "USE", 0xF0: "END", 0xFF: "BUFEND",
}
PKT = {
    0: "TERM_SEQ", 1: "TERM_FRAME", 2: "COMMAND", 3: "FRAME", 4: "OBJECT",
    5: "MOTION", 6: "EFFECT", 7: "MOTION_PACK", 8: "END", 0x1000: "MST",
}
NOSKIP = 0x80000000
ENDIAN_SWAPPED = 0x40000000
TYPE_MASK = 0x0000FFFF
TIME_BASE = 5           # 1/300 s tick units per NTSC frame (libmt.h:383, dgconf.h:49)
EFFECT_NS = 0x01000000  # high byte of every demo effect_id

CMD = {0: "OBJECT_VISIBLE", 1: "OBJECT_DESTROY", 2: "EFFECT_DESTROY", 3: "CAMERA_CREATE"}
MP_FLAG = {0x0001: "MOV", 0x0002: "ROT", 0x0004: "ROTS", 0x0008: "ABSROTS", 0x0010: "TRANS"}


def u32(v):
    return v & 0xFFFFFFFF


def mpflags(f):
    on = [n for b, n in MP_FLAG.items() if f & b]
    rest = f & ~0x1F
    if rest:
        on.append(f"UNK:0x{rest:04x}")
    return "|".join(on) or "0"


# ---------------------------------------------------------------- effect names
def load_effect_names(effect_dir):
    """effect/<hex>_<Name>.c  -- hex is 4 OR 5 digits (fff00, ffff0)."""
    names = {}
    if not effect_dir or not os.path.isdir(effect_dir):
        return names
    for fn in os.listdir(effect_dir):
        m = re.match(r"^([0-9a-f]{4,5})_(.+)\.c$", fn)
        if m:
            names[int(m.group(1), 16)] = m.group(2)
    return names


def load_cnf_flags(effect_dir):
    """Pull <id:0x..> + flags (<no_birth>, <force_effect>) out of demoeffect.cnf (UTF-8)."""
    p = os.path.join(effect_dir, "demoeffect.cnf")
    out = {}
    if not os.path.isfile(p):
        return out
    t = open(p, encoding="utf-8", errors="replace").read()
    for m in re.finditer(r"effect\s+\"[^\"]*\"\s*\[(\w+)\]\s*<\s*id\s*:\s*(0x[0-9a-fA-F]+)\s*>([^{]*)", t):
        fn, idhex, flags = m.group(1), m.group(2), m.group(3)
        out[int(idhex, 16)] = {
            "func": fn,
            "no_birth": "<no_birth>" in flags,
            "force_effect": "<force_effect>" in flags,
        }
    return out


# ---------------------------------------------------------------- chunk layer
class Chunk:
    __slots__ = ("raw", "ctype", "hi", "size", "time", "option", "off", "payload")


def read_chunks(buf):
    chunks, errs, off, n = [], [], 0, len(buf)
    while off + 16 <= n:
        raw, size, time, option = struct.unpack_from("<iiii", buf, off)
        if size < 16 or off + size > n:
            errs.append(f"chunk@0x{off:x}: size={size} invalid (file {n})")
            break
        c = Chunk()
        c.raw, c.ctype, c.hi = u32(raw), u32(raw) & 0xFFFF, (u32(raw) >> 16) & 0xFFFF
        c.size, c.time, c.option, c.off = size, time, option, off
        c.payload = buf[off + 16: off + size]
        chunks.append(c)
        off += size
        if c.ctype == 0xF0:                       # CHANK_TYPE_END ends the stream
            break
    return chunks, buf[off:], errs


# ---------------------------------------------------------------- packet layer
class Pkt:
    __slots__ = ("raw", "kind", "flags", "size", "pid", "reserved", "off", "body")


def read_packets(payload):
    pkts, errs, off, n = [], [], 0, len(payload)
    while off + 16 <= n:
        raw, size, pid, reserved = struct.unpack_from("<iiii", payload, off)
        raw = u32(raw)
        kind = raw & TYPE_MASK
        if size < 16 or off + size > n:
            errs.append(f"pkt+0x{off:x}: size={size} invalid (payload {n})")
            break
        p = Pkt()
        p.raw, p.kind, p.size, p.pid, p.reserved, p.off = raw, kind, size, pid, reserved, off
        p.flags = raw & 0xFFFF0000
        p.body = payload[off + 16: off + size]
        pkts.append(p)
        off += size
        if kind in (0, 1):                        # TERM_SEQ / TERM_FRAME end the frame
            break
    return pkts, payload[off:], errs


# ---------------------------------------------------------------- per-file scan
def scan_file(path, agg):
    buf = open(path, "rb").read()
    rel = os.path.relpath(path, agg["root"]).replace("\\", "/")
    magic = buf[:4]

    chunks, tail, errs = read_chunks(buf)
    if not chunks:
        agg["nonstream"][magic.decode("latin1")] += 1
        agg["nonstream_ex"].setdefault(magic.decode("latin1"), rel)
        agg["class_sets"]["<NOT A STREAM>"] += 1
        agg["class_examples"].setdefault("<NOT A STREAM>", rel)
        return

    ctypes = Counter(c.ctype for c in chunks)
    key = "+".join(CHANK.get(k, f"0x{k:02x}") for k in sorted(ctypes))
    agg["class_sets"][key] += 1
    agg["class_examples"].setdefault(key, rel)

    for c in chunks:
        agg["chunk_sizes"][CHANK.get(c.ctype, hex(c.ctype))].add(c.size)
        agg["chunk_option"][CHANK.get(c.ctype, hex(c.ctype))][c.option] += 1
        agg["chunk_hi"][c.hi] += 1
        if c.size % 16:
            agg["idiom"]["chunk_size_not_16B_aligned"].append((rel, c.off, c.size))
        if c.ctype == 0x04 and len(c.payload) == 0:
            agg["idiom"]["empty_CAPTION_chunk"].append((rel, c.off))
    if chunks[-1].ctype != 0xF0:
        agg["idiom"]["stream_has_no_END_chunk"].append((rel, hex(chunks[-1].ctype)))
    if tail:
        agg["idiom"]["bytes_after_END_chunk"].append((rel, len(tail)))
    if chunks[0].ctype != 0x10:
        agg["idiom"]["first_chunk_not_SYSTEM"].append((rel, hex(chunks[0].ctype)))

    if 0x02 not in ctypes:
        return   # not a demo -- classification only

    # ================= true DEMO stream =================
    demo_chunks = [c for c in chunks if c.ctype == 0x02]
    times = [c.time for c in demo_chunks]
    rec = {
        "file": rel,
        "bytes": len(buf),
        "frames": len(demo_chunks),
        "time_first": times[0], "time_last": times[-1],
        "duration_s": round((times[-1] - times[0] + TIME_BASE) / 300.0, 2),
        "pcm_bytes": sum(c.size - 16 for c in chunks if c.ctype == 0x01),
        "ctypes": {CHANK.get(k, hex(k)): v for k, v in sorted(ctypes.items())},
    }

    pkt_hist, eff_local = Counter(), Counter()
    obj_ids, animated = set(), set()
    termseq_frames, empty_frames, time_bad = [], 0, 0
    ids_seen, id_nonmono = [], 0
    prev_ctime = None
    max_pid = 0

    for fi, c in enumerate(demo_chunks):
        if prev_ctime is not None and c.time <= prev_ctime:
            agg["idiom"]["DEMO_chunk_time_not_increasing"].append((rel, c.off, prev_ctime, c.time))
        prev_ctime = c.time
        if c.time != TIME_BASE * fi:
            time_bad += 1

        pkts, ptail, perrs = read_packets(c.payload)
        for e in perrs:
            agg["idiom"]["packet_walk_error"].append((rel, c.off, e))
        if ptail:
            agg["idiom"]["bytes_after_frame_terminator"].append((rel, c.off, len(ptail)))

        kinds = Counter()
        body_pkts = 0
        for p in pkts:
            kinds[p.kind] += 1
            pkt_hist[p.kind] += 1
            agg["pkt_hist"][p.kind] += 1
            agg["pkt_sizes"][p.kind][p.size] += 1
            agg["pkt_flags"][p.kind][hex(p.flags)] += 1
            agg["pkt_reserved"][p.reserved] += 1
            if p.size % 16:
                agg["idiom"]["packet_size_not_16B_aligned"].append((rel, c.off, p.off, p.size))
            if p.kind not in PKT:
                agg["idiom"]["unknown_packet_kind"].append((rel, c.off, p.off, hex(p.raw)))
            if p.raw & ENDIAN_SWAPPED:
                agg["idiom"]["ENDIAN_SWAPPED_bit_set_on_disk"].append((rel, c.off, p.off))
            if p.kind in (0, 1) and (p.raw & NOSKIP):
                agg["idiom"]["NOSKIP_on_terminator"].append((rel, c.off, p.off, hex(p.raw)))

            if p.kind == 0:
                termseq_frames.append(fi)
            elif p.kind != 1:
                body_pkts += 1
                max_pid = max(max_pid, p.pid)
                ids_seen.append(p.pid)

            # ---- payloads
            if p.kind == 2 and len(p.body) >= 16:                      # COMMAND
                cmd, cid, p1, p2 = struct.unpack_from("<iiii", p.body, 0)
                agg["cmd_hist"][(cmd, p1, p2)] += 1
                agg["cmd_files"][(cmd, p1, p2)].add(rel)
                agg["cmd_target"][cmd][cid] += 1
                if p.pid != 0:
                    agg["cmd_pid_nonzero"] += 1

            elif p.kind == 3 and len(p.body) >= 48:                    # FRAME (camera)
                no, screen, zrot, chanl = struct.unpack_from("<ifii", p.body, 0)
                pos = struct.unpack_from("<4f", p.body, 16)
                trg = struct.unpack_from("<4f", p.body, 32)
                agg["cam_no"][no] += 1
                agg["cam_chanl"][chanl] += 1
                agg["cam_screen"][round(screen, 5)] += 1
                agg["cam_zrot"][round(struct.unpack_from("<f", p.body, 8)[0], 4)] += 1
                agg["cam_pos_w"][round(pos[3], 4)] += 1
                agg["cam_trg_w"][round(trg[3], 4)] += 1
                for v in pos[:3] + trg[:3]:
                    agg["cam_absmax"] = max(agg["cam_absmax"], abs(v))
                if p.flags:
                    agg["frame_pkt_has_flags"] += 1

            elif p.kind == 4 and len(p.body) >= 16:                    # OBJECT
                cache_id, flag, name, kms = struct.unpack_from("<iiii", p.body, 0)
                obj_ids.add(p.pid)
                agg["obj_flag"][flag] += 1
                agg["obj_pid"][p.pid] += 1
                agg["obj_cache_ext"][u32(cache_id) & 0xFF] += 1
                agg["obj_kms_zero"] += (1 if kms == 0 else 0)
                agg["obj_cache_zero"] += (1 if cache_id == 0 else 0)
                agg["obj_total"] += 1

            elif p.kind == 5 and len(p.body) >= 48:                    # MOTION (legacy)
                oid, mtype, sj, nj = struct.unpack_from("<iiii", p.body, 0)
                agg["motion_type"][mtype] += 1
                agg["motion_nj"][nj] += 1
                animated.add(oid)

            elif p.kind == 6 and len(p.body) >= 16:                    # EFFECT
                eid, etype = struct.unpack_from("<ii", p.body, 0)
                eid = u32(eid)
                pad = struct.unpack_from("<2i", p.body, 8)
                dlen = p.size - 32
                agg["eff_hist"][(eid, etype)] += 1
                agg["eff_dlen"][(eid, etype)][dlen] += 1
                agg["eff_files"][(eid, etype)].add(rel)
                agg["eff_ns"][eid >> 24] += 1
                agg["eff_pad"][pad] += 1
                agg["eff_exec_by_id"][eid].add(etype)
                eff_local[(eid, etype)] += 1
                if etype == 1 and dlen >= 16:
                    life, r1, r2, r3 = struct.unpack_from("<4i", p.body, 16)
                    agg["eff_life"][(eid, etype)][life] += 1
                    if (r1, r2, r3) != (0, 0, 0):
                        agg["idiom"]["exec1_reserved_slots_nonzero"].append(
                            (rel, c.off, p.off, hex(eid), r1, r2, r3))
                    agg["eff_life_all"][life] += 1
                elif etype == 1:
                    agg["idiom"]["exec1_effect_with_no_life_field"].append(
                        (rel, c.off, p.off, hex(eid), dlen))
                if etype not in (0, 1):
                    agg["idiom"]["effect_exec_type_not_0_or_1"].append(
                        (rel, c.off, p.off, hex(eid), etype))
                if dlen == 0:
                    agg["idiom"]["effect_with_empty_data"].append((rel, c.off, p.off, hex(eid)))

            elif p.kind == 7 and len(p.body) >= 16:                    # MOTION_PACK
                n_lists, data_off = struct.unpack_from("<ii", p.body, 0)
                agg["mp_nlists"][n_lists] += 1
                agg["mp_dataoff"][data_off] += 1
                if p.pid != 0:
                    agg["idiom"]["motionpack_id_nonzero"].append((rel, c.off, p.off, p.pid))
                hi_water = 32 + n_lists * 12
                for i in range(n_lists):
                    o = 16 + i * 12
                    if o + 12 > len(p.body):
                        agg["idiom"]["motionpack_list_overruns_packet"].append(
                            (rel, c.off, p.off, n_lists, p.size))
                        break
                    oid, flag, sj, nj, moff, mfr = struct.unpack_from("<iHBBHH", p.body, o)
                    animated.add(oid)
                    agg["mp_flag"][flag] += 1
                    agg["mp_nj"][nj] += 1
                    agg["mp_sj"][sj] += 1
                    agg["mp_frame"][mfr] += 1
                    agg["mp_oid"][oid] += 1
                    if moff < hi_water or moff >= p.size:
                        agg["idiom"]["motionpack_offset_outside_data_area"].append(
                            (rel, c.off, p.off, moff, p.size))

            elif p.kind == 0x1000:                                     # MST
                agg["mst_files"].add(rel)

        if body_pkts == 0:
            empty_frames += 1
        if 3 not in kinds:
            agg["idiom"]["frame_without_FRAME_camera_packet"].append((rel, c.off, dict(kinds)))
        if kinds.get(3, 0) > 1:
            agg["idiom"]["multiple_FRAME_packets_in_one_frame"].append((rel, c.off, kinds[3]))
        if kinds.get(1, 0) != 1:
            agg["idiom"]["frame_not_ending_in_TERM_FRAME"].append((rel, c.off, dict(kinds)))

    for a, b in zip(ids_seen, ids_seen[1:]):
        if b < a:
            id_nonmono += 1

    rec["pkt_hist"] = {PKT.get(k, hex(k)): v for k, v in sorted(pkt_hist.items())}
    rec["n_objects"] = len(obj_ids)
    rec["n_objects_animated"] = len(animated)
    rec["n_effect_pkts"] = sum(eff_local.values())
    rec["n_distinct_effects"] = len({e for e, _ in eff_local})
    rec["n_termseq"] = len(termseq_frames)
    rec["termseq_frames"] = termseq_frames
    rec["empty_frames"] = empty_frames
    rec["frames_time_ne_5x_index"] = time_bad
    rec["max_packet_id"] = max_pid
    rec["packet_id_regressions"] = id_nonmono

    if rec["n_termseq"] != 1:
        agg["idiom"]["TERM_SEQ_count_not_exactly_1"].append((rel, rec["n_termseq"]))
    if termseq_frames and termseq_frames[-1] != rec["frames"] - 1:
        agg["idiom"]["TERM_SEQ_not_on_last_frame"].append((rel, termseq_frames, rec["frames"] - 1))
    if empty_frames:
        agg["idiom"]["frame_with_no_body_packets"].append((rel, empty_frames))
    if time_bad:
        agg["idiom"]["DEMO_time_ne_5x_frame_index"].append((rel, time_bad, rec["frames"]))
    if id_nonmono:
        agg["idiom"]["packet_id_not_monotonic"].append((rel, id_nonmono))
    if rec["frames"] == 0:
        agg["idiom"]["zero_frame_demo"].append(rel)
    agg["demos"].append(rec)


# ---------------------------------------------------------------- main
def new_agg(root):
    return {
        "root": root, "demos": [],
        "nonstream": Counter(), "nonstream_ex": {},
        "class_sets": Counter(), "class_examples": {},
        "chunk_sizes": defaultdict(set), "chunk_option": defaultdict(Counter),
        "chunk_hi": Counter(),
        "pkt_hist": Counter(), "pkt_sizes": defaultdict(Counter),
        "pkt_flags": defaultdict(Counter), "pkt_reserved": Counter(),
        "cmd_hist": Counter(), "cmd_files": defaultdict(set),
        "cmd_target": defaultdict(Counter), "cmd_pid_nonzero": 0,
        "cam_no": Counter(), "cam_chanl": Counter(), "cam_screen": Counter(),
        "cam_zrot": Counter(), "cam_pos_w": Counter(), "cam_trg_w": Counter(),
        "cam_absmax": 0.0, "frame_pkt_has_flags": 0,
        "obj_flag": Counter(), "obj_pid": Counter(), "obj_cache_ext": Counter(),
        "obj_kms_zero": 0, "obj_cache_zero": 0, "obj_total": 0,
        "motion_type": Counter(), "motion_nj": Counter(),
        "eff_hist": Counter(), "eff_dlen": defaultdict(Counter),
        "eff_files": defaultdict(set), "eff_ns": Counter(), "eff_pad": Counter(),
        "eff_exec_by_id": defaultdict(set), "eff_life": defaultdict(Counter),
        "eff_life_all": Counter(),
        "mp_nlists": Counter(), "mp_dataoff": Counter(), "mp_flag": Counter(),
        "mp_nj": Counter(), "mp_sj": Counter(), "mp_frame": Counter(), "mp_oid": Counter(),
        "mst_files": set(),
        "idiom": defaultdict(list),
    }


def main():
    ap = argparse.ArgumentParser()
    studio = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    src = os.environ.get("MGS2_SOURCE_DIR")
    ap.add_argument("--root", default=os.environ.get("MGS2_DIR"),
                    help="game install (default $MGS2_DIR)")
    ap.add_argument("--effect-src",
                    default=os.path.join(src, "mgs2x", "source", "user", "mode", "demo", "effect")
                    if src else None,
                    help="effect source dir (default $MGS2_SOURCE_DIR/mgs2x/source/user/mode/demo/effect)")
    ap.add_argument("--out-dir", default=os.environ.get("MGS2CS_DATA_DIR") or os.path.join(studio, "data"))
    ap.add_argument("--effects-out", default="corpus_effects.json",
                    help="file name (in --out-dir) for the per-effect usage summary")
    ap.add_argument("--include-backups", action="store_true")
    a = ap.parse_args()
    if not a.root or not a.effect_src:
        ap.error("need --root <game dir> and --effect-src <dir> (or set MGS2_DIR and MGS2_SOURCE_DIR)")

    names = load_effect_names(a.effect_src)
    cnf = load_cnf_flags(a.effect_src)
    agg = new_agg(a.root)

    files = []
    for dirpath, _, fns in os.walk(a.root):
        for fn in fns:
            low = fn.lower()
            if low.endswith(".sdt") or (a.include_backups and ".sdt" in low):
                files.append(os.path.join(dirpath, fn))
    files.sort()
    print(f"scanning {len(files)} files under {a.root}", file=sys.stderr)
    for i, f in enumerate(files):
        try:
            scan_file(f, agg)
        except Exception as e:
            agg["idiom"]["scan_exception"].append((f, repr(e)))
        if (i + 1) % 2000 == 0:
            print(f"  {i+1}/{len(files)}", file=sys.stderr)

    os.makedirs(a.out_dir, exist_ok=True)
    ename = lambda eid: names.get(eid & 0x00FFFFFF, "?")

    # ---------- corpus_effects.json : id -> name/flags/observed usage
    ejson = {}
    for num, nm in sorted(names.items()):
        eid = EFFECT_NS | num
        used = {et: agg["eff_hist"][(eid, et)] for et in (0, 1) if agg["eff_hist"][(eid, et)]}
        ejson[f"0x{eid:08x}"] = {
            "num": f"0x{num:x}", "name": nm,
            "cnf": cnf.get(num),
            "observed_exec_types": sorted(used),
            "observed_count": sum(used.values()),
            "observed_data_len": {str(et): dict(agg["eff_dlen"][(eid, et)]) for et in used},
            "n_demos": len(set().union(*[agg["eff_files"][(eid, et)] for et in used])) if used else 0,
        }
    json.dump(ejson, open(os.path.join(a.out_dir, a.effects_out), "w", encoding="utf-8"),
              indent=1, ensure_ascii=False)

    ks = lambda t: "|".join(str(x) for x in t)
    out = {
        "root": a.root, "n_files": len(files),
        "class_sets": dict(agg["class_sets"].most_common()),
        "class_examples": agg["class_examples"],
        "nonstream_magic": dict(agg["nonstream"]), "nonstream_example": agg["nonstream_ex"],
        "n_demos": len(agg["demos"]), "demos": agg["demos"],
        "chunk_size_distinct": {k: len(v) for k, v in agg["chunk_sizes"].items()},
        "chunk_size_minmax": {k: [min(v), max(v)] for k, v in agg["chunk_sizes"].items()},
        "chunk_option": {k: dict(v.most_common(6)) for k, v in agg["chunk_option"].items()},
        "chunk_type_hi16": dict(agg["chunk_hi"]),
        "pkt_hist": {PKT.get(k, hex(k)): v for k, v in sorted(agg["pkt_hist"].items())},
        "pkt_sizes": {PKT.get(k, hex(k)): dict(sorted(v.items()))
                      for k, v in sorted(agg["pkt_sizes"].items())},
        "pkt_flags": {PKT.get(k, hex(k)): dict(v) for k, v in sorted(agg["pkt_flags"].items())},
        "pkt_reserved": dict(agg["pkt_reserved"].most_common(5)),
        "cmd_hist": {ks(k): v for k, v in sorted(agg["cmd_hist"].items())},
        "cmd_files": {ks(k): sorted(v)[:5] for k, v in agg["cmd_files"].items()},
        "cmd_target_ids": {str(k): dict(v.most_common(20)) for k, v in agg["cmd_target"].items()},
        "camera": {
            "no": dict(agg["cam_no"]), "chanl": dict(agg["cam_chanl"]),
            "screen_top": dict(agg["cam_screen"].most_common(12)),
            "screen_distinct": len(agg["cam_screen"]),
            "z_rotate_top": dict(agg["cam_zrot"].most_common(8)),
            "pos_w_top": dict(agg["cam_pos_w"].most_common(5)),
            "trg_w_top": dict(agg["cam_trg_w"].most_common(5)),
            "max_abs_coord": agg["cam_absmax"],
        },
        "object": {
            "flag": dict(agg["obj_flag"]), "pid": dict(sorted(agg["obj_pid"].items())),
            "cache_id_low_byte": dict(agg["obj_cache_ext"].most_common(10)),
            "cache_id_zero": agg["obj_cache_zero"], "kms_zero": agg["obj_kms_zero"],
            "total": agg["obj_total"],
        },
        "motion_legacy": {"type": dict(agg["motion_type"]), "n_joints": dict(agg["motion_nj"])},
        "motionpack": {
            "n_lists": dict(sorted(agg["mp_nlists"].items())),
            "data_offset": dict(agg["mp_dataoff"].most_common(6)),
            "flags": {f"0x{k:04x} ({mpflags(k)})": v for k, v in sorted(agg["mp_flag"].items())},
            "n_joints": dict(sorted(agg["mp_nj"].items())),
            "start_joint": dict(sorted(agg["mp_sj"].items())),
            "interp_frame": dict(sorted(agg["mp_frame"].items())),
            "object_id": dict(sorted(agg["mp_oid"].items())),
        },
        "mst_files": sorted(agg["mst_files"]),
        "effect_namespace_hi_byte": dict(agg["eff_ns"]),
        "effect_pad_values": {ks(k): v for k, v in agg["eff_pad"].most_common(10)},
        "effect_pad_distinct": len(agg["eff_pad"]),
        "effect_life_top": dict(agg["eff_life_all"].most_common(10)),
        "effects": [
            {"id": f"0x{eid:08x}", "num": f"0x{eid & 0xFFFFFF:x}", "name": ename(eid),
             "exec_type": et, "count": n, "n_demos": len(agg["eff_files"][(eid, et)]),
             "data_len": dict(sorted(agg["eff_dlen"][(eid, et)].items())),
             "life_distinct": len(agg["eff_life"][(eid, et)]),
             "demos": sorted(agg["eff_files"][(eid, et)])[:5]}
            for (eid, et), n in sorted(agg["eff_hist"].items(), key=lambda x: -x[1])
        ],
        "effects_with_two_exec_types": {f"0x{k:08x} {ename(k)}": sorted(v)
                                        for k, v in agg["eff_exec_by_id"].items() if len(v) > 1},
        "effect_ids_with_no_source_file": sorted(
            {f"0x{eid:08x}" for (eid, _) in agg["eff_hist"] if (eid & 0xFFFFFF) not in names}),
        "idioms": {k: {"n": len(v), "examples": v[:12]} for k, v in agg["idiom"].items()},
    }
    json.dump(out, open(os.path.join(a.out_dir, "corpus.json"), "w", encoding="utf-8"),
              indent=1, default=str)

    # ---------------------------------------------------------------- digest
    P = print
    P("\n===== (a) CLASSIFICATION by chunk-ctype set =====")
    for k, v in agg["class_sets"].most_common():
        P(f"{v:6d}  {k:44s} e.g. {agg['class_examples'][k]}")
    P(f"non-stream magics: {dict(agg['nonstream'])} e.g. {agg['nonstream_ex']}")
    P(f"TRUE DEMOS (contain a DEMO chunk): {len(agg['demos'])}")

    P("\n===== (c) PACKET histogram =====")
    for k, v in sorted(agg["pkt_hist"].items()):
        s = agg["pkt_sizes"][k]
        P(f"{PKT.get(k,hex(k)):12s} n={v:9d} sizes={min(s)}..{max(s)} distinct={len(s)} "
          f"flags={dict(agg['pkt_flags'][k])}")

    P("\n===== (d) EFFECT histogram =====")
    for e in out["effects"]:
        P(f"  {e['id']} {e['num']:>7s} {e['name'][:32]:32s} exec={e['exec_type']} "
          f"n={e['count']:<6d} demos={e['n_demos']:<3d} dlen={list(e['data_len'])}")
    P(f"  -> {len(out['effects'])} distinct (effect_id, exec_type) pairs")
    P(f"  -> ids with BOTH exec types: {out['effects_with_two_exec_types']}")
    P(f"  -> ids with no source file : {out['effect_ids_with_no_source_file']}")
    P(f"  -> effect_id high byte     : {out['effect_namespace_hi_byte']}")
    P(f"  -> distinct pad[2] values  : {out['effect_pad_distinct']} "
      f"top={list(out['effect_pad_values'].items())[:3]}")

    P("\n===== (e) COMMAND histogram (command,param1,param2) =====")
    for k, v in sorted(agg["cmd_hist"].items()):
        P(f"  cmd={k[0]} {CMD.get(k[0],'?'):16s} p1={k[1]:<6d} p2={k[2]:<6d} n={v:<7d} "
          f"targets={len(agg['cmd_target'][k[0]])}")

    P("\n===== (f) MOTION / MOTIONPACK =====")
    P(f"  legacy MOTION packets: {agg['pkt_hist'].get(5,0)}   MST packets: {len(agg['mst_files'])} files")
    P(f"  MOTIONPACK flags: {out['motionpack']['flags']}")
    P(f"  data_offset: {out['motionpack']['data_offset']}")
    P(f"  n_lists range: {min(agg['mp_nlists'])}..{max(agg['mp_nlists'])}")
    P(f"  n_joints range: {min(agg['mp_nj'])}..{max(agg['mp_nj'])}  "
      f"start_joint: {sorted(agg['mp_sj'])[:6]}")
    P(f"  interp frame values: {dict(sorted(agg['mp_frame'].items())[:8])}")
    P(f"  object ids animated: {sorted(agg['mp_oid'])[:20]} (max {max(agg['mp_oid'])})")

    P("\n===== (g) IDIOMS =====")
    for k, v in sorted(agg["idiom"].items(), key=lambda x: -len(x[1])):
        P(f"  {k:40s} n={len(v):<8d} e.g. {v[0] if v else ''}")
    P(f"\nwrote {a.out_dir}/corpus.json + {a.effects_out}")


if __name__ == "__main__":
    main()
