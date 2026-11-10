// Browser tests for every page at phone, tablet and desktop widths: no errors, no sideways
// scrolling, no text on top of other text (HTML or SVG), and the main things work.
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { chromium } from 'playwright';
import { serve } from '../../scripts/serve.mjs';

const PAGES = ['index.html', 'planner.html', 'water.html', 'method.html'];
const SIZES = [
  { name: 'desktop', width: 1440, height: 900 },
  { name: 'tablet', width: 820, height: 1180 },
  { name: 'phone', width: 390, height: 844 },
];
let server, browser, base;

before(async () => {
  server = await serve();
  base = `http://127.0.0.1:${server.address().port}/`;
  browser = await chromium.launch({ args: ['--use-gl=swiftshader', '--enable-unsafe-swiftshader'] });
});
after(async () => {
  await browser?.close();
  server?.close();
});

async function open(path, size = SIZES[0], opts = {}) {
  const context = await browser.newContext({ viewport: { width: size.width, height: size.height }, reducedMotion: 'reduce', acceptDownloads: true, ...opts });
  const page = await context.newPage();
  const errors = [];
  page.on('console', (m) => {
    if (m.type() === 'error' && !/GL Driver Message|WebGL/.test(m.text())) errors.push(m.text());
  });
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(base + path);
  await page.waitForFunction(() => document.body.dataset.ready, null, { timeout: 60000 });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(300);
  return { page, context, errors };
}

// Every piece of visible text, HTML or SVG: no two different pieces overlap.
async function textOverlaps(page) {
  return page.evaluate(() => {
    const boxes = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) {
      const n = walker.currentNode;
      if (!n.textContent.trim()) continue;
      const elx = n.parentElement;
      if (elx.closest('.visually-hidden, [hidden], .skip, option, title, select')) continue;
      const det = elx.closest('details');
      if (det && !det.open && !elx.closest('summary')) continue; // inside a closed <details>: not shown
      const cs = getComputedStyle(elx);
      if (cs.visibility === 'hidden' || cs.display === 'none' || Number(cs.opacity) === 0) continue;
      if (elx instanceof SVGElement) {
        const r = elx.getBoundingClientRect();
        if (r.width >= 1 && r.height >= 1) boxes.push({ r, text: n.textContent.trim().slice(0, 40), node: n });
        continue;
      }
      const range = document.createRange();
      range.selectNodeContents(n);
      for (const r of range.getClientRects()) if (r.width >= 1 && r.height >= 1) boxes.push({ r, text: n.textContent.trim().slice(0, 40), node: n });
    }
    const out = [];
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i], b = boxes[j];
        if (a.node === b.node) continue;
        const w = Math.min(a.r.right, b.r.right) - Math.max(a.r.left, b.r.left);
        const h = Math.min(a.r.bottom, b.r.bottom) - Math.max(a.r.top, b.r.top);
        if (w > 2 && h > Math.min(a.r.height, b.r.height) * 0.35) out.push(`"${a.text}" overlaps "${b.text}"`);
      }
    }
    return out;
  });
}

// SVG text must stay inside its drawing, and shapes inside a chart must stay inside it too.
async function svgOutside(page) {
  return page.evaluate(() => {
    const out = [];
    for (const s of document.querySelectorAll('.figure svg')) {
      const f = s.getBoundingClientRect();
      for (const t of s.querySelectorAll('text, line, rect, polyline, ellipse, circle')) {
        const r = t.getBoundingClientRect();
        if (r.width === 0 && r.height === 0) continue;
        if (r.left < f.left - 1 || r.right > f.right + 1 || r.top < f.top - 1 || r.bottom > f.bottom + 1) out.push(t.textContent || t.tagName);
      }
    }
    return out;
  });
}

