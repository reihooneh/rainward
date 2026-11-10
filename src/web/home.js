// Home: the sloping clay block, planned live, with the storm playing in 3D.
import { plan, loadLot } from './engine.js';
import { YardScene } from './yard3d.js';
import { litres, dollars, pressed } from './ui.js';

const $ = (id) => document.getElementById(id);
const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;
const scene = new YardScene($('hero'), { interactive: true, spin: false });
let which = 'plan';
let res = null;
let playing = false;

async function start() {
  const lot = await loadLot('clay-slope');
  res = await plan(lot, 'five-year', 3000);
  $('s-wall').textContent = litres(res.today.wallFlow + res.today.wallPond);
  const before = res.today.wallFlow + res.today.wallPond;
  const after = res.planned.wallFlow + res.planned.wallPond;
  $('s-cut').textContent = `${Math.round(100 - (100 * after) / before)}%`;
  $('s-cost').textContent = dollars(res.cost);
  show();
  document.body.dataset.ready = '1';
  if (!reduced()) play();
}

function show() {
  pressed($('which'), which);
  scene.show(res, which);
  if (!playing) scene.setTime(scene.rainEnd);
}

function play() {
  if (!res) return;
  if (reduced()) { scene.setTime(scene.lastFrame); return; }
  playing = true;
  scene.playing = true;
  $('play').textContent = 'Pause';
  if (scene.time >= scene.lastFrame - 0.01) scene.setTime(0);
  let last = performance.now();
  const tick = (now) => {
    if (!playing) return;
    scene.setTime(scene.time + ((now - last) / 1000) * (scene.lastFrame / 14));
    last = now;
    if (scene.time >= scene.lastFrame) stop();
    else requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

function stop() {
  playing = false;
  scene.playing = false;
  $('play').textContent = 'Play the storm';
}

$('play').addEventListener('click', () => (playing ? stop() : play()));
$('which').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b) return;
  which = b.dataset.value;
  show();
});
start();
