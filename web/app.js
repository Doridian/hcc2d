import { TYPES, build, parse } from './formats.js';

const $ = (sel) => document.querySelector(sel);
const el = (tag, props = {}, ...kids) => {
  const e = Object.assign(document.createElement(tag), props);
  e.append(...kids.filter((k) => k !== null && k !== undefined));
  return e;
};

// ── codec worker ────────────────────────────────────────────────────────────

const worker = new Worker('worker.js');
const pending = new Map();
let seq = 0;
worker.onmessage = ({ data: { id, res, error } }) => {
  if (id === -1) {
    fatal(`${error}. Build it with “make web” and serve the web/ directory over HTTP.`);
    return;
  }
  pending.get(id)?.(res);
  pending.delete(id);
};
worker.onerror = (e) => fatal(`Worker failed: ${e.message}`);
const call = (fn, args, transfer = []) => new Promise((resolve) => {
  const id = ++seq;
  pending.set(id, resolve);
  worker.postMessage({ id, fn, args }, transfer);
});

function fatal(msg) {
  const b = $('#fatal');
  b.textContent = msg;
  b.hidden = false;
}

let toastTimer;
function toast(msg) {
  const t = $('#toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove('show'), 2200);
}

// ── tabs ────────────────────────────────────────────────────────────────────

function showTab(name) {
  for (const t of ['create', 'scan']) {
    $(`#tab-${t}`).setAttribute('aria-selected', t === name);
    $(`#panel-${t}`).hidden = t !== name;
  }
  try { localStorage.setItem('hcc2d.tab', name); } catch {}
}
$('#tab-create').onclick = () => showTab('create');
$('#tab-scan').onclick = () => showTab('scan');
try { if (localStorage.getItem('hcc2d.tab') === 'scan') showTab('scan'); } catch {}

// ── create: content ─────────────────────────────────────────────────────────

const DEFAULT_PALETTES = {
  4: ['#ffffff', '#ff00ff', '#00ffff', '#000000'],
  8: ['#ffffff', '#ffff00', '#ff00ff', '#ff0000', '#00ffff', '#00ff00', '#0000ff', '#000000'],
};

const state = {
  type: 'url',
  values: Object.fromEntries(TYPES.map((t) => [t.id, Object.fromEntries(t.fields.map((f) => [f.k, f.value ?? (f.type === 'checkbox' ? false : '')]))])),
  colors: 4,
  ec: 'L',
  minVersion: 1,
  px: 10,
  qz: 4,
  palette: { 4: [...DEFAULT_PALETTES[4]], 8: [...DEFAULT_PALETTES[8]] },
};
state.values.text.text = 'Hello from a colorful QR code! HCC2D stores 2 or 3 bits in every module.';
state.values.wifi.security = 'WPA';

function renderTypes() {
  const box = $('#types');
  box.replaceChildren(...TYPES.map((t) => {
    const b = el('button', { type: 'button', textContent: t.label, role: 'radio' });
    b.setAttribute('aria-checked', t.id === state.type);
    b.onclick = () => { state.type = t.id; renderTypes(); renderFields(); update(); };
    return b;
  }));
}

function renderFields() {
  const type = TYPES.find((t) => t.id === state.type);
  const vals = state.values[type.id];
  const form = $('#fields');
  form.replaceChildren(...type.fields.map((f) => {
    const id = `f-${type.id}-${f.k}`;
    let input;
    if (f.type === 'textarea') {
      input = el('textarea', { id, rows: type.id === 'text' ? 5 : 3, placeholder: f.placeholder ?? '', value: vals[f.k] });
    } else if (f.type === 'select') {
      input = el('select', { id }, ...f.options.map(([v, l]) => el('option', { value: v, textContent: l })));
      input.value = vals[f.k] || f.options[0][0];
    } else if (f.type === 'checkbox') {
      input = el('input', { id, type: 'checkbox', checked: !!vals[f.k] });
    } else {
      input = el('input', { id, type: f.type ?? 'text', placeholder: f.placeholder ?? '', value: vals[f.k] });
      if (f.step) input.step = f.step;
    }
    input.addEventListener(f.type === 'checkbox' || f.type === 'select' ? 'change' : 'input', () => {
      vals[f.k] = f.type === 'checkbox' ? input.checked : input.value;
      if (type.id === 'wifi') renderWifiVisibility();
      update();
    });
    if (f.type === 'checkbox') return el('label', { className: 'check' }, input, f.label);
    return el('label', { className: `field${f.half ? ' half' : ''}`, htmlFor: id }, el('span', { textContent: f.label }), input);
  }));
  if (type.id === 'wifi') renderWifiVisibility();
}

function renderWifiVisibility() {
  const pw = $('#f-wifi-password');
  if (pw) pw.closest('label').hidden = state.values.wifi.security === 'nopass';
}

// ── create: options ─────────────────────────────────────────────────────────

function segmented(sel, key, parseV = (v) => v) {
  const box = $(sel);
  const sync = () => box.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', parseV(b.dataset.v) === state[key]));
  box.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    state[key] = parseV(b.dataset.v);
    sync();
    if (key === 'colors') renderPalette();
    update();
  });
  sync();
}
segmented('#opt-colors', 'colors', Number);
segmented('#opt-ec', 'ec');

