package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"
)

func loadLot(t *testing.T, name string) *Lot {
	t.Helper()
	f, err := os.Open("../lots/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	l, err := ParseLot(f)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return l
}

var exampleLots = []string{"clay-slope", "sandy-flat", "corner-loam"}

// A small lot for brute-force checks: a slope, a dip, a house with one or two downpipes.
func randomLot(rng *rand.Rand) *Lot {
	w, d := 10+rng.Float64()*6, 12+rng.Float64()*8
	soil := []string{"sand", "loam", "clay"}[rng.Intn(3)]
	hx, hy := 2+rng.Float64()*2, 3+rng.Float64()*2
	hw, hd := 4+rng.Float64()*2, 4+rng.Float64()*2
	l := &Lot{
		Name: "Random", Width: w, Depth: d, Soil: soil,
		Storm:  []string{"downpour", "five-year", "twenty-year"}[rng.Intn(3)],
		Budget: 300 + rng.Intn(1500),
		Features: []Feature{
			{Kind: "slope", DX: (rng.Float64() - 0.5) * 0.06, DY: (rng.Float64() - 0.5) * 0.08},
			{Kind: "dip", X: 1 + rng.Float64()*(w-2), Y: 1 + rng.Float64()*(d-2), R: 1 + rng.Float64()*2, H: 0.05 + rng.Float64()*0.2},
		},
		Surfaces: []Surface{{Kind: "paving", X: 0.5, Y: 0, W: 1.5, D: hy + hd}},
		Buildings: []Building{{Name: "House", Home: true, X: hx, Y: hy, W: hw, D: hd, Downpipes: []Downpipe{
			{ID: "A", X: hx, Y: hy + hd - 0.25, Roof: hw * hd / 2},
		}}},
	}
	if rng.Intn(2) == 0 {
		l.Buildings[0].Downpipes = append(l.Buildings[0].Downpipes, Downpipe{ID: "B", X: hx + hw, Y: hy + 0.25, Roof: hw * hd / 2})
	}
	if err := l.Validate(); err != nil {
		panic(err)
	}
	return l
}

func parse(text string) error {
	_, err := ParseLot(strings.NewReader(text))
	return err
}

func TestBadLotsAreRefusedWithoutEchoingThem(t *testing.T) {
	good, err := os.ReadFile("../lots/clay-slope.json")
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(good, &base); err != nil {
		t.Fatal(err)
	}
	mutate := func(f func(m map[string]any)) string {
		var m map[string]any
		_ = json.Unmarshal(good, &m)
		f(m)
		b, _ := json.Marshal(m)
		return string(b)
	}
	cases := map[string]string{
		"not json":        "{\"name\": ",
		"trailing data":   string(good) + "{}",
		"unknown field":   mutate(func(m map[string]any) { m["secret"] = "hunter2" }),
		"prototype":       mutate(func(m map[string]any) { m["__proto__"] = map[string]any{"x": 1} }),
		"huge":            strings.Repeat(" ", MaxLotBytes+1),
		"tiny lot":        mutate(func(m map[string]any) { m["width"] = 3 }),
		"huge lot":        mutate(func(m map[string]any) { m["depth"] = 500 }),
		"bad soil":        mutate(func(m map[string]any) { m["soil"] = "hunter2" }),
		"bad storm":       mutate(func(m map[string]any) { m["storm"] = "hunter2" }),
		"negative budget": mutate(func(m map[string]any) { m["budget"] = -5 }),
		"markup in name":  mutate(func(m map[string]any) { m["name"] = "<img src=x onerror=alert(1)>" }),
		"steep slope":     mutate(func(m map[string]any) { m["features"] = []any{map[string]any{"kind": "slope", "dy": 3}} }),
		"unknown feature": mutate(func(m map[string]any) { m["features"] = []any{map[string]any{"kind": "volcano"}} }),
		"off-lot surface": mutate(func(m map[string]any) {
			m["surfaces"] = []any{map[string]any{"kind": "paving", "x": 10, "y": 0, "w": 20, "d": 2}}
		}),
		"no buildings": mutate(func(m map[string]any) { m["buildings"] = []any{} }),
		"no home":      mutate(func(m map[string]any) { m["buildings"].([]any)[0].(map[string]any)["home"] = false }),
		"pipe off the wall": mutate(func(m map[string]any) {
			p := m["buildings"].([]any)[0].(map[string]any)["downpipes"].([]any)[0].(map[string]any)
			p["x"], p["y"] = 9.0, 15.0
		}),
		"duplicate pipe": mutate(func(m map[string]any) {
			m["buildings"].([]any)[1].(map[string]any)["downpipes"].([]any)[0].(map[string]any)["id"] = "A"
		}),
		"too much roof": mutate(func(m map[string]any) {
			m["buildings"].([]any)[0].(map[string]any)["downpipes"].([]any)[0].(map[string]any)["roof"] = 399
		}),
		"overlap": mutate(func(m map[string]any) {
			b := m["buildings"].([]any)[1].(map[string]any)
			b["x"], b["y"] = 5.0, 9.0
			b["downpipes"] = []any{}
		}),
		"too many features": mutate(func(m map[string]any) {
			var fs []any
			for i := 0; i <= MaxFeatures; i++ {
				fs = append(fs, map[string]any{"kind": "slope", "dx": 0.01})
			}
			m["features"] = fs
		}),
	}
	for name, text := range cases {
		err := parse(text)
		var in *InputError
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !errorsAs(err, &in) {
			t.Errorf("%s: not an InputError: %v", name, err)
		}
		if strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "<img") {
			t.Errorf("%s: the message repeats the input: %v", name, err)
		}
	}
}

