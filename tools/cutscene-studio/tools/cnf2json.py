#!/usr/bin/env python3
"""
cnf2json.py -- parse Konami's demoeffect.cnf (the MGS2 demo-effect definition
language) into machine-readable JSON for the cutscene editor's property panel.

Reimplements tools/CreateEffect (ParseLex.l + ParseGrm.y + CreateEffect.c),
including the exact `Data` struct layout that CreateEffect.c emits -- which IS
the byte layout of DEMO_EFFECT.data[] in a .sdt file.


Usage:
    python3 cnf2json.py --source <source tree> [--validate]
    python3 cnf2json.py [--cnf PATH] [--pristine PATH] [--effects-dir DIR]
                        [--out PATH] [--validate]

<source tree> is the root of the original source tree (the directory holding mgs2x/ and
tools/); it defaults to $MGS2_SOURCE_DIR. The output, <studio>/data/effects.json, is
derived from it and is gitignored -- never commit it.
"""

import argparse
import json
import os
import re
import sys
from collections import OrderedDict

# --------------------------------------------------------------------------
# 1. Encoding.  The Konami original is EUC-JP (NOT Shift-JIS -- cp932 fails
#    outright).  The copy shipped in the HD source tree was double-converted
#    (EUC-JP bytes decoded as cp932, re-encoded UTF-8), which mojibakes the
#    Japanese and, where an EUC trail byte landed in cp932's lead-byte range
#    0xE0-0xFC, *eats the closing quote* of the literal.  The lexer below is
#    tolerant of that; `demojibake()` inverts the damage where it is invertible.
# --------------------------------------------------------------------------

def read_text(path):
    raw = open(path, "rb").read()
    for enc in ("euc_jp", "utf-8", "cp932"):
        try:
            return raw.decode(enc), enc
        except UnicodeDecodeError:
            continue
    return raw.decode("utf-8", errors="replace"), "utf-8(lossy)"


def demojibake(s):
    """Invert EUC-JP-read-as-cp932.

    Returns (text, quality) where quality is one of:
      "ascii"   nothing to do
      "clean"   the cp932 bytes round-tripped to valid EUC-JP -- exact recovery.
                This includes the PUA chars (U+E000..): they re-encode to cp932
                user-defined bytes 0xF040.. which ARE valid EUC-JP lead/trail
                pairs, so they recover exactly (ｲ<U+E2B8>ｾX -> 回転X).
      "partial" the string contains a substitution artifact -- a '・' (U+30FB)
                that the original converter emitted for a cp932 sequence it
                could not map. Its 2 original EUC bytes are GONE, so recovery is
                lossy and the stream desyncs around it. Best effort only.
      "raw"     not recoverable at all; the mojibake is returned unchanged.
    """
    if not s:
        return s, "ascii"
    if all(ord(c) < 0x80 for c in s):
        return s, "ascii"
    try:
        b = s.encode("cp932")
    except UnicodeEncodeError:
        return s, "raw"
    try:
        return b.decode("euc_jp"), "clean"
    except UnicodeDecodeError:
        pass
    t = b.decode("euc_jp", errors="replace").replace("�", "?")
    return (t, "partial") if t.strip("? ") else (s, "raw")


# --------------------------------------------------------------------------
# 1b. exec_type -- VERIFIED against 129 shipped .sdt demos (us/demo, us/demo2).
#     exec_type is CONSTANT per effect (202 effects observed, 0 with variance).
#     exec_type == 0  <=>  <no_birth>   -- with exactly ONE exception:
#     DM_ControlChange (0xffff0) is <force_effect>, not <no_birth>, yet always
#     ships exec_type 0. `force_effect` is otherwise ORTHOGONAL to exec_type
#     (22 force_effect effects ship exec_type 1), so it is NOT the discriminator.
#
#     This matters: exec_type picks the base of the parameter block.
#       exec_type 0 -> launcher gets data+0                 (eft_act.c:73)
#       exec_type 1 -> launcher gets data+4 ints (+16 B),
#                      and data[0] is the effect's LIFE in frames,
#                      data[1..3] reserved                  (eft_act.c:84,90)
# --------------------------------------------------------------------------
EXEC_TYPE_0_WITHOUT_NO_BIRTH = {0xFFFF0}       # DM_ControlChange


def exec_type_for(eid, opts):
    if opts.get("no_birth") or eid in EXEC_TYPE_0_WITHOUT_NO_BIRTH:
        return 0
    return 1


# --------------------------------------------------------------------------
# 2. Lexer  (ParseLex.l)
# --------------------------------------------------------------------------

TYPE_TOKENS = {
    "char", "short", "int", "long",
    "u_char", "u_short", "u_int", "u_long",
    "float", "void",
    "FVECTOR", "SVECTOR", "IVECTOR", "FMATRIX",
}

NUM_RE = re.compile(
    r"""0[xX][0-9a-fA-F]+[uUlL]*      # hex
      | 0[0-7]+[uUlL]*                # octal
      | (?:\d+\.\d*|\.\d+|\d+)(?:[eE][+-]?\d+)?[fFlL]?   # float / dec
    """,
    re.X,
)
IDENT_RE = re.compile(r"[A-Za-z_][A-Za-z_0-9]*")


