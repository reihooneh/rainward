package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// What a plan can do, and roughly what each costs (installed, Australian dollars).
const (
	gardenPerM2   = 85.0 // dig, amended soil, plants, an overflow
	gardenDepth   = 0.15 // m of ponding in a rain garden
	extendLength  = 3.0  // m a downpipe is extended
	pipePerM      = 25.0 // buried pipe
	pipeFitting   = 50.0 // per downpipe change
	siteClearance = 3.0  // m from the home to a rain garden
	maxSites      = 10
	seeds         = 6    // single changes tried as starting points
	maxPipe       = 20.0 // m of buried pipe
	pipeFall      = 0.01 // the fall a buried pipe needs, m per m
)

// GardenSizes are the sizes of rain garden on offer: side length in cells (0.5 m).
var GardenSizes = []int{4, 6, 8} // 2 × 2 m, 3 × 3 m, 4 × 4 m

// Tanks are the rain tank sizes on offer, litres, and what each costs installed.
var Tanks = []struct {
	Litres float64
	Cost   float64
}{{2000, 1100}, {5000, 2300}}

// Site is a place a rain garden could go: the centre cell.
type Site struct {
	Cell int     `json:"cell"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// Pipe changes.
const (
	PipeKeep = iota
	PipeExtend
	PipeToGarden
)

// PipeChoice is what happens to one downpipe.
type PipeChoice struct {
	Kind   int // PipeKeep, PipeExtend, PipeToGarden
	Dir    int // which extension direction (0..2) for PipeExtend
	Garden int // site index for PipeToGarden
	Tank   int // 0 none, 1.. index+1 into Tanks
}

// State is one plan: a size for each candidate site (0 = none, else index+1 into GardenSizes),
// a choice for each downpipe, and which sides of the house are regraded.
type State struct {
	Gardens []int
	Pipes   []PipeChoice
	Regrade [4]bool // street side, back, left, right
}

func (s State) clone() State {
	return State{Gardens: append([]int(nil), s.Gardens...), Pipes: append([]PipeChoice(nil), s.Pipes...), Regrade: s.Regrade}
}

// Sides of the house, for regrading.
var sideNames = [4]string{"street side", "back", "left side", "right side"}

const (
	regradePerM = 60.0 // per metre of wall: soil, shaping, turf
	regradeOut  = 3.0  // m out from the wall (the usual advice: a fall of 15 cm over the first 3 m)
	regradeFall = 0.05 // the ground falls 5 cm per metre away from the wall
)

// sideOf says which side of the home a cell near it is on (-1 if none, or past a corner).
func sideOf(g *Grid, c int) int {
	x, y := g.xy(c)
	hx, hy, hw, hd := g.Home[0], g.Home[1], g.Home[2], g.Home[3]
	inX, inY := x >= hx && x <= hx+hw, y >= hy && y <= hy+hd
	switch {
	case inX && y < hy:
		return 0
	case inX && y > hy+hd:
		return 1
	case inY && x < hx:
		return 2
	case inY && x > hx+hw:
		return 3
	}
	return -1
}

func sideLength(g *Grid, side int) float64 {
	if side < 2 {
		return g.Home[2]
	}
	return g.Home[3]
}

func (s State) key() string {
	var b strings.Builder
	for _, g := range s.Gardens {
		fmt.Fprintf(&b, "%d,", g)
	}
	b.WriteByte('|')
	for _, r := range s.Regrade {
		if r {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	b.WriteByte('|')
	for _, p := range s.Pipes {
		fmt.Fprintf(&b, "%d.%d.%d.%d,", p.Kind, p.Dir, p.Garden, p.Tank)
	}
	return b.String()
}

// Planner holds a lot's grid, its storm and its candidate changes, and remembers every plan it
// has scored.
type Planner struct {
	Base  *Grid
	Storm *Storm
	Sites []Site
	ends  [][3]int // for each downpipe: the cell each extension direction reaches (-1 if impossible)
	reach [][]bool // reach[pipe][site]: a buried pipe can run downhill from the downpipe to the site
	// regradable[side]: some ground on that side of the house (not paving) could be built up
	regradable [4]bool
	cache      map[string]*Outcome
	Evals      int
	// Spent counts the work done (cells routed); the search stops trying new plans past
	// WorkLimit, so even the largest lot plans in seconds. Stopped says it did.
	Spent   int
	Stopped bool
}

// NewPlanner finds candidate rain garden sites (on lawn or garden beds, clear of buildings and
// paving, at least 3 m from the home and not too steep) and the extensions each downpipe allows.
func NewPlanner(g *Grid, st *Storm) *Planner {
	p := &Planner{Base: g, Storm: st, cache: map[string]*Outcome{}}
	base := g.Route(st, nil, true)
	half := GardenSizes[len(GardenSizes)-1] / 2
	type cand struct {
		cell  int
		score float64
	}
	var cands []cand
	for c := 0; c < g.n(); c++ {
		cx, cy := c%g.NX, c/g.NX
		if cx-half < 1 || cy-half < 1 || cx+half > g.NX-1 || cy+half > g.NY-1 {
			continue
		}
		ok := true
		lo, hi := math.Inf(1), math.Inf(-1)
		score := 0.0
		for y := cy - half; y < cy+half && ok; y++ {
			for x := cx - half; x < cx+half; x++ {
				i := g.index(x, y)
				k := g.Kind[i]
				if k != Lawn && k != Garden || g.HomeDist[i] < siteClearance {
					ok = false
					break
				}
				lo, hi = math.Min(lo, g.Z[i]), math.Max(hi, g.Z[i])
				score += base.Flow[i] + base.Depth[i]*g.area()
			}
		}
		if !ok || (hi-lo)/(float64(2*half)*g.Cell) > 0.12 || score <= 0 {
			continue
		}
		cands = append(cands, cand{c, score})
	}
	sort.Slice(cands, func(a, b int) bool {
		if cands[a].score != cands[b].score {
			return cands[a].score > cands[b].score
		}
		return cands[a].cell < cands[b].cell
	})
	minGap := float64(GardenSizes[len(GardenSizes)-1])*g.Cell + 0.5
	for _, c := range cands {
		x, y := g.xy(c.cell)
		clear := true
		for _, s := range p.Sites {
			if math.Abs(s.X-x) < minGap && math.Abs(s.Y-y) < minGap {
				clear = false
				break
			}
		}
		if clear {
			p.Sites = append(p.Sites, Site{Cell: c.cell, X: x, Y: y})
			if len(p.Sites) == maxSites {
				break
			}
		}
	}
	for _, pipe := range g.Pipes {
		var e [3]int
		for d := 0; d < 3; d++ {
			e[d] = -1
			ang := float64(d-1) * math.Pi / 4
			dx := pipe.NX*math.Cos(ang) - pipe.NY*math.Sin(ang)
			dy := pipe.NX*math.Sin(ang) + pipe.NY*math.Cos(ang)
			x, y := pipe.X+dx*extendLength, pipe.Y+dy*extendLength
			if x <= 0 || y <= 0 || x >= float64(g.NX)*g.Cell || y >= float64(g.NY)*g.Cell {
				continue
			}
			c := g.at(x, y)
			if c >= 0 && !g.wall(c) && g.Edge[c] == NoEdge {
				e[d] = c
			}
		}
		p.ends = append(p.ends, e)
		// A buried pipe needs a steady fall (1 in 100) all the way, and is kept to 20 m.
		r := make([]bool, len(p.Sites))
		for k, site := range p.Sites {
			length := math.Hypot(site.X-pipe.X, site.Y-pipe.Y)
			rim := math.Inf(1)
			for _, c := range gardenCells(g, site.Cell, GardenSizes[0]) {
				rim = math.Min(rim, g.Z[c])
			}
			r[k] = length <= maxPipe && rim <= g.Z[pipe.Cell]-pipeFall*length
		}
		p.reach = append(p.reach, r)
	}
	for c := 0; c < g.n(); c++ {
		if side := sideOf(g, c); side >= 0 && g.HomeDist[c] > 0 && g.HomeDist[c] <= regradeOut && g.Kind[c] != Paving {
			p.regradable[side] = true
		}
	}
	return p
}

// Empty is the plan that changes nothing.
func (p *Planner) Empty() State {
	return State{Gardens: make([]int, len(p.Sites)), Pipes: make([]PipeChoice, len(p.Base.Pipes))}
}

// Cost of a plan in dollars.
func (p *Planner) Cost(s State) float64 {
	total := 0.0
	for _, size := range s.Gardens {
		if size > 0 {
			side := float64(GardenSizes[size-1]) * p.Base.Cell
			total += side * side * gardenPerM2
		}
	}
	for side, r := range s.Regrade {
		if r {
			total += regradePerM * sideLength(p.Base, side)
		}
	}
	for i, c := range s.Pipes {
		pipe := p.Base.Pipes[i]
		switch c.Kind {
		case PipeExtend:
			total += pipeFitting + pipePerM*extendLength
		case PipeToGarden:
			site := p.Sites[c.Garden]
			total += pipeFitting + pipePerM*math.Hypot(site.X-pipe.X, site.Y-pipe.Y)
		}
		if c.Tank > 0 {
			total += Tanks[c.Tank-1].Cost
		}
	}
	return math.Round(total)
}

// Valid says whether a plan makes sense: pipes only lead to gardens that exist, extensions only
// where there is room.
func (p *Planner) Valid(s State) bool {
	for i, c := range s.Pipes {
		switch c.Kind {
		case PipeExtend:
			if p.ends[i][c.Dir] < 0 {
				return false
			}
		case PipeToGarden:
			if c.Garden < 0 || c.Garden >= len(s.Gardens) || s.Gardens[c.Garden] == 0 || !p.reach[i][c.Garden] {
				return false
			}
		}
	}
	return true
}

// Apply builds the grid with a plan's changes: rain gardens dug (with a low berm on their downhill
// side) and downpipes moved.
func (p *Planner) Apply(s State) (*Grid, map[string]float64) {
	g := p.Base.Clone()
	for i, size := range s.Gardens {
		if size == 0 {
			continue
		}
		side := GardenSizes[size-1]
		cells := gardenCells(g, p.Sites[i].Cell, side)
		lo := math.Inf(1)
		for _, c := range cells {
			lo = math.Min(lo, g.Z[c])
		}
		floor := lo - gardenDepth
		in := map[int]bool{}
		for _, c := range cells {
			g.Z[c], g.K[c], g.Kind[c] = floor, RainGardenRate/1000/3600, RainGarden
			in[c] = true
		}
		// The berm: the ring around the bowl is built up to the rim where the ground is lower.
		rim := floor + gardenDepth
		cx, cy := p.Sites[i].Cell%g.NX, p.Sites[i].Cell/g.NX
		h := side / 2
		for y := cy - h - 1; y <= cy+h; y++ {
			for x := cx - h - 1; x <= cx+h; x++ {
				c := g.index(x, y)
				if c < 0 || in[c] || g.wall(c) {
					continue
				}
				if g.Z[c] < rim {
					g.Z[c] = rim
					if g.Kind[c] == Lawn {
						g.Kind[c] = Berm
					}
				}
			}
		}
	}
	// Regrading: the ground beside the wall is built up so it falls away from the house to meet
	// the existing ground 3 m out.
	for side, r := range s.Regrade {
		if !r {
			continue
		}
		nx, ny := [4]float64{0, 0, -1, 1}[side], [4]float64{-1, 1, 0, 0}[side]
		for c := 0; c < g.n(); c++ {
			d := g.HomeDist[c]
			if d <= 0 || d > regradeOut || sideOf(g, c) != side || g.Kind[c] == BuildingCell {
				continue
			}
			// Meet the ground 3 m out, or the furthest open ground before another building or the
			// boundary gets in the way.
			x, y := g.xy(c)
			o, reach := -1, 0.0
			for step := g.Cell; step <= regradeOut-d+g.Cell+1e-9; step += g.Cell {
				k := g.at(x+nx*step, y+ny*step)
				if k < 0 || g.wall(k) {
					break
				}
				o, reach = k, step
			}
			if o < 0 {
				continue
			}
			want := p.Base.Z[o] + regradeFall*reach
			if g.Z[c] < want {
				g.Z[c] = want
			}
		}
	}
	tanks := map[string]float64{}
	for i, c := range s.Pipes {
		pipe := &g.Pipes[i]
		switch c.Kind {
		case PipeExtend:
			pipe.Cell = p.ends[i][c.Dir]
		case PipeToGarden:
			pipe.Cell = p.Sites[c.Garden].Cell
		}
		if c.Tank > 0 {
			tanks[pipe.ID] = Tanks[c.Tank-1].Litres
		}
	}
	return g, tanks
}

func gardenCells(g *Grid, centre, side int) []int {
	cx, cy := centre%g.NX, centre/g.NX
	h := side / 2
	var out []int
	for y := cy - h; y < cy-h+side; y++ {
		for x := cx - h; x < cx-h+side; x++ {
			if c := g.index(x, y); c >= 0 {
				out = append(out, c)
			}
		}
	}
	return out
}

// Score routes the storm through a plan (remembering the answer).
// WorkLimit is how many cell-routings a plan may use: about a hundred million operations.
const WorkLimit = 12_000_000

// affordable says whether scoring s is still within the work limit (plans already scored are free).
func (p *Planner) affordable(s State) bool {
	if _, ok := p.cache[s.key()]; ok || p.Spent < WorkLimit {
		return true
	}
	p.Stopped = true
	return false
}

func (p *Planner) Score(s State) *Outcome {
	k := s.key()
	if o, ok := p.cache[k]; ok {
		return o
	}
	g, tanks := p.Apply(s)
	o := g.Route(p.Storm, tanks, false)
	p.Evals++
	p.Spent += g.n()
	p.cache[k] = o
	return o
}

// moves lists every plan one step bigger than s: a new or larger garden, a downpipe extended or
// piped to a garden, a tank added or enlarged.
func (p *Planner) moves(s State) []State {
	var out []State
	for side, r := range s.Regrade {
		if !r && p.regradable[side] {
			t := s.clone()
			t.Regrade[side] = true
			out = append(out, t)
		}
	}
	for i, size := range s.Gardens {
		if size < len(GardenSizes) {
			t := s.clone()
			t.Gardens[i] = size + 1
			out = append(out, t)
		}
	}
	for i, c := range s.Pipes {
		if c.Kind == PipeKeep {
			for d := 0; d < 3; d++ {
				if p.ends[i][d] >= 0 {
					t := s.clone()
					t.Pipes[i] = PipeChoice{Kind: PipeExtend, Dir: d, Tank: c.Tank}
					out = append(out, t)
				}
			}
		}
		if c.Kind != PipeToGarden {
			for gi, size := range s.Gardens {
				if size > 0 {
					t := s.clone()
					t.Pipes[i] = PipeChoice{Kind: PipeToGarden, Garden: gi, Tank: c.Tank}
					out = append(out, t)
				}
			}
		}
		if c.Tank < len(Tanks) {
			t := s.clone()
			t.Pipes[i].Tank = c.Tank + 1
			out = append(out, t)
		}
	}
	return out
}

// without removes one thing from a plan (a garden, along with pipes into it; a pipe change; a tank).
func (p *Planner) without(s State) []State {
	var out []State
	for side, r := range s.Regrade {
		if r {
			t := s.clone()
			t.Regrade[side] = false
			out = append(out, t)
		}
	}
	for i, size := range s.Gardens {
		if size > 0 {
			t := s.clone()
			t.Gardens[i] = 0
			for k := range t.Pipes {
				if t.Pipes[k].Kind == PipeToGarden && t.Pipes[k].Garden == i {
					t.Pipes[k] = PipeChoice{Tank: t.Pipes[k].Tank}
				}
			}
			out = append(out, t)
		}
	}
	for i, c := range s.Pipes {
		if c.Kind != PipeKeep {
			t := s.clone()
			t.Pipes[i] = PipeChoice{Tank: c.Tank}
			out = append(out, t)
		}
		if c.Tank > 0 {
			t := s.clone()
			t.Pipes[i].Tank = 0
			out = append(out, t)
		}
	}
	return out
}

// Step is one change in the order the plan suggests doing them.
type Step struct {
	State  State
	Cost   float64
	Damage float64
}

// greedy grows a plan one change at a time until nothing more that helps fits the budget. By
// value it takes the change that removes the most weighted water per dollar; otherwise the one
// that removes the most.
func (p *Planner) greedy(start State, budget float64, byValue bool) State {
	s := start
	cur := p.Score(s).Damage
	for {
		var best State
		bestKey, bestDamage := 0.0, cur
		found := false
		baseCost := p.Cost(s)
		for _, t := range p.moves(s) {
			if !p.Valid(t) || !p.affordable(t) {
				continue
			}
			c := p.Cost(t)
			if c > budget {
				continue
			}
			d := p.Score(t).Damage
			gain := cur - d
			if gain <= 0.5 {
				continue
			}
			key := gain
			if byValue {
				key = gain / math.Max(1, c-baseCost)
			}
			if !found || key > bestKey+1e-12 {
				best, bestKey, bestDamage, found = t, key, d, true
			}
		}
		if !found {
			return s
		}
		s, cur = best, bestDamage
	}
}

// Best finds a good plan within the budget. Greedy by value alone can spend the budget on cheap
// small wins and miss one bigger change that does more (the knapsack trap), so it also tries
// greedy by size of gain, and greedy from each of the most useful single changes; then it keeps
// trying to swap one part of the best plan for something better.
func (p *Planner) Best(budget float64) (State, []Step) {
	empty := p.Empty()
	starts := []State{p.greedy(empty, budget, true), p.greedy(empty, budget, false)}
	type single struct {
		s    State
		gain float64
	}
	var singles []single
	base := p.Score(empty).Damage
	for _, t := range p.moves(empty) {
		if p.Valid(t) && p.Cost(t) <= budget && p.affordable(t) {
			singles = append(singles, single{t, base - p.Score(t).Damage})
		}
	}
	sort.SliceStable(singles, func(a, b int) bool { return singles[a].gain > singles[b].gain })
	for k := 0; k < len(singles) && k < seeds; k++ {
		starts = append(starts, p.greedy(singles[k].s, budget, true))
	}
	s := starts[0]
	for _, t := range starts[1:] {
		if p.Score(t).Damage < p.Score(s).Damage-1e-9 {
			s = t
		}
	}
	for round := 0; round < 4; round++ {
		improved := false
		cur := p.Score(s).Damage
		for _, t := range p.without(s) {
			if !p.Valid(t) {
				continue
			}
			for _, byValue := range []bool{true, false} {
				u := p.greedy(t, budget, byValue)
				if d := p.Score(u).Damage; d < cur-0.5 {
					s, cur, improved = u, d, true
				}
			}
		}
		if !improved {
			break
		}
	}
	// Order the final plan's parts by value, so the report can say what to do first.
	return s, p.order(s)
}

// order lists a plan's parts as steps: each time adding the part that removes the most weighted
// water per dollar, among those in the plan.
func (p *Planner) order(target State) []Step {
	s := p.Empty()
	var steps []Step
	for s.key() != target.key() {
		var best State
		bestRatio := math.Inf(-1)
		cur := p.Score(s).Damage
		for _, t := range p.moves(s) {
			if !p.Valid(t) || !partOf(t, target) || !p.affordable(t) {
				continue
			}
			gain := cur - p.Score(t).Damage
			ratio := gain / math.Max(1, p.Cost(t)-p.Cost(s))
			if ratio > bestRatio {
				best, bestRatio = t, ratio
			}
		}
		if bestRatio == math.Inf(-1) {
			break
		}
		s = best
		steps = append(steps, Step{State: s, Cost: p.Cost(s), Damage: p.Score(s).Damage})
	}
	return steps
}

// partOf says whether every part of s is (at most) what target has.
func partOf(s, target State) bool {
	for k := range s.Regrade {
		if s.Regrade[k] && !target.Regrade[k] {
			return false
		}
	}
	for i := range s.Gardens {
		if s.Gardens[i] > target.Gardens[i] {
			return false
		}
	}
	for i, c := range s.Pipes {
		t := target.Pipes[i]
		if c.Tank > t.Tank {
			return false
		}
		if c.Kind != PipeKeep && (c.Kind != t.Kind || c.Dir != t.Dir || c.Garden != t.Garden) {
			return false
		}
	}
	return true
}