func errorsAs(err error, target **InputError) bool {
	e, ok := err.(*InputError)
	if ok {
		*target = e
	}
	return ok
}

func TestEveryDropIsAccountedFor(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	lots := []*Lot{}
	for _, n := range exampleLots {
		lots = append(lots, loadLot(t, n))
	}
	for k := 0; k < 30; k++ {
		lots = append(lots, randomLot(rng))
	}
	for k, l := range lots {
		g, err := NewGrid(l)
		if err != nil {
			t.Fatal(err)
		}
		st := StormByID(l.Storm)
		for _, tanks := range []map[string]float64{nil, {"A": 2000}} {
			o := g.Route(st, tanks, true)
			in := o.Rain + o.Roof
			out := o.Tanks + o.Soaked + o.Ponded + o.Street + o.Neighbours
			if math.Abs(in-out) > 1e-9*in {
				t.Errorf("lot %d: in %.6f, out %.6f", k, in, out)
			}
			for i, d := range o.Depth {
				if d < 0 || o.Flow[i] < -1e-12 {
					t.Fatalf("lot %d: negative depth or flow", k)
				}
			}
			if tanks != nil && o.Tanks > 2+1e-9 {
				t.Errorf("lot %d: a 2,000 L tank caught %.3f m³", k, o.Tanks)
			}
		}
	}
}

func TestTheFloodTreeDrainsEveryCell(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for k := 0; k < 30; k++ {
		g, _ := NewGrid(randomLot(rng))
		fl := g.PriorityFlood()
		pos := make([]int, g.n())
		for i := range pos {
			pos[i] = -1
		}
		for idx, c := range fl.Order {
			pos[c] = idx
		}
		for c := 0; c < g.n(); c++ {
			if g.wall(c) {
				continue
			}
			if pos[c] < 0 {
				t.Fatalf("lot %d: cell %d is never reached", k, c)
			}
			if fl.Spill[c] < g.Z[c]-1e-12 {
				t.Fatalf("lot %d: a spill level below the ground", k)
			}
			p := fl.Parent[c]
			if p < 0 {
				if g.Edge[c] == NoEdge {
					t.Fatalf("lot %d: an inner cell with nowhere to drain", k)
				}
				continue
			}
			if pos[p] >= pos[c] || fl.Spill[p] > fl.Spill[c]+1e-12 {
				t.Fatalf("lot %d: water would drain uphill or out of order", k)
			}
		}
	}
}

