// Builds the site from its sources (the WebAssembly engine is built separately: `make wasm`):
//   src/pages/*.html + the shared layout  ->  site/*.html (strict CSP, no inline code)
//   src/web/*.js                          ->  site/assets/js/ (bare 'three' imports pointed at the self-hosted copy)
//   src/css/site.css, lots/*.json         ->  site/assets/
//   three.js, the fonts, Go's wasm_exec   ->  site/assets/vendor, site/assets/fonts
// CI rebuilds the site and checks the committed copy matches.
import { copyFileSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const at = (p) => join(root, p);

function copy(from, to, transform) {
  mkdirSync(dirname(at(to)), { recursive: true });
  if (transform) writeFileSync(at(to), transform(readFileSync(from.startsWith('/') ? from : at(from), 'utf8')));
  else copyFileSync(from.startsWith('/') ? from : at(from), at(to));
}

rmSync(at('site/assets/js'), { recursive: true, force: true });
rmSync(at('site/assets/lots'), { recursive: true, force: true });

// Go's loader for WebAssembly must match the Go that built the engine.
const goroot = execFileSync('go', ['env', 'GOROOT'], { encoding: 'utf8' }).trim();
const vendor = [
  ['node_modules/three/build/three.module.js', 'site/assets/vendor/three.module.js'],
  ['node_modules/three/build/three.core.js', 'site/assets/vendor/three.core.js'],
  ['node_modules/three/LICENSE', 'site/assets/vendor/three.LICENSE.txt'],
  [join(goroot, 'lib/wasm/wasm_exec.js'), 'site/assets/vendor/wasm_exec.js'],
  [join(goroot, 'LICENSE'), 'site/assets/vendor/go.LICENSE.txt'],
  ['node_modules/@fontsource-variable/bricolage-grotesque/files/bricolage-grotesque-latin-wght-normal.woff2', 'site/assets/fonts/bricolage-grotesque.woff2'],
  ['node_modules/@fontsource-variable/bricolage-grotesque/LICENSE', 'site/assets/fonts/bricolage-grotesque.LICENSE.txt'],
  ['node_modules/@fontsource-variable/atkinson-hyperlegible-next/files/atkinson-hyperlegible-next-latin-wght-normal.woff2', 'site/assets/fonts/atkinson-hyperlegible-next.woff2'],
  ['node_modules/@fontsource-variable/atkinson-hyperlegible-next/LICENSE', 'site/assets/fonts/atkinson-hyperlegible-next.LICENSE.txt'],
  ['src/css/site.css', 'site/assets/css/site.css'],
  ['src/icon.svg', 'site/assets/icon.svg'],
];
for (const [from, to] of vendor) copy(from, to);
copy('node_modules/three/examples/jsm/controls/OrbitControls.js', 'site/assets/vendor/OrbitControls.js', (js) => js.replace(/from 'three';/g, "from './three.module.js';"));

for (const f of readdirSync(at('lots')).filter((x) => x.endsWith('.json')).sort()) copy(`lots/${f}`, `site/assets/lots/${f}`);

for (const f of readdirSync(at('src/web')).filter((x) => x.endsWith('.js')).sort()) {
  copy(`src/web/${f}`, `site/assets/js/${f}`, (js) =>
    js.replace(/from 'three';/g, "from '../vendor/three.module.js';").replace(/from 'three\/addons\/controls\/OrbitControls\.js';/g, "from '../vendor/OrbitControls.js';"));
}

// ---------- Pages ----------
const CSP = [
  "default-src 'none'",
  "script-src 'self' 'wasm-unsafe-eval'",
  "worker-src 'self'",
  "style-src 'self'",
  "img-src 'self' data: blob:",
  "font-src 'self'",
  "connect-src 'self'",
  "base-uri 'none'",
  "form-action 'none'",
].join('; ');
const NAV = [
  ['index', 'index.html', 'Home'],
  ['planner', 'planner.html', 'Plan your yard'],
  ['water', 'water.html', 'How water moves'],
  ['method', 'method.html', 'How it works'],
];
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
const built = [];
for (const file of readdirSync(at('src/pages')).filter((f) => f.endsWith('.html')).sort()) {
  const raw = readFileSync(at(`src/pages/${file}`), 'utf8');
  const m = raw.match(/^<!--(\{[\s\S]*?\})-->\n/);
  if (!m) throw new Error(`${file}: missing the JSON header comment`);
  const meta = JSON.parse(m[1]);
  const body = raw.slice(m[0].length);
  const nav = NAV.map(([id, href, label]) => `<a href="${href}"${id === meta.nav ? ' aria-current="page"' : ''}>${label}</a>`).join('\n        ');
  const html = `<!doctype html>
<html lang="en-AU">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="Content-Security-Policy" content="${CSP}">
  <meta name="referrer" content="no-referrer">
  <title>${esc(meta.title)}</title>
  <meta name="description" content="${esc(meta.description)}">
  <link rel="icon" href="assets/icon.svg" type="image/svg+xml">
  <link rel="preload" href="assets/fonts/atkinson-hyperlegible-next.woff2" as="font" type="font/woff2" crossorigin>
  <link rel="preload" href="assets/fonts/bricolage-grotesque.woff2" as="font" type="font/woff2" crossorigin>
  <link rel="stylesheet" href="assets/css/site.css">
  <script type="module" src="assets/js/${meta.script}"></script>
</head>
<body class="page-${meta.nav}">
  <a class="skip" href="#main">Skip to the content</a>
  <header class="top">
    <div class="top-inner">
      <a class="mark" href="index.html" aria-label="Rainward home">
        <svg class="mark-icon" viewBox="0 0 32 32" aria-hidden="true"><path d="M16 3c4.5 6 8 10.2 8 14.5a8 8 0 0 1-16 0C8 13.2 11.5 9 16 3z"/><path d="M6 27c3-1.6 5-1.6 8 0s5 1.6 8 0 4-1.2 4-1.2"/></svg>
        <span>Rainward</span>
      </a>
      <nav class="nav" aria-label="Pages">
        ${nav}
      </nav>
    </div>
  </header>
  <main id="main">
${body.trimEnd()}
  </main>
  <footer class="foot">
    <div class="foot-inner">
      <p>Rainward is open source (MIT). It runs entirely in your browser: no sign-in, no tracking, and lots you load never leave your device. Its soil rates, storms and prices are typical figures for comparing options; for drainage near a house's footings, or anything that changes where water leaves your property, check local rules and ask a licensed plumber or drainer.</p>
      <p><a href="https://github.com/reihooneh/rainward">Source code and tests on GitHub</a></p>
    </div>
  </footer>
</body>
</html>
`;
  writeFileSync(at(`site/${file}`), html);
  built.push(file);
}
console.log(`Built ${built.length} pages: ${built.join(', ')}.`);
