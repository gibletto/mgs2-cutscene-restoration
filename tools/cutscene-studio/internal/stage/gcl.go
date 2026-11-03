package stage

// A reader for Konami's GCL scenario language, enough of it to recover a stage's world.
//
// The source is EUC-JP. We never decode it: chara types, proc names and object names are
// compared as raw bytes, and everything we emit (model names) is ASCII. That keeps the
// package on the stdlib and still gets the encoding right.
//
// Two facts from the engine drive the parser:
//
//   - GCL_GetOption() takes a CHAR. Only an option's FIRST LETTER is significant, so
//     "-size" IS "-s" and "-pos" IS "-p" (huge_sea.c:507-568, putobj.c:244-290).
//   - putobj.c:279-286 divides -s by 100. "-s 1000,1000,1000" is scale 10, not 1000.
//     The NY-crash stage is authored at 1/10 and blown up, which is why its lighting set
//     is literally called w62a_scale10_am700.

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// chara type names, as EUC-JP bytes.
const (
	tPutObject = "\xa5\xd7\xa5\xc3\xa5\xc8\xa5\xaa\xa5\xd6\xa5\xb8\xa5\xa7" // プットオブジェ
	tHugeSea   = "\xb5\xf0\xc2\xe7\xb3\xa4\xcc\xcc"                         // 巨大海面
	tPlantSea  = "\xa5\xd7\xa5\xe9\xa5\xf3\xa5\xc8\xb3\xa4\xcc\xcc"         // プラント海面
	tSky       = "\xc8\xc6\xcd\xd1\xb6\xf5"                                 // 汎用空
	tFog       = "\xa5\xd5\xa5\xa9\xa5\xb0"                                 // フォグ
	tMap       = "\xa5\xde\xa5\xc3\xa5\xd7"                                 // マップ
	tMapSet    = "\xa5\xde\xa5\xc3\xa5\xd7\xc0\xdf\xc4\xea"                 // マップ設定
)

// angUnit converts MGS2's angle units to radians: 4096 == 360 degrees (ANG2RAD, emma.h:65).
const angUnit = math.Pi / 2048

// baseDefs are the defines gclconv is invoked with (scn/_bp_gclc_args_*.txt). MGS2_DEMO=0
// matters: it compiles out the demo-team debug branch that would otherwise look like the
// stage plays a stream called "test01".
func baseDefs() map[string]string {
	return map[string]string{
		"MGS2_VRTRIAL": "0", "MGS2_ENG_VRTRIAL": "0", "MGS2_SCN": "0", "MGS2_DEMO": "0",
		"MGS2_TANKER": "1", "MGS2_PLANT": "1", "FIX_MODE": "1", "MGS2_MEMCARD": "0",
		"PSX2": "1", "ENGLISH": "1", "EU": "1", "AREA_EU": "1", "PAL": "1",
	}
}

type gcl struct {
	scnDir string
	defs   map[string]string
	lines  []string
}

// stmt is one parsed statement.
type stmt struct {
	kind  string // "chara", "mesg", "call", "open", "close"
	typ   string // chara type (raw bytes)
	name  string // instance name (raw bytes)
	opts  map[byte][]string
	words []string
}

// ParseStage reads scn/_bp/scenerio_stage_<name>.gcl (and everything it includes) and
// recovers the world. scnDir is Konami's mgs2x/scn.
func ParseStage(scnDir, stageName string) (*Stage, error) {
	g := &gcl{scnDir: scnDir, defs: baseDefs()}
	root := filepath.Join(scnDir, "_bp", "scenerio_stage_"+stageName+".gcl")
	if err := g.include(root, 0); err != nil {
		return nil, err
	}
	return g.build(stageName), nil
}