class Lexer:
    """Character-level lexer. Strings are scanned with caller-supplied resync
    characters so a literal whose closing quote was destroyed by the bad
    encoding conversion still terminates at the right structural boundary."""

    def __init__(self, text):
        self.s = text
        self.n = len(text)
        self.i = 0

    def skip_ws(self):
        s, n = self.s, self.n
        while self.i < n:
            c = s[self.i]
            if c in " \t\r\n\v\f　":
                self.i += 1
            elif c == "/" and self.i + 1 < n and s[self.i + 1] == "*":
                j = s.find("*/", self.i + 2)
                self.i = n if j < 0 else j + 2
            elif c == "/" and self.i + 1 < n and s[self.i + 1] == "/":
                j = s.find("\n", self.i)
                self.i = n if j < 0 else j + 1
            elif c == "#":  # #include -- resolved by the caller, skip the line
                j = s.find("\n", self.i)
                self.i = n if j < 0 else j + 1
            else:
                return

    def peek(self):
        self.skip_ws()
        return self.s[self.i] if self.i < self.n else ""

    def eof(self):
        return self.peek() == ""

    def accept(self, lit):
        self.skip_ws()
        if self.s.startswith(lit, self.i):
            self.i += len(lit)
            return True
        return False

    def accept_kw(self, kw):
        """accept() a bare keyword, not a prefix of a longer identifier."""
        self.skip_ws()
        j = self.i + len(kw)
        if self.s.startswith(kw, self.i) and not (
                j < self.n and (self.s[j].isalnum() or self.s[j] == "_")):
            self.i = j
            return True
        return False

    def expect(self, lit):
        if not self.accept(lit):
            raise SyntaxError(f"expected {lit!r} at {self.context()}")
        return True

    def context(self):
        return f"offset {self.i}: ...{self.s[max(0,self.i-40):self.i+40]!r}..."

    def ident(self):
        self.skip_ws()
        m = IDENT_RE.match(self.s, self.i)
        if not m:
            return None
        self.i = m.end()
        return m.group()

    def number(self):
        self.skip_ws()
        m = NUM_RE.match(self.s, self.i)
        if not m:
            return None
        self.i = m.end()
        return m.group()

    # A widget-call label run-on ends where the real argument list resumes.
    # Every MaxInput*(name, flag, ...) has an INT CONSTANT as arg 1, so the
    # comma that introduces it is the label's true end. Labels themselves do
    # contain ASCII commas ("XSI,KMS,CV2モデル"), but never ",<digits>,".
    _FLAGSEP = re.compile(r",\s*-?\d+\s*[,)]")

    def string(self, resync="", follow="", label=False, forbid=""):
        r"""Scan a "..." literal.

        ParseLex.l:91 -- LT  \"(\\.|[^\\"])*\"  -- [^\\"] matches newline, so a
        literal legitimately spans lines (the long Japanese descriptions do).
        The scan therefore runs to the closing quote across newlines.

        THE CORRUPTION: the HD-tree copy was double-converted (EUC-JP bytes
        decoded as cp932). An EUC trail byte landing in cp932's lead range
        (0xE0-0xFC) swallows the NEXT ASCII byte -- so the delimiter right after
        a Japanese run can vanish. Observed: the closing '"' of a display name,
        the '[' of a mesg id (`強制オン[2]` -> `ｶｯﾀｩ･ｪ･<PUA>2]`), and the closing
        quote of a widget label.

        Detection is an LL(1) FOLLOW-set check, not a heuristic: the grammar
        fixes what may legally follow this literal's closing quote (`[` after an
        effect name, `;` after a description, `,` or `)` after a call argument).
        If the char after the quote we found is not in that set, we did not find
        the real closing quote -- it was eaten and we ran on into a later one.
        """
        self.skip_ws()
        if self.i >= self.n or self.s[self.i] != '"':
            return None, False
        i = self.i + 1
        buf = []
        closed = False
        while i < self.n:
            c = self.s[i]
            if c == "\\" and i + 1 < self.n:
                buf.append(c + self.s[i + 1])
                i += 2
                continue
            if c == '"':
                closed = True
                break
            buf.append(c)
            i += 1
        if closed:
            j = i + 1
            while j < self.n and self.s[j] in " \t\r\n":
                j += 1
            nxt = self.s[j] if j < self.n else ""
            txt = "".join(buf)
            # FOLLOW alone can collide: one description literally starts with
            # '[', which is exactly the follow char of a display name, so a
            # run-on name closed on it and looked legal. `forbid` is the second
            # gate -- chars that can never appear INSIDE this literal.
            ok = (not follow or nxt in follow) and not any(c in forbid for c in txt)
            if ok:
                self.i = i + 1
                return txt, False
        # run-on: the closing quote was eaten. Re-scan from the start.
        rest = self.s[self.i + 1:]
        if label:
            m = self._FLAGSEP.search(rest)
            if m:
                self.i += 1 + m.start()
                return rest[:m.start()].rstrip().rstrip('"').rstrip(), True
        i = self.i + 1
        buf = []
        while i < self.n and self.s[i] not in resync:
            buf.append(self.s[i])
            i += 1
        self.i = i
        return "".join(buf).strip().rstrip('"').strip(), True


