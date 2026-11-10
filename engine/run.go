package engine

import (
	"fmt"
	"math"
)

// Action is one change in a plan, ready to show.
type Action struct {
	Kind   string  `json:"kind"` // regrade, garden, grow, extend, pipe, tank
	Text   string  `json:"text"`
	Cost   float64 `json:"cost"`   // what this step adds
	Total  float64 `json:"total"`  // the plan's cost so far
	Damage float64 `json:"damage"` // the score once this step is done
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	X2     float64 `json:"x2,omitempty"` // where a pipe leads
	Y2     float64 `json:"y2,omitempty"`
	Size   float64 `json:"size,omitempty"` // garden side, m
	Label  string  `json:"label,omitempty"`
}

// Picture is a grid ready to draw: levels in millimetres, covers, standing water and flow.
type Picture struct {
	NX    int     `json:"nx"`
	NY    int     `json:"ny"`
	Cell  float64 `json:"cell"`
	Z     []int   `json:"z"`     // mm
	Kind  []int   `json:"kind"`  // 0 lawn, 1 paving, 2 garden, 3 building, 4 rain garden, 5 berm
	Depth []int   `json:"depth"` // standing water at the end of the storm, mm
	Flow  []int   `json:"flow"`  // litres that ran through the cell
}

// Watch is a simulation to animate, on a coarser grid.
type Watch struct {
	NX     int      `json:"nx"`
	NY     int      `json:"ny"`
	Cell   float64  `json:"cell"`
	Z      []int    `json:"z"` // mm
	Every  float64  `json:"every"`
	Frames [][]byte `json:"frames"` // depth in mm, little-endian uint16 per cell
	// Where every drop went, m³: rain + roof = soaked + standing + street + neighbours.
	Rain       float64 `json:"rain"`
	Roof       float64 `json:"roof"`
	Soaked     float64 `json:"soaked"`
	Standing   float64 `json:"standing"`
	Street     float64 `json:"street"`
	Neighbours float64 `json:"neighbours"`
}

// Result is everything the page shows.
type Result struct {
	Lot       *Lot     `json:"lot"`
	Storm     Storm    `json:"storm"`
	Soil      Soil     `json:"soil"`
	Budget    float64  `json:"budget"`
	Pipes     []Pipe   `json:"pipes"`
	Sites     []Site   `json:"sites"`
	Today     *Outcome `json:"today"`
	Planned   *Outcome `json:"planned"`
	Actions   []Action `json:"actions"`
	Cost      float64  `json:"cost"`
	Evals     int      `json:"evaluations"`
	Complete  bool     `json:"complete"` // false if the search hit its work limit
	Before    Picture  `json:"before"`
	After     Picture  `json:"after"`
	WatchNow  *Watch   `json:"watchToday"`
	WatchPlan *Watch   `json:"watchPlan"`
}

// Options override a lot's own storm and budget.
type Options struct {
	Storm     string
	Budget    int // dollars, used when SetBudget is true (so a budget of 0 can be asked for)
	SetBudget bool
	Watch     bool // run the animated simulations too
}

// Run plans a lot: routes the storm today, finds the best plan in budget, and (optionally)
// simulates both for watching.
func Run(l *Lot, o Options) (*Result, error) {
	if o.Storm != "" {
		if StormByID(o.Storm) == nil {
			return nil, bad("storm is one of: downpour, five-year, twenty-year")
		}
		l.Storm = o.Storm
	}
	if o.SetBudget {
		if o.Budget < 0 || o.Budget > 50000 {
			return nil, bad("budget is 0 to 50000 dollars")
		}
		l.Budget = o.Budget
	}
	g, err := NewGrid(l)
	if err != nil {
		return nil, err
	}
	st := StormByID(l.Storm)
	p := NewPlanner(g, st)
	best, steps := p.Best(float64(l.Budget))

	res := &Result{Lot: l, Storm: *st, Soil: soils[l.Soil], Budget: float64(l.Budget), Pipes: g.Pipes, Sites: p.Sites}
	res.Today = g.Route(st, nil, true)
	after, tanks := p.Apply(best)
	res.Planned = after.Route(st, tanks, true)
	res.Cost = p.Cost(best)
	res.Actions = p.describe(steps)
	res.Before = picture(g, res.Today)
	res.After = picture(after, res.Planned)
	if o.Watch {
		res.WatchNow = watch(g, st, nil)
		res.WatchPlan = watch(after, st, tanks)
	}
	res.Evals = p.Evals
	res.Complete = !p.Stopped
	return res, nil
}