// ScanDemos lists the cutscene streams a stage's scenario names. A scenario plays one
// with `chara demo <name> -i t:<stream>`; inverting that over every stage is the whole
// demo -> stage map.
func ScanDemos(scnDir, stageName string) ([]string, error) {
	g := &gcl{scnDir: scnDir, defs: baseDefs()}
	root := filepath.Join(scnDir, "_bp", "scenerio_stage_"+stageName+".gcl")
	if err := g.include(root, 0); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range g.parseStmts() {
		if s.kind != "chara" {
			continue
		}
		for _, v := range s.opts['i'] {
			if !strings.HasPrefix(v, "t:") {
				continue
			}
			n := strings.ToLower(strings.TrimPrefix(v, "t:"))
			if n != "" && !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out, nil
}

// include inlines a file: strip comments, join continuations, run the preprocessor.
func (g *gcl) include(path string, depth int) error {
	if depth > 16 {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := joinContinuations(stripComments(string(raw)))

	type frame struct{ active, taken bool }
	var stack []frame
	live := func() bool {
		for _, f := range stack {
			if !f.active {
				return false
			}
		}
		return true
	}

	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "#") {
			d := strings.Fields(t)
			switch d[0] {
			case "#if", "#ifdef", "#ifndef":
				v := false
				if len(d) > 1 {
					expr := strings.TrimSpace(t[len(d[0]):])
					switch d[0] {
					case "#ifdef":
						_, v = g.defs[strings.TrimPrefix(d[1], "d:")]
					case "#ifndef":
						_, v = g.defs[strings.TrimPrefix(d[1], "d:")]
						v = !v
					default:
						v = g.eval(expr) != 0
					}
				}
				stack = append(stack, frame{active: v && live(), taken: v})
			case "#elif":
				if n := len(stack); n > 0 {
					f := &stack[n-1]
					v := !f.taken && g.eval(strings.TrimSpace(t[5:])) != 0
					f.active, f.taken = v, f.taken || v
				}
			case "#else":
				if n := len(stack); n > 0 {
					f := &stack[n-1]
					f.active = !f.taken
					f.taken = true
				}
			case "#endif":
				if n := len(stack); n > 0 {
					stack = stack[:n-1]
				}
			case "#define":
				if live() && len(d) >= 2 {
					val := "1"
					if len(d) >= 3 {
						val = strings.TrimSpace(t[strings.Index(t, d[1])+len(d[1]):])
					}
					g.defs[d[1]] = val
				}
			case "#include":
				if live() && len(d) >= 2 {
					name := strings.Trim(d[1], `"<>`)
					if p := g.findInclude(name, filepath.Dir(path)); p != "" {
						if err := g.include(p, depth+1); err != nil {
							return err
						}
					}
				}
			}
			continue
		}
		if live() && t != "" {
			g.lines = append(g.lines, t)
		}
	}
	return nil
}

// findInclude mirrors gclconv, which runs with cwd = scn/ and has no -I paths.
func (g *gcl) findInclude(name, curDir string) string {
	for _, d := range []string{curDir, g.scnDir, filepath.Join(g.scnDir, "_bp")} {
		p := filepath.Join(d, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func stripComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		switch {
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '*':
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return b.String()
			}
			// keep newlines so line structure survives
			for _, c := range s[i : i+2+j+2] {
				if c == '\n' {
					b.WriteByte('\n')
				}
			}
			i += 2 + j + 2
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '/':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return b.String()
			}
			i += j
		case s[i] == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				b.WriteString(s[i:])
				return b.String()
			}
			b.WriteString(s[i : i+1+j+1])
			i += 1 + j + 1
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// joinContinuations folds "...\" lines. Comments are already gone, so a line that ended
// in "\ // note" now ends in "\" plus spaces.
//
// A continuation survives a blank line, because a commented-out argument in the middle of
// a spliced statement leaves one behind -- and dropping the splice there would lose the
// rest of the statement (it is how p080_13_p08's own `chara demo` names its stream).
func joinContinuations(s string) []string {
	var out []string
	var cur strings.Builder
	pending := false
	flush := func() {
		out = append(out, cur.String())
		cur.Reset()
		pending = false
	}
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimRight(ln, " \t\r")
		t := strings.TrimSpace(ln)
		if pending {
			if t == "" {
				continue
			}
			// A directive is never part of a statement. If a splice runs into one, the
			// trailing "\" belonged to a commented-out argument -- end the statement here,
			// or the "#else" gets eaten and its whole branch stays dead.
			if t[0] == '#' {
				flush()
				out = append(out, ln)
				continue
			}
		}
		if strings.HasSuffix(ln, "\\") {
			cur.WriteString(strings.TrimSuffix(ln, "\\"))
			cur.WriteByte(' ')
			pending = true
			continue
		}
		cur.WriteString(ln)
		flush()
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ---- expressions -------------------------------------------------------------

// eval handles the preprocessor conditions and the arithmetic that shows up in args
// (e.g. "-l (32*10)"). Unknown names are 0, which is what an unset define means.
func (g *gcl) eval(s string) int64 {
	toks := tokenizeExpr(s)
	p := &exprParser{toks: toks, defs: g.defs}
	return p.parseOr()
}

func tokenizeExpr(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case strings.HasPrefix(s[i:], "&&"), strings.HasPrefix(s[i:], "||"),
			strings.HasPrefix(s[i:], "=="), strings.HasPrefix(s[i:], "!="),
			strings.HasPrefix(s[i:], ">="), strings.HasPrefix(s[i:], "<="):
			out = append(out, s[i:i+2])
			i += 2
		case strings.ContainsRune("()!+-*/<>", rune(c)):
			out = append(out, string(c))
			i++
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t()!+-*/<>&|=", rune(s[j])) {
				j++
			}
			if j == i {
				i++
				continue
			}
			out = append(out, s[i:j])
			i = j
		}
	}
	return out
}