func TestABowlHoldsExactlyItsVolume(t *testing.T) {
	// A paved, flat lot (nothing soaks in) with a dip: a big storm fills the dip to its brim and
	// the rest runs off; a small one leaves the dip holding all the rain that ran into it.
	l := &Lot{Name: "Bowl", Width: 12, Depth: 12, Soil: "clay",
		Features:  []Feature{{Kind: "slope", DY: 0.0001}, {Kind: "dip", X: 6, Y: 7, R: 3, H: 0.2}},
		Surfaces:  []Surface{{Kind: "paving", X: 0, Y: 0, W: 12, D: 12}},
		Buildings: []Building{{Name: "Shed", Home: true, X: 0.5, Y: 9.5, W: 2, D: 2, Downpipes: []Downpipe{}}}}
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	g, _ := NewGrid(l)
	fl := g.PriorityFlood()
	capacity := 0.0
	for c := 0; c < g.n(); c++ {
		if !g.wall(c) {
			capacity += (fl.Spill[c] - g.Z[c]) * g.area()
		}
	}
	// The analytic volume of a raised-cosine dip of depth h, radius r: π r² h (1/2 − 2/π²).
	analytic := math.Pi * 9 * 0.2 * (0.5 - 2/(math.Pi*math.Pi))
	if math.Abs(capacity-analytic) > 0.08*analytic {
		t.Errorf("bowl holds %.3f m³, expected about %.3f", capacity, analytic)
	}
	big := g.Route(&Storm{ID: "x", DepthMM: 200, Minutes: 60}, nil, false)
	if math.Abs(big.Ponded-capacity) > 1e-9 {
		t.Errorf("a big storm leaves %.4f m³, the bowl holds %.4f", big.Ponded, capacity)
	}
	small := g.Route(&Storm{ID: "x", DepthMM: 2, Minutes: 60}, nil, false)
	if small.Ponded <= 0 || small.Ponded >= capacity || small.Soaked != 0 {
		t.Errorf("a small storm: ponded %.4f, soaked %.4f", small.Ponded, small.Soaked)
	}
}

func TestMoreRainMoreRunoffAndSoilMatters(t *testing.T) {
	l := loadLot(t, "clay-slope")
	g, _ := NewGrid(l)
	prev := -1.0
	for _, s := range Storms {
		o := g.Route(&s, nil, false)
		if off := o.Street + o.Neighbours; off <= prev {
			t.Errorf("%s: %.2f m³ ran off, no more than a smaller storm", s.ID, off)
		} else {
			prev = off
		}
	}
	runoff := map[string]float64{}
	for _, soil := range []string{"sand", "loam", "clay"} {
		l.Soil = soil
		g, _ := NewGrid(l)
		o := g.Route(StormByID("five-year"), nil, false)
		runoff[soil] = o.Street + o.Neighbours
	}
	if !(runoff["sand"] < runoff["loam"] && runoff["loam"] < runoff["clay"]) {
		t.Errorf("runoff should rise from sand to loam to clay: %v", runoff)
	}
}

func TestTheSimulationConservesWater(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for k := 0; k < 6; k++ {
		g, _ := NewGrid(randomLot(rng))
		for _, grid := range []*Grid{g, g.Coarsen()} {
			sim := grid.Simulate(StormByID("five-year"), map[string]float64{"A": 2000}, 20, 120)
			in := sim.Rain + sim.Roof
			out := sim.Soaked + sim.Standing + sim.Street + sim.Neighbours
			if math.Abs(in-out) > 1e-6*in {
				t.Errorf("lot %d: in %.6f, out %.6f", k, in, out)
			}
			for _, d := range sim.Depth {
				if d < 0 || math.IsNaN(d) {
					t.Fatalf("lot %d: a negative or NaN depth", k)
				}
			}
		}
	}
}

func TestTheFastModelAgreesWithTheSimulation(t *testing.T) {
	// Fill-and-spill (seconds of maths) and the shallow-water simulation (every second of the
	// storm) should broadly agree on where the water goes.
	for _, name := range exampleLots {
		l := loadLot(t, name)
		g, _ := NewGrid(l)
		st := StormByID(l.Storm)
		o := g.Route(st, nil, false)
		sim := g.Coarsen().Simulate(st, nil, 30, 600)
		fast, slow := o.Street+o.Neighbours, sim.Street+sim.Neighbours
		if math.Abs(fast-slow) > 0.08*slow {
			t.Errorf("%s: %.2f m³ left the lot in the fast model, %.2f in the simulation", name, fast, slow)
		}
		if math.Abs(o.Soaked-sim.Soaked) > 0.25*sim.Soaked {
			t.Errorf("%s: soaked %.2f vs %.2f", name, o.Soaked, sim.Soaked)
		}
	}
}

