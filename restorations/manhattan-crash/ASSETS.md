# Manhattan crash: the assets you supply

This repository holds no models or textures. The restoration places and registers new ones,
and those you supply yourself in your asset folder (`MGS2_ASSETS` in `local.env`). The
soundtrack is the exception: it is original work and ships here, in `audio/`.

```
<MGS2_ASSETS>/manhattan-crash/
    models/<name>.kms         the model (Master Collection .kms)
    models/<name>.cmdl        its compiled mesh (Master Collection .cmdl, the _win variant)
    textures/<id>.ctxr        one per texture, named by its 8-digit hex id
    soundtrack.wav            optional: replaces audio/arsenal-crash.wav (see below)
```

The models were made in Blender and exported to the game's formats with the
[SeaLouse](https://github.com/Jacky720/SeaLouse) add-on. The source files aren't part of this
record. What follows is what the build expects of the models.

## The easy way: take them from the release

The released mod, [Arsenal Gear Crash on Nexus Mods](https://www.nexusmods.com/metalgearsolid2mc/mods/135),
contains every model and texture below. Download its zip and run:

```
python from_release.py "<the downloaded .zip>"
```

That copies the 36 new models and 90 textures into `<MGS2_ASSETS>/manhattan-crash/`, laid out
as below. Nothing else is taken from the zip; the build makes the rest itself. A build from
those assets reproduces the release: 178 of its 180 files exactly, and the two scenarios apart
from one field the game never reads.

## Models

Every model in the table is registered in stage `d080p07` under its cache id (the name hash,
`mgs2.strcode.cache_id`). The ones marked *game* are stock models the build copies from your
install; the rest are yours. The table is `crash/models.py`, and so is the placement of each
put-object (`PUTOBJS`: position, rotation, scale and whether it starts hidden).

| model | cache id | source | role |
|---|---|---|---|
| `vw60a_7` | 0079ae88 | yours | Manhattan, breached at the Battery |
| `vw60a_9_far` | 00d8bf7a | yours | wreck, far chunk (registered, not placed) |
| `vw60a_9_far2` | 0017ef8d | yours | wreck chunk that tilts back onto its rest |
| `vw60a_9_far3` | 0017ef8e | yours | wreck chunk, unsplit (registered, not placed) |
| `vw60a_9_far3_a` | 00be44a0 | yours | wreck chunk a, slides home |
| `vw60a_9_far3_a_skin` | 00a7c2d5 | yours | skin of chunk a |
| `vw60a_9_far3_b` | 00be44a1 | yours | wreck chunk b, slides home |
| `vw60a_9_far3_b_skin` | 00a7c2d7 | yours | skin of chunk b |
| `vw60a_9_far3_c` | 00be44a2 | yours | wreck chunk c, slides home |
| `vw60a_9_far3_c_skin` | 00a7c2d9 | yours | skin of chunk c |
| `vw60a_9_holes` | 0024e076 | yours | the wreck as the side view sees it |
| `vw60a_9_near` | 001bfdad | yours | wreck, near chunk (registered, not placed) |
| `w04a1_brk_wall_hahen` | 00c8e2ba | game | wall rubble: debris effect objcode 0xc8e2ba |
| `w04b1_brk` | 00b9a472 | game | slab rubble: debris effect objcode 0xb9a472 |
| `w32a_sky_wall2` | 00e9ff48 | game | sky wall the dawn clouds hang on |
| `w32a_sky_wall3` | 00e9ff49 | game | sky wall the dawn clouds hang on |
| `w60a_7_brooklyn` | 00989f14 | yours | intact Manhattan land with Brooklyn behind it |
| `w60a_cld1` | 009bf979 | yours | dawn cloud sheet |
| `w60a_cld2` | 009bf97a | yours | dawn cloud sheet |
| `w60a_njb` | 00450b88 | yours | New Jersey building cluster |
| `w60a_smash` | 007fabdc | yours | tower 1, the first the bow topples |
| `w60a_smash2` | 00f57bc1 | yours | tower 2 |
| `w60a_smash3` | 00f57bc2 | yours | tower 3, bulldozed aside |
| `w60a_smash4` | 00f57bc3 | yours | tower 4, shoved and toppling |
| `w60a_smash5` | 00f57bc4 | yours | tower 5, the small one that crumples |
| `w60a_smash6` | 00f57bc5 | yours | tower 6, legs punched out |
| `w60a_smash7` | 00f57bc6 | yours | tower 7, settles straight down |
| `w60a_smash7_lw` | 00e504b4 | yours | tower 7 window lights |
| `w60a_smash_lw` | 00efc9cc | yours | tower 1 window lights |
| `w60a_sol` | 00452032 | yours | Liberty Island |
| `w60a_solb` | 00a406aa | yours | the statue's torn feet and gown, left on the pedestal |
| `w60a_solp` | 00a406b8 | yours | the pedestal shaft |
| `w60a_solst` | 0080d7e8 | yours | the statue torn from her feet (origin at the feet) |
| `w60a_solw` | 00a406bf | yours | the statue whole |
| `w60a_sunq` | 00a41ef9 | yours | sun billboard |
| `w60a_tree1` | 009239e5 | yours | Battery tree |
| `vw60a_9_crash` | 00d6362a | yours | the wreck the overhead shot shows |
| `w60a_solr` | 00a406ba | yours | island reflection |
| `w60a_solpr` | 0080d786 | yours | pedestal reflection |
| `w60a_solwr` | 0080d866 | yours | statue reflection |

What the cutscene assumes of them:

- **The city frame.** `vw60a_7`, the `vw60a_9_*` wreck pieces and `w60a_7_brooklyn` are placed
  at the city's own `-p` (`-25000,1000,-500000`, scale 1000), the same frame as Konami's
  `w60a_*` city models, so they must share that model space to line up with the stock city.
- **The towers** (`w60a_smash*`) are base-centred: each is placed at its own footing and turned
  about it, so the origin must sit at the base. A `_lw` layer shares its tower's origin exactly.
- **The statue set** is placed at scale 900. `w60a_sol` and `w60a_solp` have their origin at the
  island (`y` 240 in the world); `w60a_solst`, `w60a_solw` and `w60a_solb` at the statue's feet,
  6,072 units above it, since the statue pivots about her feet as she falls. The reflections
  ride their originals' anchors and are mirrored below the waterline.
- **Textures** are looked up through the `land_high` and `building` texture groups (the island
  and city geometry in the first, the statue in the second), so the models' materials name one
  of those groups.

## Textures

Each file in `textures/` is one texture, named `<id>.ctxr` with the id in 8 hex digits (a
trailing `_alp` in a source name is dropped). The build declares every id it finds in both
texture groups and registers each one. The release used 90, with ids from `005b0000` to
`005b0183`.

## Soundtrack

`audio/arsenal-crash.wav` is the cutscene's whole soundtrack: one foleyed mix, music and sound
effects together, authored for this restoration and published with it. The build prepares it
(48 kHz stereo, a 20 ms closing fade, peak-normalised), encodes it with Media Foundation's WMA8
encoder (`crash/wma8_encode.ps1`; the only encoder found whose stream the engine plays
cleanly), packs it and grafts it in. Building from it reproduces the released stream exactly.

The build needs `numpy`, `scipy`, `soundfile` and PyAV (`av`) for that step.

To use a different mix, put one in your asset folder:

- `soundtrack.wav`: any WAV, prepared and encoded as above. It has to run the length of the
  cutscene, because the audio is the demo's clock (the release's is 70 s, 206 packets).
- `soundtrack.wma`: already encoded, 48 kHz stereo at 192 kbit/s in 8192-byte packets.
- `soundtrack.amwx`: already packed as the game carries it (see `crash/audio.py`).

`python build.py --stock-audio` uses the score of the tanker-intro cutscene, t13a1d, straight
from your install instead: music only, no foley.

## Checking your folder

`python build.py --world-only` copies and registers everything without starting the studio,
and stops with a list of whatever is missing.
