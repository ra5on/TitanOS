// Browser checks of Settings › Updates for app updates, against isolated API fixtures.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-updates.mjs
// UI_URL picks the dev server (default http://127.0.0.1:5179), UI_OUTPUT the screenshot folder.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.UI_URL || 'http://127.0.0.1:5179';
const out = process.env.UI_OUTPUT || 'out/validation/updates';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [];
const time = '2026-10-21T10:00:00Z';
const system = { hostname: 'titan-test', version: '0.8.0', kernel: 'test', uptimeSeconds: 1000, cpu: { model: 'Test CPU', threads: 8, usagePercent: 12 }, memory: { totalBytes: 16e9, usedBytes: 3e9, freeBytes: 13e9, availableBytes: 13e9 }, temperatures: [], accelerators: [], image: { slot: 'a', sizeBytes: 1e9, overlayUsedBytes: 0, overlayTotalBytes: 512e6 }, stick: { present: true, label: 'TITANOS' }, network: null, networks: [], networkPendingSeconds: 0, notificationsLatest: 0 };
const slots = [{ name: 'a', version: '0.8.0', running: true, complete: true, next: true, triesLeft: null }, { name: 'b', version: '0.7.2', running: false, complete: true, next: false, triesLeft: null }];
const manifest = { version: '0.8.1', channel: 'stable', kernel: '6.12.48', released: time, changelog: 'Kleinere Verbesserungen der VM-Seite.', files: [{ size: 300e6 }, { size: 40e6 }, { size: 10e6 }], app: { base: '0.8.0', size: 19e6 } };
let state;
let installs = 0, appRollbacks = 0, failRestart = 0;
const progress = (phase) => ({ phase, file: '', percent: phase === 'restart' ? 100 : 0, speedBytesPerSecond: 0, error: '' });
function reset() {
	state = { version: '0.8.0', release: true, slots, settings: { channel: 'stable', autoInstall: false, installTime: '03:30' }, canVerify: true, available: manifest, lastCheck: time, checkError: '', progress: progress(''), pending: null, canRollback: true, unconfirmed: false, base: '0.8.0', kind: 'app', app: { current: '', previous: '', trial: false, canRollback: false } };
}
reset();

async function context(theme = 'graphit', width = 1440) {
	const ctx = await browser.newContext({ locale: 'de-DE', viewport: { width, height: 1000 } });
	await ctx.addInitScript(({ theme }) => { localStorage.setItem('titanos.locale', 'de'); localStorage.setItem('titanos.appearance', JSON.stringify({ theme })); }, { theme });
	await ctx.route('**/api/v1/**', async (route) => {
		const path = new URL(route.request().url()).pathname.slice('/api/v1'.length);
		const method = route.request().method();
		const json = (data, status = 200) => route.fulfill({ status, json: data });
		if (path === '/session') return json({ configAvailable: true, setupRequired: false, user: { name: 'tester', role: 'owner', twoFactor: false }, hostname: 'titan-test', version: state.version });
		if (path === '/preferences') return json({ appearance: { theme }, dashboard: null, docker: null, vnc: null, sound: null });
		if (path === '/system') return json({ ...system, version: state.version });
		if (path === '/storage') return json({ volumes: [], disks: [], diskSleep: { idleMinutes: 0, available: true, errors: [] } });
		if (path === '/notifications') return json({ messages: [] });
		if (path === '/update' && method === 'GET') {
			// While titand restarts it does not answer.
			if (failRestart > 0) { failRestart--; return route.abort(); }
			return json(state);
		}
		if (path === '/update/install') {
			installs++;
			state = { ...state, progress: progress('restart') };
			// The next requests fail (restart), then the app answers.
			setTimeout(() => {
				failRestart = 1;
				state = { ...state, version: '0.8.1', available: null, kind: '', progress: progress(''), app: { current: '0.8.1', previous: '', trial: true, canRollback: false } };
			}, 300);
			return json(state);
		}
		if (path === '/update/app-rollback') {
			appRollbacks++;
			state = { ...state, progress: progress('restart') };
			setTimeout(() => { state = { ...state, version: '0.8.0', progress: progress(''), app: { current: '', previous: '0.8.1', trial: false, canRollback: true } }; }, 300);
			return json(state);
		}
		return json({});
	});
	const page = await ctx.newPage();
	page.on('pageerror', (e) => errors.push(String(e)));
	return { ctx, page };
}

try {
	{
		const { ctx, page } = await context();
		await page.goto(`${base}/settings/updates`);
		await page.getByText('TitanOS 0.8.1', { exact: true }).waitFor();
		assert.ok(await page.getByText('Ohne Neustart').isVisible());
		await page.screenshot({ path: `${out}/app-available-graphit-1440.png`, fullPage: true });
		await page.getByRole('button', { name: 'Jetzt aktualisieren' }).click();
		assert.ok(await page.getByText('startet danach nur seine Weboberfläche neu', { exact: false }).isVisible());
		assert.ok(await page.getByText('18,1 MB', { exact: false }).or(page.getByText('19 MB', { exact: false })).first().isVisible());
		await page.screenshot({ path: `${out}/app-confirm-graphit-1440.png`, fullPage: true });
		await page.locator('.dialog-actions .btn-primary').click();
		// The page waits for the new titand and says which version runs.
		await page.getByText('TitanOS 0.8.1 läuft. Das NAS wurde dafür nicht neu gestartet.').waitFor({ timeout: 15000 });
		assert.ok(await page.getByText('läuft auf Probe', { exact: false }).isVisible());
		assert.ok(await page.getByText('App-Update auf Systemabbild 0.8.0').isVisible());
		assert.equal(installs, 1);
		await page.screenshot({ path: `${out}/app-trial-graphit-1440.png`, fullPage: true });

		// Confirmed: the way back to the titand of the system image.
		state = { ...state, app: { current: '0.8.1', previous: '', trial: false, canRollback: true } };
		await page.reload();
		await page.getByRole('button', { name: 'Auf Systemabbild 0.8.0 zurück …' }).click();
		await page.locator('.dialog-actions .btn-primary').click();
		await page.getByText('TitanOS 0.8.0 läuft.', { exact: false }).waitFor({ timeout: 15000 });
		assert.equal(appRollbacks, 1);
		assert.ok(await page.getByRole('button', { name: 'Auf 0.8.1 zurück …' }).isVisible());
		await ctx.close();
	}
	{
		// A release for another system image is a system update, as before.
		reset();
		state = { ...state, kind: 'system', available: { ...manifest, version: '0.9.0', app: { base: '0.9.0', size: 19e6 } } };
		const { ctx, page } = await context('hell', 390);
		await page.goto(`${base}/settings/updates`);
		await page.getByText('TitanOS 0.9.0', { exact: true }).waitFor();
		assert.equal(await page.getByText('Ohne Neustart').count(), 0);
		await page.screenshot({ path: `${out}/system-available-hell-390.png`, fullPage: true });
		await page.getByRole('button', { name: 'Jetzt aktualisieren' }).click();
		assert.ok(await page.getByText('Aktiv wird die neue Version erst mit einem Neustart', { exact: false }).isVisible());
		const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
		assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
		await ctx.close();
	}
	assert.deepEqual(errors, []);
	console.log(`ok – screenshots in ${out}`);
} finally {
	await browser.close();
}
