# Federal Hall flag

Puts the American flag back into the ending of Metal Gear Solid 2 (Master Collection).

The ending on the roof of Federal Hall was built around a flag. It was removed before
release — the source carries the note *テロ事件に配慮し削除*, "removed in consideration of the
terrorist attacks" — and everything else was left in place: the bare pole, the procs that
raise and lower it, the falling animation, the artwork. All of it still ships with the game
and is never used.

## What it restores

| | how |
|---|---|
| The flag flying from the pole through Solidus's death | the deleted `chara` line is uncommented in the scenario source; the cutscene calls the proc that shows it |
| The flag falling onto Solidus when Raiden cuts the pole | Konami's own fall animation (`w61a_flag_fall1.anm`), re-cut to start where the shot needs it |
| The fallen flag in the next cutscene | one call to the scenario's own `静止星条旗オン` (static flag on) proc, which the cutscene never made |
| The officer dragging it away | a new animation generated from the fallen pose, following the officer's path read out of the cutscene |
| Eleven flags on the street poles, with sway | new keyframes for the shipped flag mesh, generated here, placed by eleven new `chara` lines |

It covers the three ending cutscenes (`p082_01_p01`, `p082_02_p02`, `p082_04_p03`), the
Federal Hall cutscenes before them (stage `d080p08`) and the playable Solidus fight (`w61a`).

**Everything is data.** No engine code is patched. No model is authored: every asset is
generated from files the game already ships.

## Build

Set up `local.env` first (see [docs/SETUP.md](../../docs/SETUP.md)). Needs Python 3.10+,
Go (to build the studio's `objdump`, which reads the officer's path), Konami's GCL compiler
and the scenario source from the leaked tree.

```
python build.py          # -> <build>/federal-hall-flag/update
python deploy.py         # copies it into <game>/update, reversibly
```

| switch | |
|---|---|
| `--no-poles` | leave out the street flags |
| `--no-fall` | leave out the falling flag |
| `--no-drag` | leave out the dragged flag |
| `--no-slowmo` | one flag actor, no per-shot wave rates |
| `--flag-base 11` | ticks between wave keyframes |

`deploy.py` removes files it installed previously that no longer build, so turning a switch
off and redeploying takes that part back out of the game. Players need
[MGSHDFix](https://github.com/ShizCalev/MGSHDFix): it brings the Ultimate ASI Loader, whose
`update` folder this installs into, and it merges the supplementary asset lists.

### Verified

A full build reproduces the released mod **byte for byte, all 41 files**. Every scenario
build first proves that an unmodified compile reproduces the shipped scenario exactly
(see [docs/scenario-pipeline.md](../../docs/scenario-pipeline.md)).

## How it works

### The scenarios

Three stages compile the flag, from two copies of the cut (`flag/gcl_edit.py`):

- `w61a_common.h` wraps the call in `/* … */`. Shared by `d080p08` and `w61a`.
- `P082_01_p01.w61a.gcl` comments it out line by line. The ending stage, `d082p01`.

Konami wrote `-b 5` for the wave. The PS2 stepped the wave once every two demo frames and the
port steps it every frame, so `-b 5` waves at double speed; `-b 11` gives back the PS2 rate.

### Calling procs from a cutscene

A cutscene reaches its scenario through effect `0xFFF01`, `DM_ExecProc`, which runs a proc by
name hash (`lib/mgs2/demo_edit.py`). A proc can only be called if it is in the `-p` list on the
scenario's `chara demo` line, at most 16 — adding the proc to the scenario is not enough.

### Slow motion (`flag/timing.py`)

Parts of the first ending cutscene run slow, either under effect `0x13` (which holds every
frame twice) or because the motion was animated slow. Nothing can change a running vertex
animation's rate, so the scenario creates the flag once per rate and the cutscene swaps which
one is visible at each boundary.

The keyframe actor's rate is `(base + (irnd() & ((1 << rand) - 1)) + 1) * 5 / 6` ticks per
key. At `-w 1` that lands on two values unless `base + 1` divides by six; at `-w 0` the
randomiser is masked away and any tick count is exact.

### The fall (`flag/fall.py`)

The streamed player (`put_stanime`) cannot seek and is not registered as a scenario chara, so
it is created by effect `0x4000` from the cutscene. The fall is cut from animation frame 137
(about 3 m up) to its touchdown at 162, stretched over the 139 cutscene frames from the chop
(5066) to the landing (5205), then the settle plays out and holds. One animation frame lasts
one cutscene frame here: `Act` adds the rate twice while frame-skip is active, which cancels
the player's half-speed default. In the third cutscene, which has no frame-skip effects, the
same player runs at half that speed — hence `DEMO_PER_ANIM = 2` in the drag.

### The drag (`flag/drag.py`)

The officer's root path comes out of the cutscene itself (object 3, frames 2747–3113). The
cloth starts from the fallen flag's last frame and is pulled after him: slack takes up first,
the tail flattens, then deflates. Height comes from the steps outside Federal Hall: y=3000 for
z ≤ 22600, sloping to y=2000 at z ≥ 24100.

Two rules learned the hard way:

- **Never derive the stair slope from a demo object's root.** The officer's root y is constant
  (2998) for the whole drag; the climb is in his joint animation.
- **Never write `VERTEX_ANIME_WORK.count` from outside.** The blend weight is relative to
  `base`, so a count above it extrapolates the keys into a full-screen polygon soup.

### The street flags (`flag/poles.py`)

The shipped flag is a *flying* flag. A hanging one is the same 13 × 9 cloth grid with other
vertex positions, so each pole gets new `.cv2` keyframes for the same mesh: a draped sheet
with folds, a dipped top edge and a hinged sway about the pole (Rodrigues rotation, 5°), in
three shapes so the street is not one flag repeated. Normals are recomputed off the grid and
leaned towards the stage's parallel light to a target brightness, fading down the street.

Two copies of the mesh are written, differing only in the texture-group word at `.kms` +0x10:
the ending stage registers the artwork per model, the other stages hang it off the map group,
and a mesh naming a group its stage has not got draws nothing, silently. Both get a bounding
box refitted to the drape, because the keyframe actor copies the `.kms` box straight into its
culling bounds and the shipped box describes the flying flag.

Pole positions were measured from the **MoreFlags / 911Restore** mod, which first put flags
back on those poles.

### Registration (`flag/register.py`)

New assets are registered through supplementary lists beside the stage's own, which MGSHDFix
merges, so nothing belonging to the game is edited:

```
update/<region>/stage/<stage>/manifest_flagrestore.txt
update/<region>/stage/<stage>/bp_assets_flagrestore.txt
```

See [docs/asset-registration.md](../../docs/asset-registration.md) for the line format and its
traps. Only `eu/stage` and `jp/stage` carry these stages.

## Formats used

- `.cv2` keyframes — `lib/mgs2/cv2.py`, round-trips all 159 shipped files byte for byte
- `.anm` streams (lzsh-packed) — `lib/mgs2/lzsh.py`
- `.sdt` cutscenes — `lib/mgs2/sdt.py`
- details in [docs/formats.md](../../docs/formats.md)

## Reference: the proof of concept

`reference/` holds the MGSHDFix ASI fix that first got all five features working at runtime,
before the work moved into data. It is kept because it holds the measurements this build
uses — chop frame, landing, stair profile, the officer's path, pole attach points, the hanging
frame — and because it was never committed anywhere else. It is disabled in MGSHDFix and must
stay disabled alongside this mod, or two coincident flags appear.
