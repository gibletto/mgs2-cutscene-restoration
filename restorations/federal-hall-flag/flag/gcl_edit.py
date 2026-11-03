"""The scenario edits: put Konami's deleted flag back, and add ours.

The flag was cut from the source, not just switched off. Two files hold the same
cut, each marked with the same deletion note (テロ事件に配慮し削除, "removed in
consideration of the terrorist attacks"):

  w61a_common.h          wraps the flag's `chara` call in /* ... */
                         (shared by the Federal Hall cutscenes and the boss fight)
  P082_01_p01.w61a.gcl   comments the same call out line by line (the ending stage)

The source tree is EUC-JP, so everything here reads and writes that encoding.

On top of the restored line this adds, when asked:
  --slowmo   one copy of the flag per wave rate, for the shots that run slow
  --poles    the eleven hanging flags along the street
"""
from __future__ import annotations

import pathlib
import re

from . import poles, timing

ENC = 'euc-jp'
CUT_MARK = 'テロ事件に配慮し削除'        # the deletion note
CHARA = 'プット頂点アニメオブジェ'       # "put vertex-anime object": the keyframed-mesh actor
MESG = f'mesg {CHARA}'
CONT = '\\'                              # GCL line continuation

# Konami wrote -b 5. The PS2 stepped the wave every two demo frames and the port
# steps it every frame, so it runs at double speed; -b 11 gives back the PS2 rate.
DEFAULT_BASE = 11

# proc names the scenario already has (from the deleted feature)
PROC_FLAG_ON = '旗オン'                  # flag on
PROC_FLAG_OFF = '旗オフ'                 # flag off
PROC_STATIC_ON = '静止星条旗オン'        # fallen (static) flag on
PROC_STATIC_OFF = '静止星条旗オフ'       # fallen (static) flag off
PROC_SETUP = 'フェデラルホール旗設置'    # "Federal Hall flag setup", runs at stage init


def _read(path: pathlib.Path) -> str:
    return path.read_bytes().decode(ENC)


def _write(path: pathlib.Path, text: str):
    path.write_bytes(text.encode(ENC))


def set_base(line: str, base: int) -> str:
    return re.sub(r'(-b\s+)\d+', rf'\g<1>{base}', line)


def restore(path: pathlib.Path, base: int = DEFAULT_BASE) -> bool:
    """Uncomment the flag's chara call, in whichever of the two forms the file uses."""
    out, changed, in_block, in_call = [], False, False, False

    for line in _read(path).split('\n'):
        stripped = line.strip()

        # form 1: the whole call wrapped in /* ... */
        if CUT_MARK in line and stripped.startswith('/*'):
            in_block, changed = True, True           # drop the opening comment
            continue
        if in_block:
            if stripped.startswith('*/'):
                in_block = False                      # and its closing
                continue
            out.append(set_base(line, base))
            continue

        # form 2: the call commented line by line
        if in_call or (stripped.startswith('//') and CHARA in line and 'chara' in line):
            if not stripped.startswith('//'):
                raise SystemExit(f'{path}: flag call continues into a live line: {line!r}')
            line = set_base(line.replace('//', '', 1), base)
            in_call = line.rstrip().endswith(CONT)
            out.append(line)
            changed = True
            continue

        out.append(line)

    if in_call:
        raise SystemExit(f'{path}: flag call never ended')
    if changed:
        _write(path, '\n'.join(out))
    return changed


