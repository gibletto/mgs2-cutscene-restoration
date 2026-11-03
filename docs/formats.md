# The game's files we touched

A short guide to each kind of file the restorations read or write: what it is, what we did
with it, and where the code is. The code in `lib/mgs2/` has the byte-level detail.

Almost everything in the game is looked up by a **name hash**: a short number made from a
name (`lib/mgs2/strcode.py`). Stage lists write it as eight hex digits, for example
`0010a9bc`.

| file | what it is | what we did with it | code |
|---|---|---|---|
| `.sdt` | a cutscene | lengthened one, added shots, effects and audio; added effect calls to others | `sdt.py`, `demo_edit.py`, the Cutscene Studio; see [how-cutscenes-work.md](how-cutscenes-work.md) |
| `.gcx` | a stage's compiled script: what's in the world, and routines a cutscene can call | rebuilt from Konami's source with small edits | `scenario.py`; see [scenario-pipeline.md](scenario-pipeline.md) |
| `.kms` | a model | changed the texture group it points at, and resized its bounding box | `restorations/federal-hall-flag/flag/poles.py` |
| `.cmdl` | the HD release's compiled version of the same model | copied alongside its `.kms` | |
| `.cv2` | keyframes that bend a model's vertices (the flags waving) | wrote new ones for hanging flags | `cv2.py` |
| `.anm` | a compressed vertex animation (the flag falling) | unpacked it, re-timed it, packed it again | `lzsh.py`, `restorations/federal-hall-flag/flag/fall.py` |
| `.tri` | a texture group: the list of textures a set of models can use | added new texture ids to it | `restorations/manhattan-crash/crash/register.py` |
| `.ctxr` | one texture | registered new ones | |
| `manifest*.txt`, `bp_assets*.txt` | a stage's list of what to load | added our own lists next to the game's | see [asset-registration.md](asset-registration.md) |

## A few things worth knowing

- **Models and their bounding box.** The game uses a model's stored box to decide when it's
  off screen. Our hanging flags hang lower than the flying flag the box was made for, so
  they popped in and out at the edge of the frame until the box was enlarged.
- **Models and their texture group.** A model names the texture group it draws from. If the
  stage doesn't have that group, the model is invisible and nothing reports why.
- **Compressed animations.** `.anm` files are packed with the game's own compression
  ("lzsh"). `lzsh.py` reads it and writes a simple form of it that the game accepts.
- **Texture groups.** In the HD release each texture entry in a `.tri` is 192 bytes. Konami's
  original PS2 tools used 160, so they can't edit these files.
