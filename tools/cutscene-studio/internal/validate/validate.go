// Package validate is the baker's validator: the format's rules as executable
// checks. It is what stops us shipping a cutscene that crashes, hangs, or
// silently drops half its content.
//
// Every rule has an id (C1-C20 container, P1-P36 packets, A1-A12 caption
// and audio) and its severity. A Fatal finding means the game breaks; a Warn means the
// stream deviates from every shipped byte Konami wrote; Info is commentary.
//
// Calibration: Stream() over all 262 shipped cutscene demos (1,173,986 demo frames)
// produces zero Fatal findings. A rule that fires on retail data is a wrong rule.
package validate

import (
	"fmt"
	"sort"

	"mgs2-restoration/cutscene-studio/internal/sdt"
)

type Severity int

const (
	Fatal Severity = iota
	Warn
	Info
)

func (s Severity) String() string {
	switch s {
	case Fatal:
		return "FATAL"
	case Warn:
		return "WARN"
	case Info:
		return "INFO"
	}
	return fmt.Sprintf("SEV(%d)", int(s))
}

// Finding is one rule violation.
type Finding struct {
	Rule     string   `json:"rule"` // "P6", "C11", ...
	Severity Severity `json:"severity"`
	Msg      string   `json:"msg"`    // what is wrong, with the actual numbers
	Chunk    int      `json:"chunk"`  // -1 if n/a
	Frame    int      `json:"frame"`  // demo frame, -1 if n/a
	Packet   int      `json:"packet"` // index within the frame, -1 if n/a
	Fix      string   `json:"fix"`    // how to fix it, one line
}

// MarshalJSON emits the severity as a lowercase name. An int would silently read as
// "not fatal" on the client and let a broken bake through the UI's guard.
func (s Severity) MarshalJSON() ([]byte, error) {
	switch s {
	case Fatal:
		return []byte(`"fatal"`), nil
	case Warn:
		return []byte(`"warn"`), nil
	}
	return []byte(`"info"`), nil
}

func (f Finding) String() string {
	loc := ""
	if f.Chunk >= 0 {
		loc = fmt.Sprintf(" chunk=%d", f.Chunk)
	}
	if f.Frame >= 0 {
		loc += fmt.Sprintf(" frame=%d", f.Frame)
	}
	if f.Packet >= 0 {
		loc += fmt.Sprintf(" pkt=%d", f.Packet)
	}
	return fmt.Sprintf("[%s] %s:%s %s", f.Severity, f.Rule, loc, f.Msg)
}

// Context is the out-of-band knowledge a .sdt cannot carry: which stage it plays in,
// which slot it is bound to, how big that slot is. Rules that need it are skipped when
// it is nil, and every one of them is named in its doc comment.
type Context struct {
	Path     string // C19: the file's own path
	FileSize int64  // C18: bytes on disk
	Envelope int64  // C18: next_offset - this_offset from bp_streams.txt (0 = unknown)

	// C20: the slot's tagflag carries TAGFLAG_SOUND_VAG/8BIT, so the sound driver
	// reserves a channel and HANGUP()s if the stream has no audio.
	SoundSlot bool

	// P29: composer ids linked into this demo's stage chara table (from the .lst).
	// nil = unknown, rule degrades to a catalog membership check.
	StageEffects map[int32]bool

	// P36: asset names resident in the stage manifest. nil = rule skipped.
	StageAssets map[string]bool

	// Catalog overrides the built-in effects.json table (e.g. with internal/catalog).
	Catalog Catalog
}

// Result carries the findings plus the uncapped per-rule totals; Findings is capped at
// maxPerRule entries per rule so one broken frame in a 20k-frame demo cannot bury you.
type Result struct {
	Findings []Finding
	Counts   map[string]int
}

const maxPerRule = 8

// Stream validates a decoded .sdt. Findings come back Fatal-first.
func Stream(s *sdt.Stream) []Finding { return StreamReport(s, nil).Findings }

// HasFatal reports whether any finding is Fatal: the one-line "do not bake this" test.
//
// (The brief asked for func Fatal(); Go will not let a package hold both a const Fatal and
// a func Fatal, and the Severity constant is the one every caller compares against.)
func HasFatal(f []Finding) bool {
	for _, x := range f {
		if x.Severity == Fatal {
			return true
		}
	}
	return false
}

// Bytes validates a file's raw bytes: C1 and C18 need them, everything else runs on the
// decoded stream.
func Bytes(buf []byte, ctx *Context) Result {
	c := newCheck(ctx)
	// C1: 926 shipped .sdt are GCL bytecode, not streams. Prevents: reading garbage.
	if len(buf) >= 4 && string(buf[:4]) == "LCGB" {
		c.add("C1", Fatal, -1, -1, -1, "file starts with ASCII LCGB: this is GCL bytecode, not a stream container",
			"point the tool at a real .sdt (us/demo2/_bp/*.sdt); */codec/_bp/* are not streams")
		return c.result()
	}
	s, err := sdt.Decode(buf)
	if err != nil {
		c.add("C2", Fatal, -1, -1, -1, fmt.Sprintf("decode failed: %v", err), "the container is malformed; re-bake from a known-good source")
		return c.result()
	}
	if ctx != nil && ctx.FileSize == 0 {
		cp := *ctx
		cp.FileSize = int64(len(buf))
		c.ctx = &cp
	}
	c.run(s)
	return c.result()
}

// StreamReport is Stream plus the uncapped per-rule totals.
func StreamReport(s *sdt.Stream, ctx *Context) Result {
	c := newCheck(ctx)
	c.run(s)
	return c.result()
}

type check struct {
	ctx      *Context
	cat      Catalog
	findings []Finding
	counts   map[string]int
}

func newCheck(ctx *Context) *check {
	cat := Catalog(builtin{})
	if ctx != nil && ctx.Catalog != nil {
		cat = ctx.Catalog
	}
	return &check{ctx: ctx, cat: cat, counts: map[string]int{}}
}

func (c *check) add(rule string, sev Severity, chunk, frame, packet int, msg, fix string) {
	c.counts[rule]++
	if c.counts[rule] > maxPerRule {
		return
	}
	c.findings = append(c.findings, Finding{Rule: rule, Severity: sev, Msg: msg, Chunk: chunk, Frame: frame, Packet: packet, Fix: fix})
}

func (c *check) result() Result {
	for rule, n := range c.counts {
		if n > maxPerRule {
			sev := Info
			for _, f := range c.findings {
				if f.Rule == rule {
					sev = f.Severity
					break
				}
			}
			c.findings = append(c.findings, Finding{
				Rule: rule, Severity: sev, Chunk: -1, Frame: -1, Packet: -1,
				Msg: fmt.Sprintf("%d further %s findings suppressed (%d total)", n-maxPerRule, rule, n),
				Fix: "fix the reported ones first; they are usually the same bug",
			})
		}
	}
	sort.SliceStable(c.findings, func(i, j int) bool { return c.findings[i].Severity < c.findings[j].Severity })
	return Result{Findings: c.findings, Counts: c.counts}
}

func (c *check) run(s *sdt.Stream) {
	c.container(s)
	c.demo(s)
	c.captions(s)
	c.audio(s)
}
