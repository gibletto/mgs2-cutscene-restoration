# Notice

*Metal Gear Solid 2: Sons of Liberty* and the *Master Collection* belong to Konami Digital
Entertainment. This is an unofficial fan project, not affiliated with or endorsed by Konami or
Bluepoint.

## What is not here

- **No game files.** No models, textures, animations, cutscenes, scenarios, audio or
  executables from the game, and nothing derived from them (bakes, extracted tables,
  catalogs). Every build reads them from your own install and writes its output outside the
  repository.
- **No files from the leaked source tree.** No source code, scripts, headers or tools,
  including Konami's GCL compiler (`gclconv`, `gclk`). Where code comments refer to a file in
  the tree, they name it by path and describe it in their own words.
- **No generated data.** Data that tools build from the game or the source tree (the studio's
  effect catalog and stage tables, corpus scans) is generated locally and gitignored.
- **No restoration assets.** The Manhattan crash's models and textures, and the process that
  made them, are not included.

## Reimplementations

Some modules reimplement formats so that the restorations can read and write what the game
reads. They were written for interoperability, in new code:

- `lib/mgs2/lzsh.py` packs and unpacks the lzsh compression the game uses for vertex
  animations. Its behaviour was matched against the game's decoder.
- `lib/mgs2/gcx2bp.py` converts a compiled scenario into the Master Collection's `BGCL` form,
  the job Bluepoint's own tool does. None of that tool is used.
- `lib/mgs2/sdt.py`, `lib/mgs2/cv2.py` and the Cutscene Studio's codec read and write the
  cutscene and keyframe formats.

## Original work

- `restorations/manhattan-crash/audio/arsenal-crash.wav`, the Manhattan crash soundtrack, is an
  original foleyed mix by the author of this repository and is published with it.
- The cutscene authoring (camera, ship course, effects, put-object choreography) and the
  generated flag animations are original data, produced by the code here.

The original work was made with:

| tool | used for |
|---|---|
| Ableton | audio foley |
| Blender | 3D modelling |
| GIMP | image editing |

## Credits and related projects

- [MGSHDFix](https://github.com/ShizCalev/MGSHDFix) supplies the ASI loader and the merging of
  supplementary stage lists that both restorations depend on. Players need it installed.
- [SeaLouse](https://github.com/Jacky720/SeaLouse), a Blender add-on for importing and exporting
  MGS2 models, was used to export every model the Manhattan crash adds.
- The **MoreFlags / 911Restore** mod first put flags back on the Federal Hall street poles.
  The flag restoration measured its pole positions from that mod.
- [MGS2 Audio Tool](https://github.com/romerojopro-web/MGS2-Audio-Tool) is a separate project
  for browsing, exporting and replacing the game's audio. It is not part of this repository.

## Thanks

Thanks to **Jacky720**, **Franatix** and **Afevis** from MGN and MGSHDFix for their help.