const verSel = $('#opt-version');
verSel.append(el('option', { value: 1, textContent: 'Auto (smallest)' }));
for (let v = 2; v <= 40; v++) verSel.append(el('option', { value: v, textContent: `${v} (${17 + 4 * v}×${17 + 4 * v})` }));
verSel.onchange = () => { state.minVersion = +verSel.value; update(); };

for (const [id, key] of [['px', 'px'], ['qz', 'qz']]) {
  const input = $(`#opt-${id}`);
  input.oninput = () => { state[key] = +input.value; $(`#out-${id}`).textContent = input.value; draw(); };
}

function renderPalette() {
  const pal = state.palette[state.colors];
  $('#palette-preview').replaceChildren(...pal.map((c) => el('i', { style: `background:${c}` })));
  $('#palette-edit').replaceChildren(...pal.map((c, i) => {
    const input = el('input', { type: 'color', value: c, title: `Value ${i} (${i.toString(2).padStart(Math.log2(state.colors), '0')})` });
    input.oninput = () => { pal[i] = input.value; renderPaletteSwatches(); update(); };
    return el('label', { className: 'pal-item' }, input, el('code', { textContent: i.toString(2).padStart(Math.log2(state.colors), '0') }));
  }));
}
function renderPaletteSwatches() {
  $('#palette-preview').replaceChildren(...state.palette[state.colors].map((c) => el('i', { style: `background:${c}` })));
}
$('#palette-reset').onclick = () => {
  state.palette[state.colors] = [...DEFAULT_PALETTES[state.colors]];
  renderPalette();
  update();
};

// ── create: encoding and rendering ──────────────────────────────────────────

let symbol = null;
let encSeq = 0;
let updateTimer;
function update() {
  clearTimeout(updateTimer);
  updateTimer = setTimeout(encodeNow, 60);
}

async function encodeNow() {
  const my = ++encSeq;
  const text = build(state.type, state.values[state.type]);
  $('#payload').textContent = text;
  const data = new TextEncoder().encode(text);
  const res = await call('encode', [data, {
    colors: state.colors, ec: state.ec, minVersion: state.minVersion, palette: state.palette[state.colors],
  }], [data.buffer]);
  if (my !== encSeq) return;
  const err = $('#enc-error');
  if (!res.ok) {
    err.textContent = res.error;
    err.hidden = false;
    $('.preview').classList.add('stale');
    return;
  }
  err.hidden = true;
  $('.preview').classList.remove('stale');
  symbol = res;
  draw();
  const info = $('#enc-info');
  const used = res.capacity ? res.bytes / res.capacity : 0;
  info.replaceChildren(
    ...[['Version', `${res.version}`], ['Modules', `${res.size}×${res.size}`], ['Colors', `${res.colors} · ${Math.log2(res.colors)} bit`],
      ['EC level', res.ec], ['Mask', res.mask], ['Data', `${res.bytes} / ${res.capacity} ${res.mode === 'byte' ? 'bytes' : `chars (${res.mode})`}`]]
      .map(([k, v]) => el('div', {}, el('dt', { textContent: k }), el('dd', { textContent: v }))),
  );
  $('#enc-meter').style.width = `${Math.min(100, used * 100).toFixed(1)}%`;
}

