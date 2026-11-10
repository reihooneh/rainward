// The planner page: choose a lot, a storm and a budget; see the storm play out today and with the
// plan, the plan's steps, where the water went and where it stands.
import { LOTS, STORMS, plan, loadLot, readLotFile, downloadLot, PlanError } from './engine.js';
import { YardScene } from './yard3d.js';
import { YardMap } from './map2d.js';
import { el, svg, litres, dollars, whole, pressed } from './ui.js';

const $ = (id) => document.getElementById(id);
const state = { lotId: 'clay-slope', lot: null, custom: null, storm: 'five-year', budget: 3000, which: 'plan', view: '3d', res: null, run: 0 };
const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const scene = new YardScene($('c3d'));
const map = new YardMap($('c2d'));

function status(text) { $('status').textContent = text; }

// ---------- Controls ----------
for (const s of STORMS) $('storm').append(el('button', { type: 'button', 'data-value': s.id, text: s.name }));
$('storm').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b || b.dataset.value === state.storm) return;
  state.storm = b.dataset.value;
  run();
});
$('budget').addEventListener('input', () => {
  state.budget = Number($('budget').value);
  $('budget-out').textContent = dollars(state.budget);
  clearTimeout(state.timer);
  state.timer = setTimeout(run, 250);
});
$('lot').addEventListener('change', async () => {
  state.lotId = $('lot').value;
  await useLot();
});
$('load').addEventListener('click', () => $('file').click());
$('file').addEventListener('change', async () => {
  const file = $('file').files[0];
  $('file').value = '';
  if (!file) return;
  $('load-error').hidden = true;
  try {
    const lot = await readLotFile(file);
    // Let the engine check it before it replaces the current lot.
    await plan(lot, state.storm, state.budget);
    state.custom = lot;
    if (!$('lot').querySelector('option[value="custom"]')) $('lot').append(el('option', { value: 'custom', text: 'Your lot' }));
    $('lot').value = 'custom';
    state.lotId = 'custom';
    await useLot();
  } catch (err) {
    $('load-error').textContent = err instanceof PlanError ? err.message : 'That file could not be read.';
    $('load-error').hidden = false;
  }
});
$('save').addEventListener('click', () => state.lot && downloadLot(state.lot));
$('which').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b) return;
  state.which = b.dataset.value;
  showYard();
});
$('view').addEventListener('click', (e) => {
  const b = e.target.closest('button');
  if (!b) return;
  state.view = b.dataset.value;
  showYard();
});
$('time').addEventListener('input', () => {
  stop();
  scene.setTime(Number($('time').value));
  clock();
});
$('play').addEventListener('click', () => (state.playing ? stop() : play()));

