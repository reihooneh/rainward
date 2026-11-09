// Package engine follows rain across a home's lot and plans where to catch it.
//
// A lot is a rectangle of ground: its shape (a slope plus mounds, dips and channels), what covers
// it (lawn, paving, garden beds), and its buildings with their downpipes. The engine turns that into
// a grid of cells 0.5 m across, then answers two questions for a storm: where does the water go
// (a fast fill-and-spill model, and a detailed shallow-water simulation for watching it), and
// which rain gardens, downpipe changes and tanks do the most good for the money.
package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
)

// Limits on a lot. They keep a whole plan to a few seconds in a browser.
const (
	MaxLotBytes   = 128 * 1024
	MaxFeatures   = 40
	MaxSurfaces   = 40
	MaxBuildings  = 6
	MaxDownpipes  = 16
	MinSide       = 8.0
	MaxSide       = 60.0
	CellSize      = 0.5
	MaxElevation  = 20.0 // metres above the lowest corner, either way
	maxCells      = int(MaxSide/CellSize) * int(MaxSide/CellSize)
	defaultBudget = 3000
)

// Feature shapes the ground. Heights in metres, positions in metres from the street-left corner:
// x across the lot, y back from the street.
type Feature struct {
	Kind string  `json:"kind"` // "slope", "mound", "dip" or "channel"
	X    float64 `json:"x,omitempty"`
	Y    float64 `json:"y,omitempty"`
	X1   float64 `json:"x1,omitempty"` // channel end
	Y1   float64 `json:"y1,omitempty"`
	R    float64 `json:"r,omitempty"` // radius (mound, dip) or half-width (channel)
	H    float64 `json:"h,omitempty"` // height of a mound, depth of a dip or channel
	// slope: rise in metres per metre across (dx) and back (dy)
	DX float64 `json:"dx,omitempty"`
	DY float64 `json:"dy,omitempty"`
}

// Surface is a rectangle covered with something other than lawn.
type Surface struct {
	Kind string  `json:"kind"` // "paving" or "garden"
	Name string  `json:"name,omitempty"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	D    float64 `json:"d"`
}

// Downpipe drains part of a roof onto the ground at one point beside its building.
type Downpipe struct {
	ID   string  `json:"id"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Roof float64 `json:"roof"` // m² of roof that drains to it
}

// Building is a house, garage or shed: water can't cross it.
type Building struct {
	Name      string     `json:"name"`
	X         float64    `json:"x"`
	Y         float64    `json:"y"`
	W         float64    `json:"w"`
	D         float64    `json:"d"`
	Height    float64    `json:"height,omitempty"` // for drawing only
	Home      bool       `json:"home,omitempty"`   // the house whose walls we keep dry
	Downpipes []Downpipe `json:"downpipes"`
}

// Lot is everything about one property.
type Lot struct {
	Name      string     `json:"name"`
	About     string     `json:"about,omitempty"`
	Width     float64    `json:"width"` // along the street (x)
	Depth     float64    `json:"depth"` // back from the street (y)
	Soil      string     `json:"soil"`  // "sand", "loam" or "clay"
	Features  []Feature  `json:"features"`
	Surfaces  []Surface  `json:"surfaces"`
	Buildings []Building `json:"buildings"`
	Budget    int        `json:"budget,omitempty"` // dollars
	Storm     string     `json:"storm,omitempty"`  // a storm id (see Storms)
}

// InputError is a problem with a lot. Its message never repeats the input.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

func bad(format string, args ...any) error { return &InputError{Msg: fmt.Sprintf(format, args...)} }