// draw renders the symbol: the module grid plus its palette ring, inside a
// quiet zone of state.qz modules counted from the QR grid.
function draw() {
  if (!symbol) return;
  const { size, cells } = symbol;
  const { px, qz } = state;
  const n = size + 2 * qz;
  const canvas = $('#code');
  canvas.width = canvas.height = n * px;
  const ctx = canvas.getContext('2d');
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, n * px, n * px);
  const w = size + 2;
  for (let y = 0; y < w; y++) {
    for (let x = 0; x < w; x++) {
      const i = 3 * (y * w + x);
      const [r, g, b] = [cells[i], cells[i + 1], cells[i + 2]];
      if (r === 255 && g === 255 && b === 255) continue;
      ctx.fillStyle = `rgb(${r},${g},${b})`;
      ctx.fillRect((x + qz - 1) * px, (y + qz - 1) * px, px, px);
    }
  }
}

function svgMarkup() {
  const { size, cells } = symbol;
  const { qz } = state;
  const n = size + 2 * qz;
  const w = size + 2;
  const runs = new Map();
  for (let y = 0; y < w; y++) {
    for (let x = 0; x < w;) {
      const i = 3 * (y * w + x);
      const hex = `#${[cells[i], cells[i + 1], cells[i + 2]].map((c) => c.toString(16).padStart(2, '0')).join('')}`;
      let len = 1;
      while (x + len < w && cells[3 * (y * w + x + len)] === cells[i] && cells[3 * (y * w + x + len) + 1] === cells[i + 1] && cells[3 * (y * w + x + len) + 2] === cells[i + 2]) len++;
      if (hex !== '#ffffff') {
        if (!runs.has(hex)) runs.set(hex, []);
        runs.get(hex).push(`M${x + qz - 1} ${y + qz - 1}h${len}v1h-${len}z`);
      }
      x += len;
    }
  }
  const paths = [...runs].map(([c, d]) => `<path fill="${c}" d="${d.join('')}"/>`).join('');
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${n} ${n}" width="${n * state.px}" height="${n * state.px}" shape-rendering="crispEdges"><rect width="${n}" height="${n}" fill="#fff"/>${paths}</svg>`;
}

function fileBase() {
  return `hcc2d-${state.type}-v${symbol.version}-${symbol.colors}c`;
}
function download(href, name) {
  const a = el('a', { href, download: name });
  document.body.append(a);
  a.click();
  a.remove();
}
$('#dl-png').onclick = () => symbol && $('#code').toBlob((b) => {
  const u = URL.createObjectURL(b);
  download(u, `${fileBase()}.png`);
  setTimeout(() => URL.revokeObjectURL(u), 5000);
}, 'image/png');
$('#dl-svg').onclick = () => {
  if (!symbol) return;
  const u = URL.createObjectURL(new Blob([svgMarkup()], { type: 'image/svg+xml' }));
  download(u, `${fileBase()}.svg`);
  setTimeout(() => URL.revokeObjectURL(u), 5000);
};
$('#copy-img').onclick = () => symbol && $('#code').toBlob(async (b) => {
  try {
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': b })]);
    toast('Image copied');
  } catch {
    toast('Copying images is not supported here');
  }
}, 'image/png');
$('#test-scan').onclick = async () => {
  if (!symbol) return;
  stopCamera();
  showTab('scan');
  const c = $('#code');
  await scanSource(c, c.width, c.height, 'generated code');
};

// ── scan ────────────────────────────────────────────────────────────────────