# --------------------------------------------------------------------------
# 3. Types & the C struct layout that CreateEffect.c produces
# --------------------------------------------------------------------------
# Vector/matrix types (see mgs_type.h / mgsx_type.h):
#   SVECTOR  four shorts (x, y, z, pad)          8 bytes, align 2
#   IVECTOR  four ints   (x, y, z, w), 16-aligned  16 bytes, align 16
#   FVECTOR  four floats (x, y, z, w), 16-aligned  16 bytes, align 16
#   FMATRIX  a 4x4 float matrix, 16-aligned        64 bytes, align 16
LAYOUT = {
    "char":    (1, 1),  "u_char":  (1, 1),
    "short":   (2, 2),  "u_short": (2, 2),
    "int":     (4, 4),  "u_int":   (4, 4),
    "long":    (4, 4),  "u_long":  (4, 4),
    "float":   (4, 4),
    "void":    (0, 1),
    "SVECTOR": (8, 2),
    "IVECTOR": (16, 16),
    "FVECTOR": (16, 16),
    "FMATRIX": (64, 16),
}
PTR_SIZE, PTR_ALIGN = 4, 4          # PS2 EE / Win32 -- both 32-bit

# MaxInput* prototypes (demomax.h:418-446).  The Data field's C type is the
# *declared return type* of the widget function, not the member's type.
WIDGETS = {
    #  name                     return    arg names (arg0 is always the label)
    "MaxInputGetKeyRangeMin": ("int",     ["name", "flag"]),
    "MaxInputGetKeyRangeMax": ("int",     ["name", "flag"]),
    "MaxInputGetEffectName":  ("int",     ["name", "flag"]),
    "MaxInputGetFrameRate":   ("int",     ["name", "flag"]),
    "MaxInputGetLink":        ("IVECTOR", ["name", "flag"]),
    "MaxInputGetFloat":       ("float",   ["name", "flag"]),
    "MaxInputGetInt":         ("int",     ["name", "flag"]),
    "MaxInputFVectorSpin":    ("FVECTOR", ["name", "flag", "def_x", "def_y", "def_z"]),
    "MaxInputFVector":        ("FVECTOR", ["name", "flag", "def_x", "def_y", "def_z"]),
    "MaxInputSVectorSpin":    ("SVECTOR", ["name", "flag", "def_x", "def_y", "def_z"]),
    "MaxInputSVector":        ("SVECTOR", ["name", "flag", "def_x", "def_y", "def_z"]),
    "MaxInputComboBox":       ("int",     ["name", "flag", "input", "def_v"]),
    "MaxInputListBox":        ("int",     ["name", "flag", "input", "def_v"]),
    "MaxInputHumanNum":       ("IVECTOR", ["name", "flag"]),
    "MaxInputSelectName":     ("int",     ["name", "flag"]),
    "MaxInputSlideBarF":      ("float",   ["name", "flag", "min", "max", "seg", "def_f"]),
    "MaxInputSlideBarI":      ("int",     ["name", "flag", "min", "max", "seg", "def_i"]),
    "MaxInputSelectColor":    ("IVECTOR", ["name", "flag", "def_r", "def_g", "def_b", "def_a"]),
    "MaxInputFloat":          ("float",   ["name", "flag", "def_f"]),
    "MaxInputInt":            ("int",     ["name", "flag", "def_i"]),
    "MaxInputStrCode":        ("int",     ["name", "flag", "def_s", "add_src"]),
    "MaxInputMessage":        ("int",     ["name", "flag"]),
    "MaxInputString":         ("char*",   ["name", "flag"]),   # -> char dN[128]
    "MaxInputPoint":          ("int",     ["name", "flag", "def_rx", "def_ry", "def_rz",
                                           "def_px", "def_py", "def_pz"]),
}

# English labels for the widget kinds (property-panel control to render).
WIDGET_UI = {
    "MaxInputFloat":          "float-edit",
    "MaxInputInt":            "int-edit",
    "MaxInputSlideBarF":      "float-slider",
    "MaxInputSlideBarI":      "int-slider",
    "MaxInputFVector":        "vec3-float",
    "MaxInputFVectorSpin":    "vec3-float-spin",
    "MaxInputSVector":        "vec3-short",
    "MaxInputSVectorSpin":    "vec3-short-spin",
    "MaxInputSelectColor":    "rgba-color",
    "MaxInputComboBox":       "combo",
    "MaxInputListBox":        "flags-listbox",
    "MaxInputStrCode":        "strcode-text",
    "MaxInputString":         "string-128",
    "MaxInputSelectName":     "scene-object-picker",
    "MaxInputHumanNum":       "model-joint-picker",
    "MaxInputGetLink":        "link-implicit",
    "MaxInputPoint":          "point-helper",
    "MaxInputMessage":        "internal-message",
    "MaxInputGetEffectName":  "implicit-effect-name",
    "MaxInputGetKeyRangeMin": "implicit-key-min",
    "MaxInputGetKeyRangeMax": "implicit-key-max",
    "MaxInputGetFrameRate":   "implicit-frame-rate",
    "MaxInputGetFloat":       "implicit-float",
    "MaxInputGetInt":         "implicit-int",
}

