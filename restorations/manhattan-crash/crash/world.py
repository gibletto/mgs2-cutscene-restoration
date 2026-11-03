"""The world: stage d080p07's scenario, edited for the crash.

Stage d080p07 plays the bridge flyover cutscene. Its scenario already carries
Konami's Manhattan: the skyline sets and the intact city land, created at init
and switched off, and -- commented out of init -- the crash kit, the wrecked city
land (vw60a_9) and the beached Arsenal (アーセナルギア, model w60a_amg). This
edit puts the city where the extended cutscene needs it and wires the kit back:

  1  the whole city complex moves to CITY_P (every placement at the origin next
     to a w60a_ / vw60a_ model)
  2  the far fog plane goes from 290,000 to 700,000, or the city is fogged out
  3  the sea is stretched over the bridge, the whole run and past the city
  4  the city is switched ON at init (the set procs created it and turned it off)
  5  the two init calls that create the crash kit are uncommented, and both are
     hidden right after (their own "off" lines are commented out in the source,
     so the wreck would z-fight the intact city and the hull would sit there
     from the start)
  6  a new proc, manhattan_crash, switches intact -> wrecked + beached hull
  7  every new put-object (models.PUTOBJS) is created at init, inside the proc
     that builds the intact city -- a new proc would be dead-stripped
  8  manhattan_crash is added to the cutscene's -p whitelist (at most 16)

The cutscene drives nearly everything by messaging the put-objects directly
(effect 0x8000, by name hash); see author.py.
"""
from __future__ import annotations

import pathlib

from . import models

GCL = 'P080_13_p07b.w51a.gcl'
STAGE = 'scenerio_stage_d080p07'

FOG_FAR = (b'-f  290000', b'-f  700000')
SEA_SIZE = (b'-size 400000 400000', b'-size 900000 1250000')
SEA_POS = (b'-pos 60000,0,-200000', b'-pos 0,0,-225000')


def e(s: str) -> bytes:
    return s.encode('euc-jp')


PUTOBJ = e('プットオブジェ')            # "put object": the static-model actor


def putobj(name: str, model: str, pos: str, scale: str, rot: str, hide: bool,
           flags: bytes = b'0x0010') -> list[bytes]:
    """One put-object, as the stage's own sets are written. -f is putobj's status
    bitmask: 0x0010 = TRANS_SCALE, which is what makes -s apply."""
    out = [b'',
           b'\t\tchara ' + PUTOBJ + b' ' + name.encode() + b' \\',
           b'',
           b'\t\t\t-m ' + model.encode() + b' -r ' + rot.encode() + b' -p ' + pos.encode() + b' \\',
           b'',
           b'\t\t\t-s ' + scale.encode() + b' -l d:LT2_NAME_100 \\',
           b'',
           b'\t\t\t-f ' + flags,
           b'']
    if hide:
        out += [b'\t\tmesg ' + PUTOBJ + b' ' + name.encode() + b' off', b'']
    return out


