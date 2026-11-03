# Manhattan crash

Arsenal Gear runs aground on Manhattan at the end of Metal Gear Solid 2 (Master Collection).

In the released game the Arsenal's arrival ends on a bridge flyover (`p080_13_p08`) that fades
to white over the Verrazzano-Narrows Bridge. The scenario for that stage
(`d080p07`) still carries Konami's crash kit: the wrecked city land (`vw60a_9`) and the beached
Arsenal (`アーセナルギア`, model `w60a_amg`), with the procs that switch them on. Their init
calls are commented out and nothing in the cutscene ever reaches them.

This restoration wires the kit back into the stage and extends the cutscene from 2,180 frames
to 4,111 with new shots that carry the ship up the harbour, past the Statue of
Liberty and into the Battery.

| | how |
|---|---|
| Konami's city where the cutscene can see it | the scenario's city complex moved to `-p -25000,1000,-500000`, the fog plane and sea extended, the intact city switched on at init (`crash/world.py`) |
| Konami's crash kit | the commented-out init calls that create the wrecked land and the beached hull restored, both hidden until the cutscene needs them |
| The extended cutscene | written through the Cutscene Studio (`crash/author.py`), dressed with effects (`crash/dress.py`), given its soundtrack (`crash/audio.py`) |
| The soundtrack | an original foleyed mix, `audio/arsenal-crash.wav`, encoded and grafted in |
| New models: the statue and her island, the towers the bow topples, the trees, the wreck chunks, a Brooklyn shoreline, a New Jersey town | **not included** -- you supply them (see [ASSETS.md](ASSETS.md)); this folder registers and places them |

The cutscene's camera, ship course, effects and the put-object choreography are all data,
authored here. No engine code is patched.

## What you need

- the game, and the leaked source tree for the scenario compile (see
  [docs/SETUP.md](../../docs/SETUP.md))
- Python 3.10+, and Go, which builds the Cutscene Studio on first use
- the studio's generated data (its effect catalog), see
  [tools/cutscene-studio/README.md](../../tools/cutscene-studio/README.md)
