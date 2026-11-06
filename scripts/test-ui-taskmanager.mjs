// Task manager (PLAN 61, Monitor app) against isolated fixtures: the tabs
// for owners, the overview only for users, process search/sort, ending a
// process with a simple confirmation (dangerous button left of Cancel, the
// start time is sent), protected processes without buttons, sleeping
// disks shown as asleep, polling every two seconds only while visible, no
// sideways scrolling at 1440, 800 (window width) and 390 px.
// Screenshots: out/validation/taskmanager.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5231 node scripts/test-ui-taskmanager.mjs
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || 'http://127.0.0.1:5173';
const out = process.env.UI_OUTPUT || 'out/validation/taskmanager';
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const proc = (pid, name, cpu, mem, extra = {}) => ({
	pid,
	ppid: 1,
	name,
	command: `/usr/bin/${name}`,
	user: 'root',
	state: 'S',
	cpuPercent: cpu,
	memoryBytes: mem,
	start: pid * 100,
	owner: { kind: '' },
	...extra,
});
const processes = [
	proc(1, 'systemd', 0.1, 12e6, { command: '/sbin/init splash', protected: true }),
	proc(812, 'titand', 1.2, 80e6, { protected: true }),
	proc(1500, 'smbd', 0.4, 30e6, { owner: { kind: 'service', name: 'smbd' } }),
	proc(2301, 'immich', 23.5, 1.4e9, {
		command: '/usr/src/app/immich --port 2283 --workers 4 --media /data/photos/library/originals/2026',
		owner: { kind: 'docker', id: 'a'.repeat(64), name: 'immich_server' },
	}),
	proc(3120, 'qemu-system-x86', 41.0, 4.2e9, { user: 'libvirt-qemu', owner: { kind: 'vm', id: '3', name: 'win-11' } }),
	proc(4100, 'pihole-FTL', 0.8, 60e6, { owner: { kind: 'lxc', name: 'pihole' } }),
	proc(5200, 'bash', 0, 4e6, { user: 'alex' }),
];
const threadUsage = Array.from({ length: 8 }, (_, i) => ({ id: `cpu${i}`, usagePercent: [12, 40, 5, 77, 3, 20, 9, 55][i] }));

