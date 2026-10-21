// Browser checks of the VM page against isolated API fixtures. No VMs are started.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-vms.mjs
// UI_URL picks the dev server (default http://127.0.0.1:5179), UI_OUTPUT the screenshot folder.
// README_SCREENSHOTS=docs/screenshots/v0.7.2 also captures the README pictures there.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.UI_URL || 'http://127.0.0.1:5179';
const out = process.env.UI_OUTPUT || 'out/validation/vms';
await mkdir(out, { recursive: true });
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const errors = [];
const time = '2026-10-21T10:00:00Z';
const system = { hostname: 'titan-test', version: '0.7.1', kernel: 'test', uptimeSeconds: 1000, cpu: { model: 'Test CPU', threads: 8, usagePercent: 12 }, memory: { totalBytes: 32e9, usedBytes: 12e9, freeBytes: 20e9, availableBytes: 20e9 }, temperatures: [], accelerators: [], image: { slot: 'a', sizeBytes: 1e9, overlayUsedBytes: 0, overlayTotalBytes: 512e6 }, stick: { present: true, label: 'TITANOS' }, network: null, networks: [], networkPendingSeconds: 0, notificationsLatest: 0 };
const volumes = [
	{ name: 'tank', level: 'raid1', filesystem: 'ext4', mountPoint: '/mnt/tank', mounted: true, state: 'ok', sizeBytes: 4e12, usedBytes: 3.7e12, raidDisks: 2, members: [], sync: null, mismatches: 0 },
	{ name: 'fast', level: 'raid1', filesystem: 'xfs', mountPoint: '/mnt/fast', mounted: true, state: 'ok', sizeBytes: 1e12, usedBytes: 2e11, raidDisks: 2, members: [], sync: null, mismatches: 0 }
];
const common = { description: '', exclusive: false, folders: [], tpm: false, secureBoot: false, pci: [], usb: [], job: null, available: true, cpuPercent: 0, memoryBytes: 0, since: time, cpuPin: [], firmware: 'uefi' };
const vms = [
	{ ...common, name: 'home-assistant', displayName: 'Home Assistant', template: 'linux', memoryMiB: 4096, vcpus: 2, volume: 'fast', disks: [{ file: 'vdisk1.qcow2', format: 'qcow2', sizeBytes: 32 * 2 ** 30, volume: 'fast' }], iso: '', autostart: true, network: 'bridge', mac: '52:54:00:00:00:02', state: 'running', cpuPercent: 6, memoryBytes: 3.1 * 2 ** 30, folders: [{ path: '/mnt/tank/Medien', tag: 'medien', readOnly: true }] },
	{ ...common, name: 'debian', displayName: 'Debian Test', template: 'linux', memoryMiB: 2048, vcpus: 2, volume: 'fast', disks: [{ file: 'vdisk1.qcow2', format: 'qcow2', sizeBytes: 20 * 2 ** 30, volume: 'fast' }], iso: '', autostart: false, network: 'user', mac: '52:54:00:00:00:03', state: 'stopped' },
	{ ...common, name: 'windows-11', displayName: 'Windows 11', description: 'Spiele und Office', template: 'windows', memoryMiB: 8192, vcpus: 4, cpuPin: [2, 3, 4, 5], volume: 'tank', disks: [{ file: 'vdisk1.qcow2', format: 'qcow2', sizeBytes: 64 * 2 ** 30, volume: 'tank' }], iso: '/mnt/tank/isos/virtio-win.iso', autostart: true, tpm: true, secureBoot: true, pci: ['0000:01:00.0', '0000:01:00.1'], network: 'bridge', mac: '52:54:00:00:00:01', state: 'running', cpuPercent: 18, memoryBytes: 5 * 2 ** 30 }
];
const hardware = { hardware: { iommu: true, pci: [{ address: '0000:01:00.0', vendor: '10de', device: '2684', vendorName: 'NVIDIA', kind: 'gpu', driver: 'vfio-pci', iommuGroup: 12, primary: false, companions: ['0000:01:00.1'], passable: true, reason: '' }], usb: [] }, usedBy: {} };
const media = [{ path: '/mnt/tank/isos/debian-13.iso', sizeBytes: 700e6 }, { path: '/mnt/tank/isos/virtio-win.iso', sizeBytes: 600e6 }];
const requests = [];

