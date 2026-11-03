# MGS2 restoration

> **About AI use.** Cutscene Studio was created with Claude to enable visualization and editing
> of cutscenes. It is a very rough tool, which should be used for visualization or scripting,
> not direct editing if possible. Humans made the models, the animations, the soundtrack foley,
> the textures and the script, and hand-animated a lot of this. Scripts were both hand-written
> and modified with AI.

A public record of how cut content was restored to *Metal Gear Solid 2: Sons of Liberty* in the
PC Master Collection, and the tools and format notes it took.

Each restoration is a folder with its own write-up and a build that reproduces the released
mod from your own copy of the game. The repository holds **code and documentation only**:
no game files, no files from the leaked source tree, nothing generated from either, and no
compilers or binaries. See [NOTICE.md](NOTICE.md).

## Restorations

| | |
|---|---|
| [**Federal Hall flag**](restorations/federal-hall-flag/README.md) | The American flag put back into the ending: flying through Solidus's death, falling onto him when the pole is cut, dragged away afterwards, and hanging from the street poles. Everything is generated from assets the game already ships. |
| [**Manhattan crash**](restorations/manhattan-crash/README.md) | Arsenal Gear runs aground on Manhattan. Konami's unused crash kit is wired back into the stage, and the arrival cutscene is extended from 2,180 to 4,111 frames with new shots, effects and an original soundtrack. The new models and textures are not included here; the build takes them from the [released mod](https://www.nexusmods.com/metalgearsolid2mc/mods/135). |

## Tools and documentation

| | |
|---|---|
| [tools/cutscene-studio](tools/cutscene-studio/README.md) | Cutscene Studio: a timeline editor for the game's `.sdt` cutscenes. It imports a shipped cutscene, edits it and bakes it back byte for byte. The Manhattan build drives it headlessly. |
| [lib/mgs2](lib/mgs2/) | Shared Python: the file formats, the name hash, the scenario rebuild, reversible deployment |
| [docs/SETUP.md](docs/SETUP.md) | What you need, and how to configure a build |
| [docs/how-cutscenes-work.md](docs/how-cutscenes-work.md) | How the game's cutscenes work, in plain terms |
| [docs/scenario-pipeline.md](docs/scenario-pipeline.md) | Rebuilding a stage's scenario (`.gcx`) from Konami's source, with an edit |
| [docs/asset-registration.md](docs/asset-registration.md) | Making a stage load new models and textures |
| [docs/formats.md](docs/formats.md) | The game's files we touched, and what we did with each |

## How it fits together

Nothing here runs on its own. A build pairs this code with three things you bring yourself:

- **the game**, a normal Steam install, which is only ever read;
- **the leaked source tree**, for the scenario source and Konami's GCL compiler, which are used
  in place and never copied here;
- for the Manhattan crash, **its models and textures**, which `from_release.py` copies out of
  the [released mod](https://www.nexusmods.com/metalgearsolid2mc/mods/135).

Configure them in `local.env` ([docs/SETUP.md](docs/SETUP.md)), then run a restoration's
`build.py`. It writes an `update/` folder under your build directory. `deploy.py` copies that
into the game's own `update/` folder, which the Ultimate ASI Loader from
[MGSHDFix](https://github.com/ShizCalev/MGSHDFix) redirects file opens into. Deployment is
reversible, and the game's own files are never modified.

Each build checks itself against what the game ships. A scenario rebuild first proves that
an unmodified compile reproduces the shipped file. A cutscene that hasn't been edited bakes
back byte for byte. The write-ups say what each build was verified against.

## Thanks

Thanks to **Jacky720**, **Franatix** and **Afevis** from MGN and MGSHDFix. Credits for the
tools and mods this work builds on are in [NOTICE.md](NOTICE.md).