type exprParser struct {
	toks []string
	i    int
	defs map[string]string
}

func (p *exprParser) peek() string {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return ""
}
func (p *exprParser) next() string { t := p.peek(); p.i++; return t }

func (p *exprParser) parseOr() int64 {
	v := p.parseAnd()
	for p.peek() == "||" {
		p.next()
		r := p.parseAnd()
		v = b2i(v != 0 || r != 0)
	}
	return v
}
func (p *exprParser) parseAnd() int64 {
	v := p.parseCmp()
	for p.peek() == "&&" {
		p.next()
		r := p.parseCmp()
		v = b2i(v != 0 && r != 0)
	}
	return v
}
func (p *exprParser) parseCmp() int64 {
	v := p.parseAdd()
	for {
		switch p.peek() {
		case "==":
			p.next()
			v = b2i(v == p.parseAdd())
		case "!=":
			p.next()
			v = b2i(v != p.parseAdd())
		case "<":
			p.next()
			v = b2i(v < p.parseAdd())
		case ">":
			p.next()
			v = b2i(v > p.parseAdd())
		case "<=":
			p.next()
			v = b2i(v <= p.parseAdd())
		case ">=":
			p.next()
			v = b2i(v >= p.parseAdd())
		default:
			return v
		}
	}
}
func (p *exprParser) parseAdd() int64 {
	v := p.parseMul()
	for {
		switch p.peek() {
		case "+":
			p.next()
			v += p.parseMul()
		case "-":
			p.next()
			v -= p.parseMul()
		default:
			return v
		}
	}
}
func (p *exprParser) parseMul() int64 {
	v := p.parseUnary()
	for {
		switch p.peek() {
		case "*":
			p.next()
			v *= p.parseUnary()
		case "/":
			p.next()
			if d := p.parseUnary(); d != 0 {
				v /= d
			}
		default:
			return v
		}
	}
}
func (p *exprParser) parseUnary() int64 {
	switch p.peek() {
	case "!":
		p.next()
		return b2i(p.parseUnary() == 0)
	case "-":
		p.next()
		return -p.parseUnary()
	case "+":
		p.next()
		return p.parseUnary()
	case "(":
		p.next()
		v := p.parseOr()
		if p.peek() == ")" {
			p.next()
		}
		return v
	}
	t := p.next()
	if t == "" {
		return 0
	}
	return p.atom(t, 0)
}

