# How Rainward works

## 1. Architecture

```
lots/*.json ─ParseLot─▶ Lot ─NewGrid─▶ Grid (0.5 m cells: Z, K, Kind, Edge, HomeDist, Pipes, Eaves)
                                         │
                ┌────────────────────────┼─────────────────────────────┐
                ▼                        ▼                             ▼
        PriorityFlood + Route      NewPlanner(grid, storm)       Coarsen → Simulate
        (fill and spill:           sites, extensions, pipes,     (local inertial shallow water,
         one storm in ~1 ms)       tanks, regrading; Best()      1 m cells, frames every 90 s)
                ▲                  scores plans with Route ──┐           │
                └────────────────────────────────────────────┘           │
                                         │                               │
                                      Run(): Result {today, planned, actions, pictures, watch}
                                         │
                  cmd/rainward (text/JSON)    cmd/wasm → Web Worker → planner.js, yard3d.js, map2d.js
```

## 2. File tour

- **`engine/lot.go`:**
  - the lot format, and `ParseLot`/`Validate` (strict JSON, every limit);
  - soil soaking rates and the three design storms.
- **`engine/grid.go`:**
  - `NewGrid` turns features into ground levels, and covers and buildings into cells;
  - it finds where each downpipe empties, and spreads roof without downpipes along the eaves;
  - `Ghost` is the ground just outside an edge cell; `Coarsen` merges 2 × 2 blocks for the simulation.
- **`engine/route.go`:** `PriorityFlood` (spill levels, drainage tree, visiting order) and `Route` (fill and spill), giving an `Outcome` with every litre accounted for and the weighted score.
- **`engine/flood.go`:** `Simulate`, the shallow-water scheme with rain, downpipes, soaking and outflow, plus frames for the page.
- **`engine/plan.go`:**
  - `NewPlanner` finds the candidates; `Apply` digs gardens, moves downpipes and regrades;
  - `greedy` and `Best` do the search; `order` puts the plan in order of value;
  - `WorkLimit` bounds the work.
- **`engine/run.go`:** `Run` puts it all together and writes the actions in words.
- **`cmd/wasm/main.go`:** `rainward.plan(lotJSON, storm, budget)` for the worker (strings in, a string out).
- **`src/web/`:**
  - `worker.js` and `engine.js`: messaging and file checks;
  - `yard3d.js`: the three.js scene, water mesh and rain; `map2d.js`: the canvas map;
  - `planner.js`, `home.js`, `water.js`: the pages.
- **`tests/web/site.test.mjs`:** Playwright at three sizes: overlaps, sideways scrolling, CSP, end-to-end flows and hostile files.

## 3. Key concepts, simply

**Cells and covers.** The yard becomes a grid of half-metre squares. Each knows its height, how fast it soaks water up (from the soil and what covers it), and whether it's a building. A rain garden is a square dug 15 cm below its lowest corner, filled with a mix that soaks 60 mm an hour, with a low berm built up on its downhill side so it holds water like a bowl.

**Priority flood: where can water escape?** Imagine the yard slowly flooding from just outside its edges, always rising at the lowest point next. Rainward simulates exactly that with a priority queue (Barnes et al. 2014). Each cell learns its spill level: the lowest water level from which its water could get off the lot. Cells whose spill level is above the ground are in a hollow. The order cells are reached in is also an order in which water can drain, because everything downstream is reached first.

Outside the lot, the ground is taken to carry on the slope of the edge (the "ghost" cell). So water leaves over the fence only where the ground falls away, the same rule in both models.

**Fill and spill: where does it go?** Walking that order backwards (from the high ground down):
- each cell soaks up what it can in the hour;
- the rest splits among lower neighbours in proportion to the drop (a multiple-flow-direction rule, Quinn et al. 1991), which is how a sheet of water spreads across a lawn;
- a hollow collects everything that reaches it. It soaks some, holds up to its capacity, and overflows the rest at its lip.

Standing water then soaks away at the wetted area's rate. More than a day means a dead lawn and mosquitoes.

**Counting water at the walls once.** Water running alongside a house passes many cells near the wall. Summing per cell would count the same litre a dozen times. Rainward counts water when it enters the band within a metre of the house, or lands in it.

**The storm second by second.** Water depth lives in cells, flow on the faces between them. Each step:
- every face's flow is pushed by the slope of the water surface and slowed by friction (grass is rougher than concrete);
- each cell's depth changes by what flows in and out, plus rain and downpipes, minus soaking.

