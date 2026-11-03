# Setup

The code here does nothing on its own. It reads three things you have to bring, and none of
them is, or ever will be, in this repository.

| | what | used for |
|---|---|---|
| **the game** | *Metal Gear Solid 2: Sons of Liberty - Master Collection* (Steam), a normal install | every restoration edits copies of its files; nothing in it is modified |
| **the source tree** | the leaked MGS2 development tree, the folder holding `mgs2x/` and `bp/` | the scenario source and Konami's GCL compiler (`gclconv.exe`, `gclk.exe` under `bp/Bin/Tools/Release`), and the effect definitions the studio's catalog is generated from |
| **the crash's models and textures** | taken from the [released mod](https://www.nexusmods.com/metalgearsolid2mc/mods/135) with `from_release.py` | only the Manhattan restoration needs them; see its [ASSETS.md](../restorations/manhattan-crash/ASSETS.md) |

## Software

- Python 3.10 or later. The Manhattan restoration's soundtrack step also needs
  `pip install numpy scipy soundfile av`; nothing else needs a package.
- Go 1.25 or later, on `PATH`. The Cutscene Studio is built from `tools/cutscene-studio` the
  first time a build needs it.
- Windows. The GCL compiler is a Windows program, the scenario pipeline uses directory
  junctions to stage the source tree, and the soundtrack is encoded with Windows' own WMA8
  encoder.
- For playing the result: [MGSHDFix](https://github.com/ShizCalev/MGSHDFix). It installs the
  Ultimate ASI Loader, which redirects the game's file opens into `<game>/update`, and it
  merges the supplementary stage lists (`manifest_*.txt`, `bp_assets_*.txt`) the
  restorations register their assets with.

## local.env

Copy `local.env.example` to `local.env` at the repository root and fill it in. Environment
variables of the same names win over the file.

| key | |
|---|---|
| `MGS2_GAME` | the install: the folder holding `METAL GEAR SOLID2.exe` |
| `MGS2_LEAK` | the source tree: the folder holding `mgs2x/` and `bp/` |
| `MGS2_GCL_BIN` | where `gclconv.exe` and `gclk.exe` are, if not `<MGS2_LEAK>/bp/Bin/Tools/Release` |
| `MGS2_ASSETS` | your asset folder (one subfolder per restoration) |
| `MGS2_BUILD` | where builds go; default `<repo>/build` |

`local.env` and `build/` are gitignored.

## The studio's data

The Cutscene Studio needs an effect catalog generated from the source tree before it can
bake. See [tools/cutscene-studio/README.md](../tools/cutscene-studio/README.md), "Setup". The
generated files go in `tools/cutscene-studio/data/`, which is gitignored.

## What a build writes

A build writes only into `MGS2_BUILD`. `lib/mgs2/outputs.py` refuses any write into the game
or the source tree, so a wrong path fails instead of writing over your files. The
restorations' `deploy.py` is the only step that writes into the game, and only into
`<game>/update`:

- it keeps any file it replaces that it did not put there, once, as `<name>.pre-<tag>`;
- it remembers what it installed, so a redeploy removes files a build no longer produces.

To uninstall, delete the files a build lists in its `FILES.txt`.

## First run

```
cd lib
python -m mgs2.scenario scenerio_stage_d082p01
```

This compiles one stage's scenario unmodified and checks the result is byte-identical to the
one your game ships. If it passes, the compiler, the source tree and the game agree, and
every restoration's scenario build will work. See [scenario-pipeline.md](scenario-pipeline.md).