var (
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 '.,&()/-]{0,59}$`)
	aboutRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 '.,&()/:;%-]{0,239}$`)
	idRe    = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,3}$`)
)

// ParseLot reads and checks a lot from JSON. Unknown fields, trailing data, oversize input and
// out-of-range values are all refused.
func ParseLot(r io.Reader) (*Lot, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxLotBytes+1))
	if err != nil {
		return nil, bad("the lot could not be read")
	}
	if len(data) > MaxLotBytes {
		return nil, bad("the lot is larger than 128 KB")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var l Lot
	if err := dec.Decode(&l); err != nil {
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syn):
			return nil, bad("the lot is not valid JSON (near byte %d)", syn.Offset)
		case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
			return nil, bad("the lot is not valid JSON (it ends early)")
		case errors.As(err, &typ):
			return nil, bad("a field has the wrong type")
		default:
			return nil, bad("the lot has a field Rainward doesn't know, or isn't a JSON object")
		}
	}
	if dec.More() {
		return nil, bad("the lot has extra data after the JSON object")
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func within(v, lo, hi float64) bool { return finite(v) && v >= lo && v <= hi }

// Validate checks every field and fills in defaults.
func (l *Lot) Validate() error {
	if l.Name == "" {
		l.Name = "My lot"
	}
	if !nameRe.MatchString(l.Name) {
		return bad("the name uses letters, digits, spaces and ' . , & ( ) / - (up to 60)")
	}
	if l.About != "" && !aboutRe.MatchString(l.About) {
		return bad("the description uses letters, digits, spaces and simple punctuation (up to 240)")
	}
	if !within(l.Width, MinSide, MaxSide) || !within(l.Depth, MinSide, MaxSide) {
		return bad("width and depth are %g to %g m", MinSide, MaxSide)
	}
	if _, ok := soils[l.Soil]; !ok {
		return bad("soil is \"sand\", \"loam\" or \"clay\"")
	}
	if l.Budget == 0 {
		l.Budget = defaultBudget
	}
	if l.Budget < 0 || l.Budget > 50000 {
		return bad("budget is 0 to 50000 dollars")
	}
	if l.Storm == "" {
		l.Storm = "five-year"
	}
	if StormByID(l.Storm) == nil {
		return bad("storm is one of: downpour, five-year, twenty-year")
	}
	if l.Features == nil {
		l.Features = []Feature{}
	}
	if l.Surfaces == nil {
		l.Surfaces = []Surface{}
	}
	if len(l.Features) > MaxFeatures || len(l.Surfaces) > MaxSurfaces {
		return bad("a lot has up to %d ground features and %d surfaces", MaxFeatures, MaxSurfaces)
	}
	if len(l.Buildings) == 0 || len(l.Buildings) > MaxBuildings {
		return bad("a lot has 1 to %d buildings", MaxBuildings)
	}
	inside := func(x, y float64) bool { return within(x, 0, l.Width) && within(y, 0, l.Depth) }
	for i, f := range l.Features {
		if !finite(f.X, f.Y, f.X1, f.Y1, f.R, f.H, f.DX, f.DY) {
			return bad("ground feature %d: numbers must be finite", i+1)
		}
		switch f.Kind {
		case "slope":
			if !within(f.DX, -0.25, 0.25) || !within(f.DY, -0.25, 0.25) {
				return bad("ground feature %d: a slope rises at most 0.25 m per metre", i+1)
			}
		case "mound", "dip":
			if !inside(f.X, f.Y) || !within(f.R, 0.5, 20) || !within(f.H, 0.02, 2) {
				return bad("ground feature %d: a %s has its centre on the lot, radius 0.5 to 20 m and height 0.02 to 2 m", i+1, f.Kind)
			}
		case "channel":
			if !inside(f.X, f.Y) || !inside(f.X1, f.Y1) || !within(f.R, 0.25, 5) || !within(f.H, 0.02, 1) {
				return bad("ground feature %d: a channel runs between two points on the lot, half-width 0.25 to 5 m, depth 0.02 to 1 m", i+1)
			}
		default:
			return bad("ground feature %d: kind is slope, mound, dip or channel", i+1)
		}
	}
	rect := func(x, y, w, d float64) bool {
		return within(w, CellSize, MaxSide) && within(d, CellSize, MaxSide) && inside(x, y) && inside(x+w, y+d)
	}
	for i, s := range l.Surfaces {
		if s.Kind != "paving" && s.Kind != "garden" {
			return bad("surface %d: kind is paving or garden", i+1)
		}
		if s.Name != "" && !nameRe.MatchString(s.Name) {
			return bad("surface %d: names use letters, digits, spaces and simple punctuation", i+1)
		}
		if !rect(s.X, s.Y, s.W, s.D) {
			return bad("surface %d: it must lie on the lot", i+1)
		}
	}
	pipes := 0
	ids := map[string]bool{}
	homes := 0
	for i := range l.Buildings {
		b := &l.Buildings[i]
		if !nameRe.MatchString(b.Name) {
			return bad("building %d: names use letters, digits, spaces and simple punctuation", i+1)
		}
		if !rect(b.X, b.Y, b.W, b.D) || b.W < 2 || b.D < 2 {
			return bad("building %d: at least 2 m each way, and on the lot", i+1)
		}
		if b.Height == 0 {
			b.Height = 3
		}
		if !within(b.Height, 2, 12) {
			return bad("building %d: height is 2 to 12 m", i+1)
		}
		if b.Home {
			homes++
		}
		if b.Downpipes == nil {
			b.Downpipes = []Downpipe{}
		}
		roof := 0.0
		for j, p := range b.Downpipes {
			pipes++
			if !idRe.MatchString(p.ID) || ids[p.ID] {
				return bad("building %d, downpipe %d: ids are unique, a capital letter then up to 3 letters or digits", i+1, j+1)
			}
			ids[p.ID] = true
			if !within(p.Roof, 1, 400) {
				return bad("building %d, downpipe %d: roof area is 1 to 400 m²", i+1, j+1)
			}
			roof += p.Roof
			// A downpipe sits on the building's outline (within half a metre).
			dx := math.Max(math.Max(b.X-p.X, p.X-(b.X+b.W)), 0)
			dy := math.Max(math.Max(b.Y-p.Y, p.Y-(b.Y+b.D)), 0)
			onEdge := math.Min(math.Min(math.Abs(p.X-b.X), math.Abs(p.X-b.X-b.W)), math.Min(math.Abs(p.Y-b.Y), math.Abs(p.Y-b.Y-b.D)))
			if !finite(p.X, p.Y) || math.Hypot(dx, dy) > 0.5 || (dx == 0 && dy == 0 && onEdge > 0.5) {
				return bad("building %d, downpipe %d: it must be on the building's outline", i+1, j+1)
			}
		}
		if roof > b.W*b.D*1.6 {
			return bad("building %d: its downpipes drain more roof than it has", i+1)
		}
	}
	if pipes > MaxDownpipes {
		return bad("a lot has up to %d downpipes", MaxDownpipes)
	}
	if homes != 1 {
		return bad("exactly one building is the home (\"home\": true)")
	}
	for i := range l.Buildings {
		for j := i + 1; j < len(l.Buildings); j++ {
			a, b := l.Buildings[i], l.Buildings[j]
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.D && b.Y < a.Y+a.D {
				return bad("buildings %d and %d overlap", i+1, j+1)
			}
		}
	}
	return nil
}

// Soil holds how fast water soaks into ground covered in different ways, in millimetres an hour.
type Soil struct {
	Name   string  `json:"name"`
	Lawn   float64 `json:"lawn"`
	Garden float64 `json:"garden"`
}

// Typical steady infiltration rates once the ground is wet (lawns compact the top; garden beds
// are loosened and mulched). They are rough, and vary a lot from yard to yard.
var soils = map[string]Soil{
	"sand": {Name: "Sandy", Lawn: 30, Garden: 45},
	"loam": {Name: "Loam", Lawn: 12, Garden: 20},
	"clay": {Name: "Clay", Lawn: 3, Garden: 6},
}

// RainGardenRate is the infiltration rate of a rain garden's amended soil, mm/h.
const RainGardenRate = 60.0

// Storm is a design storm: a total depth of rain falling evenly over its duration.
type Storm struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	DepthMM  float64 `json:"depthMM"`
	Minutes  float64 `json:"minutes"`
	Describe string  `json:"describe"`
}

// Storms are three one-hour storms of the size suburban drainage is checked against.
var Storms = []Storm{
	{ID: "downpour", Name: "Summer downpour", DepthMM: 25, Minutes: 60, Describe: "25 mm in an hour: happens most years"},
	{ID: "five-year", Name: "Five-year storm", DepthMM: 40, Minutes: 60, Describe: "40 mm in an hour: about a one-in-five chance each year"},
	{ID: "twenty-year", Name: "Twenty-year storm", DepthMM: 55, Minutes: 60, Describe: "55 mm in an hour: about a one-in-twenty chance each year"},
}

// StormByID finds a storm, or nil.
func StormByID(id string) *Storm {
	for i := range Storms {
		if Storms[i].ID == id {
			return &Storms[i]
		}
	}
	return nil
}
