package engine

import (
	"math"
)

// Ground cover of a cell.
const (
	Lawn uint8 = iota
	Paving
	Garden
	BuildingCell
	RainGarden
	Berm
)

// Edge says where water leaving a boundary cell goes.
const (
	NoEdge int8 = iota
	StreetEdge
	NeighbourEdge
)

// Pipe is a downpipe as the grid sees it: the cell its water lands in.
type Pipe struct {
	ID       string  `json:"id"`
	Building int     `json:"building"`
	Roof     float64 `json:"roof"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	NX       float64 `json:"nx"` // outward direction from the wall
	NY       float64 `json:"ny"`
	Cell     int     `json:"cell"`
	OnHome   bool    `json:"onHome"`
}

// Grid is a lot cut into square cells. Cell i is at column i % NX, row i / NX; row 0 touches the
// street.
type Grid struct {
	NX, NY   int
	Cell     float64
	Z        []float64 // ground level, m
	K        []float64 // infiltration rate, m/s
	Kind     []uint8
	Edge     []int8
	HomeDist []float64 // metres to the home's walls (0 inside it)
	Pipes    []Pipe
	Eaves    map[int]float64 // cell -> m² of roof that drips there (roofs without enough downpipes)
	Home     [4]float64      // the home's outline: x, y, w, d
}

func (g *Grid) n() int { return g.NX * g.NY }
func (g *Grid) xy(i int) (float64, float64) {
	return (float64(i%g.NX) + 0.5) * g.Cell, (float64(i/g.NX) + 0.5) * g.Cell
}
func (g *Grid) at(x, y float64) int {
	return g.index(int(math.Floor(x/g.Cell)), int(math.Floor(y/g.Cell)))
}
func (g *Grid) wall(i int) bool { return g.Kind[i] == BuildingCell }
func (g *Grid) area() float64   { return g.Cell * g.Cell }
func (g *Grid) index(cx, cy int) int {
	if cx < 0 || cy < 0 || cx >= g.NX || cy >= g.NY {
		return -1
	}
	return cy*g.NX + cx
}

// neighbours4 lists the up-to-four side neighbours of cell i that aren't buildings.
func (g *Grid) neighbours4(i int, out []int) []int {
	out = out[:0]
	cx, cy := i%g.NX, i/g.NX
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		j := g.index(cx+d[0], cy+d[1])
		if j >= 0 && !g.wall(j) {
			out = append(out, j)
		}
	}
	return out
}

func smoothBump(d, r float64) float64 {
	if d >= r {
		return 0
	}
	return 0.5 * (1 + math.Cos(math.Pi*d/r))
}

func segmentDistance(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	l2 := vx*vx + vy*vy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*vx+(py-ay)*vy)/l2))
	}
	return math.Hypot(px-ax-t*vx, py-ay-t*vy)
}

// NewGrid builds the grid for a lot: ground levels, covers, buildings and where each downpipe drains.
func NewGrid(l *Lot) (*Grid, error) {
	g := &Grid{
		NX:   int(math.Round(l.Width / CellSize)),
		NY:   int(math.Round(l.Depth / CellSize)),
		Cell: CellSize,
	}
	n := g.n()
	if n > maxCells {
		return nil, bad("the lot is too large")
	}
	g.Z = make([]float64, n)
	g.K = make([]float64, n)
	g.Kind = make([]uint8, n)
	g.Edge = make([]int8, n)
	g.HomeDist = make([]float64, n)
	g.Eaves = map[int]float64{}
	soil := soils[l.Soil]

	lo, hi := math.Inf(1), math.Inf(-1)
	for i := 0; i < n; i++ {
		x, y := g.xy(i)
		z := 0.0
		for _, f := range l.Features {
			switch f.Kind {
			case "slope":
				z += f.DX*x + f.DY*y
			case "mound":
				z += f.H * smoothBump(math.Hypot(x-f.X, y-f.Y), f.R)
			case "dip":
				z -= f.H * smoothBump(math.Hypot(x-f.X, y-f.Y), f.R)
			case "channel":
				z -= f.H * smoothBump(segmentDistance(x, y, f.X, f.Y, f.X1, f.Y1), f.R)
			}
		}
		g.Z[i] = z
		lo, hi = math.Min(lo, z), math.Max(hi, z)
		g.K[i] = soil.Lawn / 1000 / 3600
	}
	if hi-lo > MaxElevation {
		return nil, bad("the ground rises more than %g m across the lot", MaxElevation)
	}
	for i := range g.Z {
		g.Z[i] -= lo
	}
	cover := func(x0, y0, w, d float64, fn func(i int)) {
		for i := 0; i < n; i++ {
			x, y := g.xy(i)
			if x >= x0 && x < x0+w && y >= y0 && y < y0+d {
				fn(i)
			}
		}
	}
	for _, s := range l.Surfaces {
		kind, rate := Paving, 0.0
		if s.Kind == "garden" {
			kind, rate = Garden, soil.Garden/1000/3600
		}
		cover(s.X, s.Y, s.W, s.D, func(i int) { g.Kind[i], g.K[i] = kind, rate })
	}
	for _, b := range l.Buildings {
		cover(b.X, b.Y, b.W, b.D, func(i int) { g.Kind[i], g.K[i] = BuildingCell, 0 })
	}
	for i := 0; i < n; i++ {
		cx, cy := i%g.NX, i/g.NX
		switch {
		case g.wall(i):
		case cy == 0:
			g.Edge[i] = StreetEdge
		case cx == 0 || cx == g.NX-1 || cy == g.NY-1:
			g.Edge[i] = NeighbourEdge
		}
	}
	open := 0
	for i := 0; i < n; i++ {
		if !g.wall(i) {
			open++
		}
	}
	if open < n/4 {
		return nil, bad("buildings cover most of the lot; there is too little ground to plan")
	}

	// Distance from every cell to the home's outline.
	for _, b := range l.Buildings {
		if !b.Home {
			continue
		}
		g.Home = [4]float64{b.X, b.Y, b.W, b.D}
		for i := 0; i < n; i++ {
			x, y := g.xy(i)
			dx := math.Max(math.Max(b.X-x, x-(b.X+b.W)), 0)
			dy := math.Max(math.Max(b.Y-y, y-(b.Y+b.D)), 0)
			g.HomeDist[i] = math.Hypot(dx, dy)
		}
	}

	// Each downpipe empties just outside the wall it is on.
	for bi, b := range l.Buildings {
		piped := 0.0
		for _, p := range b.Downpipes {
			nx, ny := outward(b, p.X, p.Y)
			c := g.at(p.X+nx*0.3, p.Y+ny*0.3)
			if c < 0 || g.wall(c) {
				return nil, bad("downpipe %s empties onto another building or off the lot", p.ID)
			}
			g.Pipes = append(g.Pipes, Pipe{ID: p.ID, Building: bi, Roof: p.Roof, X: p.X, Y: p.Y, NX: nx, NY: ny, Cell: c, OnHome: b.Home})
			piped += p.Roof
		}
		// Roof not served by downpipes drips evenly off its edges.
		if left := b.W*b.D - piped; left > 0.01 {
			var ring []int
			for i := 0; i < n; i++ {
				if g.wall(i) {
					continue
				}
				x, y := g.xy(i)
				dx := math.Max(math.Max(b.X-x, x-(b.X+b.W)), 0)
				dy := math.Max(math.Max(b.Y-y, y-(b.Y+b.D)), 0)
				if d := math.Hypot(dx, dy); d > 0 && d <= g.Cell {
					ring = append(ring, i)
				}
			}
			for _, i := range ring {
				g.Eaves[i] += left / float64(len(ring))
			}
		}
	}
	return g, nil
}

// outward is the unit direction away from the nearest wall of b at (x, y).
func outward(b Building, x, y float64) (float64, float64) {
	d := []float64{math.Abs(x - b.X), math.Abs(x - (b.X + b.W)), math.Abs(y - b.Y), math.Abs(y - (b.Y + b.D))}
	best := 0
	for k := 1; k < 4; k++ {
		if d[k] < d[best] {
			best = k
		}
	}
	return [4][2]float64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}[best][0], [4][2]float64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}}[best][1]
}

// Ghost is the ground level just outside edge cell i, carrying on the slope from the cell inside
// it: lower than the edge cell if the ground falls away there, higher if it rises. At a corner it
// is the lower of the two.
func (g *Grid) Ghost(i int) float64 {
	cx, cy := i%g.NX, i/g.NX
	best := math.Inf(1)
	try := func(inner int) {
		if inner < 0 || g.wall(inner) {
			best = math.Min(best, g.Z[i])
			return
		}
		best = math.Min(best, 2*g.Z[i]-g.Z[inner])
	}
	if cy == 0 {
		try(g.index(cx, 1))
	}
	if cy == g.NY-1 {
		try(g.index(cx, cy-1))
	}
	if cx == 0 {
		try(g.index(1, cy))
	}
	if cx == g.NX-1 {
		try(g.index(cx-1, cy))
	}
	return best
}

// Coarsen merges each 2 × 2 block of cells into one, for a faster simulation to watch. Ground
// level, soaking rate and roughness are averaged; a block is a building if most of it is; downpipes
// and eaves keep their water.
func (g *Grid) Coarsen() *Grid {
	c := &Grid{NX: (g.NX + 1) / 2, NY: (g.NY + 1) / 2, Cell: g.Cell * 2, Eaves: map[int]float64{}}
	n := c.NX * c.NY
	c.Z, c.K = make([]float64, n), make([]float64, n)
	c.Kind, c.Edge, c.HomeDist = make([]uint8, n), make([]int8, n), make([]float64, n)
	for i := range c.HomeDist {
		c.HomeDist[i] = math.Inf(1)
	}
	count := make([]int, n)
	walls := make([]int, n)
	kinds := make([][6]int, n)
	for i := 0; i < g.n(); i++ {
		j := c.index(i%g.NX/2, i/g.NX/2)
		count[j]++
		c.HomeDist[j] = math.Min(c.HomeDist[j], g.HomeDist[i])
		if g.wall(i) {
			walls[j]++
			continue
		}
		c.Z[j] += g.Z[i]
		c.K[j] += g.K[i]
		kinds[j][g.Kind[i]]++
	}
	for j := 0; j < n; j++ {
		open := count[j] - walls[j]
		if 2*walls[j] > count[j] || open == 0 {
			c.Kind[j] = BuildingCell
			c.Z[j], c.K[j] = 0, 0
			continue
		}
		c.Z[j] /= float64(open)
		c.K[j] /= float64(open)
		best := Lawn
		for k := range kinds[j] {
			if kinds[j][k] > kinds[j][best] {
				best = uint8(k)
			}
		}
		c.Kind[j] = best
	}
	for j := 0; j < n; j++ {
		cx, cy := j%c.NX, j/c.NX
		switch {
		case c.wall(j):
		case cy == 0:
			c.Edge[j] = StreetEdge
		case cx == 0 || cx == c.NX-1 || cy == c.NY-1:
			c.Edge[j] = NeighbourEdge
		}
	}
	// Water bound for a cell that became a building goes to the nearest open block.
	place := func(i int) int {
		j := c.index(i%g.NX/2, i/g.NX/2)
		if !c.wall(j) {
			return j
		}
		x, y := g.xy(i)
		best, bestD := j, math.Inf(1)
		for k := 0; k < n; k++ {
			if c.wall(k) {
				continue
			}
			kx, ky := c.xy(k)
			if d := math.Hypot(kx-x, ky-y); d < bestD {
				best, bestD = k, d
			}
		}
		return best
	}
	for _, p := range g.Pipes {
		q := p
		q.Cell = place(p.Cell)
		c.Pipes = append(c.Pipes, q)
	}
	for i, roof := range g.Eaves {
		c.Eaves[place(i)] += roof
	}
	return c
}

// Clone copies a grid so a plan can change it.
func (g *Grid) Clone() *Grid {
	c := *g
	c.Z = append([]float64(nil), g.Z...)
	c.K = append([]float64(nil), g.K...)
	c.Kind = append([]uint8(nil), g.Kind...)
	c.Pipes = append([]Pipe(nil), g.Pipes...)
	return &c
}