# Widgets with no dialog: the Max plugin fills these from scene state, the
# artist never types them.  Still occupy a slot in data[].
IMPLICIT = {
    "MaxInputGetKeyRangeMin", "MaxInputGetKeyRangeMax", "MaxInputGetEffectName",
    "MaxInputGetFrameRate", "MaxInputGetLink", "MaxInputGetFloat", "MaxInputGetInt",
}


# Glossary for the property panel. Machine-glossing only -- longest-match token
# substitution over the recovered Japanese. `*_en` fields are marked
# `enMachine: true` so a human can review; nothing here is invented, every entry
# is a term that actually occurs in the recovered labels.
GLOSSARY = [
    ("ランダムシード", "random seed"), ("ダミーポイント", "dummy point"),
    ("発生ポイント", "spawn point"), ("エフェクト", "effect"),
    ("コントロール", "control"), ("パラメータ", "parameter"),
    ("装備フラグ", "equip flags"), ("ＲＧＢ強度", "RGB intensity"),
    ("フレーム", "frame"), ("テクスチャ", "texture"), ("アルファ", "alpha"),
    ("スケール", "scale"), ("モデル", "model"), ("オフセット", "offset"),
    ("中心位置", "center position"), ("発生半径", "spawn radius"),
    ("最大", "max"), ("最小", "min"), ("初速", "initial velocity"),
    ("全画面", "fullscreen"), ("物理シム", "physics sim"),
    ("使用禁止", "DO NOT USE"), ("水しぶき", "splash"),
    ("カメラ", "camera"), ("モード", "mode"), ("サイズ", "size"),
    ("大きさ", "size"), ("半径", "radius"), ("名前", "name"),
    ("回転", "rotation"), ("位置", "position"), ("始点", "start point"),
    ("終点", "end point"), ("速度", "velocity"), ("時間", "time"),
    ("角度", "angle"), ("方向", "direction"), ("透明", "transparency"),
    ("明度", "brightness"), ("濃度", "density"), ("強度", "intensity"),
    ("装備", "equipment"), ("関節", "joint"), ("色", "color"),
    ("幅", "width"), ("数", "count"), ("血", "blood"), ("煙", "smoke"),
    ("風", "wind"), ("火", "fire"), ("光", "light"), ("影", "shadow"),
    ("音", "sound"), ("水", "water"), ("空", "sky"), ("雨", "rain"),
]


def gloss(s):
    """Longest-match token gloss. Returns None when nothing was recognised."""
    if not s:
        return None
    if all(ord(c) < 0x80 for c in s):
        return s
    out, i, hit = [], 0, False
    while i < len(s):
        for ja, en in GLOSSARY:
            if s.startswith(ja, i):
                out.append(en)
                i += len(ja)
                hit = True
                break
        else:
            out.append(s[i])
            i += 1
    return "".join(out).strip() if hit else None


def data_field_ctype(widget):
    """CreateEffect.c:138-149 -- char* -> char[128]; everything else -> scalar
    with the pointer level forced to 0."""
    ret = WIDGETS[widget][0]
    if ret == "char*":
        return "char", 128
    return ret, 0


# --------------------------------------------------------------------------
# 4. Parser  (ParseGrm.y)
# --------------------------------------------------------------------------

class Call:
    __slots__ = ("name", "args")
    def __init__(self, name, args):
        self.name = name
        self.args = args


