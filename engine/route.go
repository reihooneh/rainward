package engine

import (
	"container/heap"
	"math"
	"sort"
)

// Fill-and-spill: where a storm's water ends up, without simulating every second of it.
//
// A priority flood from the lot's edges finds, for every cell, the lowest level water there would
// have to rise to before it could escape (its spill level), and a drainage tree that leads every
// cell to an edge. Cells below their spill level form hollows. Water is then passed down the tree
// from the top: each cell soaks up what it can, each hollow fills before it overflows at its spill
// point, and whatever reaches an edge leaves the lot.

// Hollow is a place where water pools.
type Hollow struct {
	Cells      []int   `json:"-"`
	Capacity   float64 `json:"capacity"` // m³ it can hold before overflowing
	Stored     float64 `json:"stored"`   // m³ held at the end of the storm
	Level      float64 `json:"level"`    // water surface, m
	MaxDepth   float64 `json:"maxDepth"` // m
	WetArea    float64 `json:"wetArea"`  // m²
	DrainHours float64 `json:"drainHours"`
	NearHome   bool    `json:"nearHome"`
	X          float64 `json:"x"` // deepest point
	Y          float64 `json:"y"`
	spill      int
	exit       int8
	floor      float64
}

// Outcome is where a storm's water went, in cubic metres.
type Outcome struct {
	Rain       float64   `json:"rain"`       // fell on open ground
	Roof       float64   `json:"roof"`       // came off roofs
	Tanks      float64   `json:"tanks"`      // caught in rain tanks
	Soaked     float64   `json:"soaked"`     // soaked into the ground during the storm
	Ponded     float64   `json:"ponded"`     // left standing in hollows
	Street     float64   `json:"street"`     // ran off to the street
	Neighbours float64   `json:"neighbours"` // ran off onto neighbours' land
	WallPond   float64   `json:"wallPond"`   // standing within a metre of the home
	WallFlow   float64   `json:"wallFlow"`   // water that runs within a metre of the home, counted once
	LongPond   float64   `json:"longPond"`   // standing in hollows that take more than a day to drain
	Damage     float64   `json:"damage"`     // the weighted score the planner minimises (litres)
	Hollows    []Hollow  `json:"hollows"`
	Depth      []float64 `json:"-"` // standing water per cell, m
	Flow       []float64 `json:"-"` // water passing through each cell, m³
}

// Weights turn an outcome into one score, in "weighted litres".
var Weights = struct {
	Wall, WallFlow, Neighbours, Street, LongPond float64
}{Wall: 5, WallFlow: 1, Neighbours: 0.5, Street: 0.1, LongPond: 0.3}

const (
	nearHome = 1.0  // metres: water closer than this is against the house
	longPond = 24.0 // hours
	eps      = 1e-9
)

type floodItem struct {
	level  float64
	seq    int
	cell   int
	parent int
}
type floodHeap []floodItem

func (h floodHeap) Len() int { return len(h) }
func (h floodHeap) Less(a, b int) bool {
	if h[a].level != h[b].level {
		return h[a].level < h[b].level
	}
	return h[a].seq < h[b].seq
}
func (h floodHeap) Swap(a, b int) { h[a], h[b] = h[b], h[a] }
func (h *floodHeap) Push(x any)   { *h = append(*h, x.(floodItem)) }
func (h *floodHeap) Pop() any     { old := *h; it := old[len(old)-1]; *h = old[:len(old)-1]; return it }

// Flood is the result of the priority flood: spill levels, the drainage tree and visiting order.
type Flood struct {
	Spill  []float64 // lowest level from which water at the cell can escape
	Parent []int     // next cell downstream (-1 for an edge cell)
	Order  []int     // cells in the order they were reached: every cell after its parent
}

// PriorityFlood runs the flood inwards from just outside the lot. Outside each edge cell the
// ground is taken to carry on at the same slope (see Ghost), so water only leaves where the ground
// falls away; buildings are walls.
func (g *Grid) PriorityFlood() *Flood {
	n := g.n()
	f := &Flood{Spill: make([]float64, n), Parent: make([]int, n), Order: make([]int, 0, n)}
	done := make([]bool, n)
	h := &floodHeap{}
	seq := 0
	for i := 0; i < n; i++ {
		f.Parent[i] = -1
		if g.Edge[i] != NoEdge {
			heap.Push(h, floodItem{level: math.Max(g.Z[i], g.Ghost(i)), seq: seq, cell: i, parent: -1})
			seq++
		}
	}
	nb := make([]int, 0, 4)
	for h.Len() > 0 {
		it := heap.Pop(h).(floodItem)
		if done[it.cell] {
			continue
		}
		done[it.cell] = true
		f.Spill[it.cell] = it.level
		f.Parent[it.cell] = it.parent
		f.Order = append(f.Order, it.cell)
		for _, j := range g.neighbours4(it.cell, nb) {
			if !done[j] {
				heap.Push(h, floodItem{level: math.Max(g.Z[j], it.level), seq: seq, cell: j, parent: it.cell})
				seq++
			}
		}
	}
	// Ground walled in by buildings (a courtyard) can't drain: it keeps Parent -1 and isn't in Order.
	return f
}

