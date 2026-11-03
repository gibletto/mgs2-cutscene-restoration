# MGS2 Cutscene Studio

A timeline editor for Metal Gear Solid 2's cutscenes (`.sdt` "demo" streams), targeting the
PC Master Collection. Import a shipped cutscene, edit the camera, cast, effects and subtitles
on a Resolve/AE-style timeline, bake it back, and the game loads it.

    go build -o mgs2cs.exe ./cmd/mgs2cs
    ./mgs2cs.exe serve            # http://127.0.0.1:8733

This directory ships **code only**. It contains no game files, no files from the original
source tree and no data generated from either. Pair it with your own game install and your
own copy of the source tree, and generate the data it needs locally -- see [Setup](#setup).

## How it stays safe

A cutscene is a flipbook: every frame spells out exactly where the camera and everything else
is (see [how cutscenes work](../../docs/how-cutscenes-work.md)). The studio lets you edit with
keys and curves, then writes out every frame, the way a video editor renders.

When it writes a cutscene back, it starts from Konami's original and only replaces the parts
you edited. Everything else is copied through untouched. So a cutscene you open and save
without editing comes back identical to the original, and `mgs2cs roundtrip <dir>` checks
exactly that on the shipped cutscenes.

Before writing anything it runs a checker over the result. The checker is set up so that
every cutscene Konami shipped passes it, and it refuses anything that would crash or hang
the game.

## Commands

    mgs2cs serve [port]        the editor
    mgs2cs ls                  list cutscenes in the configured game dir
    mgs2cs inspect <f.sdt>     frames, cast, effects, captions, camera
    mgs2cs check <f.sdt>       run the validator
    mgs2cs roundtrip <dir>     import -> bake -> byte-compare

Also in `cmd/`: `sdtinspect`, `objdump` and `framedump`, small diagnostic dumpers.

## Setup

### Requirements

- **Go 1.25** or newer (`go.mod`). No third-party Go modules.
- **Node.js 18+ and npm**, only to build the web UI (React + three.js, built with Vite).
- **Python 3.9+**, only to generate the effect catalog and corpus statistics.
- Your own **MGS2 Master Collection** install (PC/Steam).
- Your own copy of the **original source tree** (the directory that holds `mgs2x/` and
  `tools/`), to generate the effect catalog and the stage tables.

### 1. Build

    cd web && npm ci && npm run build && cd ..
    go build -o mgs2cs.exe ./cmd/mgs2cs
    ./mgs2cs.exe serve

The UI is embedded into the binary from `web/dist/`, so build the UI first, then the binary.
`go build` also works before the UI was ever built (only `web/dist/.keep` is committed); that
binary serves the HTTP API only and says so on startup.

### 2. Generate the data

Everything the studio reads that is derived from Konami's material lives in `data/` (next to
this README, gitignored). Set two environment variables, or pass the equivalent flags:

    MGS2_DIR=<game dir>            # e.g. C:\Program Files (x86)\Steam\steamapps\common\MGS2
    MGS2_SOURCE_DIR=<source tree>  # the directory holding mgs2x/ and tools/

Then, from this directory:

| File | What it is | Needed by | Command |
|---|---|---|---|
| `data/effects.json` | the 245-effect catalog: ids, exec types, `Data` layouts, widgets, labels, defaults | **the studio** (effect panels, codec, validator) | `python tools/cnf2json.py --source <source tree> --validate` |
| `data/stage/stages.json`, `data/stage/demos.json` | world geometry per stage, and the demo -> stage map | the viewport's world; the `/api/stage/toggle` whitelist | `go run ./internal/stage/gen.go -scn <source tree>/mgs2x/scn -game <game dir>` (or `go generate ./internal/stage` with the env vars set) |
| `data/corpus.json` (+ `data/corpus_effects.json`) | statistics over every shipped `.sdt` | reference only | `python tools/corpus_scan.py --root <game dir> --effect-src <source tree>/mgs2x/source/user/mode/demo/effect` |
| `data/demos.json` | per-demo table (frames, audio, cast, effects, sizes) | reference only | `python tools/corpus_idioms.py --root <game dir>` |
| `data/exec_type_observed.json` | effect id -> exec types seen in the US demos | reference only | `python tools/exec_type_observed.py --root <game dir>` |
| `data/effect_limits.json` | engine-enforced parameter ranges, read out of the engine source by hand | optional (range warnings / E1 findings) | *no generator* -- hand-authored; see below |