class Parser:
    def __init__(self, text, path=""):
        self.lx = Lexer(text)
        self.path = path
        self.effects = []
        self.messages = []
        self.warnings = []
        self.func_ls = []
        self.arg_index = -1     # position within the current call's arg list

    # ---- expressions -----------------------------------------------------
    # Function-call nodes are appended to self.func_ls in *reduce* order, i.e.
    # post-order (innermost call first). That is exactly the order bison
    # populates parse_work.func_space (ParseGrm.y:1625), and CreateEffect.c
    # numbers the Data fields d<index-into-func_ls>.

    def p_primary(self):
        lx = self.lx
        if lx.accept("("):
            e = self.p_expr()
            lx.expect(")")
            return e
        if lx.peek() == '"':
            # arg 0 of a MaxInput* call is the Japanese label: an ASCII ',' can
            # never occur inside one (Japanese uses U+3001), so a ',' in the
            # scanned text means the closing quote was eaten and we ran on into
            # the next argument. Later args (model names, combo strings) are
            # ASCII and may legitimately contain ','.
            s, corrupt = lx.string(resync=",)", follow=",)",
                                   label=(self.arg_index == 0))
            return ("str", s, corrupt)
        n = lx.number()
        if n is not None:
            return ("num", n)
        if lx.s.startswith("%%", lx.i):
            lx.i += 2
            return ("stackvar", "%%")
        m = re.match(r"%\d+", lx.s[lx.i:])
        if m:
            lx.i += m.end()
            return ("stackvar", m.group())
        name = lx.ident()
        if name is None:
            raise SyntaxError(f"bad primary at {lx.context()}")
        return ("id", name)

    def p_postfix(self):
        lx = self.lx
        e = self.p_primary()
        while True:
            if lx.accept("("):
                args = []
                saved_ai = self.arg_index
                if lx.peek() != ")":
                    self.arg_index = 0
                    args.append(self.p_expr())
                    while lx.accept(","):
                        self.arg_index = len(args)
                        args.append(self.p_expr())
                self.arg_index = saved_ai
                lx.expect(")")
                if e[0] != "id":
                    raise SyntaxError("call on non-identifier")
                call = Call(e[1], args)
                self.func_ls.append(call)      # <-- post-order registration
                e = ("call", call)
            elif lx.accept("->"):
                lx.ident()
                e = ("member", e)
            elif lx.peek() == "." and not re.match(r"\.\d", lx.s[lx.i:]):
                lx.accept(".")
                lx.ident()
                e = ("member", e)
            else:
                return e

    def p_unary(self):
        lx = self.lx
        for op in ("&", "*", "+", "-", "~", "!"):
            if lx.peek() == op:
                lx.accept(op)
                return ("unary", op, self.p_unary())
        return self.p_postfix()

    def p_binary(self):
        # precedence climbing over the yacc chain: * / + - & ^ | && ||
        e = self.p_unary()
        while True:
            for op in ("&&", "||", "*", "/", "+", "-", "&", "^", "|"):
                if self.lx.s.startswith(op, self.skip_to()):
                    # don't mistake a lone & / | that begins && / ||
                    if op in ("&", "|") and self.lx.s.startswith(op * 2, self.lx.i):
                        continue
                    self.lx.i += len(op)
                    e = ("bin", op, e, self.p_unary())
                    break
            else:
                return e

    def skip_to(self):
        self.lx.skip_ws()
        return self.lx.i

    def p_expr(self):
        lx = self.lx
        save = lx.i
        # assignment:  %%  = cond   |   ident = cond
        if lx.s.startswith("%%", self.skip_to()):
            lx.i += 2
            if lx.accept("="):
                return ("assign", "%%", self.p_binary())
            lx.i = save
        else:
            m = IDENT_RE.match(lx.s, self.skip_to())
            if m:
                j = m.end()
                k = j
                while k < lx.n and lx.s[k] in " \t\r\n":
                    k += 1
                if k < lx.n and lx.s[k] == "=" and (k + 1 >= lx.n or lx.s[k + 1] != "="):
                    lx.i = k + 1
                    return ("assign", m.group(), self.p_binary())
        return self.p_binary()

    # ---- declarations ----------------------------------------------------

    def p_type(self):
        lx = self.lx
        save = lx.i
        lx.skip_ws()
        if lx.s.startswith("const", lx.i):
            lx.i += 5
            lx.skip_ws()
        m = IDENT_RE.match(lx.s, lx.i)
        if not m or m.group() not in TYPE_TOKENS:
            lx.i = save
            return None
        lx.i = m.end()
        return m.group()

    def p_options(self):
        """<id : 0x107> <no_birth> <add_src : "a.bmp,b.bmp"> ..."""
        opts = OrderedDict()
        lx = self.lx
        while lx.peek() == "<":
            lx.accept("<")
            key = lx.ident()
            val = True
            if lx.accept(":"):
                if lx.peek() == '"':
                    val, _ = lx.string(resync=">", follow=">")
                else:
                    toks = []
                    while lx.peek() not in (">", ""):
                        n = lx.number()
                        if n is not None:
                            toks.append(n)
                            continue
                        idt = lx.ident()
                        if idt is not None:
                            toks.append(idt)
                            continue
                        lx.i += 1
                    val = "".join(toks)
            lx.expect(">")
            # NB: ParseGrm.y:1258-1264 packs the option name into 4+4 chars,
            # so only the first 8 characters of the key are significant.
            opts[key] = val
        return opts

    def p_effect(self):
        lx = self.lx
        # '[' cannot occur inside a display name, so it is both the resync point
        # and a forbidden char (guards against the eaten-quote run-on).
        name_ja, corrupt = lx.string(resync="[", follow="[", forbid="[]{};\n")
        lx.expect("[")
        cname = lx.ident()
        if lx.accept("("):                    # optional function_arguments
            while lx.peek() not in (")", ""):
                lx.i += 1
            lx.expect(")")
        lx.expect("]")
        opts = self.p_options()

        self.func_ls = []
        members = []
        if lx.accept("{"):
            while lx.peek() != "}":
                if lx.eof():
                    raise SyntaxError("EOF in effect body")
                ty = self.p_type()
                if ty is None:
                    raise SyntaxError(f"bad member type at {lx.context()}")
                ptr = 0
                while lx.accept("*"):
                    ptr += 1
                mname = lx.ident()
                arr = None
                while lx.accept("["):
                    a = lx.number()
                    arr = int(a, 0) if a else 0
                    lx.expect("]")
                    ptr += 1
                if lx.accept("{"):
                    while lx.peek() != "}":
                        self.p_expr()
                        lx.expect(";")
                    lx.expect("}")
                lx.expect(";")
                members.append(dict(name=mname, ctype=ty, pointer=ptr, arrayLen=arr))
            lx.expect("}")

        # A description runs to ';'. If its closing quote was eaten the scan runs
        # on into the NEXT effect, which always crosses this effect's ';'.
        desc_ja, dcorrupt = ("", False)
        if lx.peek() == '"':
            desc_ja, dcorrupt = lx.string(resync=";", follow=";")
        lx.expect(";")

        self.effects.append(dict(
            displayName_ja=name_ja, displayName_corrupt=corrupt,
            cname=cname, options=opts, members=members,
            func_ls=list(self.func_ls),
            description_ja=desc_ja, description_corrupt=dcorrupt,
        ))

    _NEXT_DECL = re.compile(r"\n[ \t]*(?:effect|mesg)\b")

    def p_mesg(self):
        """mesg "EffectName" <name-literal> [ id ] $b:args... "comment" ;

        Both delimiters here sit directly after Japanese runs and so are prime
        victims of the bad conversion: the target's closing '"' AND the '[' of
        the id (seen: `強制オン[2]` -> `ｶｯﾀｩ･ｪ･<PUA>2]`). The message name is a
        bare, UNQUOTED Japanese NAME_LITERAL (ParseLex.l:327 -- {W}({W}|{A})*),
        so there is no reliable ASCII anchor to scan to.

        Rather than risk a resync running away and swallowing the NEXT effect
        (it did: 13 effects vanished that way), bound the statement to the text
        before the next top-level `effect`/`mesg` and regex within that slice.
        Messages are metadata; nothing here feeds the data[] byte layout.
        """
        lx = self.lx
        m = self._NEXT_DECL.search(lx.s, lx.i)
        end = m.start() if m else lx.n
        seg = lx.s[lx.i:end]
        lx.i = end

        strs = re.findall(r'"((?:\\.|[^\\"])*)"', seg, re.S)
        target = strs[0] if strs else ""
        comment = strs[-1] if len(strs) > 1 else ""
        mid = None
        idm = re.search(r"(\d+)\s*\]", seg)
        if idm:
            mid = int(idm.group(1))
        mname = ""
        if idm:
            pre = seg[:idm.start()]
            q = pre.rfind('"')
            mname = pre[q + 1:].strip().rstrip("[").strip() if q >= 0 else pre.strip()
        args = re.findall(r"\$[bwist]:\S+", seg)
        self.messages.append(dict(effect=target, name=mname, id=mid,
                                  args=args, comment=comment))

    def parse(self):
        lx = self.lx
        while not lx.eof():
            if lx.accept_kw("effect"):
                self.p_effect()
            elif lx.accept_kw("mesg"):
                self.p_mesg()
            else:
                # a plain C function prototype from an #include'd header
                ty = self.p_type()
                if ty is None:
                    lx.i += 1
                    continue
                while lx.accept("*"):
                    pass
                lx.ident()
                if lx.accept("("):
                    depth = 1
                    while depth and not lx.eof():
                        c = lx.s[lx.i]
                        depth += (c == "(") - (c == ")")
                        lx.i += 1
                lx.accept(";")
        return self


