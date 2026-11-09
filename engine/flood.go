package engine

import (
	"math"
)

// The storm, second by second: a shallow-water simulation for watching water move.
//
// This is the "local inertial" scheme used in flood models (Bates, Horritt & Fewtrell 2010; de
// Almeida et al. 2012). Water depth lives at cell centres, flow between neighbouring cells on their
// shared faces. Each step, every face's flow is pushed by the difference in water surface and held
// back by friction (Manning's n); then every cell's depth changes by what flows in and out, plus
// rain, downpipe water and what soaks in. Every drop is accounted for: rain + roofs = standing +
// soaked + left the lot, to rounding.

const (
	gravity  = 9.81
	dryDepth = 1e-4 // m: thinner than this, water doesn't flow
	cfl      = 0.7
	maxStep  = 2.0 // s
)

// manning gives the roughness of each cover.
func manning(kind uint8) float64 {
	switch kind {
	case Paving:
		return 0.015
	case Garden, RainGarden:
		return 0.06
	default: // lawn, berm
		return 0.035
	}
}

// Simulation is a storm played out in time.
type Simulation struct {
	Minutes    float64   `json:"minutes"` // simulated, including the time after the rain stops
	Every      float64   `json:"every"`   // seconds between frames
	Frames     [][]byte  `json:"frames"`  // per frame: depth per cell, mm, as little-endian uint16
	Rain       float64   `json:"rain"`    // m³ (open ground + eaves)
	Roof       float64   `json:"roof"`    // m³ through downpipes (after tanks)
	Soaked     float64   `json:"soaked"`
	Standing   float64   `json:"standing"` // at the end
	Street     float64   `json:"street"`
	Neighbours float64   `json:"neighbours"`
	Steps      int       `json:"steps"`
	MaxDepth   float64   `json:"maxDepth"`
	Depth      []float64 `json:"-"` // final depth per cell
}