def edit_gcl(path: pathlib.Path, city_p: str = models.CITY_P):
    raw = path.read_bytes()
    lines = raw.split(b'\n')
    pos = ('-p %s' % city_p).encode()

    # 1) the city complex moves together: skyline, both land models, the beached
    #    hull. NB '-m vw60a_7' does not contain '-m w60a_', so the v-prefixed
    #    models are listed explicitly.
    def near_model(i):
        for k in range(max(0, i - 3), min(len(lines), i + 3)):
            if (b'-m w60a_' in lines[k] or b'-m vw60a_9' in lines[k] or b'-m vw60a_7' in lines[k]
                    or b'-m w60a_amg' in lines[k]):
                return True
        return False
    moved = 0
    for i, ln in enumerate(lines):
        if b'-p 0,0,0' in ln and near_model(i):
            lines[i] = ln.replace(b'-p 0,0,0', pos)
            moved += 1

    # 2) fog: the city's near edge is ~414k from the bridge cameras; the stock far
    #    plane would swallow it. 700k leaves a hazy silhouette on the horizon.
    for i, ln in enumerate(lines):
        if FOG_FAR[0] in ln:
            lines[i] = ln.replace(*FOG_FAR)

    # 3) the sea covers bridge -> Arsenal's whole run -> past the city's far edge
    for i, ln in enumerate(lines):
        if SEA_SIZE[0] in ln:
            lines[i] = ln.replace(*SEA_SIZE)
        if SEA_POS[0] in ln:
            lines[i] = ln.replace(*SEA_POS)

    # 4) the intact city visible from init: inside the skyline and intact-land set
    #    procs, flip the put-object "off" messages to "on"
    cur, flipped = None, 0
    for i, ln in enumerate(lines):
        s = ln.strip()
        if s.startswith(e('proc ')):
            cur = s
        if cur and (e('マンハッタン遠景セット') in cur or e('マンハッタン壊れ前陸地') in cur):  # skyline set / intact land
            if e('mesg ') + PUTOBJ in ln and b' w60a_' in ln and ln.rstrip().endswith(b' off'):
                lines[i] = ln[:ln.rfind(b' off')] + b' on'
                flipped += 1

    # 5) the crash kit: uncomment the init calls that create the wrecked land and the
    #    beached Arsenal (not their "on" procs), then hide both right after
    uncommented = 0
    for i, ln in enumerate(lines):
        if (ln.lstrip().startswith(b'//') and (e('@マンハッタン壊れ後陸地') in ln or e('@アーセナルセット') in ln)
                and e('オン') not in ln):                    # wrecked land / Arsenal set; not "...on"
            lines[i] = ln.replace(b'//', b'', 1)
            uncommented += 1
    zfix = 0
    for i, ln in enumerate(lines):
        if ln.strip() == e('@アーセナルセット'):
            lines[i + 1:i + 1] = [b'\tmesg ' + PUTOBJ + b' vw60a_9 off',
                                  b'\tmesg ' + PUTOBJ + b' ' + e('アーセナルギア') + b' off']
            zfix = 1
            break

    # 6) the crash switch, after Konami's own "wrecked land on" proc
    proc = [b'',
            b'\tproc manhattan_crash {',
            b'',
            b'\t\tmesg ' + PUTOBJ + b' w60a_7 off',
            b'',
            b'\t\tmesg ' + PUTOBJ + b' vw60a_9 on',
            b'',
            b'\t\tmesg ' + PUTOBJ + b' ' + e('アーセナルギア') + b' on',
            b'',
            b'\t}',
            b'']
    inserted = False
    for i, ln in enumerate(lines):
        if e('proc マンハッタン壊れ後陸地オン') in ln:
            for j in range(i + 1, min(len(lines), i + 10)):
                if b'}' in lines[j]:
                    lines[j + 1:j + 1] = proc
                    inserted = True
                    break
            break

    # 7) the new put-objects, inlined into the intact-land set proc, which runs at
    #    init. Anchor on the proc's own header: step 4 has already flipped its
    #    "w60a_7 off" line, so the only other match would be inside manhattan_crash.
    #    Every one carries an explicit -p -- step 1 has already run.
    created = 0
    for i, ln in enumerate(lines):
        if ln.strip() == e('proc マンハッタン壊れ前陸地 {'):
            body = []
            for p in models.PUTOBJS:
                body += putobj(*p)
            lines[i + 1:i + 1] = body
            created = 1
            break

    # 8) whitelist the switch in the cutscene's -p list, on the continuation line of
    #    the bridge/Manhattan switch proc
    wl = 0
    for i, ln in enumerate(lines):
        s = ln.strip()
        key = e('ベラザノ橋マンハッタン切り替え')                  # Verrazzano bridge / Manhattan switch
        if s.startswith(key) and s.endswith(b'\\') and b'//' not in ln.split(b'\\')[0]:
            lines[i] = ln.replace(key, key + b' manhattan_crash', 1)
            wl += 1

    path.write_bytes(b'\n'.join(lines))
    print(f'   moved={moved} flipped_on={flipped} init_uncommented={uncommented} zfix={zfix} '
          f'proc_inserted={inserted} whitelisted={wl} putobjs={len(models.PUTOBJS) if created else 0}')
    assert moved >= 10 and flipped >= 2 and uncommented == 2 and zfix == 1 and inserted and wl >= 1, \
        'the edit did not land as expected'
    assert created == 1, 'the new put-objects were never created at init'


def edit(scn: pathlib.Path):
    """The edit callback for mgs2.scenario.build()."""
    hits = list(scn.rglob(GCL))
    assert len(hits) == 1, hits
    edit_gcl(hits[0])