const drop = $('#drop');
const fileInput = $('#file');
fileInput.onchange = () => fileInput.files[0] && scanFile(fileInput.files[0]);
drop.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); fileInput.click(); } });
for (const ev of ['dragenter', 'dragover']) drop.addEventListener(ev, (e) => { e.preventDefault(); drop.classList.add('over'); });
for (const ev of ['dragleave', 'drop']) drop.addEventListener(ev, () => drop.classList.remove('over'));
drop.addEventListener('drop', (e) => {
  e.preventDefault();
  const f = [...e.dataTransfer.files].find((f) => f.type.startsWith('image/'));
  if (f) scanFile(f);
});
document.addEventListener('paste', (e) => {
  const item = [...(e.clipboardData?.items ?? [])].find((i) => i.type.startsWith('image/'));
  if (!item) return;
  showTab('scan');
  scanFile(item.getAsFile());
});
$('#opt-classifier').onchange = () => lastSource && scanSource(...lastSource);
$('#try-sample').onclick = async () => {
  const blob = await fetch('sample.png').then((r) => (r.ok ? r.blob() : Promise.reject(new Error(r.statusText))));
  scanFile(blob, 'sample');
};

async function scanFile(file, name = file.name) {
  stopCamera();
  setStatus('busy', `Reading ${name || 'image'}…`);
  let bmp;
  try {
    bmp = await createImageBitmap(file);
  } catch {
    setStatus('error', 'That file could not be read as an image.');
    return;
  }
  await scanSource(bmp, bmp.width, bmp.height, name);
}

let lastSource = null;
let scanSeq = 0;

// scanSource decodes an image, downscaling large photos first. If a
// downscaled attempt fails, it retries at a higher resolution.
async function scanSource(src, w, h, name) {
  lastSource = [src, w, h, name];
  const my = ++scanSeq;
  setStatus('busy', `Scanning ${name || 'image'}…`);
  const limits = [1600, 2800].filter((m, i) => i === 0 || Math.max(w, h) > [1600, 2800][i - 1]);
  let res;
  let scale = 1;
  for (const limit of limits) {
    scale = Math.min(1, limit / Math.max(w, h));
    const cw = Math.round(w * scale);
    const ch = Math.round(h * scale);
    const c = new OffscreenCanvas(cw, ch);
    const ctx = c.getContext('2d', { willReadFrequently: true });
    ctx.fillStyle = '#fff';
    ctx.fillRect(0, 0, cw, ch);
    ctx.drawImage(src, 0, 0, cw, ch);
    const pixels = new Uint8Array(ctx.getImageData(0, 0, cw, ch).data.buffer);
    res = await call('decode', [pixels, cw, ch, { classifier: $('#opt-classifier').value }], [pixels.buffer]);
    if (my !== scanSeq) return;
    if (res.ok) break;
  }
  showScan(src, w, h, res.ok ? res.corners.map(([x, y]) => [x / scale, y / scale]) : null);
  if (!res.ok) {
    $('#scan-result').hidden = true;
    setStatus('error', `No HCC2D code could be read: ${res.error}.`,
      'Tips: make sure the whole code including its colored border strips is visible, evenly lit and in focus.');
    return;
  }
  const status = $('#scan-status');
  status.className = 'card status';
  status.hidden = true;
  showResult(res);
}

function setStatus(kind, msg, hint) {
  const s = $('#scan-status');
  s.hidden = false;
  s.className = `card status ${kind}`;
  s.replaceChildren(el('p', { textContent: msg }), hint ? el('p', { className: 'muted', textContent: hint }) : null);
  $('#scan-result').hidden = true;
}

function showScan(src, w, h, corners) {
  $('#scan-view').hidden = false;
  const canvas = $('#scan-canvas');
  const scale = Math.min(1, 900 / Math.max(w, h));
  canvas.width = Math.round(w * scale);
  canvas.height = Math.round(h * scale);
  const ctx = canvas.getContext('2d');
  ctx.drawImage(src, 0, 0, canvas.width, canvas.height);
  if (!corners) return;
  drawOutline(ctx, corners.map(([x, y]) => [x * scale, y * scale]), canvas.width);
}

// ── scan: camera ────────────────────────────────────────────────────────────

const video = $('#cam-video');
const overlay = $('#cam-overlay');
const camSelect = $('#cam-select');
let cam = null; // {stream, token}
let camToken = 0;

function camStatus(msg) {
  $('#cam-status').textContent = msg;
}

