"""Register new assets with a stage, through MGSHDFix's supplementary lists.

The game keeps files under real names but caches them by GV_StrCode of the name,
printed as eight hex digits. manifest.txt and bp_assets.txt map one to the other,
per stage, and MGSHDFix merges any manifest_*.txt / bp_assets_*.txt beside them.

Each line is: source path, load path, cache key.
"""
import pathlib

from mgs2.strcode import cache_id

STAGE = 'd082p01'               # the ending demos
EARLY_STAGE = 'd080p08'         # the Federal Hall cutscenes before them
BOSS_STAGE = 'w61a'             # the playable Solidus fight on the same roof
REGIONS = ('eu', 'jp')          # the only trees that carry these stages


def lines(model: str, region: str, *, kms: str, tri: str | None, texture: str | None,
          cv2: str | None = None, anm: str | None = None,
          stage: str = STAGE) -> tuple[str, str]:
    """The lines registering `model`.

    tri/texture are None where the stage hangs the artwork off the map group
    rather than per model -- publishing our own there points the mesh at a group
    the stage hasn't got, and nothing draws.
    """
    code = cache_id(model)
    manifest = [
        f'assets/kms/us/{kms}.kms,us/stage/{stage}/cache/{code}.kms,cache/{code}.kms',
    ]
    if tri:
        manifest.insert(0, f'assets/tri/us/{tri}.tri,us/stage/{stage}/cache/{code}.tri,'
                           f'cache/{code}.tri')
    if cv2:
        manifest.append(
            f'assets/cv2/us/{cv2}.cv2,us/stage/{stage}/cache/{code}.cv2,cache/{code}.cv2')
    if anm:
        manifest.append(
            f'assets/anm/us/{anm}.anm,us/stage/{stage}/cache/{code}.anm,cache/{code}.anm')

    bp_assets = [
        f'assets/kms/us/{kms}.cmdl,us/stage/{stage}/cache/{code}.cmdl,'
        f'{region}/stage/{stage}/cache/{code}.cmdl',
    ]
    if texture:
        bp_assets.append(
            f'textures/flatlist/{texture}.bmp.ctxr,stage/{stage}/cache/{texture}.bmp.ctxr,'
            f'{region}/stage/{stage}/cache/{code}/{cache_id(texture)}.ctxr')
    return manifest, bp_assets


def write(dist: pathlib.Path, models: list[dict], tag: str = 'flagrestore',
          stage: str = STAGE):
    """Write one pair of supplementary lists per region, covering every model."""
    written = []
    for region in REGIONS:
        d = dist / region / 'stage' / stage
        d.mkdir(parents=True, exist_ok=True)
        manifest, bp_assets = [], []
        for m in models:
            a, b = lines(region=region, stage=stage, **m)
            manifest += a
            bp_assets += b
        # The game's own lists end lines with \r\r\n, a quirk of whatever wrote
        # them. MGSHDFix's merger discards CR and re-emits \n, so a plain \n file
        # is what it expects and cannot carry a stray CR into a name.
        (d / f'manifest_{tag}.txt').write_text('\n'.join(manifest) + '\n', newline='')
        (d / f'bp_assets_{tag}.txt').write_text('\n'.join(bp_assets) + '\n', newline='')
        written += [f'{region}/stage/{stage}/manifest_{tag}.txt',
                    f'{region}/stage/{stage}/bp_assets_{tag}.txt']
    return written


def write_all(dist: pathlib.Path, pole_flags: bool, drag: bool) -> list[str]:
    """Write the lists for whichever models this build ships."""
    from . import drag as drag_mod, poles
    pole_models = poles.registration() if pole_flags else []
    drag_models = [drag_mod.registration()] if drag else []
    if not pole_models and not drag_models:
        print('  nothing new to register')
        return []

    # the other two stages hang the artwork off the map group, so their copies
    # name the map-group mesh and no per-model texture
    bare = [dict(m, kms=poles.HANG_KMS_MAP, tri=None, texture=None) for m in pole_models]
    written = []
    for stage, models in ((STAGE, pole_models + drag_models), (EARLY_STAGE, bare),
                          (BOSS_STAGE, bare)):
        if models:
            written += write(dist, models, stage=stage)
    for m in pole_models + drag_models:
        print(f"  {m['model']} -> cache id {cache_id(m['model'])}")
    return written