// Sources is where water arrives during a storm, in m³ per cell, and what tanks caught.
type Sources struct {
	Water []float64
	Rain  float64
	Roof  float64
	Tanks float64
}

// sources spreads a storm's rain over open ground and sends each roof's water to its downpipe.
// tanks gives litres of tank on a downpipe; redirect moves where a downpipe empties.
func (g *Grid) sources(st *Storm, tanks map[string]float64) Sources {
	n := g.n()
	s := Sources{Water: make([]float64, n)}
	p := st.DepthMM / 1000
	a := g.area()
	for i := 0; i < n; i++ {
		if !g.wall(i) {
			s.Water[i] += p * a
			s.Rain += p * a
		}
	}
	for i, roof := range g.Eaves {
		s.Water[i] += p * roof
		s.Roof += p * roof
	}
	for _, pipe := range g.Pipes {
		v := p * pipe.Roof
		s.Roof += v
		if t := tanks[pipe.ID] / 1000; t > 0 {
			caught := math.Min(v, t)
			s.Tanks += caught
			v -= caught
		}
		s.Water[pipe.Cell] += v
	}
	return s
}

// Route passes a storm's water through the lot and reports where it went.
func (g *Grid) Route(st *Storm, tanks map[string]float64, detail bool) *Outcome {
	fl := g.PriorityFlood()
	return g.route(fl, st, g.sources(st, tanks), detail)
}

func (g *Grid) route(fl *Flood, st *Storm, src Sources, detail bool) *Outcome {
	n := g.n()
	a := g.area()
	secs := st.Minutes * 60
	out := &Outcome{Rain: src.Rain, Roof: src.Roof, Tanks: src.Tanks, Hollows: []Hollow{}}

	// Hollows: connected cells below their spill level. (Their spill level is the same throughout.)
	hollowOf := make([]int, n)
	for i := range hollowOf {
		hollowOf[i] = -1
	}
	var hollows []*Hollow
	stack := []int{}
	nb := make([]int, 0, 4)
	for _, i := range fl.Order {
		if hollowOf[i] >= 0 || fl.Spill[i] <= g.Z[i]+eps {
			continue
		}
		h := &Hollow{floor: math.Inf(1)}
		id := len(hollows)
		hollows = append(hollows, h)
		hollowOf[i] = id
		stack = append(stack[:0], i)
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			h.Cells = append(h.Cells, c)
			h.Capacity += (fl.Spill[c] - g.Z[c]) * a
			if g.Z[c] < h.floor {
				h.floor = g.Z[c]
				h.X, h.Y = g.xy(c)
			}
			for _, j := range g.neighbours4(c, nb) {
				if hollowOf[j] < 0 && fl.Spill[j] > g.Z[j]+eps {
					hollowOf[j] = id
					stack = append(stack, j)
				}
			}
		}
		// It overflows where the drainage tree first leaves it.
		// (If it reaches the lot's edge first, it overflows off the lot there.)
		c := i
		for c >= 0 && hollowOf[c] == id {
			if fl.Parent[c] < 0 {
				h.exit = g.Edge[c]
			}
			c = fl.Parent[c]
		}
		h.spill = c
	}

	in := make([]float64, n)
	recvBuf := make([]int, 0, 5)
	shareBuf := make([]float64, 0, 5)
	pool := make([]float64, len(hollows))
	soakCap := make([]float64, len(hollows))
	left := make([]int, len(hollows))
	for id, h := range hollows {
		left[id] = len(h.Cells)
	}
	var depth, flow []float64
	if detail {
		depth = make([]float64, n)
		flow = make([]float64, n)
	}
	for k := len(fl.Order) - 1; k >= 0; k-- {
		c := fl.Order[k]
		w := in[c] + src.Water[c]
		capacity := g.K[c] * secs * a
		if id := hollowOf[c]; id >= 0 {
			pool[id] += w
			soakCap[id] += capacity
			left[id]--
			if left[id] > 0 {
				continue
			}
			h := hollows[id]
			soak := math.Min(pool[id], soakCap[id])
			stored := math.Min(pool[id]-soak, h.Capacity)
			over := pool[id] - soak - stored
			out.Soaked += soak
			g.fillHollow(h, stored, fl)
			out.Ponded += stored
			switch {
			case h.spill >= 0:
				in[h.spill] += over
			case h.exit == StreetEdge:
				out.Street += over
			case h.exit == NeighbourEdge:
				out.Neighbours += over
			default:
				out.Ponded += over
			}
			continue
		}
		soak := math.Min(w, capacity)
		pass := w - soak
		out.Soaked += soak
		if detail {
			flow[c] = pass
		}
		if near(g, c) && src.Water[c] > 0 {
			out.WallFlow += math.Min(src.Water[c], pass) // what lands right by the walls and runs on
		}
		// Water spreads to every lower neighbour in proportion to the drop (and over the edge,
		// if the ground outside is lower), the way a sheet of water runs down a slope; on the
		// flat it follows the drainage tree.
		recv, share := g.receivers(c, fl, recvBuf, shareBuf)
		if len(recv) == 0 {
			if p := fl.Parent[c]; p >= 0 {
				recv, share = append(recvBuf[:0], p), append(shareBuf[:0], 1)
			} else {
				recv, share = append(recvBuf[:0], -1), append(shareBuf[:0], 1)
			}
		}
		for k, j := range recv {
			v := pass * share[k]
			if j < 0 {
				switch g.Edge[c] {
				case StreetEdge:
					out.Street += v
				case NeighbourEdge:
					out.Neighbours += v
				default:
					out.Ponded += v
				}
				continue
			}
			if near(g, j) && !near(g, c) && hollowOf[j] < 0 {
				out.WallFlow += v // water running in to the walls from further out
			}
			in[j] += v
		}
	}
	// Water landing in a walled-in courtyard never entered the order: it stays.
	if len(fl.Order) < n {
		reached := make([]bool, n)
		for _, c := range fl.Order {
			reached[c] = true
		}
		for c := 0; c < n; c++ {
			if !reached[c] && !g.wall(c) {
				out.Ponded += src.Water[c]
			}
		}
	}

	for _, h := range hollows {
		if h.Stored <= 1e-6 {
			continue
		}
		wetRate := 0.0
		for _, c := range h.Cells {
			d := h.Level - g.Z[c]
			if d <= 0 {
				continue
			}
			wetRate += g.K[c] * a
			if detail {
				depth[c] = d
			}
			if g.HomeDist[c] > 0 && g.HomeDist[c] <= nearHome {
				h.NearHome = true
				out.WallPond += d * a
			}
		}
		h.DrainHours = 999
		if wetRate > 0 {
			h.DrainHours = math.Min(999, h.Stored/wetRate/3600)
		}
		if h.DrainHours > longPond {
			out.LongPond += h.Stored
		}
		out.Hollows = append(out.Hollows, *h)
	}
	sort.Slice(out.Hollows, func(a, b int) bool { return out.Hollows[a].Stored > out.Hollows[b].Stored })
	out.Damage = 1000 * (Weights.Wall*out.WallPond + Weights.WallFlow*out.WallFlow +
		Weights.Neighbours*out.Neighbours + Weights.Street*out.Street + Weights.LongPond*out.LongPond)
	out.Depth, out.Flow = depth, flow
	return out
}

