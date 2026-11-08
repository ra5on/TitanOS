// README pictures of the current UI with isolated demonstration data (docs/screenshots/README.md).
// PW=/abs/playwright/index.mjs UI_URL=http://127.0.0.1:5290 node scripts/capture-readme-screenshots.mjs
import { fixtureFactory } from './test-ui-fixtures.mjs';
import { mkdir } from 'node:fs/promises';
const { chromium } = await import(process.env.PW || 'playwright');
await mkdir(process.env.OUT || 'out/readme-shots', { recursive: true });
const browser = await chromium.launch({ args: ['--use-gl=angle', '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'] });
const create = fixtureFactory(browser, []);
const out = (process.env.OUT || 'out/readme-shots') + '/';
const names = [['Angebote', true], ['Rechnungen 2026', true], ['Verträge', true], ['Urlaub Garda', true], ['Gehäuse Deckel.3mf', false, 2.4e6], ['Angebot Gartenhaus.docx', false, 24e3], ['Halterung.stl', false, 820e3], ['Kfz-Versicherung 2026.pdf', false, 210e3], ['Strom Abschlag.xlsx', false, 38e3], ['Notiz.txt', false, 1e3]];
const files = names.map(([n, d, s]) => ({ name: n, dir: d, size: s ?? 0, modified: '2026-11-0' + (1 + (n.length % 7)) + 'T10:00:00Z' }));
async function shot(path, name, { w = 1600, h = 1000, max = true, act } = {}) {
  const made = await create('owner', 'graphit', w);
  await made.page.setViewportSize({ width: w, height: h });
  await made.ctx.addInitScript(() => localStorage.removeItem('titanos.layout'));
  await made.ctx.route(/\/api\/v1\/files\/list/, (r) => r.fulfill({ json: files }));
  // More apps than the fixture's one, for the Docker window.
  const app = (name, image, state, port, cpu, mem, update = false) => ({ id: name, name: name + '-server-1', image, state, since: '2026-11-08T06:00:00Z', ports: port ? [{ host: port, container: port, protocol: 'tcp' }] : [], app: name, service: 'server', managed: true, cpuPercent: cpu, memoryBytes: mem, cpuHistory: [], webui: port ? String(port) : '', publicUrl: '', icon: '', updateAvailable: update });
  made.data.dockerContainers = [app('immich', 'ghcr.io/immich-app/immich-server:release', 'running', 2283, 6, 1.2e9, true), app('jellyfin', 'jellyfin/jellyfin:latest', 'running', 8096, 3, 640e6), app('paperless', 'ghcr.io/paperless-ngx/paperless-ngx:latest', 'running', 8000, 1, 410e6), app('home-assistant', 'ghcr.io/home-assistant/home-assistant:stable', 'running', 8123, 2, 520e6), app('vaultwarden', 'vaultwarden/server:latest', 'exited', 8085, 0, 0)];
  made.data.dockerApps = ['immich', 'jellyfin', 'paperless', 'home-assistant', 'vaultwarden'];
  await made.page.goto((process.env.UI_URL || 'http://127.0.0.1:5290') + path);
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
await shot('/docker', 'docker');
await shot('/files?root=share/Documents', 'dateien', { act: async (p) => { const f = p.frames().find((x) => x !== p.mainFrame()); await f.locator('text=Gehäuse Deckel.3mf').click(); await f.locator('text=Kfz-Versicherung 2026.pdf').click({ modifiers: ['Control'] }); await p.waitForTimeout(300); } });
await shot('/vms', 'vms');
await shot('/shares', 'freigaben');
await shot('/storage', 'speicher');
await shot('/settings/appearance', 'darstellung');
await shot('/', 'handy-start', { w: 390, h: 844, max: false });
// The login page: logged out with four accounts.
{
  const made = await create('owner', 'graphit', 1600);
  await made.page.setViewportSize({ width: 1600, height: 1000 });
  await made.ctx.addInitScript(() => localStorage.setItem('titanos.login.accounts', JSON.stringify([{ name: 'patrick', displayName: 'Patrick' }])));
  await made.ctx.route('**/api/v1/session', (r) => r.fulfill({ json: { configAvailable: true, setupRequired: false, user: null, hostname: 'titan' } }));
  await made.ctx.route('**/api/v1/login/accounts', (r) => r.fulfill({ json: { style: 'circles', accounts: [{ name: 'alicia', displayName: 'Alicia', picture: { builtin: 'kueste' } }, { name: 'lena', displayName: 'Lena', picture: { builtin: 'wueste' } }, { name: 'max', displayName: 'Max', picture: { builtin: 'nebelwald' } }] } }));
  await made.page.goto((process.env.UI_URL || 'http://127.0.0.1:5290') + '/'); await made.page.waitForTimeout(3500);
  await made.page.screenshot({ path: out + 'anmeldeseite.png' });
  await made.ctx.close();
}
await browser.close();