async function open(role, width, height = 1000) {
	const made = await context(role, 'graphit', width);
	const { ctx, data } = made;
	data.signals = [];
	data.processCalls = 0;
	data.systemCalls = 0;
	data.guestCalls = 0;
	await ctx.route('**/api/v1/system', (route) => {
		data.systemCalls++;
		route.fulfill({
			json: {
				...data.system,
				cpu: { model: 'Test CPU', threads: 8, usagePercent: 10 + (data.systemCalls % 5) * 7, threadUsage },
				memory: { totalBytes: 16e9, usedBytes: 6e9, freeBytes: 2e9, availableBytes: 9e9 },
				network: { interface: 'enp3s0', state: 'up', speedMbps: 2500, rxBytesPerSecond: 3.2e6, txBytesPerSecond: 0.4e6 },
				networks: [
					{ interface: 'enp3s0', state: 'up', speedMbps: 2500, rxBytesPerSecond: 3.2e6 + data.systemCalls * 1e5, txBytesPerSecond: 0.4e6, ipv4Addresses: [], ipv6Addresses: [] },
					{ interface: 'br0', state: 'up', speedMbps: 0, rxBytesPerSecond: 1e3, txBytesPerSecond: 2e3, ipv4Addresses: [], ipv6Addresses: [] },
					{ interface: 'enp4s0', state: 'down', speedMbps: 0, rxBytesPerSecond: 0, txBytesPerSecond: 0, ipv4Addresses: [], ipv6Addresses: [] },
				],
				accelerators: [{ address: '0000:00:02.0', name: 'Intel UHD 770', vendor: 'intel', kind: 'gpu', driver: 'i915', state: 'ok', usagePercent: 14 }],
				disks: [
					{ name: 'sda', readBytesPerSecond: 12e6, writeBytesPerSecond: 3e6, busyPercent: 18 },
					{ name: 'sdb', readBytesPerSecond: 11e6, writeBytesPerSecond: 3e6, busyPercent: 17 },
					{ name: 'sdc', readBytesPerSecond: 0, writeBytesPerSecond: 0, busyPercent: 0 },
					{ name: 'nvme0n1', readBytesPerSecond: 80e6, writeBytesPerSecond: 20e6, busyPercent: 30 },
				],
			},
		});
	});
	const disk = (name, model, volume, temperatureC) => ({
		name,
		path: `/dev/${name}`,
		model,
		serial: name.toUpperCase(),
		sizeBytes: 8e12,
		kind: name.startsWith('nvme') ? 'nvme' : 'hdd',
		role: 'member',
		volume,
		smart: { healthy: true, temperatureC, powerOnHours: 1000, errors: 0, rotationRate: 7200, wearPercent: 0 },
	});
	const volume = (name, members) => ({ name, level: 'raid1', filesystem: 'ext4', mountPoint: `/mnt/${name}`, mounted: true, state: 'ok', sizeBytes: 8e12, usedBytes: 2e12, raidDisks: members.length, members: members.map((disk) => ({ disk, state: 'active' })), sync: null, mismatches: 0, shareQuotas: 'off' });
	await ctx.route('**/api/v1/storage', (route) =>
		route.fulfill({
			json: {
				volumes: [volume('Daten', ['sda', 'sdb']), volume('Archiv', ['sdc']), volume('Schnell', ['nvme0n1p1'])],
				disks: [disk('sda', 'WD Red Plus 8TB', 'Daten', 34), disk('sdb', 'WD Red Plus 8TB', 'Daten', 35), disk('sdc', 'Seagate IronWolf 12TB', 'Archiv', 29), disk('nvme0n1', 'Samsung 990 Pro', 'Schnell', 41)],
				diskSleep: {
					idleMinutes: 30,
					available: true,
					errors: [],
					listed: true,
					diskStatus: [
						{ id: 'a', name: 'sda', model: '', serial: '', sizeBytes: 8e12, override: null, effective: 30, origin: 'default', state: 'active' },
						{ id: 'b', name: 'sdb', model: '', serial: '', sizeBytes: 8e12, override: null, effective: 30, origin: 'default', state: 'active' },
						{ id: 'c', name: 'sdc', model: '', serial: '', sizeBytes: 12e12, override: null, effective: 30, origin: 'default', state: 'asleep' },
					],
				},
			},
		}),
	);
	await ctx.route('**/api/v1/system/processes', (route) => {
		data.processCalls++;
		route.fulfill({ json: { processes, cpus: 8, memoryTotalBytes: 16e9, sampledSeconds: 2 } });
	});
	await ctx.route('**/api/v1/system/processes/*/signal', (route) => {
		data.signals.push([route.request().url().split('/').at(-2), route.request().postDataJSON()]);
		route.fulfill({ status: 204 });
	});
	await ctx.route('**/api/v1/docker', (route) =>
		(data.guestCalls++, route).fulfill({
			json: {
				settings: { enabled: true, volume: 'Daten' },
				running: true,
				version: 'test',
				apps: [],
				jobs: {},
				updatesChecked: '',
				containers: [
					{ id: 'a'.repeat(64), name: 'immich_server', image: 'ghcr.io/immich-app/immich-server:release', state: 'running', cpuPercent: 23.5, memoryBytes: 1.4e9, ports: [], app: 'immich', service: 'server', managed: true, cpuHistory: [], webui: '', publicUrl: '', icon: '', updateAvailable: false },
					{ id: 'b'.repeat(64), name: 'paperless', image: 'paperlessngx/paperless-ngx:latest', state: 'exited', cpuPercent: 0, memoryBytes: 0, ports: [], app: 'paperless', service: 'web', managed: true, cpuHistory: [], webui: '', publicUrl: '', icon: '', updateAvailable: false },
				],
			},
		}),
	);
	await ctx.route('**/api/v1/docker/containers/*/*', (route) => {
		(data.containerCalls ??= []).push(route.request().url().split('/').slice(-2).join('/'));
		route.fulfill({ status: 204 });
	});
	await ctx.route('**/api/v1/vms', (route) =>
		(data.guestCalls++, route).fulfill({
			json: {
				kvm: true,
				bridge: true,
				host: { cpus: 8, memoryBytes: 16e9 },
				vms: [
					{ name: 'win-11', displayName: 'Windows 11', state: 'running', available: true, cpuPercent: 41, memoryBytes: 4.2e9 },
					{ name: 'ubuntu', state: 'stopped', available: true, cpuPercent: 0, memoryBytes: 0 },
				],
			},
		}),
	);
	await ctx.route('**/api/v1/lxc', (route) =>
		(data.guestCalls++, route).fulfill({
			json: {
				installed: true,
				dev: false,
				host: { cpus: 8, memoryBytes: 16e9 },
				bridge: true,
				networks: [],
				containers: [{ name: 'pihole', state: 'running', available: true, cpuPercent: 0.8, memoryBytes: 60e6, volume: 'Daten', distribution: 'debian', release: 'trixie', arch: 'amd64', network: 'bridge', mac: '', autostart: true, created: '' }],
			},
		}),
	);
	if (width < 761) await made.page.setViewportSize({ width, height: 844 });
	else await made.page.setViewportSize({ width, height });
	await made.page.goto(base + '/tools/taskmanager');
	await made.page.getByRole('heading', { name: 'Taskmanager', level: 1 }).waitFor();
	return made;
}