// receivers lists where water leaving cell c goes: each neighbour whose spill level is lower, and
// the ground outside the lot (-1) if c is on the edge and that is lower, with the share each gets.
func (g *Grid) receivers(c int, fl *Flood, recv []int, share []float64) ([]int, []float64) {
	recv, share = recv[:0], share[:0]
	level := fl.Spill[c]
	total := 0.0
	cx, cy := c%g.NX, c/g.NX
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		j := g.index(cx+d[0], cy+d[1])
		if j < 0 || g.wall(j) {
			continue
		}
		if drop := level - fl.Spill[j]; drop > eps {
			recv, share = append(recv, j), append(share, drop)
			total += drop
		}
	}
	if g.Edge[c] != NoEdge {
		if drop := level - g.Ghost(c); drop > eps {
			recv, share = append(recv, -1), append(share, drop)
			total += drop
		}
	}
	for k := range share {
		share[k] /= total
	}
	return recv, share
}

func near(g *Grid, c int) bool { return g.HomeDist[c] > 0 && g.HomeDist[c] <= nearHome }

// fillHollow finds the level that holds `stored` m³ in a hollow, filling it from its lowest cell
// up as one pool.
func (g *Grid) fillHollow(h *Hollow, stored float64, fl *Flood) {
	h.Stored = stored
	if stored <= 0 {
		h.Level = h.floor
		return
	}
	a := g.area()
	zs := make([]float64, len(h.Cells))
	for k, c := range h.Cells {
		zs[k] = g.Z[c]
	}
	sort.Float64s(zs)
	top := fl.Spill[h.Cells[0]]
	vol := 0.0
	for k := 0; k < len(zs); k++ {
		next := top
		if k+1 < len(zs) {
			next = math.Min(zs[k+1], top)
		}
		// Between zs[k] and next, k+1 cells are wet.
		need := (next - zs[k]) * a * float64(k+1)
		if vol+need >= stored-1e-12 || next >= top {
			level := zs[k] + (stored-vol)/(a*float64(k+1))
			h.Level = math.Min(level, top)
			h.MaxDepth = h.Level - zs[0]
			h.WetArea = a * float64(k+1)
			return
		}
		vol += need
	}
	h.Level = top
	h.MaxDepth = top - zs[0]
	h.WetArea = a * float64(len(zs))
}