async function context(theme = 'graphit', width = 1440, locale = 'de') {
	const ctx = await browser.newContext({ locale: locale === 'de' ? 'de-DE' : 'en-US', viewport: { width, height: 1000 } });
	await ctx.addInitScript(({ theme, locale }) => { localStorage.setItem('titanos.locale', locale); localStorage.setItem('titanos.appearance', JSON.stringify({ theme })); }, { theme, locale });
	await ctx.route('**/api/v1/**', async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname.slice('/api/v1'.length);
		const method = route.request().method();
		requests.push([method, path, route.request().postData()]);
		const json = (data, status = 200) => route.fulfill({ status, json: data });
		if (path === '/session') return json({ configAvailable: true, setupRequired: false, user: { name: 'tester', role: 'owner', twoFactor: false }, hostname: 'titan-test' });
		if (path === '/preferences') return json({ appearance: { theme }, dashboard: null, docker: null, vnc: null, sound: null });
		if (path === '/system') return json(system);
		if (path === '/storage') return json({ volumes, disks: [], diskSleep: { idleMinutes: 0, available: true, errors: [] } });
		if (path === '/notifications') return json({ messages: [] });
		if (path === '/vms') return json({ kvm: true, bridge: true, host: { cpus: 8, memoryBytes: 32e9, usedMemoryBytes: 12e9 }, vms });
		if (path === '/vms/media') return json({ media });
		if (path === '/hardware') return json(hardware);
		const medium = path.match(/^\/vms\/([^/]+)\/medium$/);
		if (medium && method === 'PUT') {
			const vm = vms.find((v) => v.name === medium[1]);
			vm.iso = route.request().postDataJSON().path;
			return route.fulfill({ status: 204 });
		}
		const addresses = path.match(/^\/vms\/([^/]+)\/addresses$/);
		if (addresses) return json(addresses[1] === 'windows-11' ? { addresses: ['192.168.1.42', 'fd00::42'], source: 'agent' } : { addresses: [], source: '' });
		if (/^\/vms\/[^/]+\/snapshots$/.test(path)) return json({ snapshots: path.includes('windows') ? [] : [{ id: 's1', name: 'Vor dem Update', created: time, memory: false, automatic: false, bytes: 3e9 }], schedule: { enabled: !path.includes('windows'), time: '03:00', days: [0, 1, 2, 3, 4, 5, 6], keep: 7, memory: false, allowPause: false }, pending: false, reason: path.includes('windows') ? 'passthrough' : '', job: null });
		if (/^\/vms\/[^/]+\/(start|shutdown|reboot)$/.test(path)) return route.fulfill({ status: 204 });
		return json({});
	});
	const page = await ctx.newPage();
	page.on('pageerror', (e) => errors.push(String(e)));
	return { ctx, page };
}

async function shot(page, name) {
	await page.evaluate(() => document.fonts.ready);
	await page.screenshot({ path: `${out}/${name}.png`, fullPage: true });
}

