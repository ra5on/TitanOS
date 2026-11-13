// README pictures of the current UI with isolated demonstration data (docs/screenshots/README.md).
// PW=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5290 OUT=docs/screenshots/current node scripts/capture-readme-screenshots.mjs
// ONLY=name,name captures just these pictures; SHOW_VERSION sets the version the pages name (default 0.78.0).
import { fixtureFactory } from './test-ui-fixtures.mjs';
import { installNvr } from './test-ui-nvr-fixtures.mjs';
import { mkdir } from 'node:fs/promises';
const { chromium } = await import(process.env.PW || 'playwright');
const base = process.env.UI_URL || 'http://127.0.0.1:5290';
const out = (process.env.OUT || 'out/readme-shots') + '/';
const version = process.env.SHOW_VERSION || '0.78.0';
const only = process.env.ONLY ? process.env.ONLY.split(',') : null;
const want = (name) => !only || only.includes(name);
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'] });
const create = fixtureFactory(browser, []);
const names = [['Angebote', true], ['Rechnungen 2026', true], ['Verträge', true], ['Urlaub Garda', true], ['Gehäuse Deckel.3mf', false, 2.4e6], ['Angebot Gartenhaus.docx', false, 24e3], ['Halterung.stl', false, 820e3], ['Kfz-Versicherung 2026.pdf', false, 210e3], ['Strom Abschlag.xlsx', false, 38e3], ['Notiz.txt', false, 1e3]];
const files = names.map(([n, d, s]) => ({ name: n, dir: d, size: s ?? 0, modified: '2026-11-0' + (1 + (n.length % 7)) + 'T10:00:00Z' }));
const frameOf = (p) => p.frames().find((x) => x !== p.mainFrame());

// Two VMs instead of the fixture's stopped one with 512 MB: a running Home Assistant OS and a stopped Windows 11.
const vm = (name, displayName, state, template, memoryMiB, vcpus, cpuPercent, memoryBytes, mac) => ({ name, displayName, volume: 'Data', state, available: true, job: null, description: '', template, memoryMiB, vcpus, disks: [], iso: '', autostart: state === 'running', cpuPin: [], exclusive: false, folders: [], tpm: template === 'windows11', secureBoot: template === 'windows11', pci: [], usb: [], network: 'bridge', mac, cpuPercent, memoryBytes, since: state === 'running' ? new Date(Date.now() - 3 * 86400e3 - 5 * 3600e3).toISOString() : '' });
const vms = { vms: [vm('haos', 'Home Assistant OS', 'running', 'linux', 4096, 2, 6, 2.6e9, '52:54:00:4a:1e:07'), vm('win11', 'Windows 11', 'stopped', 'windows11', 8192, 4, 0, 0, '52:54:00:9c:22:31')], kvm: true, bridge: true, host: { cpus: 8, memoryBytes: 16e9, usedMemoryBytes: 3e9, availableMemoryBytes: 13e9, cpuModel: 'Test CPU' } };

async function setup(w, h, { nvr = false, init } = {}) {
  const made = await create('owner', 'graphit', w);
  await made.page.setViewportSize({ width: w, height: h });
  await made.ctx.addInitScript(() => localStorage.removeItem('titanos.layout'));
  if (init) await made.ctx.addInitScript(init);
  made.data.system.version = version;
  await made.ctx.route(/\/api\/v1\/files\/list/, (r) => r.fulfill({ json: files }));
  await made.ctx.route(/\/api\/v1\/vms(\?.*)?$/, (r) => (r.request().method() === 'GET' ? r.fulfill({ json: vms }) : r.fallback()));
  await made.ctx.route(/\/api\/v1\/vms\/haos\/addresses/, (r) => r.fulfill({ json: { addresses: ['192.168.1.40'], source: 'agent' } }));
  // More apps than the fixture's one, for the Docker window.
  const app = (name, image, state, port, cpu, mem, update = false) => ({ id: name, name: name + '-server-1', image, state, since: '2026-11-08T06:00:00Z', ports: port ? [{ host: port, container: port, protocol: 'tcp' }] : [], app: name, service: 'server', managed: true, cpuPercent: cpu, memoryBytes: mem, cpuHistory: [], webui: port ? String(port) : '', publicUrl: '', icon: '', updateAvailable: update });
  made.data.dockerContainers = [app('immich', 'ghcr.io/immich-app/immich-server:release', 'running', 2283, 6, 1.2e9, true), app('jellyfin', 'jellyfin/jellyfin:latest', 'running', 8096, 3, 640e6), app('paperless', 'ghcr.io/paperless-ngx/paperless-ngx:latest', 'running', 8000, 1, 410e6), app('home-assistant', 'ghcr.io/home-assistant/home-assistant:stable', 'running', 8123, 2, 520e6), app('vaultwarden', 'vaultwarden/server:latest', 'exited', 8085, 0, 0)];
  made.data.dockerApps = ['immich', 'jellyfin', 'paperless', 'home-assistant', 'vaultwarden'];
  // PLAN 79-7: the NVR with four sample cameras (test-ui-nvr-fixtures.mjs).
  if (nvr) await installNvr(made.ctx);
  return made;
}

