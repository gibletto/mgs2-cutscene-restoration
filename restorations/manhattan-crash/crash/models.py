"""Every model the restoration places or registers, and where it comes from.

`PUTOBJS` is the scenario's side: the put-objects world.py creates in stage
d080p07, in creation order. `MODELS` is the asset side: every model the stage
lists register, in list order (which is load order). The two differ because some
models are created by Konami's own procs, some are only referenced by effects
(debris), and three are registered but no longer placed (left from drafts; kept so
the build reproduces the release exactly).

Sources:
  'game'   copied from the game's own assets/kms/us
  'assets' supplied by you in MGS2_ASSETS/manhattan-crash (see ASSETS.md)
"""

CITY_P = '-25000,1000,-500000'      # the city set's -p: where the whole complex now stands
STATUE_SCALE = '900,900,900'        # Liberty Island and her statue, x0.9 of the city scale


# (putobj name, model, -p, -s, -r, hidden at init)
def _p(name, model, pos, scale='1000,1000,1000', rot='0,0,0', hide=True):
    return (name, model, pos, scale, rot, hide)


PUTOBJS = [
    # the breach city: Manhattan with the Battery torn open, shown from the bow impact
    _p('vw60a_7', 'vw60a_7', CITY_P),
    # the towers the bow topples (base-centred so -r pivots on the footing) and the
    # window-light layers of two of them, which flicker separately
    _p('w60a_smash', 'w60a_smash', '-51425,-4200,-404605'),
    _p('w60a_smash2', 'w60a_smash2', '-47795,-4200,-401575'),
    _p('w60a_smash3', 'w60a_smash3', '-55865,-9950,-409545'),
    _p('w60a_smash4', 'w60a_smash4', '-67050,-10820,-411855'),
    _p('w60a_smash5', 'w60a_smash5', '-71985,-5950,-407295'),
    _p('w60a_smash6', 'w60a_smash6', '-54800,-16450,-420395'),
    _p('w60a_smash7', 'w60a_smash7', '-43860,-4200,-403920'),
    _p('w60a_smash_lw', 'w60a_smash_lw', '-51425,-4200,-404605'),
    _p('w60a_smash7_lw', 'w60a_smash7_lw', '-43860,-4200,-403920'),
    # the Battery's trees, swept along by the bow
    _p('w60a_tree1', 'w60a_tree1', '-44930,-3780,-383120', hide=False),
    _p('w60a_tree2', 'w60a_tree1', '-54400,-3780,-385689', hide=False),
    _p('w60a_tree3', 'w60a_tree1', '-60863,-3780,-387788', hide=False),
    _p('w60a_tree4', 'w60a_tree1', '-54699,-3780,-393373', hide=False),
    _p('w60a_tree5', 'w60a_tree1', '-61671,-3780,-395439', hide=False),
    _p('w60a_tree6', 'w60a_tree1', '-60034,-3780,-400536', hide=False),
    _p('w60a_tree7', 'w60a_tree1', '-63957,-3448,-403904', hide=False),
    _p('w60a_tree8', 'w60a_tree1', '-64857,-3448,-405004', hide=False),
    _p('w60a_tree9', 'w60a_tree1', '-63157,-3448,-403104', hide=False),
    # Liberty Island: the island, the pedestal shaft (split off so it can lean and
    # fall), the statue whole, the statue torn from her feet, the stump left behind,
    # and a mirrored reflection of each piece under the waterline
    _p('w60a_sol', 'w60a_sol', '-102100,240,-340500', STATUE_SCALE, '0,683,0', hide=False),
    _p('w60a_solst', 'w60a_solst', '-102100,6312,-340500', STATUE_SCALE, hide=False),
    _p('w60a_solb', 'w60a_solb', '-102100,6312,-340500', STATUE_SCALE, hide=False),
    _p('w60a_solw', 'w60a_solw', '-102100,6312,-340500', STATUE_SCALE, hide=False),
    _p('w60a_solp', 'w60a_solp', '-102100,240,-340500', STATUE_SCALE, hide=False),
    _p('w60a_solr', 'w60a_solr', '-102100,240,-340500', STATUE_SCALE, '0,683,0', hide=False),
    _p('w60a_solpr', 'w60a_solpr', '-102100,240,-340500', STATUE_SCALE, '0,-341,0', hide=False),
    _p('w60a_solwr', 'w60a_solwr', '-102100,6312,-340500', STATUE_SCALE, '0,-341,0', hide=False),
    # a New Jersey shoreline town for the tour's far bank: one building cluster,
    # placed nine times, turned so it does not read as a copy
    _p('w60a_njb1', 'w60a_njb', '-257453,2000,-304950', hide=False),
    _p('w60a_njb2', 'w60a_njb', '-240000,2000,-255000', rot='0,1024,0', hide=False),
    _p('w60a_njb3', 'w60a_njb', '-266000,2000,-352000', rot='0,2048,0', hide=False),
    _p('w60a_njb4', 'w60a_njb', '-285000,2000,-290000', rot='0,512,0', hide=False),
    _p('w60a_njb5', 'w60a_njb', '-295000,2000,-330000', rot='0,3072,0', hide=False),
    _p('w60a_njb6', 'w60a_njb', '-278000,2000,-240000', rot='0,2560,0', hide=False),
    _p('w60a_njb7', 'w60a_njb', '-305000,2000,-370000', rot='0,1536,0', hide=False),
    _p('w60a_njb8', 'w60a_njb', '-262000,2000,-390000', rot='0,1024,0', hide=False),
    _p('w60a_njb9', 'w60a_njb', '-248000,2000,-222000', rot='0,2048,0', hide=False),
    # dawn: cloud sheets and a sun billboard NE of the harbour (created hidden)
    _p('w60a_dawn2', 'w60a_cld1', '150000,-1000,-660000', rot='0,1536,0'),
    _p('w60a_dawn3', 'w60a_cld2', '110000,500,-640000', '800,800,800', '0,1536,0'),
    _p('w60a_sunq', 'w60a_sunq', '130000,1500,-655000', '250,250,250', '0,1536,0'),
    # the wreck: chunks knocked loose by the hull that right themselves (and their
    # skins), the side view's wreck with holes, and the wreck the overhead shows
    _p('vw60a_9_far2', 'vw60a_9_far2', CITY_P),
    _p('vw60a_9_far3_a', 'vw60a_9_far3_a', CITY_P),
    _p('vw60a_9_far3_b', 'vw60a_9_far3_b', CITY_P),
    _p('vw60a_9_far3_c', 'vw60a_9_far3_c', CITY_P),
    _p('vw60a_9_holes', 'vw60a_9_holes', CITY_P),
    _p('vw60a_9_crash', 'vw60a_9_crash', CITY_P),
    _p('vw60a_9_far3_a_skin', 'vw60a_9_far3_a_skin', CITY_P),
    _p('vw60a_9_far3_b_skin', 'vw60a_9_far3_b_skin', CITY_P),
    _p('vw60a_9_far3_c_skin', 'vw60a_9_far3_c_skin', CITY_P),
    # the intact city with Brooklyn behind it, standing in for Konami's w60a_7
    _p('w60a_7_brooklyn', 'w60a_7_brooklyn', CITY_P, hide=False),
]