func picture(g *Grid, o *Outcome) Picture {
	n := g.n()
	pic := Picture{NX: g.NX, NY: g.NY, Cell: g.Cell, Z: make([]int, n), Kind: make([]int, n), Depth: make([]int, n), Flow: make([]int, n)}
	for i := 0; i < n; i++ {
		pic.Kind[i] = int(g.Kind[i]) // as numbers: a byte slice would travel as base64 text
		pic.Z[i] = int(math.Round(g.Z[i] * 1000))
		pic.Depth[i] = int(math.Round(o.Depth[i] * 1000))
		pic.Flow[i] = int(math.Round(o.Flow[i] * 1000))
	}
	return pic
}

func watch(g *Grid, st *Storm, tanks map[string]float64) *Watch {
	c := g.Coarsen()
	sim := c.Simulate(st, tanks, 30, 90)
	w := &Watch{NX: c.NX, NY: c.NY, Cell: c.Cell, Z: make([]int, c.n()), Every: sim.Every, Frames: sim.Frames,
		Rain: sim.Rain, Roof: sim.Roof, Soaked: sim.Soaked, Standing: sim.Standing, Street: sim.Street, Neighbours: sim.Neighbours}
	for i := range w.Z {
		w.Z[i] = int(math.Round(c.Z[i] * 1000))
	}
	return w
}

// describe turns the plan's steps into words, naming rain gardens G1, G2... in the order they
// first appear.
func (p *Planner) describe(steps []Step) []Action {
	var out []Action
	prev := p.Empty()
	labels := map[int]string{}
	label := func(site int) string {
		if l, ok := labels[site]; ok {
			return l
		}
		labels[site] = fmt.Sprintf("G%d", len(labels)+1)
		return labels[site]
	}
	prevCost := 0.0
	for _, st := range steps {
		s := st.State
		a := Action{Total: st.Cost, Cost: st.Cost - prevCost, Damage: st.Damage}
		for side := range s.Regrade {
			if s.Regrade[side] && !prev.Regrade[side] {
				a.Kind = "regrade"
				a.Text = fmt.Sprintf("Regrade the %s of the house so the ground falls away from it", sideNames[side])
				hx, hy, hw, hd := p.Base.Home[0], p.Base.Home[1], p.Base.Home[2], p.Base.Home[3]
				a.X, a.Y = [4]float64{hx, hx, hx, hx + hw}[side], [4]float64{hy, hy + hd, hy, hy}[side]
				a.X2, a.Y2 = [4]float64{hx + hw, hx + hw, hx, hx + hw}[side], [4]float64{hy, hy + hd, hy + hd, hy + hd}[side]
			}
		}
		for i := range s.Gardens {
			if s.Gardens[i] == prev.Gardens[i] {
				continue
			}
			side := float64(GardenSizes[s.Gardens[i]-1]) * p.Base.Cell
			a.X, a.Y, a.Size, a.Label = p.Sites[i].X, p.Sites[i].Y, side, label(i)
			if prev.Gardens[i] == 0 {
				a.Kind = "garden"
				a.Text = fmt.Sprintf("Dig a %g × %g m rain garden (%s)", side, side, a.Label)
			} else {
				a.Kind = "grow"
				a.Text = fmt.Sprintf("Make rain garden %s %g × %g m", a.Label, side, side)
			}
		}
		for i := range s.Pipes {
			c, was := s.Pipes[i], prev.Pipes[i]
			pipe := p.Base.Pipes[i]
			if c == was {
				continue
			}
			a.X, a.Y = pipe.X, pipe.Y
			switch {
			case c.Tank != was.Tank:
				a.Kind = "tank"
				if was.Tank == 0 {
					a.Text = fmt.Sprintf("Fit a %s L rain tank to downpipe %s", thousands(Tanks[c.Tank-1].Litres), pipe.ID)
				} else {
					a.Text = fmt.Sprintf("Make downpipe %s's tank %s L", pipe.ID, thousands(Tanks[c.Tank-1].Litres))
				}
			case c.Kind == PipeExtend:
				a.Kind = "extend"
				end := p.ends[i][c.Dir]
				a.X2, a.Y2 = p.Base.xy(end)
				a.Text = fmt.Sprintf("Extend downpipe %s %g m away from the %s", pipe.ID, extendLength, buildingWord(pipe))
			case c.Kind == PipeToGarden:
				a.Kind = "pipe"
				site := p.Sites[c.Garden]
				a.X2, a.Y2 = site.X, site.Y
				a.Label = label(c.Garden)
				a.Text = fmt.Sprintf("Pipe downpipe %s underground to rain garden %s (%.0f m)", pipe.ID, a.Label, math.Hypot(site.X-pipe.X, site.Y-pipe.Y))
			}
		}
		out = append(out, a)
		prev, prevCost = s, st.Cost
	}
	if out == nil {
		out = []Action{}
	}
	return out
}

func buildingWord(p Pipe) string {
	if p.OnHome {
		return "house"
	}
	return "building"
}

func thousands(v float64) string {
	n := int(v)
	if n >= 1000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprint(n)
}