// A settings page is longer than the window: grow the browser window until nothing is left to scroll,
// so the picture ends below the last row instead of cutting one in half.
async function fit(p) {
  const f = frameOf(p);
  const rest = await f.evaluate(() => {
    let most = 0;
    for (const el of [document.scrollingElement, ...document.querySelectorAll('*')]) {
      if (!el) continue;
      const style = getComputedStyle(el);
      if (el !== document.scrollingElement && !/(auto|scroll)/.test(style.overflowY)) continue;
      most = Math.max(most, el.scrollHeight - el.clientHeight);
    }
    return most;
  });
  if (rest > 0) {
    const size = p.viewportSize();
    await p.setViewportSize({ width: size.width, height: size.height + rest + 24 });
    await p.waitForTimeout(1200);
  }
}

async function shot(path, name, { w = 1600, h = 1000, max = true, act, nvr = false, init } = {}) {
  if (!want(name)) return;
  const made = await setup(w, h, { nvr, init });
  await made.page.goto(base + path);
  await made.page.waitForTimeout(4000);
  if (max && path !== '/') {
    const bar = made.page.locator('section.win header.titlebar').first();
    if (await bar.count()) { await bar.dblclick({ position: { x: 300, y: 10 } }); await made.page.waitForTimeout(800); }
  }
  if (act) await act(made.page);
  await made.page.screenshot({ path: out + name + '.png' });
  await made.ctx.close();
}

await shot('/', 'arbeitsplatz', { max: false });
await shot('/docker', 'docker', { act: async (p) => { await frameOf(p).locator('tr', { hasText: 'immich' }).first().click({ position: { x: 400, y: 20 } }); await p.waitForTimeout(1200); } });
await shot('/files?root=share/Documents', 'dateien', { act: async (p) => { const f = frameOf(p); await f.locator('text=Gehäuse Deckel.3mf').click(); await f.locator('text=Kfz-Versicherung 2026.pdf').click({ modifiers: ['Control'] }); await p.waitForTimeout(300); } });
await shot('/vms', 'vms', { act: async (p) => { await frameOf(p).getByText('Übersicht', { exact: true }).first().click(); await p.waitForTimeout(1200); } });
await shot('/nvr', 'nvr', { nvr: true, act: async (p) => { await frameOf(p).locator('button[aria-pressed]', { hasText: 'Ereignisse' }).click(); await p.waitForTimeout(800); } });
await shot('/shares', 'freigaben');
await shot('/storage', 'speicher');
await shot('/settings/appearance', 'darstellung', { act: fit });
await shot('/settings/systemdata', 'systemdaten', { act: fit });
await shot('/', 'handy-start', { w: 390, h: 844, max: false });
await shot('/settings', 'systemsteuerung', { act: fit });
await shot('/', 'hauptmenue', { max: false, act: async (p) => { await p.locator('.topbar [data-menu-button]').click(); await p.waitForTimeout(800); } });
await shot('/', 'dock-bearbeiten', { max: false, act: async (p) => {
  const d = await p.locator('.dock').first().boundingBox();
  await p.mouse.click(d.x + 4, d.y + d.height / 2, { button: 'right' });
  await p.getByRole('menuitem', { name: 'Dock bearbeiten' }).click();
  await p.getByRole('group', { name: 'Dock bearbeiten' }).waitFor();
  await p.waitForTimeout(500);
} });