async function startCamera(deviceId) {
  if (!navigator.mediaDevices?.getUserMedia) {
    setStatus('error', 'Camera access is not available.', 'Browsers only allow it on HTTPS pages or on localhost.');
    return;
  }
  stopCamera();
  const token = ++camToken;
  $('#cam-view').hidden = false;
  $('#scan-view').hidden = true;
  setStatus('busy', 'Waiting for the camera…');
  camStatus('Starting camera…');
  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({
      audio: false,
      video: deviceId
        ? { deviceId: { exact: deviceId }, width: { ideal: 1920 }, height: { ideal: 1080 } }
        : { facingMode: { ideal: 'environment' }, width: { ideal: 1920 }, height: { ideal: 1080 } },
    });
  } catch (err) {
    if (token !== camToken) return;
    $('#cam-view').hidden = true;
    setStatus('error', `Could not open the camera: ${err.message || err.name}.`,
      err.name === 'NotAllowedError' ? 'Allow camera access for this page in your browser settings and try again.' : undefined);
    return;
  }
  if (token !== camToken) {
    stream.getTracks().forEach((t) => t.stop());
    return;
  }
  cam = { stream, token };
  video.srcObject = stream;
  await video.play().catch(() => {});
  if (token !== camToken) return;
  setStatus('busy', 'Point the camera at an HCC2D code.', 'Hold it steady so the whole code, including its colored border strips, is in the frame.');
  listCameras(stream);
  camLoop(token);
}

async function listCameras(stream) {
  const devices = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'videoinput');
  const current = stream.getVideoTracks()[0]?.getSettings().deviceId;
  camSelect.replaceChildren(...devices.map((d, i) => el('option', { value: d.deviceId, textContent: d.label || `Camera ${i + 1}` })));
  if (current) camSelect.value = current;
  camSelect.hidden = devices.length < 2;
}
camSelect.onchange = () => startCamera(camSelect.value);

function stopCamera() {
  camToken++;
  $('#cam-view').hidden = true;
  if (!cam) return;
  cam.stream.getTracks().forEach((t) => t.stop());
  cam = null;
  video.srcObject = null;
}

// camLoop decodes frames back to back: each frame goes to the worker as
// soon as the previous one is done. The first successful read freezes that
// frame and stops the camera.
async function camLoop(token) {
  const frame = new OffscreenCanvas(1, 1);
  const fctx = frame.getContext('2d', { willReadFrequently: true });
  let frames = 0;
  const t0 = performance.now();
  while (token === camToken) {
    const w = video.videoWidth;
    const h = video.videoHeight;
    if (!w || video.readyState < 2) {
      await new Promise((r) => setTimeout(r, 100));
      continue;
    }
    // Phone sensors deliver more pixels than the decoder needs.
    const scale = Math.min(1, 1280 / Math.max(w, h));
    frame.width = Math.round(w * scale);
    frame.height = Math.round(h * scale);
    fctx.drawImage(video, 0, 0, frame.width, frame.height);
    const pixels = new Uint8Array(fctx.getImageData(0, 0, frame.width, frame.height).data.buffer);
    const res = await call('decode', [pixels, frame.width, frame.height, { classifier: $('#opt-classifier').value }], [pixels.buffer]);
    if (token !== camToken) return;
    frames++;
    overlay.width = w;
    overlay.height = h;
    const octx = overlay.getContext('2d');
    octx.clearRect(0, 0, w, h);
    if (!res.ok) {
      camStatus(`Scanning… ${(frames / ((performance.now() - t0) / 1000)).toFixed(1)} frames/s`);
      continue;
    }
    const corners = res.corners.map(([x, y]) => [x / scale, y / scale]);
    drawOutline(octx, corners, w);
    navigator.vibrate?.(80);
    // Keep the frame that decoded so the result (and a classifier change)
    // refers to what was seen.
    const still = new OffscreenCanvas(w, h);
    still.getContext('2d').drawImage(video, 0, 0);
    stopCamera();
    lastSource = [still, w, h, 'camera frame'];
    showScan(still, w, h, corners);
    const status = $('#scan-status');
    status.className = 'card status';
    status.hidden = true;
    showResult(res);
    return;
  }
}

