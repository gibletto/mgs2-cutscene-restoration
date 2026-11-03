# How MGS2 cutscenes work

This is what we had to understand about the game's cutscenes to build the two restorations.
It explains ideas, not every byte. The byte-level details are in the code that reads and
writes the files (`lib/mgs2/sdt.py` and the Cutscene Studio in `tools/cutscene-studio`).

## A cutscene is a flipbook

Each cutscene is one `.sdt` file, for example `us/demo2/_bp/p080_13_p08.sdt`. The game reads it
from disk every time the cutscene plays, so you can replace the file and replay it without
restarting.

The file is a flipbook running at 60 pages a second. Each page (a **frame**) says exactly
where everything is on that frame. The game does not smooth anything in between, so a slow
camera pan is just hundreds of pages, each with the camera nudged a little further. Writing
out every one of those pages from a smooth curve is called **baking**.

Each page is a short list of instructions, called **packets**:

- **the camera**: where it is, what it looks at, how far it zooms, and its roll;
- **the actors**: each character's pose for this frame;
- **effects**: everything else (see below);
- **an end marker**, which every page needs. A page without one can crash the game.

The same file also carries the cutscene's **audio** and its **subtitles**, mixed in with the
pages.

## The cast

Actors are introduced on the first page and stay for the whole cutscene. They "enter" and
"leave" by being shown and hidden.

A cutscene can only use models its stage has already loaded. To use a new model you register
it with the stage first ([asset-registration.md](asset-registration.md)).

An actor is only drawn on pages that have a pose for it. That caught us out when we
lengthened the Manhattan cutscene: past the original last page, the actors simply vanished.
Anything that had to appear in the new part became a **put-object**, a prop placed by the
stage's script, which stays put until told otherwise.

## Effects do everything else

Lights, fog, fades, fire, smoke, splashes, blur, slow motion, controller rumble: all of these
are **effects**. The game has about 245 kinds built in, each with its own settings. A
cutscene switches one on by writing an effect packet onto a page.

- Effects that change smoothly (a light dimming, a prop moving) are written again on every
  page with new values.
- Effects that run by themselves (a fire, a splash) are started once with a lifetime in
  frames.

Things to know:

- **A stage only has some effects.** Using one the stage doesn't include crashes the game.
  The spark effect does this in the Manhattan stage.
- **There is a budget.** A cutscene can start at most 512 self-running effects in total. The
  512th crashes the game, so the Manhattan build stops itself at 500.
- **Effects can talk to the stage.** One effect sends a message to a named put-object
  (show, hide, move, turn), which is how the Manhattan cutscene swaps the city, topples the
  towers and carries the statue. A message sent on frame *f* shows up on frame *f + 1*, so
  anything meant to change under a camera cut is sent one frame early.
- **Effects can run the stage's script.** Another effect calls a named routine in the
  stage's script. The flag restoration uses it to raise the flag and show the fallen one.
  The routine has to be on the cutscene's allowed list in the script (at most 16 names).

## The soundtrack is the clock

While a cutscene plays, the soundtrack keeps time: the pages follow the audio. So the audio
has to be right.

- It must last the whole cutscene. When ours ran short, the cutscene froze at the end on
  the white screen.
- It is stored as WMA, cut into fixed-size pieces, with a header at the front. We copy the
  framing from an existing cutscene (the tanker intro) and swap in our own audio.
- Not every WMA encoder works. Windows' own WMA8 encoder plays correctly; ffmpeg's produced
  noise.

## Changing a cutscene's length

Every page carries a timestamp, so pages can't be cut out of a finished file and pasted into
another. Edits happen to the scene before it is baked, and the bake numbers every page again.

- **Adding** pages is fine. Music written for the original will land early relative to the
  pictures, so it needs re-placing.
- **Removing** pages needs the same amount of audio removed too. Otherwise the audio runs
  ahead and the game races to catch up.
- The HD release's widescreen camera framing is stored separately, by page number. Pages
  added past the original end have no entry there.

## How the restorations edit cutscenes

**The Cutscene Studio** (`tools/cutscene-studio`) opens a shipped cutscene as an editable
scene with a timeline, a viewport and effect panels, and bakes it back out. It only rewrites
what you changed. Everything else is copied through untouched, so a cutscene you open and
bake without editing comes back identical to the original. That's what makes it safe to
build on Konami's files.

**The Manhattan crash** drives the studio from a script, in three passes:

1. **Author**: open Konami's cutscene, lengthen it and write the new shots (camera, the
   ship's course, the put-object messages), then bake.
2. **Dress**: add fire, smoke, debris, splashes and rumble straight into the baked pages.
3. **Audio**: encode the soundtrack and put it in.

**The Federal Hall flag** needs much less. It adds a few effect packets to the existing
cutscenes at chosen frames (`lib/mgs2/demo_edit.py`): calls into the stage's script, and the
packet that plays the falling-flag animation.
