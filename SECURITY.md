# Security

Rainward is a static website plus a command-line tool. On the site everything happens in the visitor's browser: there is no server code, no account, no database, no analytics and no third-party script. Lots you load are never sent anywhere. No software is perfectly secure; this is how the risks are handled, and how to report a problem.

## Threats and defences

| Threat | Defence |
|---|---|
| **A hostile lot file** ("Load a lot (JSON)") trying to run script, pollute prototypes or hang the tab | **In the page:** the file's size is checked before it's read (128 KB). It is parsed with `JSON.parse` only, and refused if any key anywhere is `__proto__`, `constructor` or `prototype`.<br>**In the engine (Go):** it decodes the JSON again with unknown fields refused and trailing data refused. Every number is range-checked: lot 8–60 m, slopes, heights, radii, positions on the lot, roof areas, budget. Names may only use letters, digits, spaces and simple punctuation, so no markup. Ids are unique, every downpipe must sit on its building's outline, buildings may not overlap, and exactly one is the home.<br>**Errors** never repeat the file's contents, and the tests check this. |
| **Cross-site scripting** through names or descriptions | Pages are built with `createElement` and `textContent` only. A test fails the build if `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `document.write`, `eval` or `new Function` appear in shipped code. |
| **Injected or third-party script** | A strict Content-Security-Policy on every page: `default-src 'none'; script-src 'self' 'wasm-unsafe-eval'`, plus `base-uri 'none'`, `form-action 'none'` and `referrer: no-referrer`. There are no inline scripts, styles or handlers. three.js and the fonts are self-hosted and pinned. The tests check every built page for all of this. |
| **Crafted messages to the engine's worker** | The worker only accepts a request whose id is a number, whose lot is a string under 128 KB, whose storm is one of three ids, and whose budget is 0–50,000. Anything else is ignored. The engine sees only those values: strings and a number cross into WebAssembly, and a JSON string comes back. A panic inside the engine is caught and reported as a plain error. |
| **Denial of service: a huge or pathological lot** | **Limits:** 60 × 60 m (14,400 cells), 40 ground features, 40 surfaces, 6 buildings and 16 downpipes.<br>**A work limit on the planner** (12 million cell-routings): the largest possible lot plans in about six seconds natively, and the page says when the limit was reached. The simulation's time step follows the Courant condition with a hard step cap.<br>**All of it runs in a Web Worker**, so the page stays responsive. |
| **Path traversal** in the local preview server (`scripts/serve.mjs`) | It serves only files inside `site/`, from a fixed list of types, normalising and checking every path. The tests try `../` and encoded tricks. The live site is served by GitHub Pages. |
| **Command line** (`rainward lot.json`) | It reads only a regular file (not a link, folder or device), through the same parser and limits. Errors never echo the input. |
| **Supply chain** | The engine uses Go's standard library only. The site's runtime code is three.js (vendored and pinned) and Go's own `wasm_exec.js`. Development tools (Playwright, font packages) are pinned in `package-lock.json`, and CI runs `npm audit`.<br>The WebAssembly is rebuilt in CI without version-control stamps, and must match the committed file byte for byte. The workflow is read-only except for the Pages deploy job.<br>`govulncheck` couldn't run in the build environment (no route to the Go vulnerability database). It is worth running before a release: `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`. |

## Not security, but safety

Rainward's soil rates, storms and prices are typical figures for comparing options, not site measurements. Some changes need council approval and a licensed plumber or drainer:
- anything that drains near a house's footings;
- anything that connects to the street drain;
- anything that changes where water leaves your property.

## Reporting a problem

Please open a private security advisory on the GitHub repository (Security → Report a vulnerability), or an issue if it isn't sensitive.
