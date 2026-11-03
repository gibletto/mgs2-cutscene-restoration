"""When the flag waves and how fast.

Parts of the ending run slow, either because effect 0x13 holds every frame twice
or because the motion was simply authored slow. Actors keep ticking regardless,
so the flag needs a copy per rate and the cutscene swaps which one is visible --
nothing can change a vertex animation's rate once it is running.

The keyframed-mesh actor works a rate out as
    ticks per key = (base + (irnd() & ((1 << rand) - 1)) + 1) * 5 / 6
At -w 1 that lands on two values unless base + 1 divides by six; at -w 0 the
randomiser is masked away and any tick count is exact.
"""

# name -> (-b, -w, what it is for)
RATES = {
    'normal': (11, 1, 'the PS2 rate; Konami wrote 5, which is double speed on the port'),
    'slow':   (23, 1, 'half again, matching the frame-skip effect holding every frame twice'),
    'slower': (31, 0, 'a further 30%, for the shots whose motion was animated slow'),
}

SHOW_FRAME = 3            # just after the frame-2 call that hides the flag

# (first frame, last frame, rate) of every shot of p082_01_p01 that is not full speed
SLOW_WINDOWS = [
    (0, 302, 'slow'),        # effect 0x13, frame 0 life 302
    (366, 700, 'slower'),    # animated slow, no effect
    (775, 963, 'slower'),    # animated slow, no effect
    (3390, 3701, 'slower'),  # animated slow, no effect
    (4806, 5014, 'slow'),    # effect 0x13, frame 4806 life 208
]

FLAG_ACTOR = 'アメリカの旗'                       # "American flag": Konami's own actor
ACTORS = {'slow': 'アメリカの旗スロー',           # ...slow
          'slower': 'アメリカの旗スロー２'}       # ...slow 2
PROCS = {'normal': '旗スローオフ',                # flag slow off
         'slow': '旗スローオン',                  # flag slow on
         'slower': '旗スロー２オン'}              # flag slow 2 on


def ticks(rate: str) -> int:
    """How many ticks a key lasts at this rate. Every rate here is deterministic."""
    base, rand, _ = RATES[rate]
    values = {((base + r + 1) * 5) // 6 for r in range(1 << rand)}
    if len(values) != 1:
        raise SystemExit(f'rate {rate!r} (-b {base} -w {rand}) is not deterministic: {sorted(values)}')
    return values.pop()


def rate_at(frame: int) -> str:
    for first, last, rate in SLOW_WINDOWS:
        if first <= frame <= last:
            return rate
    return 'normal'


def calls(show_frame: int = SHOW_FRAME) -> list[tuple[int, str]]:
    """The proc calls the cutscene needs, in frame order: one where the flag
    appears, for the rate right there, then one at every rate change."""
    out = [(show_frame, PROCS[rate_at(show_frame)])]
    edges = sorted({f for first, last, _ in SLOW_WINDOWS for f in (first, last + 1)
                    if f > show_frame})
    for frame in edges:
        rate = rate_at(frame)
        if rate != rate_at(frame - 1):
            out.append((frame, PROCS[rate]))
    return out


def describe():
    base_ticks = ticks('normal')
    for name, (base, rand, why) in RATES.items():
        t = ticks(name)
        print(f'  {name:7} -b {base:2} -w {rand}  {t:2} ticks per key  '
              f'({t / base_ticks:.2f}x the normal rate)  -- {why}')
    for first, last, rate in SLOW_WINDOWS:
        print(f'  frames {first:5d} .. {last:<5d} {rate}')
    for frame, proc in calls():
        print(f'  frame {frame:5d}  {proc}')