# --------------------------------------------------------------------------
# 5. Data-struct layout  (CreateEffect.c:129-154)
# --------------------------------------------------------------------------

def build_params(eff):
    """Walk func_ls in order; every MaxInput* entry contributes one Data field
    named d<index-in-func_ls>. Non-MaxInput calls (DM_*, sceVu0*) consume an
    index but emit no field -- which is why d-indices have holes."""
    params, off, maxalign = [], 0, 1
    for idx, call in enumerate(eff["func_ls"]):
        if not call.name.startswith("MaxInput"):
            continue
        if call.name not in WIDGETS:
            continue
        ctype, arrlen = data_field_ctype(call.name)
        size, align = LAYOUT[ctype]
        if arrlen:
            size *= arrlen
        off = (off + align - 1) // align * align
        maxalign = max(maxalign, align)

        argnames = WIDGETS[call.name][1]
        raw = [render_arg(a) for a in call.args]
        label_ja = raw[0] if raw and isinstance(raw[0], str) else ""
        named = OrderedDict(zip(argnames, raw))

        params.append(OrderedDict(
            name=f"d{idx}",
            member=owning_member(eff, idx),
            ctype=ctype,
            isPointer=False,
            arrayLen=arrlen or None,
            widget=call.name,
            ui=WIDGET_UI.get(call.name),
            implicit=call.name in IMPLICIT,
            widgetArgs=raw,
            widgetArgsNamed=named,
            label_ja=label_ja,
            default=extract_default(call.name, named),
            min=named.get("min"),
            max=named.get("max"),
            byteOffset=off,
            byteSize=size,
        ))
        off += size
    struct_size = (off + maxalign - 1) // maxalign * maxalign if off else 0
    return params, struct_size, maxalign


def owning_member(eff, idx):
    """Best-effort: which declared member's initializer this call belongs to.
    (Not needed for serialization; useful for grouping the property panel.)"""
    return None