# (model, source, what it is)
MODELS = [
    ('vw60a_7', 'assets', 'Manhattan, breached at the Battery'),
    ('vw60a_9_far', 'assets', 'wreck, far chunk (registered, not placed)'),
    ('vw60a_9_far2', 'assets', 'wreck chunk that tilts back onto its rest'),
    ('vw60a_9_far3', 'assets', 'wreck chunk, unsplit (registered, not placed)'),
    ('vw60a_9_far3_a', 'assets', 'wreck chunk a, slides home'),
    ('vw60a_9_far3_a_skin', 'assets', 'skin of chunk a'),
    ('vw60a_9_far3_b', 'assets', 'wreck chunk b, slides home'),
    ('vw60a_9_far3_b_skin', 'assets', 'skin of chunk b'),
    ('vw60a_9_far3_c', 'assets', 'wreck chunk c, slides home'),
    ('vw60a_9_far3_c_skin', 'assets', 'skin of chunk c'),
    ('vw60a_9_holes', 'assets', 'the wreck as the side view sees it'),
    ('vw60a_9_near', 'assets', 'wreck, near chunk (registered, not placed)'),
    ('w04a1_brk_wall_hahen', 'game', 'wall rubble: debris effect objcode 0xc8e2ba'),
    ('w04b1_brk', 'game', 'slab rubble: debris effect objcode 0xb9a472'),
    ('w32a_sky_wall2', 'game', 'sky wall the dawn clouds hang on'),
    ('w32a_sky_wall3', 'game', 'sky wall the dawn clouds hang on'),
    ('w60a_7_brooklyn', 'assets', 'intact Manhattan land with Brooklyn behind it'),
    ('w60a_cld1', 'assets', 'dawn cloud sheet'),
    ('w60a_cld2', 'assets', 'dawn cloud sheet'),
    ('w60a_njb', 'assets', 'New Jersey building cluster'),
    ('w60a_smash', 'assets', 'tower 1, the first the bow topples'),
    ('w60a_smash2', 'assets', 'tower 2'),
    ('w60a_smash3', 'assets', 'tower 3, bulldozed aside'),
    ('w60a_smash4', 'assets', 'tower 4, shoved and toppling'),
    ('w60a_smash5', 'assets', 'tower 5, the small one that crumples'),
    ('w60a_smash6', 'assets', 'tower 6, legs punched out'),
    ('w60a_smash7', 'assets', 'tower 7, settles straight down'),
    ('w60a_smash7_lw', 'assets', 'tower 7 window lights'),
    ('w60a_smash_lw', 'assets', 'tower 1 window lights'),
    ('w60a_sol', 'assets', 'Liberty Island'),
    ('w60a_solb', 'assets', 'the statue\'s torn feet and gown, left on the pedestal'),
    ('w60a_solp', 'assets', 'the pedestal shaft'),
    ('w60a_solst', 'assets', 'the statue torn from her feet (origin at the feet)'),
    ('w60a_solw', 'assets', 'the statue whole'),
    ('w60a_sunq', 'assets', 'sun billboard'),
    ('w60a_tree1', 'assets', 'Battery tree'),
    ('vw60a_9_crash', 'assets', 'the wreck the overhead shot shows'),
    ('w60a_solr', 'assets', 'island reflection'),
    ('w60a_solpr', 'assets', 'pedestal reflection'),
    ('w60a_solwr', 'assets', 'statue reflection'),
]


def check():
    """Every placed model is registered."""
    registered = {m for m, _, _ in MODELS}
    placed = {model for _, model, *_ in PUTOBJS}
    missing = placed - registered
    assert not missing, f'placed but not registered: {sorted(missing)}'


check()