const settle = (page) =>
	page.waitForTimeout(400).then(() => page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => {})))));
const noSideScroll = (page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1);
// No two visible text boxes inside the page cover each other.
const overlaps = (page) =>
	page.evaluate(() => {
		const boxes = [...document.querySelectorAll('.taskmanager :is(.name, .cmd, dd, dt, .state, .num, .io, button, .chip, .mono)')]
			.filter((el) => el.offsetParent && el.getClientRects().length)
			.map((el) => ({ el, r: el.getBoundingClientRect() }))
			.filter(({ r }) => r.width > 0 && r.height > 0);
		const found = [];
		for (let i = 0; i < boxes.length; i++)
			for (let j = i + 1; j < boxes.length; j++) {
				const a = boxes[i], b = boxes[j];
				if (a.el.contains(b.el) || b.el.contains(a.el)) continue;
				const x = Math.min(a.r.right, b.r.right) - Math.max(a.r.left, b.r.left);
				const y = Math.min(a.r.bottom, b.r.bottom) - Math.max(a.r.top, b.r.top);
				if (x > 2 && y > 2) found.push(`${a.el.textContent.trim().slice(0, 30)} | ${b.el.textContent.trim().slice(0, 30)}`);
			}
		return found;
	});
const tabNames = ['Übersicht', 'Prozesse', 'Container', 'VMs und Systemcontainer', 'Netzwerk', 'Platten'];
const files = ['uebersicht', 'prozesse', 'container', 'vms', 'netzwerk', 'platten'];