// Controls must not sit on top of each other (buttons, inputs, the 3D view, tables).
async function boxOverlaps(page) {
  return page.evaluate(() => {
    const els = [...document.querySelectorAll('main .button, main input:not([type=radio]), main select, main .stage, main .plan2d, main table, main .figure, main .seg, main .stat')]
      .filter((e) => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
    const out = [];
    for (let i = 0; i < els.length; i++) {
      for (let j = i + 1; j < els.length; j++) {
        if (els[i].contains(els[j]) || els[j].contains(els[i])) continue;
        const a = els[i].getBoundingClientRect(), b = els[j].getBoundingClientRect();
        const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        if (w > 1 && h > 1) out.push(`${els[i].id || els[i].className} overlaps ${els[j].id || els[j].className}`);
      }
    }
    return out;
  });
}


for (const path of PAGES) {
  for (const size of SIZES) {
    test(`${path} at ${size.name}: no errors, no sideways scroll, nothing overlapping`, async () => {
      const { page, context, errors } = await open(path, size);
      const scroll = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      assert.ok(scroll <= 0, `page scrolls sideways by ${scroll}px`);
      assert.deepEqual(await textOverlaps(page), []);
      assert.deepEqual(await svgOutside(page), []);
      assert.deepEqual(await boxOverlaps(page), []);
      assert.deepEqual(errors, []);
      await context.close();
    });
  }
}

test('home: the live numbers come from the engine', async () => {
  const { page, context, errors } = await open('index.html');
  assert.match(await page.textContent('#s-wall'), /^\d{1,3}(,\d{3})* L$/);
  assert.match(await page.textContent('#s-cut'), /^\d{1,3}%$/);
  assert.match(await page.textContent('#s-cost'), /^\$\d{1,3}(,\d{3})*$/);
  await page.click('#which button[data-value="today"]');
  assert.equal(await page.getAttribute('#which button[data-value="today"]', 'aria-pressed'), 'true');
  assert.deepEqual(errors, []);
  await context.close();
});

test('planner: the plan, storms, budget, views, the storm playing, and downloading', async () => {
  const { page, context, errors } = await open('planner.html');
  const settled = () => page.waitForFunction(() => /^Compared [\d,]+ plans?/.test(document.getElementById('status').textContent), null, { timeout: 120000 });
  await settled();
  assert.match(await page.textContent('#headline'), /^Weighted litres: [\d,]+ today, [\d,]+ with this plan \(\d+% less\)\.$/);
  assert.ok((await page.locator('#steps li').count()) >= 3);
  assert.match(await page.textContent('#total'), /^Total \$[\d,]+ of \$3,000$/);
  assert.equal(await page.locator('#stats .stat').count(), 4);
  assert.equal(await page.locator('#balance rect').count() > 3, true);
  assert.ok((await page.locator('#ponds tbody tr').count()) >= 1);
  // A bigger storm.
  const before = await page.textContent('#headline');
  await page.click('#storm button[data-value="twenty-year"]');
  await page.waitForFunction((t) => document.getElementById('headline').textContent !== t, before, { timeout: 120000 });
  await settled();
  // No budget: nothing to do.
  await page.$eval('#budget', (el) => { el.value = '0'; el.dispatchEvent(new Event('input')); });
  await page.waitForFunction(() => document.getElementById('total').textContent === 'No changes', null, { timeout: 120000 });
  // Views.
  await page.click('#v-today');
  await page.click('#v-map');
  assert.equal(await page.isVisible('#c2d'), true);
  assert.equal(await page.isVisible('#stage'), false);
  await page.click('#v-3d');
  assert.equal(await page.isVisible('#stage'), true);
  await page.$eval('#time', (el) => { el.value = '10'; el.dispatchEvent(new Event('input')); });
  assert.match(await page.textContent('#clock'), /^15 min · raining$/);
  const [dl] = await Promise.all([page.waitForEvent('download'), page.click('#save')]);
  assert.equal(JSON.parse(readFileSync(await dl.path(), 'utf8')).name, 'Sloping block on clay');
  await page.selectOption('#lot', 'corner-loam');
  await page.waitForFunction(() => /corner block/i.test(document.getElementById('about').textContent), null, { timeout: 120000 });
  await settled();
  assert.match(await page.textContent('#total'), /of \$5,000$/);
  await page.evaluate(() => window.scrollTo(0, 0)); // the sticky header sits over scrolled text
  assert.deepEqual(await textOverlaps(page), []);
  assert.deepEqual(errors, []);
  await context.close();
});

test('planner: hostile lot files are refused and nothing from them reaches the page', async () => {
  const { page, context, errors } = await open('planner.html');
  const dir = mkdtempSync(join(tmpdir(), 'rainward-'));
  try {
    const put = (name, text) => {
      const p = join(dir, name);
      writeFileSync(p, text);
      return p;
    };
    const lot = JSON.parse(readFileSync(new URL('../../lots/sandy-flat.json', import.meta.url), 'utf8'));
    const refused = async (file, re) => {
      await page.setInputFiles('#file', file);
      await page.waitForFunction((src) => new RegExp(src).test(document.getElementById('load-error').textContent), re.source, { timeout: 60000 });
      assert.equal(await page.isVisible('#load-error'), true);
      assert.doesNotMatch(await page.textContent('#load-error'), /hunter2|<img|script/);
    };
    await refused(put('junk.json', 'not json <script>alert(1)</script>'), /isn't valid JSON/);
    await refused(put('html.json', JSON.stringify({ ...lot, name: '<img src=x onerror=alert(1)>' })), /name uses letters/);
    await refused(put('proto.json', JSON.stringify(lot).replace('{', '{"__proto__":{"isAdmin":true},')), /unknown field/);
    await refused(put('extra.json', JSON.stringify({ ...lot, secret: 'hunter2' })), /field Rainward doesn't know/);
    await refused(put('big.json', `{"x":"${'a'.repeat(140000)}"}`), /too big/);
    await refused(put('array.json', '[1,2,3]'), /isn't a lot/);
    await page.setInputFiles('#file', put('mine.json', JSON.stringify({ ...lot, name: 'My yard' })));
    await page.waitForFunction(() => document.getElementById('lot').value === 'custom' && /^Compared/.test(document.getElementById('status').textContent), null, { timeout: 120000 });
    assert.equal(await page.isHidden('#load-error'), true);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  assert.equal(await page.$('img[src="x"]'), null);
  assert.equal(await page.evaluate(() => ({}).isAdmin), undefined);
  assert.deepEqual(errors, []);
  await context.close();
});

test('how water moves: more rain fills the hollows, then spills; clay sheds more', async () => {
  const { page, context, errors } = await open('water.html');
  const read = async (id) => Number((await page.textContent(id)).replace(/[^\d]/g, ''));
  const set = (v) => page.$eval('#x-rain', (el, v) => { el.value = String(v); el.dispatchEvent(new Event('input')); }, v);
  await set(0);
  assert.equal(await read('#r-pond'), 0);
  await set(20);
  const pond20 = await read('#r-pond');
  await set(80);
  assert.ok((await read('#r-pond')) >= pond20 && (await read('#r-off')) > 0);
  const off = await read('#r-off');
  await page.selectOption('#x-soil', 'clay');
  assert.ok((await read('#r-off')) > off);
  assert.deepEqual(await svgOutside(page), []);
  assert.deepEqual(errors, []);
  await context.close();
});

test('the static server refuses paths outside the site', async () => {
  for (const p of ['../package.json', '..%2fpackage.json', '%2e%2e/%2e%2e/etc/passwd', 'assets/../../go.mod']) {
    const res = await fetch(base + p);
    assert.notEqual(res.status, 200, p);
  }
});

test('built pages: strict CSP, no inline code, nothing from other sites, no HTML from strings', () => {
  const dir = new URL('../../site/', import.meta.url);
  for (const f of readdirSync(dir).filter((x) => x.endsWith('.html'))) {
    const html = readFileSync(new URL(f, dir), 'utf8');
    assert.match(html, /Content-Security-Policy" content="default-src 'none'; script-src 'self' 'wasm-unsafe-eval';/, f);
    assert.doesNotMatch(html, /'unsafe-inline'|'unsafe-eval'/, f);
    assert.doesNotMatch(html, /<script(?![^>]*\bsrc=)[^>]*>/, `${f} has an inline script`);
    assert.doesNotMatch(html, /\sstyle=|<style|\son[a-z]+=/i, `${f} has inline styles or handlers`);
    const external = [...html.matchAll(/(?:src|href)="(https?:[^"]+)"/g)].map((m) => m[1]);
    for (const url of external) assert.ok(/^https:\/\/github\.com\/reihooneh\//.test(url), `${f} links ${url}`);
  }
  for (const f of readdirSync(new URL('assets/js/', dir))) {
    const js = readFileSync(new URL(`assets/js/${f}`, dir), 'utf8');
    assert.doesNotMatch(js, /innerHTML|outerHTML|insertAdjacentHTML|document\.write|\beval\(|new Function/, `${f} builds HTML or code from strings`);
    assert.doesNotMatch(js, /from 'three/, `${f} still has a bare import`);
  }
});