func TestPlansStayInBudgetAndHelp(t *testing.T) {
	for _, name := range exampleLots {
		l := loadLot(t, name)
		for _, budget := range []int{0, 250, 1500, 6000} {
			res, err := Run(l, Options{Budget: budget, SetBudget: true})
			if err != nil {
				t.Fatal(err)
			}
			if res.Cost > float64(budget) {
				t.Errorf("%s $%d: the plan costs %.0f", name, budget, res.Cost)
			}
			if res.Planned.Damage > res.Today.Damage {
				t.Errorf("%s $%d: the plan makes things worse", name, budget)
			}
			total := 0.0
			for _, a := range res.Actions {
				total += a.Cost
				if a.Text == "" {
					t.Errorf("%s: an action without words", name)
				}
			}
			if math.Abs(total-res.Cost) > 0.5 {
				t.Errorf("%s: the steps add up to %.0f, the plan costs %.0f", name, total, res.Cost)
			}
		}
	}
}

func TestRainGardensAndPipesFollowTheRules(t *testing.T) {
	for _, name := range exampleLots {
		l := loadLot(t, name)
		g, _ := NewGrid(l)
		p := NewPlanner(g, StormByID(l.Storm))
		best, _ := p.Best(8000)
		for i, size := range best.Gardens {
			if size == 0 {
				continue
			}
			for _, c := range gardenCells(g, p.Sites[i].Cell, GardenSizes[size-1]) {
				if g.HomeDist[c] < siteClearance || (g.Kind[c] != Lawn && g.Kind[c] != Garden) {
					t.Errorf("%s: a rain garden too close to the house or on paving", name)
				}
			}
		}
		for i, c := range best.Pipes {
			if c.Kind != PipeToGarden {
				continue
			}
			pipe, site := g.Pipes[i], p.Sites[c.Garden]
			length := math.Hypot(site.X-pipe.X, site.Y-pipe.Y)
			rim := math.Inf(1)
			for _, k := range gardenCells(g, site.Cell, GardenSizes[0]) {
				rim = math.Min(rim, g.Z[k])
			}
			if length > maxPipe || rim > g.Z[pipe.Cell]-pipeFall*length {
				t.Errorf("%s: a buried pipe that runs uphill or too far", name)
			}
		}
		// A dug garden really holds water: its bowl is a hollow of about its area × 15 cm.
		for i := range p.Sites {
			s := p.Empty()
			s.Gardens[i] = 2
			gg, _ := p.Apply(s)
			fl := gg.PriorityFlood()
			hold := 0.0
			for _, c := range gardenCells(gg, p.Sites[i].Cell, GardenSizes[1]) {
				hold += (fl.Spill[c] - gg.Z[c]) * gg.area()
			}
			if want := 9 * gardenDepth; hold < 0.95*want {
				t.Errorf("%s site %d: a 3 × 3 m garden holds %.2f m³, want at least %.2f", name, i, hold, want)
			}
		}
	}
}