try {
	{
		const { ctx, page } = await context();
		await page.goto(`${base}/vms`);
		await page.locator('#vm-title').waitFor();
		// The first VM runs: its screen is the first tab shown.
		assert.equal(await page.locator('#vm-title').textContent(), 'Home Assistant');
		assert.equal(await page.locator('.tabs a[aria-current="page"]').textContent(), 'Bildschirm');
		await page.getByRole('button', { name: 'Senden' }).click();
		assert.ok(await page.getByRole('menuitem', { name: 'Strg+Alt+Entf' }).isVisible());
		await page.keyboard.press('Escape');
		await shot(page, 'screen-graphit-1440');

		// Filter: only the VMs that are off.
		await page.getByRole('button', { name: /^Aus/ }).click();
		assert.equal(await page.locator('.vm').count(), 1);
		await page.getByRole('button', { name: /^Alle/ }).click();
		assert.equal(await page.locator('.vm').count(), 3);

		// Windows: overview by link, notices folded, address from the agent.
		await page.goto(`${base}/vms?vm=windows-11&tab=overview`);
		await page.getByText('192.168.1.42').waitFor();
		assert.ok(await page.getByText('laut Gastagent').isVisible());
		assert.ok(await page.getByText('NVIDIA', { exact: false }).first().isVisible());
		assert.ok(await page.getByText('Mit durchgereichter Grafikkarte', { exact: false }).isVisible());
		assert.ok(await page.getByText('Wenig Platz auf tank', { exact: false }).isVisible());
		assert.ok(await page.getByText('virtio-win.iso').isVisible());
		await shot(page, 'overview-graphit-1440');

		// Eject, then insert another medium.
		await page.getByRole('button', { name: 'Auswerfen' }).click();
		await page.getByText('CD-Laufwerk leer').waitFor();
		assert.deepEqual(JSON.parse(requests.findLast(([m, p]) => m === 'PUT' && p === '/vms/windows-11/medium')[2]), { path: '' });
		await page.getByRole('button', { name: 'Einlegen …' }).click();
		await page.locator('select').selectOption('/mnt/tank/isos/debian-13.iso');
		await shot(page, 'medium-dialog-graphit-1440');
		await page.getByRole('button', { name: 'Einlegen', exact: true }).click();
		await page.getByText('debian-13.iso').waitFor();

		// The row menu of a VM in the list.
		await page.getByRole('button', { name: 'Aktionen für Debian Test' }).click();
		assert.ok(await page.getByRole('menu').isVisible());
		assert.ok(await page.getByRole('menuitem', { name: 'Starten' }).isVisible());
		await page.keyboard.press('Escape');

		// Header menu: removing is only offered while the VM is off.
		await page.locator('.head .dropdown button').click();
		assert.equal(await page.getByRole('menuitem', { name: /Entfernen/ }).count(), 0);
		await page.keyboard.press('Escape');

		// Snapshots as a tab.
		await page.goto(`${base}/vms?vm=debian&tab=snapshots`);
		await page.getByText('Vor dem Update').waitFor();
		assert.ok(await page.getByRole('button', { name: 'Snapshot erstellen' }).isEnabled());
		await shot(page, 'snapshots-graphit-1440');

		// A VM that is off opens on its overview.
		await page.goto(`${base}/vms?vm=debian`);
		assert.equal(await page.locator('.tabs a[aria-current="page"]').textContent(), 'Übersicht');
		await page.getByText('Die VM ist ausgeschaltet.').waitFor();
		await ctx.close();
	}
	{
		const { ctx, page } = await context('hell');
		await page.goto(`${base}/vms?vm=windows-11&tab=overview`);
		await page.getByText('192.168.1.42').waitFor();
		await shot(page, 'overview-hell-1440');
		await ctx.close();
	}
	{
		const { ctx, page } = await context('graphit', 390);
		await page.goto(`${base}/vms?vm=windows-11&tab=overview`);
		await page.getByText('192.168.1.42').waitFor();
		// The list is folded behind the name of the VM.
		assert.equal(await page.locator('.vm').first().isVisible(), false);
		await shot(page, 'overview-graphit-390');
		await page.locator('.picker').click();
		assert.ok(await page.locator('.vm').first().isVisible());
		await shot(page, 'list-open-graphit-390');
		const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
		assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
		await ctx.close();
	}
	{
		const { ctx, page } = await context('graphit', 1440, 'en');
		await page.goto(`${base}/vms?vm=windows-11&tab=overview`);
		await page.getByText('reported by the guest agent').waitFor();
		await ctx.close();
	}
	if (process.env.README_SCREENSHOTS) {
		const folder = process.env.README_SCREENSHOTS;
		system.version = '0.7.2';
		await mkdir(folder, { recursive: true });
		const fonts = (page) => page.evaluate(async () => {
			for (const font of ['400 14px "IBM Plex Sans"', '600 14px "IBM Plex Sans"', '400 14px "IBM Plex Mono"']) await document.fonts.load(font);
			await document.fonts.ready;
		});
		for (const [theme, width, file] of [['graphit', 1440, 'vms.png'], ['hell', 390, 'vms-mobil.png']]) {
			const { ctx, page } = await context(theme, width);
			await page.goto(`${base}/vms?vm=windows-11&tab=overview`);
			await page.getByText('192.168.1.42').waitFor();
			await fonts(page);
			await page.screenshot({ path: `${folder}/${file}`, fullPage: true });
			await ctx.close();
		}
		const { ctx, page } = await context('graphit', 1440);
		await page.goto(`${base}/vms?vm=home-assistant&tab=screen`);
		await page.getByRole('button', { name: 'Senden' }).click();
		await fonts(page);
		await page.screenshot({ path: `${folder}/vms-bildschirm.png`, clip: { x: 300, y: 110, width: 1140, height: 600 } });
		await ctx.close();
	}
	assert.deepEqual(errors, []);
	console.log(`ok – screenshots in ${out}`);
} finally {
	await browser.close();
}