// Simulate runs the storm for `after` extra minutes once the rain stops, saving a frame every
// `every` seconds.
func (g *Grid) Simulate(st *Storm, tanks map[string]float64, after, every float64) *Simulation {
	n := g.n()
	a := g.area()
	dx := g.Cell
	nx := g.NX
	h := make([]float64, n)
	qx := make([]float64, n) // flow across the face to the right of each cell, m²/s
	qy := make([]float64, n) // flow across the face above (away from the street) each cell
	src := g.sources(st, tanks)
	rainSecs := st.Minutes * 60
	inflow := make([]float64, n) // m/s of depth added while it rains
	for i := 0; i < n; i++ {
		inflow[i] = src.Water[i] / a / rainSecs
	}
	// Friction on each face: the mean of the two cells' roughness, squared.
	n2x := make([]float64, n)
	n2y := make([]float64, n)
	for i := 0; i < n; i++ {
		if i%nx+1 < nx {
			n2x[i] = math.Pow(0.5*(manning(g.Kind[i])+manning(g.Kind[i+1])), 2)
		}
		if i+nx < n {
			n2y[i] = math.Pow(0.5*(manning(g.Kind[i])+manning(g.Kind[i+nx])), 2)
		}
	}
	ghost := make([]float64, n)
	n2e := make([]float64, n)
	for i := 0; i < n; i++ {
		if g.Edge[i] != NoEdge {
			ghost[i] = g.Ghost(i)
			n2e[i] = math.Pow(manning(g.Kind[i]), 2)
		}
	}
	sim := &Simulation{Minutes: st.Minutes + after, Every: every, Rain: 0, Roof: src.Roof - src.Tanks}
	for i := 0; i < n; i++ {
		if !g.wall(i) {
			sim.Rain += src.Water[i]
		}
	}
	// What arrived through downpipes is part of Water; split it back out for the totals.
	sim.Rain -= sim.Roof
	total := st.Minutes*60 + after*60
	t, nextFrame := 0.0, 0.0
	out := make([]float64, n)

	flux := func(q, hl, hr, zl, zr, n2, dt float64) float64 {
		etaL, etaR := zl+hl, zr+hr
		hf := math.Max(etaL, etaR) - math.Max(zl, zr)
		if hf <= dryDepth {
			return 0
		}
		slope := (etaR - etaL) / dx
		// hf^(7/3) as hf²·∛hf: the same number, several times faster than math.Pow.
		return (q - gravity*hf*dt*slope) / (1 + gravity*dt*n2*math.Abs(q)/(hf*hf*math.Cbrt(hf)))
	}

	for t < total-1e-9 {
		if t >= nextFrame-1e-9 {
			sim.Frames = append(sim.Frames, frame(h))
			nextFrame += every
		}
		hmax := 0.0
		for _, v := range h {
			hmax = math.Max(hmax, v)
		}
		dt := maxStep
		if hmax > dryDepth {
			dt = math.Min(maxStep, cfl*dx/math.Sqrt(gravity*hmax))
		}
		dt = math.Min(dt, total-t)
		dt = math.Min(dt, nextFrame-t)
		if t < rainSecs {
			dt = math.Min(dt, rainSecs-t)
		}
		if dt <= 0 {
			dt = 1e-6
		}
		raining := t < rainSecs-1e-12

		// Faces between cells.
		for i := 0; i < n; i++ {
			if g.wall(i) {
				qx[i], qy[i] = 0, 0
				continue
			}
			cx, cy := i%nx, i/nx
			if cx+1 < nx && !g.wall(i+1) {
				qx[i] = flux(qx[i], h[i], h[i+1], g.Z[i], g.Z[i+1], n2x[i], dt)
			} else {
				qx[i] = 0
			}
			if cy+1 < g.NY && !g.wall(i+nx) {
				qy[i] = flux(qy[i], h[i], h[i+nx], g.Z[i], g.Z[i+nx], n2y[i], dt)
			} else {
				qy[i] = 0
			}
		}
		// Water leaving over the lot's edges, towards dry ground just outside that carries on the
		// slope (Ghost): it only leaves where the ground falls away.
		for i := 0; i < n; i++ {
			if g.Edge[i] == NoEdge {
				continue
			}
			q := flux(-out[i], h[i], 0, g.Z[i], ghost[i], n2e[i], dt)
			out[i] = math.Max(0, q)
		}
		// Keep depths from going negative: scale a cell's outflows if they would take more than it has.
		for i := 0; i < n; i++ {
			if g.wall(i) {
				continue
			}
			leaving := out[i]
			if qx[i] > 0 {
				leaving += qx[i]
			}
			if qy[i] > 0 {
				leaving += qy[i]
			}
			if i%nx > 0 && qx[i-1] < 0 {
				leaving -= qx[i-1]
			}
			if i >= nx && qy[i-nx] < 0 {
				leaving -= qy[i-nx]
			}
			if want := leaving * dt / dx; want > h[i] && want > 0 {
				f := h[i] / want
				out[i] *= f
				if qx[i] > 0 {
					qx[i] *= f
				}
				if qy[i] > 0 {
					qy[i] *= f
				}
				if i%nx > 0 && qx[i-1] < 0 {
					qx[i-1] *= f
				}
				if i >= nx && qy[i-nx] < 0 {
					qy[i-nx] *= f
				}
			}
		}
		for i := 0; i < n; i++ {
			if g.wall(i) {
				continue
			}
			net := -qx[i] - qy[i] - out[i]
			if i%nx > 0 {
				net += qx[i-1]
			}
			if i >= nx {
				net += qy[i-nx]
			}
			d := h[i] + net*dt/dx
			if raining {
				d += inflow[i] * dt
			}
			if d < 0 {
				d = 0 // rounding only: the limiter above keeps this tiny
			}
			soak := math.Min(d, g.K[i]*dt)
			d -= soak
			sim.Soaked += soak * a
			left := out[i] * dt * dx
			if g.Edge[i] == StreetEdge {
				sim.Street += left
			} else {
				sim.Neighbours += left
			}
			h[i] = d
		}
		t += dt
		sim.Steps++
		if sim.Steps > 5_000_000 {
			break // cannot happen with the CFL step on lots this size; a guard all the same
		}
	}
	sim.Frames = append(sim.Frames, frame(h))
	for i := 0; i < n; i++ {
		sim.Standing += h[i] * a
		sim.MaxDepth = math.Max(sim.MaxDepth, h[i])
	}
	sim.Depth = h
	return sim
}

// frame packs depths as millimetres in little-endian uint16s (up to 65 m: plenty).
func frame(h []float64) []byte {
	b := make([]byte, 2*len(h))
	for i, v := range h {
		mm := int(math.Round(v * 1000))
		if mm > 65535 {
			mm = 65535
		}
		b[2*i] = byte(mm)
		b[2*i+1] = byte(mm >> 8)
	}
	return b
}