def add_slow_flags(path: pathlib.Path) -> bool:
    """One copy of the flag per slow rate; the cutscene swaps which one is visible.

    Nothing can change a vertex animation's rate once it runs -- only show/hide.
    """
    text = _read(path)
    if any(a in text for a in timing.ACTORS.values()):
        return False

    created = f'\tchara {CHARA} {timing.FLAG_ACTOR} \\'
    if created not in text:
        raise SystemExit(f'{path}: the flag has not been restored yet')

    everyone = {'normal': timing.FLAG_ACTOR, **timing.ACTORS}

    # the twins go straight after the flag itself, each hidden as it is made so
    # that only ever one of them is up
    twins = ['\n\n\t// the same flag again at the slower rates, for the shots that run slow']
    for rate, actor in timing.ACTORS.items():
        base, rand, why = timing.RATES[rate]
        twins.append(f'\tchara {CHARA} {actor} \\\n'
                     f'\t\t-m d:FLAG_NAME \\\n'
                     f'\t\t-b {base} \\\n'
                     f'\t\t-w {rand} \\\n'
                     f'\t\t-p 0,0,0\t// {timing.ticks(rate)} ticks a key: {why}\n'
                     f'\t{MESG} {actor} 非表示')                       # 非表示 = hide
    end = text.index('-p 0,0,0', text.index(created)) + len('-p 0,0,0')
    text = text[:end] + '\n'.join(twins) + text[end:]

    # hiding the flag hides all of them, whichever was up at the time
    hide_all = '\n            '.join(f'{MESG} {a} 非表示' for a in everyone.values())
    text = text.replace(f'proc {PROC_FLAG_OFF} {{ {MESG} {timing.FLAG_ACTOR} 非表示 }}',
                        f'proc {PROC_FLAG_OFF} {{ {hide_all} }}')

    # and one proc per rate, each hiding the others so a missed call cannot leave
    # two flags overlapping
    procs = []
    for rate, proc in timing.PROCS.items():
        show = everyone[rate]
        body = [f'{MESG} {a} 非表示' for r, a in everyone.items() if r != rate]
        body.append(f'{MESG} {show} 表示')                                # 表示 = show
        pad = ' ' * len(f'proc {proc} {{ ')
        procs.append(f'proc {proc} {{ ' + f'\n{pad}'.join(body) + ' }')

    anchor = text.index(f'proc {PROC_FLAG_OFF} {{')
    end = text.index('}', anchor) + 1
    text = text[:end] + '\n\n' + '\n'.join(procs) + text[end:]

    # DM_ExecProc can only reach procs the demo was started with, so the new ones
    # join the -p list on every `chara demo` in the file (the limit is 16)
    listed = f'{PROC_FLAG_ON} {PROC_FLAG_OFF} {PROC_STATIC_ON} {PROC_STATIC_OFF}'
    if listed not in text:
        raise SystemExit(f'{path}: cannot find the demo proc list to register the swaps in')
    added = ' '.join(timing.PROCS.values())
    text = text.replace(listed, f'{PROC_FLAG_ON} {PROC_FLAG_OFF} {added} '
                                f'{PROC_STATIC_ON} {PROC_STATIC_OFF}')
    _write(path, text)
    return True


def add_pole_flags(path: pathlib.Path) -> bool:
    """Add the street flags to the proc that raises the main one.

    It runs once at stage setup, which is where dressing belongs. The defines go
    directly above the proc, not beside FLAG_NAME: that lives in the including
    file, which w61a.gcl is not, so anchoring there put them out of scope.
    """
    text = _read(path)
    if poles.MODELS[0] in text:
        return False

    marker = f'proc {PROC_SETUP} {{'
    if marker not in text:
        raise SystemExit(f'{path}: no {PROC_SETUP} proc to hang them in')

    # one define per drape variant, on the lines directly above the proc
    head = text[:text.index(marker)].rsplit('\n', 1)
    defines = '\n'.join(f'#define\t\tHANG_FLAG_NAME{k}\t{m}\t// street pole drape {k}'
                        for k, m in enumerate(poles.MODELS))
    text = head[0] + '\n' + defines + '\n' + head[1] + text[text.index(marker):]

    # The proc's own closing brace, matched on its indentation. w61a_common.h nests
    # its procs one tab in, so a plain '\n}' search runs straight past the end of
    # the proc and drops the calls into whatever block comes next -- where they
    # compile to nothing at all.
    start = text.index(marker)
    indent = text[:start].split('\n')[-1]
    end = text.index('\n' + indent + '}', start + len(marker))
    text = (text[:end] + '\n\n\t// the flags along the street, hanging from their poles\n'
            + poles.gcl_lines() + '\n' + text[end:])
    _write(path, text)
    return True


def make_edit(base: int = DEFAULT_BASE, slowmo: bool = True, pole_flags: bool = True):
    """The edit callback for mgs2.scenario.build()."""
    def edit(scn: pathlib.Path):
        ticks = sorted({((base + r + 1) * 5) // 6 for r in range(2)})
        for name in ('w61a_common.h', 'P082_01_p01.w61a.gcl'):
            for f in scn.rglob(name):
                print(f'   {"restored" if restore(f, base) else "no change"}: {f.name}')
        print(f'   wave: -b {base} -> {ticks} ticks per key '
              f'(Konami wrote -b 5 = 5 ticks, which runs at double rate on the port)')

        if slowmo:
            for f in scn.rglob('P082_01_p01.w61a.gcl'):
                if add_slow_flags(f):
                    rates = ', '.join(f'{r} at {timing.ticks(r)} ticks/key' for r in timing.ACTORS)
                    print(f'   slow motion: {len(timing.ACTORS)} more flags ({rates}) '
                          f'for {len(timing.SLOW_WINDOWS)} windows')
        if pole_flags:
            # both copies of the setup proc: the ending stage's own, and the shared one
            for name in ('P082_01_p01.w61a.gcl', 'w61a_common.h'):
                for f in scn.rglob(name):
                    if add_pole_flags(f):
                        print(f'   street poles: {len(poles.POLES)} hanging flags added to {f.name}')
    return edit
