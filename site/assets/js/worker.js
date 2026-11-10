/* The engine runs here, off the page's main thread, so the page never freezes while it works.
   It loads Go's WebAssembly support and the engine once, then answers one request at a time.
   Requests and replies are plain strings and numbers: nothing else crosses into the engine. */
/* global Go */
'use strict';
importScripts('../vendor/wasm_exec.js');

const STORMS = ['downpour', 'five-year', 'twenty-year'];
let ready = null;
function start() {
  if (!ready) {
    ready = (async () => {
      const go = new Go();
      const { instance } = await WebAssembly.instantiateStreaming(fetch('../rainward.wasm'), go.importObject);
      go.run(instance); // runs main(), which registers self.rainward and then waits
      if (!self.rainward || !self.rainward.ready) throw new Error('engine did not start');
    })();
  }
  return ready;
}

self.onmessage = async (event) => {
  const d = event.data;
  if (!d || typeof d.id !== 'number' || typeof d.lot !== 'string' || d.lot.length > 131072 ||
      !STORMS.includes(d.storm) || typeof d.budget !== 'number' || !(d.budget >= 0 && d.budget <= 50000)) return;
  let result;
  try {
    await start();
    result = self.rainward.plan(d.lot, d.storm, Math.round(d.budget));
  } catch {
    result = JSON.stringify({ error: 'The planner could not start in this browser.' });
  }
  self.postMessage({ id: d.id, result: String(result) });
};
