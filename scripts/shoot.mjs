// Screenshots of the built site, for the README (docs/) or for checking a change by eye.
//   node scripts/shoot.mjs [outdir] [width] [height]
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';
import { serve } from './serve.mjs';

const out = process.argv[2] || 'docs';
const width = Number(process.argv[3]) || 1440;
const height = Number(process.argv[4]) || 900;
mkdirSync(out, { recursive: true });
const server = await serve();
const base = `http://127.0.0.1:${server.address().port}/`;
const browser = await chromium.launch({ args: ['--use-gl=swiftshader', '--enable-unsafe-swiftshader'] });
for (const page of ['index', 'planner', 'water', 'method']) {
  const p = await browser.newPage({ viewport: { width, height }, deviceScaleFactor: 1, reducedMotion: 'reduce' });
  p.on('pageerror', (e) => console.log(page, 'error:', e.message));
  p.on('console', (m) => { if (m.type() === 'error') console.log(page, 'console:', m.text()); });
  await p.goto(base + page + '.html');
  await p.waitForFunction(() => document.body.dataset.ready, null, { timeout: 120000 });
  await p.waitForTimeout(800);
  await p.screenshot({ path: `${out}/${page}.png`, fullPage: page !== 'index' });
  await p.close();
}
await browser.close();
server.close();