def render_arg(a):
    t = a[0]
    if t == "str":
        return a[1]
    if t == "num":
        return num_value(a[1])
    if t == "unary" and a[1] == "-":
        v = render_arg(a[2])
        return -v if isinstance(v, (int, float)) else v
    if t == "bin":
        l, r = render_arg(a[2]), render_arg(a[3])
        if isinstance(l, (int, float)) and isinstance(r, (int, float)):
            try:
                return {"*": lambda: l * r, "/": lambda: l / r,
                        "+": lambda: l + r, "-": lambda: l - r,
                        "&": lambda: int(l) & int(r), "|": lambda: int(l) | int(r),
                        "^": lambda: int(l) ^ int(r)}[a[1]]()
            except (KeyError, ZeroDivisionError):
                return None
        return None
    if t == "id":
        return a[1]
    if t == "call":
        return f"{a[1].name}(...)"
    return None


def num_value(tok):
    t = tok.rstrip("uUlLfF")
    try:
        if t.lower().startswith("0x"):
            return int(t, 16)
        if re.fullmatch(r"0[0-7]+", t):
            return int(t, 8)
        if re.search(r"[.eE]", t):
            return float(t)
        return int(t)
    except ValueError:
        return None


def extract_default(widget, named):
    if widget in ("MaxInputFVector", "MaxInputFVectorSpin",
                  "MaxInputSVector", "MaxInputSVectorSpin"):
        return [named.get("def_x"), named.get("def_y"), named.get("def_z")]
    if widget == "MaxInputSelectColor":
        return [named.get("def_r"), named.get("def_g"),
                named.get("def_b"), named.get("def_a")]
    if widget == "MaxInputPoint":
        return dict(rot=[named.get("def_rx"), named.get("def_ry"), named.get("def_rz")],
                    pos=[named.get("def_px"), named.get("def_py"), named.get("def_pz")])
    for k in ("def_f", "def_i", "def_v", "def_s"):
        if k in named:
            return named[k]
    return None


# --------------------------------------------------------------------------
# 6. Driver
# --------------------------------------------------------------------------