On Windows consoles run the Python scripts with `PYTHONUTF8=1` (they print Japanese text).
The scripts only read the game and the source tree; they never write into either.

Notes:

- `corpus_scan.py` and `corpus_idioms.py` walk *every* `.sdt` under the game dir, including
  an `update/` override tree if you have deployed anything. Their output only matches a stock
  install when there is no such tree.
- `effect_limits.json` has no generator: it is a hand-written list of per-parameter engine
  limits with `file:line` citations into the engine source. The studio runs without it --
  parameters simply carry no engine range, so the out-of-range check (`E1`) has nothing to
  check. Put your own copy in `data/` if you have one.
- The stage tables reflect whatever `mgs2x/scn` you point the generator at. Generate them
  from a scenario tree with your restoration's `.gcl` edits applied if you want the editor
  to know about procs those edits add (the `/api/stage/toggle` endpoint only accepts procs
  that appear in the stage table).
- Without `effects.json` effects are opaque blobs (still passed through byte-exact) and the
  validator's catalog rules are skipped; without the stage tables the viewport shows no world.

### 3. Configure

The config file is `<user config dir>/mgs2-cutscene-studio/config.json` -- on Windows
`%APPDATA%\mgs2-cutscene-studio\config.json`, on macOS
`~/Library/Application Support/mgs2-cutscene-studio/config.json`, on Linux
`~/.config/mgs2-cutscene-studio/config.json`. Every key is optional; empty keys fall back to
an environment variable, then to auto-detection:

| Key | Meaning | Fallback |
|---|---|---|
| `gameDir` | the Master Collection install (contains `us/`, `jp/`, `assets/`) | `$MGS2_DIR`, then the usual Steam spots (`C:\Program Files (x86)\Steam\...`, `<drive>:\SteamLibrary\...`) |
| `gameDataDir` | the Bluepoint GameData tree (flatlist + cooked assets); only for real models/textures in the viewport | `$MGS2_GAMEDATA` |
| `toolsDir` | Konami's tool tree; only for `.gcx` recompiles | `$MGS2_TOOLS_DIR` |
| `dataDir` | where the generated data lives | `$MGS2CS_DATA_DIR`, then `./data`, `<exe dir>/data`, `<module root>/data` |
| `catalogPath` | the effect catalog | `effects.json` in the data dir |
| `putobjRegistry` | `putobj_registry.json` for the viewport's put-object ghosts (produced by the restoration tooling, not by this tool) | `$MGS2CS_PUTOBJ_REGISTRY`, then next to the executable, then the working directory |
| `outDir` | where a bake is staged. Baking never writes into the game; deploying does, and only when asked | `./out` |

The editor's settings panel writes the same file.

## Tests

    go test ./...

Tests that need the game install, the GameData tree or the generated data **skip** when they
are missing, naming the variable that would supply it (`MGS2_DIR`, `MGS2_GAMEDATA`,
`MGS2CS_DATA_DIR`). The corpus tests read the whole install (about 12 GB) and only read it.

The web UI has two node smoke tests (`npm run smoke`, `npm run smoke:render`); the catalog
parts need `data/effects.json`, which `npm run mock-data` copies into `web/public/mock/` for
the UI's offline mock backend (`?mock=1`).

## Two things to watch

**Don't remove a cutscene's audio** unless the stage's stream table says that cutscene has
none. If the game expects sound and finds none, it stops dead.

**Changing a cutscene's length affects the HD widescreen framing.** The HD release keeps its
widescreen camera adjustments in a separate table, by frame number. Editing frames in place is
fine; adding or removing frames shifts everything after the change against that table.