// atom resolves a token: an integer, or a define (possibly chained: d:A -> d:B -> 3).
func (p *exprParser) atom(t string, depth int) int64 {
	t = strings.TrimPrefix(t, "d:")
	if v, err := strconv.ParseInt(t, 0, 64); err == nil {
		return v
	}
	if depth > 8 {
		return 0
	}
	if v, ok := p.defs[t]; ok {
		sub := &exprParser{toks: tokenizeExpr(v), defs: p.defs}
		if len(sub.toks) == 1 {
			return sub.atom(sub.toks[0], depth+1)
		}
		return sub.parseOr()
	}
	return 0
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// resolve expands a bare argument: "d:LT2_NAME_100" -> "w62a_scale10_am700".
func (g *gcl) resolve(s string, depth int) string {
	if depth > 8 {
		return s
	}
	if v, ok := g.defs[strings.TrimPrefix(s, "d:")]; ok && strings.HasPrefix(s, "d:") {
		return g.resolve(strings.TrimSpace(v), depth+1)
	}
	return s
}

// num parses an argument as a number, running it through the defines and the arithmetic
// evaluator so "(32*10)" and "d:SOME_SIZE" both work.
func (g *gcl) num(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	if v, err := strconv.ParseInt(s, 0, 64); err == nil { // 0x0010
		return float64(v)
	}
	return float64(g.eval(s))
}

// ---- statements --------------------------------------------------------------

// isOpt: an option starts with '-' and a LETTER. "-20000" is a negative number, not an option.
func isOpt(w string) bool {
	return len(w) >= 2 && w[0] == '-' && (w[1] >= 'a' && w[1] <= 'z' || w[1] >= 'A' && w[1] <= 'Z')
}

// splitArgs flattens "0,0,0" and "35, 40, 41" into separate values.
func splitArgs(ws []string) []string {
	var out []string
	for _, w := range ws {
		for _, p := range strings.Split(w, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// parseStmts turns the flattened source into a statement list, keyed by the first letter
// of each option -- which is all the engine reads.
func (g *gcl) parseStmts() []stmt {
	var out []stmt
	for _, ln := range g.lines {
		ws := strings.Fields(ln)
		for len(ws) > 0 {
			// a line can be "} proc x {" etc; peel braces
			switch ws[0] {
			case "{":
				out = append(out, stmt{kind: "open"})
				ws = ws[1:]
				continue
			case "}":
				out = append(out, stmt{kind: "close"})
				ws = ws[1:]
				continue
			}
			break
		}
		if len(ws) == 0 {
			continue
		}
		// trailing brace on the same line ("proc x {", "chara y -exec{ ... }")
		open, closeN := 0, 0
		for _, w := range ws {
			open += strings.Count(w, "{")
			closeN += strings.Count(w, "}")
		}

		head := ws[0]
		switch {
		case head == "chara" && len(ws) >= 3:
			s := stmt{kind: "chara", typ: ws[1], name: ws[2], opts: map[byte][]string{}, words: ws}
			var cur byte
			var acc []string
			flush := func() {
				if cur != 0 {
					s.opts[cur] = append(s.opts[cur], splitArgs(acc)...)
				}
				acc = nil
			}
			for _, w := range ws[3:] {
				if isOpt(w) {
					flush()
					cur = w[1]
					if _, ok := s.opts[cur]; !ok {
						s.opts[cur] = []string{}
					}
					continue
				}
				if cur != 0 {
					acc = append(acc, w)
				}
			}
			flush()
			out = append(out, s)
		case head == "mesg" && len(ws) >= 4:
			out = append(out, stmt{kind: "mesg", typ: ws[1], name: ws[2], words: ws})
		case head == "proc" && len(ws) >= 2:
			out = append(out, stmt{kind: "proc", name: ws[1], words: ws})
		case head == "command" && len(ws) >= 3:
			out = append(out, stmt{kind: "command", typ: ws[1], name: ws[2], words: ws})
		case strings.HasPrefix(head, "@"):
			out = append(out, stmt{kind: "call", name: strings.TrimPrefix(head, "@"), words: ws})
		default:
			out = append(out, stmt{kind: "other", words: ws})
		}
		for i := 0; i < open; i++ {
			out = append(out, stmt{kind: "open"})
		}
		for i := 0; i < closeN; i++ {
			out = append(out, stmt{kind: "close"})
		}
	}
	return out
}

// ---- world building ----------------------------------------------------------

type body struct{ from, to int } // statement range, exclusive of the braces

func (g *gcl) build(stageName string) *Stage {
	sts := g.parseStmts()

	procs := map[string]body{}
	var initBodies []body
	for i := 0; i < len(sts); i++ {
		s := sts[i]
		isInit := s.kind == "command" && s.typ == tMapSet
		if s.kind != "proc" && !isInit {
			continue
		}
		// find the "{" that opens this block, then its matching "}"
		j := i + 1
		for j < len(sts) && sts[j].kind != "open" {
			if sts[j].kind == "proc" || sts[j].kind == "command" {
				break
			}
			j++
		}
		if j >= len(sts) || sts[j].kind != "open" {
			continue
		}
		depth, k := 1, j+1
		for ; k < len(sts) && depth > 0; k++ {
			switch sts[k].kind {
			case "open":
				depth++
			case "close":
				depth--
			}
		}
		b := body{j + 1, k - 1}
		if isInit {
			initBodies = append(initBodies, b)
		} else {
			procs[s.name] = b
		}
	}

	w := &world{g: g, sts: sts, procs: procs, byName: map[string]int{}}

	// Statements outside every proc and command block run at stage load -- that is where
	// `chara マップ <name> -h <hzx> -l <lt2>` lives.
	inBlock := make([]bool, len(sts))
	mark := func(b body) {
		for i := b.from; i < b.to && i < len(sts); i++ {
			inBlock[i] = true
		}
	}
	for _, b := range procs {
		mark(b)
	}
	for _, b := range initBodies {
		mark(b)
	}
	for i, s := range sts {
		if !inBlock[i] && s.kind == "chara" {
			w.chara(s, true)
		}
	}

	for _, b := range initBodies {
		w.run(b, map[string]bool{})
	}
	w.collectUnreached()

	st := &Stage{Name: stageName, Placements: w.places, Sky: w.sky, Sea: w.sea, Fog: w.fog,
		HZX: w.hzx, LT2: w.lt2, Toggles: w.toggles}
	labelStage(st)
	return st
}

type world struct {
	g       *gcl
	sts     []stmt
	procs   map[string]body
	places  []Placement
	byName  map[string]int // chara instance name -> index into places
	sky     *Sky
	sea     *Sea
	fog     *Fog
	hzx     string
	lt2     string
	toggles []Toggle
	demoPrc []string
}

// run executes a block: create charas, apply mesg on/off, follow @proc calls -- in order,
// because "create then switch off" is exactly how Konami hides the Manhattan skyline.
func (w *world) run(b body, seen map[string]bool) {
	for i := b.from; i < b.to && i < len(w.sts); i++ {
		s := w.sts[i]
		switch s.kind {
		case "call":
			if seen[s.name] {
				continue
			}
			if p, ok := w.procs[s.name]; ok {
				seen[s.name] = true
				w.run(p, seen)
				delete(seen, s.name)
			}
		case "chara":
			w.chara(s, true)
		case "mesg":
			if s.typ == tPutObject && len(s.words) >= 4 {
				if idx, ok := w.byName[s.name]; ok {
					switch s.words[3] {
					case "on":
						w.places[idx].Visible = true
					case "off":
						w.places[idx].Visible = false
					}
				}
			}
		}
	}
}

func (w *world) chara(s stmt, created bool) {
	g := w.g
	switch s.typ {
	case tPutObject:
		models := s.opts['m']
		if len(models) == 0 {
			return
		}
		name := g.resolve(models[0], 0)
		if !isASCIIIdent(name) { // "$$:ファイル名" -- a parameterised proc, not a fixed model
			return
		}
		p := Placement{
			Name:    name,
			CacheID: StrCode(name), // the manifest confirms this and adds the real extension
			Ext:     "kms",
			Scale:   [3]float32{1, 1, 1},
			Visible: true,
			Created: created,
			LT2:     g.resolve(first(s.opts['l']), 0),
		}
		for _, m := range models[1:] {
			if m = g.resolve(m, 0); isASCIIIdent(m) {
				p.LODs = append(p.LODs, m)
			}
		}
		if v := s.opts['p']; len(v) >= 3 {
			for i := 0; i < 3; i++ {
				p.Pos[i] = float32(g.num(v[i]))
			}
		}
		if v := s.opts['r']; len(v) >= 3 {
			for i := 0; i < 3; i++ {
				p.Rot[i] = float32(g.num(v[i]) * angUnit)
			}
		}
		if v := s.opts['s']; len(v) >= 3 {
			for i := 0; i < 3; i++ {
				p.Scale[i] = float32(g.num(v[i]) / 100) // putobj.c:281 -- -s is hundredths
			}
		}
		if v := s.opts['f']; len(v) >= 1 {
			p.Flags = int32(g.num(v[0]))
		}
		if idx, ok := w.byName[s.name]; ok { // re-placed: last one wins
			w.places[idx] = p
			return
		}
		w.byName[s.name] = len(w.places)
		w.places = append(w.places, p)

	case tSky:
		sk := &Sky{Kind: "generic_sky", Params: map[string]any{}}
		if v := s.opts['c']; len(v) >= 3 {
			sk.Params["baseColor"] = []any{g.num(v[0]), g.num(v[1]), g.num(v[2])}
		}
		if v := s.opts['p']; len(v) >= 3 {
			sk.Params["pos"] = []any{g.num(v[0]), g.num(v[1]), g.num(v[2])}
		}
		if v := s.opts['l']; len(v) >= 1 {
			sk.Params["level"] = g.num(v[0])
		}
		// -t N then N layers of (tex r g b a); -m N then N walls of (model r g b p q)
		if v := s.opts['t']; len(v) >= 1 {
			var layers []any
			n := int(g.num(v[0]))
			for i := 0; i < n && 1+i*5+4 < len(v); i++ {
				b := v[1+i*5:]
				if b[0] == "0" {
					continue
				}
				layers = append(layers, map[string]any{
					"tex": b[0], "tint": []any{g.num(b[1]), g.num(b[2]), g.num(b[3])}, "alpha": g.num(b[4]),
				})
			}
			if len(layers) > 0 {
				sk.Params["cloudLayers"] = layers
			}
		}
		if v := s.opts['m']; len(v) >= 1 {
			var walls []any
			n := int(g.num(v[0]))
			for i := 0; i < n && 1+i*6+5 < len(v); i++ {
				b := v[1+i*6:]
				walls = append(walls, map[string]any{
					"name": b[0], "cacheId": float64(StrCode(b[0])), "ext": "kms",
					"tint": []any{g.num(b[1]), g.num(b[2]), g.num(b[3])},
				})
			}
			if len(walls) > 0 {
				sk.Params["walls"] = walls
			}
		}
		w.sky = sk

	case tHugeSea, tPlantSea:
		kind := "huge_sea"
		if s.typ == tPlantSea {
			kind = "plant_sea"
		}
		sea := &Sea{Kind: kind, Size: [2]float32{400000, 400000}} // huge_sea.c:503 defaults
		if v := s.opts['p']; len(v) >= 3 {
			for i := 0; i < 3; i++ {
				sea.Pos[i] = float32(g.num(v[i]))
			}
		}
		if v := s.opts['s']; len(v) >= 2 {
			sea.Size[0] = float32(g.num(v[0]))
			sea.Size[1] = float32(g.num(v[1]))
		}
		for _, t := range s.opts['t'] {
			if isASCIIIdent(t) {
				sea.Tex = append(sea.Tex, t)
			}
		}
		w.sea = sea

	case tFog:
		f := &Fog{}
		if v := s.opts['c']; len(v) >= 3 {
			for i := 0; i < 3; i++ {
				f.Color[i] = int32(g.num(v[i]))
			}
		}
		if v := s.opts['n']; len(v) >= 1 {
			f.Near = float32(g.num(v[0]))
		}
		if v := s.opts['f']; len(v) >= 1 {
			f.Far = float32(g.num(v[0]))
		}
		w.fog = f

	case tMap:
		if v := s.opts['h']; len(v) >= 1 {
			w.hzx = g.resolve(v[0], 0)
		}
		if v := s.opts['l']; len(v) >= 1 {
			w.lt2 = g.resolve(v[0], 0)
		}

	case "demo":
		// chara demo <name> -i t:<stream> -p <proc>... : the procs the cutscene may fire.
		for _, p := range s.opts['p'] {
			w.demoPrc = append(w.demoPrc, p)
		}
	}
}

// collectUnreached records geometry the shipped script builds but never runs -- the
// wrecked-Manhattan land (vw60a_9) sits in a proc that is commented out of stage init.
// The editor should be able to show it; that is the whole Manhattan-restoration finding.
func (w *world) collectUnreached() {
	for name, b := range w.procs {
		for i := b.from; i < b.to && i < len(w.sts); i++ {
			s := w.sts[i]
			if s.kind != "chara" || s.typ != tPutObject {
				continue
			}
			if _, done := w.byName[s.name]; done {
				continue
			}
			w.chara(s, false)
			if idx, ok := w.byName[s.name]; ok {
				w.places[idx].Created = false
				w.places[idx].Visible = false
			}
		}
		_ = name
	}
	// what the cutscene itself can switch mid-shot
	for _, pname := range w.demoPrc {
		b, ok := w.procs[pname]
		if !ok {
			continue
		}
		t := Toggle{Proc: hexIfNotASCII(pname), ProcID: StrCode(pname)}
		for i := b.from; i < b.to && i < len(w.sts); i++ {
			s := w.sts[i]
			if s.kind != "mesg" || s.typ != tPutObject || len(s.words) < 4 {
				continue
			}
			idx, ok := w.byName[s.name]
			if !ok {
				continue
			}
			switch s.words[3] {
			case "on":
				t.On = append(t.On, w.places[idx].Name)
			case "off":
				t.Off = append(t.Off, w.places[idx].Name)
			}
		}
		if len(t.On) > 0 || len(t.Off) > 0 {
			w.toggles = append(w.toggles, t)
		}
	}
}

func first(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func isASCIIIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x80 || c == '$' || c == ':' {
			return false
		}
	}
	return true
}

func hexIfNotASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return "proc_" + strconv.FormatUint(uint64(strcode(s)), 16)
		}
	}
	return s
}

// strcode is a stable id for a non-ASCII proc name, so JSON stays valid UTF-8.
func strcode(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}
