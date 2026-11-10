// How water moves: fill-and-spill on one slice of ground, and how fast different ground soaks.
import { svg } from './ui.js';

const $ = (id) => document.getElementById(id);
const N = 120;           // cells across the slice
const DX = 0.25;         // m per cell: a 30 m slice
const WIDTH = 1;         // m: a one-metre strip of yard
const RATES = { sand: 30, loam: 12, clay: 3 };

// The ground: rising from the street (left) to the back fence (right), with two hollows.
const Z = Array.from({ length: N }, (_, i) => {
  const x = i * DX;
  const bump = (c, r, h) => (Math.abs(x - c) < r ? h * 0.5 * (1 + Math.cos((Math.PI * (x - c)) / r)) : 0);
  return 0.025 * x - bump(11, 2.6, 0.32) - bump(21, 3, 0.26) + bump(16, 1.8, 0.06);
});

/** Fill and spill on the slice: the street (left) is the only way out. */
export function fillSpill(rainMM, soakMM) {
  const a = DX * WIDTH;
  const spill = [];
  let top = -Infinity;
  for (let i = 0; i < N; i++) { top = Math.max(top, Z[i]); spill.push(top); }
  const flow = new Array(N).fill(0);
  const level = new Array(N).fill(null);
  let carry = 0;
  let soaked = 0;
  let ponded = 0;
  let i = N - 1;
  while (i >= 0) {
    if (spill[i] <= Z[i] + 1e-12) {
      const w = carry + (rainMM / 1000) * a;
      const s = Math.min(w, (soakMM / 1000) * a);
      soaked += s;
      carry = w - s;
      flow[i] = carry;
      i--;
      continue;
    }
    // A hollow: every cell below the lip to the left of it.
    const right = i;
    while (i >= 0 && spill[i] > Z[i] + 1e-12) i--;
    const cells = [];
    for (let k = i + 1; k <= right; k++) cells.push(k);
    const lip = spill[right];
    const cap = cells.reduce((v, k) => v + (lip - Z[k]) * a, 0);
    const water = carry + cells.length * (rainMM / 1000) * a;
    const s = Math.min(water, cells.length * (soakMM / 1000) * a);
    soaked += s;
    const stored = Math.min(water - s, cap);
    ponded += stored;
    carry = water - s - stored;
    // Fill from the bottom: find the level that holds `stored`.
    let lo = Math.min(...cells.map((k) => Z[k]));
    let hi = lip;
    for (let it = 0; it < 50; it++) {
      const mid = (lo + hi) / 2;
      const v = cells.reduce((acc, k) => acc + Math.max(0, mid - Z[k]) * a, 0);
      if (v > stored) hi = mid; else lo = mid;
    }
    for (const k of cells) if (lo > Z[k] + 1e-5) level[k] = lo;
    for (const k of cells) flow[k] = carry;
  }
  return { soaked, ponded, off: carry, flow, level };
}

function drawSlice() {
  const rain = Number($('x-rain').value);
  const soil = $('x-soil').value;
  $('x-rain-out').textContent = `${rain} mm`;
  const r = fillSpill(rain, RATES[soil]);
  const W = 640;
  const H = 230;
  const zmin = Math.min(...Z) - 0.05;
  const zmax = Math.max(...Z) + 0.05;
  const X = (i) => 20 + (i / (N - 1)) * (W - 40);
  const Y = (z) => 40 + (1 - (z - zmin) / (zmax - zmin)) * (H - 70);
  const s = svg('svg', { viewBox: `0 0 ${W} ${H}`, role: 'img', 'aria-label': 'A slice of ground with water in its hollows' });
  // Ground.
  let d = `M ${X(0)} ${H - 20}`;
  for (let i = 0; i < N; i++) d += ` L ${X(i).toFixed(1)} ${Y(Z[i]).toFixed(1)}`;
  d += ` L ${X(N - 1)} ${H - 20} Z`;
  s.append(svg('path', { d, fill: '#9cc47f', stroke: '#3f7d34', 'stroke-width': 1.5 }));
  // Ponds.
  let i = 0;
  while (i < N) {
    if (r.level[i] === null) { i++; continue; }
    const start = i;
    while (i < N && r.level[i] !== null) i++;
    const lv = r.level[start];
    let p = `M ${X(start - 0.5).toFixed(1)} ${Y(lv).toFixed(1)}`;
    for (let k = start; k < i; k++) p += ` L ${X(k).toFixed(1)} ${Y(Z[k]).toFixed(1)}`;
    p += ` L ${X(i - 0.5).toFixed(1)} ${Y(lv).toFixed(1)} Z`;
    s.append(svg('path', { d: p, fill: '#1d84b5', 'fill-opacity': 0.85 }));
  }
  // Flowing water: a band whose thickness grows with the flow.
  const maxFlow = Math.max(1e-6, ...r.flow);
  for (let k = 0; k < N - 1; k++) {
    if (r.level[k] !== null || r.flow[k] < 1e-5) continue;
    const t = 1 + 6 * (r.flow[k] / maxFlow);
    s.append(svg('line', { x1: X(k).toFixed(1), y1: (Y(Z[k]) - t / 2).toFixed(1), x2: X(k + 1).toFixed(1), y2: (Y(Z[k + 1]) - t / 2).toFixed(1), stroke: '#5cc8e8', 'stroke-width': t.toFixed(1), 'stroke-linecap': 'round' }));
  }
  s.append(svg('text', { x: 20, y: 22, 'font-size': 13 }, 'Street'));
  s.append(svg('text', { x: W - 20, y: 22, 'font-size': 13, 'text-anchor': 'end' }, 'Back fence'));
  $('x-slice').replaceChildren(s);
  const L = (m3) => `${Math.round(m3 * 1000)} L`;
  $('r-soak').textContent = L(r.soaked);
  $('r-pond').textContent = L(r.ponded);
  $('r-off').textContent = L(r.off);
}

function drawRates() {
  const rows = [
    ['Lawn on clay', 3, '#8a6440'], ['Lawn on loam', 12, '#9a7a4e'], ['Lawn on sand', 30, '#c9a96e'],
    ['Garden bed on loam', 20, '#7a5c3e'], ['Rain garden mix', 60, '#1d84b5'],
  ];
  const W = 520;
  const s = svg('svg', { viewBox: `0 0 ${W} 250`, role: 'img', 'aria-label': 'How fast different ground soaks up water, in millimetres an hour' });
  s.append(svg('text', { x: 0, y: 16, class: 'title', 'font-size': 14 }, 'Millimetres soaked up in an hour, once wet'));
  rows.forEach(([label, v, colour], k) => {
    const y = 40 + k * 40;
    s.append(svg('text', { x: 0, y: y + 17, 'font-size': 13 }, label));
    s.append(svg('rect', { x: 170, y, width: (v / 60) * 290, height: 24, rx: 4, fill: colour }));
    s.append(svg('text', { x: 170 + (v / 60) * 290 + 8, y: y + 17, 'font-size': 13 }, `${v}`));
  });
  $('x-rates').replaceChildren(s);
}

$('x-rain').addEventListener('input', drawSlice);
$('x-soil').addEventListener('change', drawSlice);
drawSlice();
drawRates();
document.body.dataset.ready = '1';
