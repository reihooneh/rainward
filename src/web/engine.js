// Talks to the engine (Go compiled to WebAssembly) running in a Web Worker, and checks lots the
// visitor loads before they go anywhere.

export const LOTS = [
  { id: 'clay-slope', name: 'Sloping block on clay' },
  { id: 'corner-loam', name: 'Corner block on loam' },
  { id: 'sandy-flat', name: 'Flat block on sand' },
];
export const STORMS = [
  { id: 'downpour', name: 'Downpour', about: '25 mm in an hour: most years' },
  { id: 'five-year', name: 'Five-year storm', about: '40 mm in an hour: about a one-in-five chance each year' },
  { id: 'twenty-year', name: 'Twenty-year storm', about: '55 mm in an hour: about a one-in-twenty chance each year' },
];
export const MAX_BYTES = 128 * 1024;

let worker = null;
let seq = 0;
const waiting = new Map();
const cache = new Map();

function getWorker() {
  if (!worker) {
    worker = new Worker(new URL('./worker.js', import.meta.url));
    worker.onmessage = (event) => {
      const { id, result } = event.data || {};
      const done = waiting.get(id);
      if (!done) return;
      waiting.delete(id);
      done(result);
    };
  }
  return worker;
}

export class PlanError extends Error {}

/** Plans a lot for a storm and budget. Resolves with the result or rejects with a PlanError. */
export async function plan(lot, storm, budget) {
  const text = JSON.stringify(lot);
  const key = `${storm}|${budget}|${text}`;
  if (cache.has(key)) return cache.get(key);
  const id = ++seq;
  const raw = await new Promise((resolve) => {
    waiting.set(id, resolve);
    getWorker().postMessage({ id, lot: text, storm, budget });
  });
  let result;
  try {
    result = JSON.parse(raw);
  } catch {
    throw new PlanError('The planner sent back something unreadable.');
  }
  if (result.error) throw new PlanError(cap(result.error));
  if (cache.size > 12) cache.delete(cache.keys().next().value);
  cache.set(key, result);
  return result;
}

const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1) + (/[.!?]$/.test(s) ? '' : '.');

/** Loads one of the bundled lots by id (only ids from the list above). */
export async function loadLot(id) {
  if (!LOTS.some((l) => l.id === id)) throw new PlanError('Unknown lot.');
  const res = await fetch(new URL(`../lots/${id}.json`, import.meta.url));
  if (!res.ok) throw new PlanError('The lot could not be loaded.');
  return res.json();
}

/** Every key anywhere in a parsed JSON value. */
function* keys(v) {
  if (Array.isArray(v)) for (const x of v) yield* keys(x);
  else if (v && typeof v === 'object') for (const k of Object.keys(v)) { yield k; yield* keys(v[k]); }
}

/** Reads a lot file the visitor chose. The engine checks every field; this catches what it can't
 *  explain well (size, broken JSON, prototype keys) without ever repeating the file back. */
export async function readLotFile(file) {
  if (!file || file.size > MAX_BYTES) throw new PlanError(`That file is too big: a lot is at most ${MAX_BYTES / 1024} KB.`);
  let data;
  try {
    data = JSON.parse(await file.text());
  } catch {
    throw new PlanError("That file isn't valid JSON.");
  }
  if (!data || typeof data !== 'object' || Array.isArray(data)) throw new PlanError("That file isn't a lot: it should be one JSON object.");
  for (const k of keys(data)) {
    if (k === '__proto__' || k === 'constructor' || k === 'prototype') throw new PlanError('That file has an unknown field.');
  }
  return data;
}

/** Downloads a lot as a JSON file. */
export function downloadLot(lot) {
  const blob = new Blob([JSON.stringify(lot, null, 2) + '\n'], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `${(lot.name || 'lot').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'lot'}.json`;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

/** Decodes one animation frame: little-endian uint16 millimetres (base64 from Go). */
export function decodeFrame(b64) {
  const bin = atob(b64);
  const out = new Float32Array(bin.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = (bin.charCodeAt(2 * i) | (bin.charCodeAt(2 * i + 1) << 8)) / 1000;
  return out;
}
