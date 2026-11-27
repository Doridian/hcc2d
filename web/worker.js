// Runs the Go/WebAssembly HCC2D codec off the main thread. Messages are
// {id, fn, args}; replies are {id, res}. A failed start-up replies with
// {id: -1, error}.
importScripts('wasm_exec.js');

let markReady;
const ready = new Promise((resolve) => { markReady = resolve; });
self.hcc2dReady = () => markReady();

const go = new Go();
WebAssembly.instantiateStreaming(fetch('colorqr.wasm'), go.importObject)
  .then(({ instance }) => go.run(instance))
  .catch((err) => postMessage({ id: -1, error: `Could not load colorqr.wasm: ${err}` }));

self.onmessage = async ({ data: { id, fn, args } }) => {
  await ready;
  let res;
  try {
    res = self.hcc2d[fn](...args);
  } catch (err) {
    res = { ok: false, error: String(err) };
  }
  const transfer = [res.cells?.buffer, res.data?.buffer].filter(Boolean);
  postMessage({ id, res }, transfer);
};
