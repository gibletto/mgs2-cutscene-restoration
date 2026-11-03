#!/usr/bin/env python3
"""Write putobj_registry.json: the put-objects of the crash world, for the studio's viewport.

The studio draws a stage's put-objects as ghosts so shots can be framed against
them. It cannot read them out of a compiled scenario, so this lists them: every
put-object world.py creates, plus Konami's own city and bridge sets that the crash
leaves standing (the city moved to CITY_P by the world edit).

    python putobj_registry.py [out.json]      default <build>/manhattan-crash/putobj_registry.json

Point the studio at it with MGS2CS_PUTOBJ_REGISTRY.
"""
import json
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parents[1] / 'lib'))
sys.path.insert(0, str(HERE))

from mgs2 import config                   # noqa: E402
from mgs2.outputs import checked          # noqa: E402
from mgs2.strcode import strcode          # noqa: E402
from crash import models                  # noqa: E402

MODEL_CACHE = 0x0A000000                  # the model namespace of a cache id


def vec(s: str) -> list[float]:
    return [float(x) for x in s.split(',')]


def entries():
    out = []

    def add(name, model, p, s=(1000, 1000, 1000), r=(0, 0, 0), hidden=False):
        if any(e['name'] == name for e in out):
            return                                        # vw60a_7: ours, already listed
        out.append({'name': name, 'code': strcode(name), 'model': model,
                    'modelCacheId': MODEL_CACHE | strcode(model),
                    'p': list(p), 'r': list(r), 's': list(s), 'hidden': hidden})

    for name, model, pos, scale, rot, hide in models.PUTOBJS:
        add(name, model, vec(pos), vec(scale), vec(rot), hide)

    city = vec(models.CITY_P)
    for n in ['w60a_%d' % i for i in (1, 2, 3, 4, 5, 6, 8)] + ['w60a_7']:
        add(n, n, city)                                   # skyline + intact land
    for n in ('vw60a_7', 'vw60a_9', 'w60a_amg'):
        add(n, n, city, hidden=True)                      # breach / wreck / beached hull
    bridge = 'ベラザノ橋_w10a_'                            # "Verrazzano bridge"
    for n in ('1_hi_reso', '2_hi_reso', '3_hi_reso', '4_hi_reso', '7_hi_reso', '5', '6'):
        add(bridge + n, 'w10a_' + n, (0, 0, 0))
    add(bridge + '8', 'w10a_8', (0, 0, 0), s=(100, 100, 100))
    return out


def main():
    dst = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else \
        config.build_dir() / 'manhattan-crash' / 'putobj_registry.json'
    e = entries()
    checked(dst).write_text(json.dumps({'putobjs': e}, indent=1))
    print(f'registry: {len(e)} put-objects -> {dst}')


if __name__ == '__main__':
    main()