$('#cam-start').onclick = () => startCamera();
$('#cam-stop').onclick = () => {
  stopCamera();
  setStatus('idle', 'Camera stopped.');
};
document.addEventListener('visibilitychange', () => {
  if (document.hidden && cam) {
    stopCamera();
    setStatus('idle', 'Camera stopped while the page was in the background.');
  }
});
$('#tab-create').addEventListener('click', () => cam && stopCamera());

function drawOutline(ctx, pts, width) {
  ctx.save();
  ctx.lineWidth = Math.max(2, width / 200);
  ctx.strokeStyle = '#22c55e';
  ctx.fillStyle = 'rgba(34,197,94,0.12)';
  ctx.beginPath();
  pts.forEach(([x, y], i) => (i ? ctx.lineTo(x, y) : ctx.moveTo(x, y)));
  ctx.closePath();
  ctx.fill();
  ctx.stroke();
  // Mark the top-left corner so the orientation is visible.
  ctx.fillStyle = '#22c55e';
  ctx.beginPath();
  ctx.arc(pts[0][0], pts[0][1], ctx.lineWidth * 2.5, 0, 2 * Math.PI);
  ctx.fill();
  ctx.restore();
}

function showResult(res) {
  const box = $('#scan-result');
  box.hidden = false;
  const text = res.text;
  const info = text !== undefined ? parse(text) : { type: 'binary', title: 'Binary data', fields: [] };
  $('#res-type').textContent = info.type;
  $('#res-type').dataset.type = info.type;
  $('#res-title').textContent = info.title;
  $('#res-fields').replaceChildren(...info.fields.map(([k, v]) => el('div', {}, el('dt', { textContent: k }), el('dd', { textContent: v }))));
  $('#res-fields').hidden = !info.fields.length;

  const actions = [];
  if (info.action) {
    const a = el('a', { className: 'button primary', textContent: info.action.label, href: info.action.href, rel: 'noopener noreferrer' });
    if (info.action.download) a.download = info.action.download;
    else if (/^https?:/i.test(info.action.href)) a.target = '_blank';
    actions.push(a);
  }
  if (info.copy) actions.push(el('button', { type: 'button', textContent: 'Copy password', onclick: () => copyText(info.copy, 'Password copied') }));
  if (text === undefined) {
    const u = URL.createObjectURL(new Blob([res.data], { type: 'application/octet-stream' }));
    actions.push(el('a', { className: 'button primary', textContent: 'Download data', href: u, download: 'hcc2d-data.bin' }));
  }
  $('#res-actions').replaceChildren(...actions);
  $('#res-actions').hidden = !actions.length;

  $('#res-raw').textContent = text ?? hexdump(res.data);
  $('#res-copy').onclick = (e) => { e.preventDefault(); copyText(text ?? hexdump(res.data), 'Copied'); };
  $('#res-info').replaceChildren(
    ...[['Version', `${res.version} (${res.size}×${res.size})`], ['Colors', `${res.colors} · ${Math.log2(res.colors)} bit`], ['EC level', res.ec],
      ['Mask', res.mask], ['Bytes', res.data.length], ['Corrected', `${res.corrected} byte${res.corrected === 1 ? '' : 's'}`], ['Time', `${res.ms.toFixed(0)} ms`]]
      .map(([k, v]) => el('div', {}, el('dt', { textContent: k }), el('dd', { textContent: v }))),
  );
}

function hexdump(bytes) {
  const out = [];
  for (let i = 0; i < bytes.length; i += 16) {
    const row = [...bytes.slice(i, i + 16)];
    out.push(`${i.toString(16).padStart(6, '0')}  ${row.map((b) => b.toString(16).padStart(2, '0')).join(' ').padEnd(48)}  ${row.map((b) => (b >= 32 && b < 127 ? String.fromCharCode(b) : '.')).join('')}`);
  }
  return out.join('\n');
}

async function copyText(t, msg) {
  try {
    await navigator.clipboard.writeText(t);
    toast(msg);
  } catch {
    toast('Clipboard not available');
  }
}

// ── start ───────────────────────────────────────────────────────────────────

renderTypes();
renderFields();
renderPalette();
update();
