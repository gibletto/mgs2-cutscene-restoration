# The scenario pipeline

A stage's scenario (`assets/gcx/<region>/_bp/scenerio_stage_<stage>.gcx`) is its script: which
objects exist and where, the procs a cutscene can call, the fog, the sea. Konami's source for
every stage is in the leaked tree (`mgs2x/scn`), and so is their compiler. A restoration
edits the source and recompiles it: `lib/mgs2/scenario.py`.

```python
from mgs2 import scenario
out = scenario.build('scenerio_stage_d082p01', edit=my_edit, name='flag')
# {'eu': Path(.../eu/scenerio_stage_d082p01.gcx), 'us': Path(...)}
```

`edit(scn_dir)` changes files in a fresh copy of the source and raises if its change did not
land. Restorations write their edit as Python that finds an anchor line and asserts it found
it. They never apply a patch file: an edit that silently misses builds a scenario with no
edit in it.

## Every build compiles twice

1. **Unmodified.** The pristine source is compiled and compared with the scenario the game
   ships. After the residuals below it must be byte for byte identical, or the build stops.
   This proves the toolchain: the compiler, the source revision, the argument files and the
   conversion all agree with what Konami built.
2. **Edited.** The same, with the edit applied and the same residuals. So the only difference
   between the shipped scenario and the built one is the edit.

`gcxdiff.py` then prints that difference: procs added and changed, strings, sizes.

## The compile

```
gclconv @_bp_gclc_args_us.txt @_bp_gclc_args_shared.txt -o <stage>.gco _bp/<stage>.gcl
gclk    @_bp_gclk_args.txt                               -o <stage>.gcx <stage>.gco
gcx2bp  (lib/mgs2/gcx2bp.py)                             -> the Master Collection's BGCL form
```

- Run from the `scn` folder, with paths relative to it. gclk misreads an absolute Windows path
  at the drive colon and fails on `linkvar.def`.
- Both regions use the **US** argument files. The EU ones carry `-n 1:2:3:4:5`, which keeps
  the PAL multi-language proc variants the Master Collection does not ship. A stage without
  them does not change, but `w61a` comes out 3 KB too big.
- The source folder is copied for each pass. The folders it includes from (`module`,
  `source`, `list`) are linked to, not copied.
- `gcx2bp.py` is a reimplementation of the conversion the port's tool does (final-form header,
  the text blob as UTF-8), written for interoperability.

## Residuals

A clean compile still differs from the shipped file in places that are not about the edit:

| residual | why | handled |
|---|---|---|
| stale symbols | the dump's EU voice and demo tables (`vox.lst`, `demo2.lst`) are a later revision than the build that shipped | each changed value is substituted, with its use count checked |
| font field | Konami's compiler leaves it uninitialised, so the shipped file holds whatever was in memory | copied from the shipped file |
| region values | a few short table values differ by region | copied |
| text blob | gclk writes text in the engine's own character codes; the port ships UTF-8 | the shipped blob is swapped in, and the offsets after it are shifted |

`residuals.py` learns them by diffing pass 1 against the shipped file. It refuses unless
applying them reproduces that file exactly. The same residuals are then applied to the
edited build. The US scenario needs only the font field; the EU one also needs the stale
symbols.

## Regions

| the game's release | reads scenarios from | reads cutscenes from | stage lists |
|---|---|---|---|
| US / EU | `assets/gcx/eu/_bp/` | `us/demo2/_bp/` | `eu/stage/` |
| Japanese | `assets/gcx/us/_bp/` | `jp/demo2/_bp/` | `jp/stage/` |

The two shipped scenarios of a stage are different builds, so each region learns its own
residuals from its own file.