// The planner is a heuristic (greedy, then swaps). On small lots, compare it with trying every
// plan within the budget.
func TestThePlannerIsCloseToTheBestPossible(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	exact, compared := 0, 0
	worst := 0.0
	for k := 0; k < 40; k++ {
		l := randomLot(rng)
		g, _ := NewGrid(l)
		p := NewPlanner(g, StormByID(l.Storm))
		if len(p.Sites) > 3 {
			p.Sites = p.Sites[:3]
			for i := range p.reach {
				p.reach[i] = p.reach[i][:3]
			}
		}
		budget := float64(l.Budget)
		best := math.Inf(1)
		var visit func(s State, i int)
		states := 0
		visit = func(s State, i int) {
			if p.Cost(s) > budget {
				return
			}
			switch {
			case i < len(s.Gardens):
				for v := 0; v <= len(GardenSizes); v++ {
					t := s.clone()
					t.Gardens[i] = v
					visit(t, i+1)
				}
			case i < len(s.Gardens)+len(s.Pipes):
				j := i - len(s.Gardens)
				var opts []PipeChoice
				for tank := 0; tank <= len(Tanks); tank++ {
					opts = append(opts, PipeChoice{Tank: tank})
					for d := 0; d < 3; d++ {
						opts = append(opts, PipeChoice{Kind: PipeExtend, Dir: d, Tank: tank})
					}
					for gi := range s.Gardens {
						opts = append(opts, PipeChoice{Kind: PipeToGarden, Garden: gi, Tank: tank})
					}
				}
				for _, o := range opts {
					t := s.clone()
					t.Pipes[j] = o
					visit(t, i+1)
				}
			case i < len(s.Gardens)+len(s.Pipes)+4:
				side := i - len(s.Gardens) - len(s.Pipes)
				visit(s, i+1)
				if p.regradable[side] {
					t := s.clone()
					t.Regrade[side] = true
					visit(t, i+1)
				}
			default:
				if p.Valid(s) {
					states++
					best = math.Min(best, p.Score(s).Damage)
				}
			}
		}
		visit(p.Empty(), 0)
		s, _ := p.Best(budget)
		got := p.Score(s).Damage
		if got < best-1e-6 {
			t.Fatalf("lot %d: the heuristic beat exhaustive search, so the search is wrong", k)
		}
		gap := (got - best) / math.Max(1, p.Score(p.Empty()).Damage-best)
		worst = math.Max(worst, gap)
		if gap < 1e-9 {
			exact++
		}
		compared++
		t.Logf("lot %d: %d plans, heuristic %.0f, best %.0f, gap %.1f%% of the possible improvement", k, states, got, best, 100*gap)
	}
	if worst > 0.15 {
		t.Errorf("the heuristic got within %.0f%% of the best possible improvement at worst; want 15%%", 100*worst)
	}
	if exact*2 < compared {
		t.Errorf("the heuristic found the best plan on only %d of %d lots", exact, compared)
	}
}

func TestRunsAreRepeatable(t *testing.T) {
	l := loadLot(t, "corner-loam")
	a, _ := Run(l, Options{Watch: true})
	l2 := loadLot(t, "corner-loam")
	b, _ := Run(l2, Options{Watch: true})
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Error("the same lot gave two different results")
	}
}

func TestTheStormIsWatchable(t *testing.T) {
	l := loadLot(t, "clay-slope")
	res, err := Run(l, Options{Watch: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []*Watch{res.WatchNow, res.WatchPlan} {
		cells := w.NX * w.NY
		if len(w.Frames) < 40 || len(w.Z) != cells {
			t.Fatalf("frames %d, cells %d", len(w.Frames), cells)
		}
		for _, f := range w.Frames {
			if len(f) != 2*cells {
				t.Fatal("a frame of the wrong size")
			}
		}
		in := w.Rain + w.Roof
		if out := w.Soaked + w.Standing + w.Street + w.Neighbours; math.Abs(in-out) > 1e-6*in {
			t.Errorf("watch: in %.4f out %.4f", in, out)
		}
	}
}

func FuzzParseLot(f *testing.F) {
	for _, n := range exampleLots {
		data, _ := os.ReadFile("../lots/" + n + ".json")
		f.Add(string(data))
	}
	f.Add(`{"width": 1e309}`)
	f.Add(`{"buildings":[{"downpipes":[{}]}]}`)
	f.Fuzz(func(t *testing.T, text string) {
		l, err := ParseLot(strings.NewReader(text))
		if err != nil {
			if _, ok := err.(*InputError); !ok {
				t.Fatalf("not an InputError: %v", err)
			}
			return
		}
		if _, err := NewGrid(l); err != nil {
			if _, ok := err.(*InputError); !ok {
				t.Fatalf("not an InputError: %v", err)
			}
		}
	})
}

var _ = fmt.Sprint