function play() {
  if (!state.res) return;
  if (reduced()) {
    scene.setTime(scene.lastFrame);
    syncTime();
    return;
  }
  state.playing = true;
  scene.playing = true;
  $('play').textContent = 'Pause';
  if (scene.time >= scene.lastFrame - 0.01) scene.setTime(0);
  let last = performance.now();
  const tick = (now) => {
    if (!state.playing) return;
    const dt = (now - last) / 1000;
    last = now;
    scene.setTime(scene.time + dt * (scene.lastFrame / 14)); // the whole storm in about 14 seconds
    syncTime();
    if (scene.time >= scene.lastFrame) stop();
    else requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
}

function stop() {
  state.playing = false;
  scene.playing = false;
  $('play').textContent = 'Play the storm';
}

function syncTime() {
  $('time').value = String(scene.time);
  clock();
}

function clock() {
  const m = Math.round(scene.minutes());
  const raining = m < (state.res?.storm.minutes ?? 60);
  $('clock').textContent = `${m} min · ${raining ? 'raining' : 'rain stopped'}`;
}

// ---------- Running ----------
async function useLot() {
  $('load-error').hidden = true;
  try {
    state.lot = state.lotId === 'custom' ? state.custom : await loadLot(state.lotId);
  } catch (err) {
    status(err.message);
    return;
  }
  $('about').textContent = state.lot.about || '';
  state.storm = STORMS.some((s) => s.id === state.lot.storm) ? state.lot.storm : 'five-year';
  state.budget = Math.max(0, Math.min(10000, Math.round((state.lot.budget ?? 3000) / 250) * 250));
  $('budget').value = String(state.budget);
  $('budget-out').textContent = dollars(state.budget);
  scene.placed = false;
  await run();
}

async function run() {
  const id = ++state.run;
  pressed($('storm'), state.storm);
  $('storm-about').textContent = STORMS.find((s) => s.id === state.storm).about;
  status('Following the storm across the lot and comparing plans…');
  let res;
  try {
    res = await plan(state.lot, state.storm, state.budget);
  } catch (err) {
    if (id === state.run) status(err instanceof PlanError ? err.message : 'Something went wrong planning this lot.');
    return;
  }
  if (id !== state.run) return;
  state.res = res;
  render(res);
  status(`Compared ${whole(res.evaluations)} ${res.evaluations === 1 ? 'plan' : 'plans'} for a ${res.storm.name.toLowerCase()} on ${res.soil.name.toLowerCase()} soil${res.complete ? '' : ' (a big lot: the search stopped at its time limit, so this is the best plan found)'}.`);
  document.body.dataset.ready = '1';
}

function render(res) {
  const t = res.today;
  const p = res.planned;
  const cut = t.damage > 0 ? Math.round(100 - (100 * p.damage) / t.damage) : 0;
  $('headline').textContent = res.actions.length
    ? `Weighted litres: ${whole(t.damage)} today, ${whole(p.damage)} with this plan (${cut}% less).`
    : `Weighted litres: ${whole(t.damage)}. Nothing within the budget helps enough to be worth doing.`;
  const tiles = [
    ['Water running along or pooling at the house', t.wallFlow + t.wallPond, p.wallFlow + p.wallPond],
    ["Onto the neighbours' land", t.neighbours, p.neighbours],
    ['To the street drain', t.street, p.street],
    ['Soaked in or caught in tanks', t.soaked + t.tanks, p.soaked + p.tanks, true],
  ];
  $('stats').replaceChildren(...tiles.map(([k, a, b, more]) => {
    const better = more ? b >= a - 1e-6 : b <= a + 1e-6;
    return el('div', { class: 'stat' },
      el('div', { class: 'k', text: k }),
      el('div', { class: 'v', text: litres(b) }),
      el('div', { class: `d${better ? '' : ' worse'}`, text: `${litres(a)} today` }));
  }));

  $('steps').replaceChildren(...res.actions.map((a) => el('li', {},
    el('span', {}, a.text, el('span', { class: 'after', text: `Score after this step: ${whole(a.damage)}` })),
    el('span', { class: 'cost', text: dollars(a.cost) }))));
  $('total').textContent = res.actions.length ? `Total ${dollars(res.cost)} of ${dollars(res.budget)}` : 'No changes';

  balance(res);
  ponds(res);
  showYard();
}

function balance(res) {
  const rows = [
    ['Caught in tanks', 'tanks', '#3f6f8a'],
    ['Soaked in during the storm', 'soaked', '#7a5c3e'],
    ['Left standing in hollows', 'ponded', '#145f8a'],
    ['Ran to the street', 'street', '#8fa3b3'],
    ["Ran onto neighbours' land", 'neighbours', '#c9502f'],
  ];
  const total = res.today.rain + res.today.roof;
  const s = svg('svg', { viewBox: '0 0 360 120', role: 'img', 'aria-label': 'Where the storm water went, today and with the plan' });
  [['Today', res.today], ['With the plan', res.planned]].forEach(([label, o], k) => {
    const y = 14 + k * 54;
    s.append(svg('text', { x: 0, y: y + 22, class: 'bar-label' }, label));
    let x = 92;
    for (const [, key, colour] of rows) {
      const w = (o[key] / total) * 264;
      if (w > 0.2) s.append(svg('rect', { x: x.toFixed(2), y, width: w.toFixed(2), height: 34, fill: colour, rx: 2 }));
      x += w;
    }
  });
  $('balance').replaceChildren(s);
  $('balance-table').tBodies[0].replaceChildren(...rows.map(([label, key, colour]) => el('tr', {},
    el('td', {}, el('i', { class: 'swatch', 'data-colour': colour }), ` ${label}`),
    el('td', { class: 'num', text: litres(res.today[key]) }),
    el('td', { class: 'num', text: litres(res.planned[key]) }))));
  for (const sw of $('balance-table').querySelectorAll('.swatch')) sw.style.background = sw.dataset.colour;
}

function ponds(res) {
  const o = state.which === 'plan' ? res.planned : res.today;
  const list = o.hollows.filter((h) => h.stored >= 0.005).slice(0, 8);
  const where = (h) => `${h.x.toFixed(0)} m from the left, ${h.y.toFixed(0)} m back${h.nearHome ? ' (against the house)' : ''}`;
  const hours = (h) => (h.drainHours >= 999 ? 'never (paved)' : h.drainHours < 1 ? 'under an hour' : `${Math.round(h.drainHours)} h`);
  $('ponds').tBodies[0].replaceChildren(...list.map((h) => el('tr', {},
    el('td', { text: where(h) }),
    el('td', { class: 'num', text: litres(h.stored) }),
    el('td', { class: 'num', text: `${Math.round(h.maxDepth * 100)} cm` }),
    el('td', { class: 'num', text: hours(h) }))));
  $('ponds-note').textContent = list.length
    ? `${state.which === 'plan' ? 'With the plan' : 'Today'}: rain gardens are meant to hold water for a few hours; water standing more than a day kills lawn and breeds mosquitoes.`
    : 'No water is left standing after this storm.';
}

function showYard() {
  const res = state.res;
  if (!res) return;
  pressed($('which'), state.which);
  pressed($('view'), state.view);
  const three = state.view === '3d';
  $('stage').hidden = !three;
  $('c2d').hidden = three;
  $('time').disabled = !three;
  $('play').disabled = !three;
  if (three) {
    scene.show(res, state.which);
    $('time').max = String(scene.lastFrame);
    if (!state.playing) scene.setTime(scene.rainEnd);
    syncTime();
  } else {
    stop();
    map.show(res, state.which);
  }
  ponds(res);
}

$('budget-out').textContent = dollars(state.budget);
if (!LOTS.some((l) => l.id === state.lotId)) state.lotId = LOTS[0].id;
useLot();