let lastPage;
try {
	{
		// Owner at 1440: every tab, values from the fixtures.
		const { ctx, page, data } = await open('owner', 1440);
		lastPage = page;
		const tabs = page.getByRole('tablist', { name: 'Bereiche des Taskmanagers' });
		assert.deepEqual(await tabs.getByRole('tab').allInnerTexts(), tabNames);
		const cpu = page.getByRole('region', { name: 'CPU' });
		await cpu.getByText('Test CPU').waitFor();
		assert.equal(await page.getByRole('list', { name: 'Auslastung je Kern in Prozent' }).getByRole('listitem').count(), 8);
		const volumes = page.getByRole('region', { name: 'Volumes' });
		await volumes.getByText('Daten').waitFor();
		assert.ok((await volumes.innerText()).includes('Lesen 23 MB/s'), await volumes.innerText());
		await page.getByRole('region', { name: 'Grafik und Beschleuniger' }).getByText('Intel UHD 770').waitFor();
		// Polling every two seconds, nothing about processes on this tab.
		const before = data.systemCalls;
		await page.waitForTimeout(4500);
		assert.ok(data.systemCalls - before >= 2, `polled ${data.systemCalls - before}`);
		assert.equal(data.processCalls, 0);
		await settle(page);
		await page.screenshot({ path: `${out}/uebersicht-1440.png`, fullPage: true });

		// Processes: search, sort, protected rows, ending one.
		await tabs.getByRole('tab', { name: 'Prozesse' }).click();
		const list = page.getByRole('list', { name: 'Prozesse' });
		await list.getByText('qemu-system-x86', { exact: true }).waitFor();
		const names = () => list.locator('.proc-main .name').allInnerTexts();
		assert.deepEqual((await names()).slice(0, 2), ['qemu-system-x86', 'immich']);
		await page.getByRole('radio', { name: 'Name' }).click();
		assert.equal((await names())[0], 'bash');
		await page.getByRole('radio', { name: 'RAM' }).click();
		assert.equal((await names())[0], 'qemu-system-x86');
		await page.getByRole('searchbox', { name: 'Prozesse suchen' }).fill('immich_server');
		assert.deepEqual(await names(), ['immich']);
		await list.getByText('Container immich_server').waitFor();
		await page.getByRole('searchbox', { name: 'Prozesse suchen' }).fill('');
		const init = list.locator('li', { hasText: '/sbin/init' });
		await init.getByText('geschützt').waitFor();
		assert.equal(await init.getByRole('button').count(), 0, 'no buttons on a protected process');
		await page.getByRole('radio', { name: 'CPU' }).click();
		assert.ok(await noSideScroll(page));
		assert.deepEqual(await overlaps(page), []);
		await settle(page);
		await page.screenshot({ path: `${out}/prozesse-1440.png`, fullPage: true });
		await page.getByRole('button', { name: 'bash (PID 5200) sofort beenden' }).click();
		const dialog = page.getByRole('dialog', { name: 'Prozess sofort beenden?' });
		await dialog.getByText('SIGKILL').waitFor();
		const danger = dialog.getByRole('button', { name: 'Erzwingen' });
		const cancel = dialog.getByRole('button', { name: 'Abbrechen' });
		assert.ok((await danger.boundingBox()).x < (await cancel.boundingBox()).x, 'dangerous button left of Cancel');
		assert.equal(await dialog.locator('input').count(), 0, 'nothing to type');
		await settle(page);
		await page.screenshot({ path: `${out}/prozess-beenden-1440.png` });
		await cancel.click();
		assert.deepEqual(data.signals, []);
		await page.getByRole('button', { name: 'bash (PID 5200) beenden', exact: true }).click();
		await page.getByRole('dialog', { name: 'Prozess beenden?' }).getByRole('button', { name: 'Beenden' }).click();
		await page.getByRole('dialog').waitFor({ state: 'detached' });
		assert.deepEqual(data.signals, [['5200', { signal: 'term', start: 520000 }]]);

		// Containers, guests, network, disks.
		for (const [i, name] of tabNames.entries()) {
			if (i < 2) continue;
			await tabs.getByRole('tab', { name, exact: true }).click();
			await page.waitForLoadState('networkidle');
			await page.waitForTimeout(300);
			assert.ok(await noSideScroll(page), name);
			assert.deepEqual(await overlaps(page), [], name);
			await settle(page);
			await page.screenshot({ path: `${out}/${files[i]}-1440.png`, fullPage: true });
		}
		const disks = page.locator('.taskmanager');
		const sleeping = disks.locator('li', { hasText: 'sdc' });
		await sleeping.getByText('schläft').waitFor();
		assert.ok(!(await sleeping.innerText()).includes('MB/s'), 'no activity for a sleeping disk');
		await disks.locator('li', { hasText: 'sda' }).getByText('34 °C').waitFor();
		await tabs.getByRole('tab', { name: 'Container', exact: true }).click();
		await page.locator('li', { hasText: 'immich_server' }).getByRole('button', { name: 'Stoppen' }).click();
		const stop = page.getByRole('dialog', { name: 'Stoppen?' });
		await stop.getByRole('button', { name: 'Stoppen' }).click();
		await stop.waitFor({ state: 'detached' });
		assert.deepEqual(data.containerCalls, [`${'a'.repeat(64)}/stop`]);

		// Hidden page: no more requests until it is visible again.
		await tabs.getByRole('tab', { name: 'Übersicht' }).click();
		await page.waitForTimeout(500);
		await page.evaluate(() => {
			Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
			Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
			document.dispatchEvent(new Event('visibilitychange'));
		});
		await page.waitForTimeout(500);
		const hidden = data.systemCalls;
		await page.waitForTimeout(4500);
		assert.equal(data.systemCalls, hidden, 'no polling while hidden');
		await page.evaluate(() => {
			Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
			Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' });
			document.dispatchEvent(new Event('visibilitychange'));
		});
		await page.waitForTimeout(500);
		assert.ok(data.systemCalls > hidden, 'polling again when visible');
		await ctx.close();
	}
	{
		// 800 px: the width of a desktop window.
		const { ctx, page } = await open('owner', 800, 900);
		lastPage = page;
		const tabs = page.getByRole('tablist', { name: 'Bereiche des Taskmanagers' });
		for (const [i, name] of tabNames.entries()) {
			await tabs.getByRole('tab', { name, exact: true }).click();
			await page.waitForLoadState('networkidle');
			await page.waitForTimeout(300);
			assert.ok(await noSideScroll(page), `800: ${name}`);
			assert.deepEqual(await overlaps(page), [], `800: ${name}`);
			if (i < 2 || i === 5) {
				await settle(page);
				await page.screenshot({ path: `${out}/${files[i]}-800.png`, fullPage: true });
			}
		}
		await ctx.close();
	}
	{
		// Phone.
		const { ctx, page } = await open('owner', 390);
		lastPage = page;
		const tabs = page.getByRole('tablist', { name: 'Bereiche des Taskmanagers' });
		for (const [i, name] of tabNames.entries()) {
			await tabs.getByRole('tab', { name, exact: true }).click();
			await page.waitForLoadState('networkidle');
			await page.waitForTimeout(300);
			assert.ok(await noSideScroll(page), `390: ${name}`);
			assert.deepEqual(await overlaps(page), [], `390: ${name}`);
			await settle(page);
			await page.screenshot({ path: `${out}/${files[i]}-390.png`, fullPage: true });
		}
		await ctx.close();
	}
	{
		// Users: the overview only, read only, no process list.
		const { ctx, page, data } = await open('user', 1440);
		lastPage = page;
		await page.getByRole('region', { name: 'CPU' }).getByText('Test CPU').waitFor();
		await page.getByText('Live-Werte des NAS, nur zum Ansehen.').waitFor();
		assert.equal(await page.getByRole('tablist').count(), 0);
		assert.equal(await page.getByRole('navigation', { name: 'Werkzeugbereiche' }).count(), 0);
		await page.waitForTimeout(2500);
		assert.equal(data.processCalls, 0);
		assert.equal(data.guestCalls, 0);
		await settle(page);
		await page.screenshot({ path: `${out}/benutzer-1440.png`, fullPage: true });
		await page.setViewportSize({ width: 390, height: 844 });
		await page.waitForTimeout(300);
		assert.ok(await noSideScroll(page));
		await page.screenshot({ path: `${out}/benutzer-390.png`, fullPage: true });
		await ctx.close();
	}
	assert.deepEqual(errors, []);
	console.log('task manager UI ok');
} catch (err) {
	console.error(errors.join('\n').slice(0, 3000));
	await lastPage?.screenshot({ path: `${out}/fehler.png` }).catch(() => {});
	throw err;
} finally {
	await browser.close();
}