The step is kept below the Courant limit (water can't cross a cell in one step), and a limiter scales a cell's outflows so its depth never goes negative. The books balance to rounding.

**Scoring and planning.** One number makes plans comparable, in weighted litres:
- water pooled against the house counts five times;
- water running along the house once;
- water onto the neighbours a half;
- long-lasting pools three tenths;
- water to the street a tenth.

Changes interact (a tank on a downpipe makes the garden it fed less useful), so the planner scores whole plans. It grows a plan one change at a time, three ways, then tries swapping each part for something better.

## 4. Trade-offs

- **Two models, not one:** the shallow-water simulation is the more physical, but a storm takes a few hundred milliseconds even on 1 m cells. Fill and spill answers the planning question (where does each litre end up?) in about a millisecond, so hundreds of plans can be compared. The tests keep the two honest with each other.
- **Heuristic search, checked:** finding the best plan is a combinatorial problem (a knapsack with interacting items). Exhaustive search is impossible beyond a handful of options. The greedy-plus-swaps search is fast, and the tests measure how often it matches exhaustive search on small lots (39 of 40).
- **Typical numbers, not measurements:** soil rates, storms and prices are representative. That's enough to rank options on a given yard (the planner's job) but not to size drainage for a permit. The README and site say so.
- **Go to WebAssembly:** one engine for the command line, the tests and the browser. The cost is a 4 MB download, cached after the first visit.

## 5. Interview questions

**Q: Why does the reverse priority-flood order guarantee that water is passed downstream correctly?**
A: The flood pops cells in non-decreasing order of spill level, and a cell's parent, and every lower neighbour it sends water to, has a strictly lower or equal spill level. So it was popped earlier. Walking the order backwards therefore visits every cell before anything downstream of it, so all of a cell's inflow is known when it's processed. A hollow is finalised when its last cell is processed. Its overflow goes to its spill cell, which was popped earlier, so it's still to come.

**Q: How do you know the two water models agree, and why wouldn't they exactly?**
A: A test runs both on each example lot and compares the water leaving the lot (within 8%) and the water soaked in (within 25%). They can't match exactly:
- fill and spill assumes every cell soaks at its full rate for the whole hour, while in the simulation a cell only soaks while it's wet;
- the simulation's flow takes time to arrive, so some water is still on the move when it ends;
- the simulation runs on coarser 1 m cells for speed.

What matters for planning is that both send the water the same way.

**Q: Your planner is greedy. What goes wrong with greedy here, and what did you do about it?**
A: By value per dollar, greedy spends the budget on cheap small wins and can't afford one bigger change that would do more. That's the classic knapsack trap. Changes also interact: a tank can make a garden pointless. So Rainward:
- also runs greedy by absolute gain;
- starts greedy from each of the six most useful single changes;
- then tries removing each part of the best plan and regrowing it.

On 40 small random lots with every plan enumerated, that finds the true best 39 times and is within 1% on the last.

**Q: How do you stop a malicious or just enormous lot from freezing the page?**
A: In layers:
- the engine runs in a Web Worker, so the page stays responsive whatever happens;
- inputs are capped: 60 m a side (14,400 cells), 40 features, 16 downpipes;
- the planner has a work budget in cell-routings, so the largest possible lot plans in about six seconds and the page says the search stopped early;
- the simulation's step count is bounded by the Courant condition and a hard cap.

A test builds the worst lot the limits allow and checks it.

**Q: Why build the WebAssembly reproducibly, and what broke when it wasn't?**
A: CI rebuilds the engine and requires it to match the committed file byte for byte. Otherwise, the site people download could differ from the code that was reviewed and tested. The first attempt failed because Go embeds version-control information (the commit and whether the tree was dirty) into binaries, so every commit changed the file. Building with `-buildvcs=false` and `-trimpath` makes it depend only on the source and the Go version, which CI pins.

## 6. Extension ideas

- **Draw a lot on the map:** drag out the house, paving and beds, then sculpt slopes with a brush. The JSON becomes an export, not something to write by hand.
- **Measured levels:** import a phone LiDAR scan or council contours, resampled to the grid. Everything downstream already works on any height field.
- **Storm bursts and saturation:** use design-rainfall temporal patterns instead of an even hour, and Green-Ampt infiltration so soaking slows as the ground saturates. Both drop straight into the simulation's per-step source and soak terms.
