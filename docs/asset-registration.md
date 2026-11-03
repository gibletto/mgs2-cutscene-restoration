# Registering new assets with a stage

A stage loads only what its lists name. The Master Collection keeps files under their real
names, but a stage loads them from a cache keyed by a name hash (`GV_StrCode`, printed as
eight hex digits; `mgs2.strcode.cache_id`). Two lists per stage map one to the other:

```
<region>/stage/<stage>/manifest.txt
<region>/stage/<stage>/bp_assets.txt
```

The restorations never edit those. They write **supplementary lists** beside them, which
[MGSHDFix](https://github.com/ShizCalev/MGSHDFix) merges at load:

```
update/<region>/stage/<stage>/manifest_<tag>.txt
update/<region>/stage/<stage>/bp_assets_<tag>.txt
```

`<region>` is `eu` for the US/EU release and `jp` for the Japanese one. Not every stage exists
in every region's tree, so check which ones carry yours. The stages both restorations touch
are only in `eu/stage` and `jp/stage`.

Code: `restorations/federal-hall-flag/flag/register.py` (per-model groups, keyframes) and
`restorations/manhattan-crash/crash/register.py` (shared groups, many textures).

## Lines

Each line is `source path,load path,cache path`. `<code>` is the cache id of the **name the
stage asks for**: the name a put-object or demo object uses in the scenario. Several names can
point at the same source file, which is how one flag mesh serves many poles.

**manifest** (the platform-neutral data):

```
assets/tri/us/<group>.tri,us/stage/<stage>/cache/<code>.tri,cache/<code>.tri
assets/kms/us/<file>.kms,us/stage/<stage>/cache/<code>.kms,cache/<code>.kms
assets/cv2/us/<file>.cv2,us/stage/<stage>/cache/<code>.cv2,cache/<code>.cv2
assets/anm/us/<file>.anm,us/stage/<stage>/cache/<code>.anm,cache/<code>.anm
```

**bp_assets** (the Master Collection's own formats; the third field names the region's tree):

```
assets/kms/us/<file>.cmdl,us/stage/<stage>/cache/<code>.cmdl,<region>/stage/<stage>/cache/<code>.cmdl
textures/flatlist/<tex>.ctxr,stage/<stage>/cache/<tex>.ctxr,<region>/stage/<stage>/cache/<group>/<tex id>.ctxr
```

A texture is registered against the texture group (`.tri`) that declares it, so its cache path
is `<group id>/<texture id>`. A per-model group is registered under the model's own `<code>`;
a shared group (the map's, or `land_high` and `building` in `d080p07`) under its own id.

A file the game already ships can be registered as it is. A new file goes under `update/` at
the source path its line gives.

## Traps

- **Line endings.** The game's own lists end lines with `\r\r\n`. The flag build writes `\n`
  and the crash build `\r\n`, and both load. What must not happen is a stray `\r` left inside
  a field: the name no longer matches and its lookup fails.
- **Textures before models.** A model binds its textures once, when it loads. A texture
  registered after the model that uses it is never bound, and the model renders grey.
- **A model names its texture group in its `.kms`** (the word at `+0x10`). If the stage has no
  group by that id, the mesh draws nothing, and nothing reports it. Some stages register
  artwork per model; others hang it off the map's group. The flag restoration ships two
  copies of the street-flag mesh, differing only in that word, one for each kind.
- **New texture ids must be declared in a group.** A group lists every id it serves. To add
  textures, register a rebuilt copy of the group with the new ids appended; the pixels come
  from each texture's own `.ctxr` line. The Master Collection's `DG_TEX` record is 192 bytes,
  not the PS2's 160.
- **Culling bounds come from the `.kms`.** The keyframe actor copies the `.kms` bounding box
  into its culling bounds, so a mesh whose keyframes move it outside that box is culled while
  still on screen. Refit the box to the animation.
