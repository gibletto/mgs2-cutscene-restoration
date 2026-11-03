#!/usr/bin/env python3
"""
exec_type_observed.py -- which exec_type(s) each effect id actually ships with.

Scans the shipped US cutscene streams (us/demo/_bp and us/demo2/_bp, 129 files on the PC
Master Collection) with corpus_scan.py's packet walker and writes

    { "0x1000000": [0], "0x1000001": [1], ... }

i.e. effect id (hex, as Python's hex() prints it) -> sorted list of exec_types observed.
This is the evidence behind cnf2json.py's exec_type rule (202 effects observed, none with
more than one exec_type).

Output: <studio>/data/exec_type_observed.json (gitignored -- measured from the game).

Usage:
    python3 exec_type_observed.py --root <game dir> [--out <path>]   # --root defaults to $MGS2_DIR
"""
import argparse
import glob
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import corpus_scan  # noqa: E402  (same directory)


def main():
    studio = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=os.environ.get("MGS2_DIR"),
                    help="game install (default $MGS2_DIR)")
    ap.add_argument("--out", default=os.path.join(os.environ.get("MGS2CS_DATA_DIR") or
                                                  os.path.join(studio, "data"),
                                                  "exec_type_observed.json"))
    a = ap.parse_args()
    if not a.root:
        ap.error("need --root <game dir> (or set MGS2_DIR)")

    files = []
    for sub in ("demo", "demo2"):
        files += sorted(glob.glob(os.path.join(a.root, "us", sub, "_bp", "*.sdt")))
    if not files:
        ap.error(f"no .sdt under {a.root}/us/demo*/_bp")

    agg = corpus_scan.new_agg(a.root)
    for f in files:
        corpus_scan.scan_file(f, agg)

    seen = {}
    for (eid, et), n in agg["eff_hist"].items():
        if n:
            seen.setdefault(eid, set()).add(et)
    out = {hex(k): sorted(v) for k, v in sorted(seen.items())}

    os.makedirs(os.path.dirname(os.path.abspath(a.out)), exist_ok=True)
    with open(a.out, "w") as f:
        f.write(json.dumps(out, indent=1))
    multi = [k for k, v in out.items() if len(v) > 1]
    print(f"scanned {len(files)} streams: {len(out)} effect ids, {len(multi)} with more than one exec_type")
    print(f"wrote {a.out}")


if __name__ == "__main__":
    main()