def main():
    here = os.path.dirname(os.path.abspath(__file__))
    root = os.path.dirname(here)
    pre = argparse.ArgumentParser(add_help=False)
    pre.add_argument("--source", default=os.environ.get("MGS2_SOURCE_DIR"))
    src = pre.parse_known_args()[0].source
    effdir = os.path.join(src, "mgs2x", "source", "user", "mode", "demo", "effect") if src else None
    ap = argparse.ArgumentParser()
    ap.add_argument("--source", default=src,
                    help="root of the original source tree (default $MGS2_SOURCE_DIR)")
    ap.add_argument("--cnf", default=os.path.join(effdir, "demoeffect.cnf") if effdir else None,
                    help="structure source (the 248-effect HD-tree copy)")
    ap.add_argument("--pristine", default=os.path.join(src, "tools", "CreateEffect", "demoeffect.cnf")
                    if src else None,
                    help="clean EUC-JP copy used to recover Japanese text (78 effects)")
    ap.add_argument("--effects-dir", default=effdir)
    ap.add_argument("--out", default=os.path.join(os.environ.get("MGS2CS_DATA_DIR") or
                                                  os.path.join(root, "data"), "effects.json"))
    ap.add_argument("--validate", action="store_true")
    a = ap.parse_args()
    if not a.cnf or not a.effects_dir:
        ap.error("need --source <source tree> (or set MGS2_SOURCE_DIR), or --cnf and --effects-dir")

    text, enc = read_text(a.cnf)
    print(f"[cnf ] {a.cnf}\n       encoding={enc} chars={len(text)}")
    P = Parser(text, a.cnf).parse()
    print(f"[parse] effects={len(P.effects)} messages={len(P.messages)}")

    # pristine Japanese, keyed by C name
    # NB: keyed by ID, not cname. cname is NOT unique -- DM_SendEffectMessage is
    # declared at 4 different ids and NewRopeModel3_called at 8 (one launcher
    # reused with different params). The <id> is the only unique key.
    pristine = {}
    if a.pristine and os.path.exists(a.pristine):
        ptext, penc = read_text(a.pristine)
        Q = Parser(ptext, a.pristine).parse()
        for e in Q.effects:
            if "id" in e["options"]:
                pristine[int(str(e["options"]["id"]), 0)] = (
                    e["displayName_ja"], e["description_ja"])
        print(f"[clean] {a.pristine} encoding={penc} effects={len(Q.effects)}")

    # CreateEffect.c:507 stamps the effect's display name into the header comment
    # of every generated launcher (a one-line "launcher function for effect <name>"
    # banner in Japanese; the regex below matches it).
    # Those .c files were converted to UTF-8 CORRECTLY, so they carry all 245
    # display names in clean Japanese -- the .cnf's mojibake does not apply here.
    names_c = {}
    import glob as _g
    for p in _g.glob(os.path.join(a.effects_dir, "*.c")):
        base = os.path.basename(p)
        m = re.search(r"エフェクト[　\s]+(.+?)[　\s]+のランチャー関数",
                      open(p, encoding="utf-8", errors="replace").read())
        if m:
            names_c[base] = m.group(1).strip()
    print(f"[names] recovered {len(names_c)} clean display names from generated .c")

    out, seen = [], {}
    for e in P.effects:
        opts = e["options"]
        if "id" not in opts:
            P.warnings.append(f"{e['cname']}: no <id>")
            continue
        eid = int(str(opts["id"]), 0)
        params, size, align = build_params(e)

        nm, nq = demojibake(e["displayName_ja"])
        ds, dq = demojibake(e["description_ja"])
        src = "hd-tree(" + nq + ")"
        if eid in pristine:                 # clean EUC-JP .cnf (78-effect copy)
            nm, ds = pristine[eid]
            src, nq = "pristine-euc", "clean"
        srcfile = f"{eid:04x}_{e['cname']}.c"
        if srcfile in names_c:              # authoritative: the generator wrote it
            nm, src, nq = names_c[srcfile], "generated-c-header", "clean"

        for p in params:
            p["label_ja"], p["labelQuality"] = demojibake(p["label_ja"])
            p["label_en"] = gloss(p["label_ja"])
            if isinstance(p.get("default"), str):
                p["default"], _ = demojibake(p["default"])

        no_birth = "no_birth" in opts
        etype = exec_type_for(eid, dict(no_birth=no_birth))
        rec = OrderedDict(
            id=eid,
            hexid=f"0x{eid:x}",
            effectId=0x01000000 | eid,          # what actually appears in .sdt
            effectIdHex=f"0x{0x01000000 | eid:08X}",
            launcher=f"{e['cname']}_{eid:04x}Launch",
            sourceFile=f"{eid:04x}_{e['cname']}.c",
            cname=e["cname"],
            displayName_ja=nm,
            displayName_en=gloss(nm),
            description_ja=ds,
            description_en=None,      # too long to gloss usefully; translate on demand
            textSource=src,
            textQuality=nq,
            enMachine=True,
            options=OrderedDict(
                no_birth=no_birth,
                force_effect="force_effect" in opts,
                efct_on=int(str(opts["efct_on"]), 0) if "efct_on" in opts else None,
                efct_off=int(str(opts["efct_off"]), 0) if "efct_off" in opts else None,
                add_src=[s.strip() for s in re.split(r"[,\s]+", str(opts.get("add_src", "")))
                         if s.strip()],
            ),
            execType=etype,
            dataBaseOffset=0 if etype == 0 else 16,
            lifeField=None if etype == 0 else dict(byteOffset=0, ctype="int",
                                                   note="frames; data[1..3] reserved"),
            dataStructSize=size,
            dataStructAlign=align,
            packetBodySize=(16 + (0 if etype == 0 else 16)
                            + ((size + 15) // 16 * 16 if size else 0)),
            members=e["members"],
            params=params,
        )
        if eid in seen:
            P.warnings.append(f"duplicate id 0x{eid:x}")
        seen[eid] = rec
        out.append(rec)

    os.makedirs(os.path.dirname(a.out), exist_ok=True)
    with open(a.out, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=1)
    print(f"[out ] {a.out}  ({len(out)} effects)")
    for w in P.warnings:
        print("  ! " + w)

    if a.validate:
        validate(out, a.effects_dir)
    return out


def validate(effects, effdir):
    """Diff our predicted Data struct against the 245 .c files that the real
    ef_create emitted. This is the ground truth for the data[] byte layout."""
    ok = bad = missing = 0
    fails = []
    by_file = {e["sourceFile"]: e for e in effects}
    import glob
    files = sorted(glob.glob(os.path.join(effdir, "*.c")))
    for p in files:
        base = os.path.basename(p)
        s = open(p, encoding="utf-8", errors="replace").read()
        # a few files were hand-renamed in the HD port to avoid tag clashes:
        # the struct tag gains an id prefix (_000e_NewCigarette_Demo_data_t), so
        # match any tag ending in data_t whose typedef name is Data.
        m = re.search(r"typedef struct \w*data_t \{(.*?)\} Data", s, re.S)
        actual = []
        if m:
            for ty, nm, arr in re.findall(
                    r"(\w+)\s+\*?\s*d(\d+)\s*(\[\d+\])?\s*;", m.group(1)):
                actual.append((ty, int(nm), arr or ""))
        e = by_file.get(base)
        if e is None:
            missing += 1
            continue
        pred = [(p_["ctype"], int(p_["name"][1:]),
                 f"[{p_['arrayLen']}]" if p_["arrayLen"] else "")
                for p_ in e["params"]]
        if pred == actual:
            ok += 1
        else:
            bad += 1
            if len(fails) < 8:
                fails.append((base, pred, actual))
    print(f"\n[validate] against {len(files)} generated .c files in {effdir}")
    print(f"           MATCH={ok}  MISMATCH={bad}  no-cnf-entry={missing}")
    for b, p_, a_ in fails:
        print(f"  ! {b}\n      pred  ={p_}\n      actual={a_}")
    return bad == 0


if __name__ == "__main__":
    main()