// The remote view ("Fernansicht") of the Docker app immich: the stream is mocked like in
// scripts/test-ui-viewer.mjs – the browser encodes the photo /backgrounds/kueste.webp itself
// and a routed WebSocket sends those frames. No app runs; the picture is a stand-in.
if (want('fernansicht') || want('fernansicht-mobil')) {
  const W = 1280;
  const H = 800;
  const media = await (async () => {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    await page.route(base + '/encoder', (r) => r.fulfill({ contentType: 'text/html', body: '<!doctype html><title>enc</title>' }));
    await page.goto(base + '/encoder');
    const result = await page.evaluate(async ({ W, H }) => {
      const photo = await createImageBitmap(await (await fetch('/backgrounds/kueste.webp')).blob());
      const b64 = (u8) => { let s = ''; for (const b of u8) s += String.fromCharCode(b); return btoa(s); };
      let config = null;
      for (const codec of ['avc1.42E01F', 'vp8']) {
        const c = { codec, width: W, height: H, latencyMode: 'realtime' };
        if (codec.startsWith('avc')) c.avc = { format: 'annexb' };
        if ((await VideoEncoder.isConfigSupported(c)).supported && (await VideoDecoder.isConfigSupported({ codec })).supported) { config = c; break; }
      }
      if (!config) return null;
      const frames = [];
      const enc = new VideoEncoder({ output: (chunk) => { const a = new Uint8Array(chunk.byteLength); chunk.copyTo(a); frames.push({ key: chunk.type === 'key', ts: chunk.timestamp, data: b64(a) }); }, error: () => {} });
      enc.configure(config);
      const canvas = new OffscreenCanvas(W, H);
      const g = canvas.getContext('2d');
      for (let i = 0; i < 30; i++) {
        g.drawImage(photo, 0, 0, W, H);
        const f = new VideoFrame(canvas, { timestamp: i * 33333 });
        enc.encode(f, { keyFrame: i % 15 === 0 });
        f.close();
      }
      await enc.flush();
      return { codec: config.codec, frames };
    }, { W, H });
    await ctx.close();
    if (!result) throw new Error('this Chromium cannot encode VP8 or H.264');
    return result;
  })();
  const videoMsg = (f) => { const data = Buffer.from(f.data, 'base64'); const m = Buffer.alloc(10 + data.length); m[0] = 1; m[1] = f.key ? 1 : 0; m.writeBigUInt64BE(BigInt(f.ts), 2); data.copy(m, 10); return m; };
  const timers = new Set();
  const viewer = (page) => page.routeWebSocket(/\/api\/v1\/viewer\/[^/]+\/stream/, (ws) => {
    ws.send(JSON.stringify({ t: 'starting' }));
    ws.send(JSON.stringify({ t: 'hello', codec: media.codec, width: W, height: H, gpu: 'vaapi', audio: false }));
    let timer = null;
    const stream = () => { clearInterval(timer); let i = 0; timer = setInterval(() => { ws.send(videoMsg(media.frames[i % media.frames.length])); i++; }, 33); timers.add(timer); };
    let greeted = false;
    ws.onMessage((m) => {
      if (typeof m !== 'string') return;
      const msg = JSON.parse(m);
      if (msg.t === 'size' && !greeted) {
        greeted = true;
        ws.send(JSON.stringify({ t: 'hello', codec: media.codec, width: W, height: H, gpu: 'vaapi', audio: false }));
        stream();
        ws.send(JSON.stringify({ t: 'stats', fps: 30, kbps: 2400, latencyMs: 18 }));
      }
      if (msg.t === 'keyframe') stream();
    });
    ws.onClose(() => clearInterval(timer));
  });
  const painted = async (page, canvas) => page.waitForFunction((el) => {
    if (!el || el.width < 2 || getComputedStyle(el).visibility === 'hidden') return false;
    const [r, g, b, a] = el.getContext('2d').getImageData(el.width - 20, el.height - 20, 1, 1).data;
    return a > 0 && r + g + b > 0;
  }, await canvas.elementHandle(), { timeout: 15000 });
  if (want('fernansicht')) {
    const made = await setup(1600, 1000);
    viewer(made.page);
    await made.page.goto(base + '/');
    await made.page.waitForTimeout(3500);
    await made.page.locator('.ico', { hasText: 'immich' }).first().click();
    const win = made.page.locator('section.win', { hasText: 'immich' });
    await painted(made.page, win.locator('.remote canvas').first());
    await win.locator('header.titlebar').first().dblclick({ position: { x: 300, y: 10 } });
    await made.page.waitForTimeout(1500);
    await win.locator('.titlebar button[aria-label="Mehr"]').click();
    await made.page.waitForTimeout(800);
    await made.page.screenshot({ path: out + 'fernansicht.png' });
    await made.ctx.close();
  }
  if (want('fernansicht-mobil')) {
    const made = await setup(390, 844);
    viewer(made.page);
    await made.page.goto(base + '/');
    await made.page.waitForTimeout(3500);
    await made.page.locator('.home .app', { hasText: 'immich' }).first().click();
    await painted(made.page, made.page.locator('.remote-full .remote.phone canvas').first());
    await made.page.waitForTimeout(1000);
    await made.page.screenshot({ path: out + 'fernansicht-mobil.png' });
    await made.ctx.close();
  }
  for (const t of timers) clearInterval(t);
}

// The login page: logged out with four accounts.
if (want('anmeldeseite')) {
  const made = await setup(1600, 1000);
  await made.ctx.addInitScript(() => localStorage.setItem('titanos.login.accounts', JSON.stringify([{ name: 'patrick', displayName: 'Patrick' }])));
  await made.ctx.route('**/api/v1/session', (r) => r.fulfill({ json: { configAvailable: true, setupRequired: false, user: null, hostname: 'titan' } }));
  await made.ctx.route('**/api/v1/login/accounts', (r) => r.fulfill({ json: { style: 'circles', accounts: [{ name: 'alicia', displayName: 'Alicia', picture: { builtin: 'kueste' } }, { name: 'lena', displayName: 'Lena', picture: { builtin: 'wueste' } }, { name: 'max', displayName: 'Max', picture: { builtin: 'nebelwald' } }] } }));
  await made.page.goto(base + '/'); await made.page.waitForTimeout(3500);
  await made.page.screenshot({ path: out + 'anmeldeseite.png' });
  await made.ctx.close();
}
await browser.close();