- the models and textures: `python from_release.py <zip>` takes them from the
  [released mod](https://www.nexusmods.com/metalgearsolid2mc/mods/135) (see [ASSETS.md](ASSETS.md))
- for the soundtrack: `pip install numpy scipy soundfile av`, and Windows (its WMA8 encoder)

## Build

```
python build.py                  # -> <build>/manhattan-crash/update
python build.py --stock-audio    # the game's score (t13a1d) instead of the soundtrack
python deploy.py                 # copies it into <game>/update, reversibly
```

`--world-only` and `--demo-only` build one half. `putobj_registry.py` writes the list of the
crash world's put-objects for the studio's viewport (`MGS2CS_PUTOBJ_REGISTRY`), so shots can
be framed against them.

Players need [MGSHDFix](https://github.com/ShizCalev/MGSHDFix), which brings the Ultimate ASI
Loader (whose `update` folder this installs into) and merges the supplementary asset lists.

### Verified

Built against the release's own models and textures, this reproduces all **180 files** of the
released mod: 178 exactly, and the two scenarios exactly apart from one 4-byte field the
game never reads (see [docs/scenario-pipeline.md](../../docs/scenario-pipeline.md)).

## How it works

### The world (`crash/world.py`, `crash/models.py`)

One source file is edited, `P080_13_p07b.w51a.gcl`, and compiled through the shared scenario
pipeline, which first proves an unmodified compile reproduces the shipped scenario. The edit:

1. moves the whole city complex (skyline, both land models, the beached hull) to `CITY_P`
2. pushes the far fog plane from 290,000 to 700,000; the city's near edge is ~414,000 from the
   bridge cameras
3. stretches the sea over the bridge, the ship's whole run and past the city
4. switches the intact city on at init
5. restores the crash kit's two init calls and hides both right after -- their own "off"
   lines are commented out too, so without that the wreck z-fights the intact city
6. adds a `manhattan_crash` proc (intact off, wreck and hull on) and whitelists it on the
   cutscene's `-p` line
7. creates every new put-object (`models.PUTOBJS`, 49 of them) inside the proc that builds
   the intact city, which runs at init. A new proc of their own would be dead-stripped.

In the end the cutscene does not call `manhattan_crash`: it switches everything by messaging
the put-objects directly, which also lets it move, turn and hide them per frame.

### The cutscene, in three passes

**Author** (`crash/author.py`). The build starts a private studio server, opens Konami's
`p080_13_p08`, extends it to 4,111 frames and edits the scene document:

- from frame 1100 the convoy's course bends toward the statue -- Konami's own animation rides
  on top, pivoted about the bow -- and from the cut it runs a new track to its stop on the
  Battery; the deck dolls and the wake rig go with it
- the hull mounts Liberty Island, heaves, banks, grinds and splashes back down
- the takeover: at frame 1446 the stock bridge camera is seized mid-shot, no cut, and arcs
  wide onto the statue and the city; then a helicopter tour of the statue, a jib by Castle
  Clinton, the landfall, the plough through the Battery and the wreck
- a hand-held sway over every new shot, with impact shake through the mount and the landfall
- the original's closing white-out is suppressed, and new flashes and fades are added
- put-object messages (effect `0x8000`, `DM_SendEffectMessage`, addressed by name hash) move
  the sets between eras, lean the pedestal, tear the statue off it and carry her on the deck
  until she rolls off into the water, swap the city intact -> breached -> wrecked, topple
  seven towers, sweep the trees and right the wreck chunks
- the water eruptions of the landfall are fired from the bow's live splash spawners

**Dress** (`crash/dress.py`). Effects written straight into the bake's packets: fires, smoke,
dust, debris, water eruptions, fog wisps and pall, depth of field, slow motion, and the
controller rumble. All of them are Konami's own effects, cloned from the cutscenes that use them.
This pass also thins the wake spray to Konami's measured rhythm.

**Audio** (`crash/audio.py`). The soundtrack is resampled, encoded with Media Foundation's WMA8
encoder (`crash/wma8_encode.ps1`) and grafted into the PCM channel, framed exactly like the
donor cutscene's (t13a1d, the tanker intro, whose score builds to the same crescendo). With
`--stock-audio` that donor's own score goes in unchanged.

Only that encoder's 8192-byte superframes play in game; ffmpeg's `wmav2` encoder produces
streams the engine decodes as noise.

### The timeline (`crash/timeline.py`)

The cutscene was cut twice. Rather than re-time every hand-tuned constant, everything after
frame 2390 is still written in the first cut's frame numbers and mapped by `T()`; the physics
runs in that "old time" behind `O()`, its inverse.

| frames | |
|---|---|
| 0 - 1445 | Konami's cutscene (the course bends from 1100) |
| 1446 - 2180 | the takeover of the stock camera |
| 2181 - 3163 | the statue: the tour, the mount, the abduction |
| 3164 - 3320 | Castle Clinton |
| 3321 - 4110 | landfall, the plough, the wreck, the white-out |

### Rules learned the hard way

- **512 effect births per cutscene, total.** Each exec-1 effect is a lifetime birth and
  `DM_MAX_EFFECT` is 512; the 512th overruns the effect list and crashes (it did, at frame
  3616). Density comes from overlap and long lives, and the dressing pass refuses a build
  over 500.
- **A put-object message lands one frame late.** Sent at frame *f*, it renders at *f + 1*, so
  every set swap meant to hide under a cut is sent one frame early. (The studio applies
  messages at their own frame, so its viewport shows these swaps a frame early; the game is right.)
- **Demo dolls do not draw past the original length.** They only draw where they have base
  packets, so anything that must appear in the extension is a put-object.
- **`NewTs_Spark` (`0x600e`) is not linked into this stage.** A spark packet crashes it.
- **The audio is the clock.** The stream must run the full length; a starved stream freezes
  the demo clock and the closing white screen never ends. The first PCM chunk must hold the
  header and seek table and nothing else.
- **Calling the crash proc from this cutscene trapped (`int3`) in testing**, so the switches
  message the put-objects instead.

### Registration (`crash/register.py`)

The new models are registered in supplementary lists that MGSHDFix merges, so nothing of the
game's is edited:

```
update/<region>/stage/d080p07/manifest_manhattan_crash.txt
update/<region>/stage/d080p07/bp_assets_manhattan_crash.txt
```

A model looks its textures up in a texture group, so rebuilt copies of the two groups the new
models use (`land_high`, `building`) declare the new texture ids next to the stock ones.
Textures are listed first: a model binds its textures once, at load. See
[docs/asset-registration.md](../../docs/asset-registration.md).

## Files

| | |
|---|---|
| `build.py`, `deploy.py` | build into `<build>/manhattan-crash/update`; install it |
| `from_release.py` | fill your asset folder from the released mod's zip |
| `crash/world.py` | the scenario edit |
| `crash/models.py` | every put-object and every registered model |
| `crash/timeline.py` | the clock, and the ship's course along it |
| `crash/author.py` | pass 1: the scene document |
| `crash/dress.py` | pass 2: the effects |
| `crash/audio.py`, `crash/wma8_encode.ps1` | pass 3: the soundtrack |
| `audio/arsenal-crash.wav` | the soundtrack: original work, published with the restoration |
| `crash/register.py` | the stage lists and the texture groups |
| `putobj_registry.py` | the put-object list for the studio's viewport |
| `package/README.txt` | the player-facing readme that goes with a build |
