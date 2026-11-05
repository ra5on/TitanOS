// Photos in the file manager (PLAN 52): timeline, viewer with the AI
// description, album, search, map (off and on), photo folders, phone.
// Isolated API data only; thumbnails and map tiles are painted squares, no
// request leaves the browser.
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { deflateSync } from 'node:zlib';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || 'http://127.0.0.1:5195';
const out = process.env.UI_OUTPUT || 'out/validation/photos';
await mkdir(out, { recursive: true });
const errors = [];
const create = fixtureFactory(browser, errors);

// A plain PNG of one colour (CRC over chunk type and data).
function crc32(buf) {
	let c, crc = 0xffffffff;
	for (let n = 0; n < buf.length; n++) {
		c = (crc ^ buf[n]) & 0xff;
		for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
		crc = (crc >>> 8) ^ c;
	}
	return (crc ^ 0xffffffff) >>> 0;
}
function png(r, g, b, w = 16, h = 12) {
	const chunk = (type, data) => {
		const len = Buffer.alloc(4);
		len.writeUInt32BE(data.length);
		const td = Buffer.concat([Buffer.from(type), data]);
		const crc = Buffer.alloc(4);
		crc.writeUInt32BE(crc32(td));
		return Buffer.concat([len, td, crc]);
	};
	const ihdr = Buffer.alloc(13);
	ihdr.writeUInt32BE(w, 0);
	ihdr.writeUInt32BE(h, 4);
	ihdr[8] = 8;
	ihdr[9] = 2;
	const rows = [];
	for (let y = 0; y < h; y++) {
		rows.push(0);
		for (let x = 0; x < w; x++) rows.push(r, g, b);
	}
	return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(Buffer.from(rows))), chunk('IEND', Buffer.alloc(0))]);
}
const colors = [[110, 170, 220], [230, 200, 140], [80, 120, 90], [200, 110, 80], [150, 130, 200], [60, 70, 100]];

const item = (i, extra = {}) => {
	const month = i < 5 ? 10 : i < 11 ? 9 : 11;
	const year = i < 11 ? 2026 : 2025;
	return {
		root: 'share/Media',
		path: `Fotos/${year}/IMG_${4800 + i}.jpg`,
		kind: 'image',
		size: 3_200_000,
		modified: new Date(Date.UTC(year, month - 1, 20 - i)).toISOString(),
		taken: new Date(Date.UTC(year, month - 1, 20 - i, 15, 42)).toISOString(),
		...extra,
	};
};
const items = [
	item(0, { desc: 'Ein brauner Hund läuft über einen Sandstrand, im Hintergrund Wellen.\nStichwörter: Hund, Strand, Meer', gps: true, lat: 55.5578, lon: 8.0833, camera: 'iPhone 15' }),
	item(1, { desc: 'Dünen und blauer Himmel.\nStichwörter: Strand, Dünen', gps: true, lat: 55.56, lon: 8.09 }),
	item(2, { gps: true, lat: 53.55, lon: 9.99 }),
	item(3),
	item(4, { kind: 'video', path: 'Fotos/2026/clip.mp4' }),
	...Array.from({ length: 6 }, (_, i) => item(5 + i)),
	item(11, { desc: 'Ein geschmückter Weihnachtsbaum im Wohnzimmer.\nStichwörter: Weihnachten, Baum' }),
	item(12),
];

async function open(width, height, { folders = [{ root: 'share/Media', path: 'Fotos' }], map = false } = {}) {
	const made = await create('owner', 'graphit', width);
	await made.page.setViewportSize({ width, height });
	const appearance = { theme: 'graphit', nav: { layout: 'side' } };
	made.data.prefs.appearance = appearance;
	await made.ctx.addInitScript((v) => localStorage.setItem('titanos.appearance', JSON.stringify(v)), appearance);
	const seen = { puts: [], posts: [] };
	const state = { prefs: { folders, describe: 'manual', from: 1, to: 6, map, started: false }, albums: [] };
	await made.ctx.route('https://tile.openstreetmap.org/**', (route) => route.fulfill({ contentType: 'image/png', body: png(170, 211, 223, 256, 256) }));
	await made.ctx.route('**/api/v1/**', async (route) => {
		const url = new URL(route.request().url());
		const p = url.pathname.slice(7);
		const method = route.request().method();
		const json = (value, status = 200) => route.fulfill({ status, json: value });
		if (p === '/files/thumb') {
			const n = [...(url.searchParams.get('path') ?? '')].reduce((a, c) => a + c.charCodeAt(0), 0);
			return route.fulfill({ contentType: 'image/png', body: png(...colors[n % colors.length]) });
		}
		if (p === '/photos' && method === 'GET')
			return json({ prefs: state.prefs, items: state.prefs.folders.length ? items : [], described: 3, scanning: false, describe: { running: false, done: 0, total: 0 }, ai: true });
		if (p === '/photos/prefs' && method === 'PUT') {
			const body = route.request().postDataJSON();
			seen.puts.push({ p, body });
			state.prefs = { ...state.prefs, ...body };
			return json(state.prefs);
		}
		if (p === '/photos/search') {
			const q = (url.searchParams.get('q') ?? '').toLowerCase();
			return json({ items: items.filter((i) => (i.desc ?? '').toLowerCase().includes(q.split(' ')[0])) });
		}
		if (p === '/photos/albums' && method === 'GET') return json({ albums: state.albums });
		if (p === '/photos/accounts') return json({ accounts: ['maria'] });
		if (p.startsWith('/photos/albums/') && method === 'PUT') {
			const body = route.request().postDataJSON();
			seen.puts.push({ p, body });
			let a = state.albums.find((x) => x.id === p.split('/').pop());
			if (!a) {
				a = { id: 'a1aaaaaaaaaa0001', name: body.name, items: [], created: new Date().toISOString() };
				state.albums.push(a);
			}
			if (body.name) a.name = body.name;
			if (body.members) a.members = body.members;
			for (const k of body.add ?? []) if (!a.items.includes(k)) a.items.push(k);
			return json(a);
		}
		if (p === '/photos/describe') {
			seen.posts.push(p);
			return route.fulfill({ status: 202, body: '' });
		}
		return route.fallback();
	});
	return { ...made, seen, state };
}

async function noOverflow(page, what) {
	const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
	assert.ok(overflow <= 1, `${what}: horizontal scroll ${overflow}`);
}

try {
	// Desktop: timeline, viewer, album, search, albums, map.
	{
		const { ctx, page, seen } = await open(1440, 1000);
		await page.goto(base + '/files');
		await page.locator('#file-places').getByRole('button', { name: 'Zeitleiste' }).click();
		await page.getByRole('heading', { name: /Oktober 2026/ }).waitFor();
		assert.ok(await page.getByRole('heading', { name: /November 2025/ }).isVisible(), 'months of the timeline');
		assert.ok(await page.getByText('3 von 12 Fotos beschrieben').isVisible(), 'AI bar counts the pictures');
		await page.waitForTimeout(400);
		await page.screenshot({ path: `${out}/1-timeline.png` });
		await noOverflow(page, 'timeline');
		await page.getByRole('button', { name: /brauner Hund/ }).first().click();
		const viewer = page.getByRole('dialog', { name: 'IMG_4800.jpg' });
		await viewer.waitFor();
		assert.ok(await viewer.getByText('Ein brauner Hund läuft').isVisible(), 'AI description');
		assert.ok(await viewer.getByText('iPhone 15').isVisible(), 'camera');
		assert.ok(await viewer.getByText(/55,56° N/).isVisible(), 'place');
		await page.screenshot({ path: `${out}/2-viewer.png` });
		await viewer.getByRole('button', { name: 'Zu Album' }).click();
		await viewer.getByLabel('Neues Album').fill('Urlaub 2026');
		await viewer.getByRole('button', { name: 'Anlegen' }).click();
		await page.waitForTimeout(200);
		const albumPut = seen.puts.find((x) => x.p === '/photos/albums/new');
		assert.deepEqual(albumPut?.body.add, ['share/Media:Fotos/2026/IMG_4800.jpg']);
		await page.keyboard.press('ArrowRight');
		await page.getByRole('dialog', { name: 'IMG_4801.jpg' }).waitFor();
		await page.keyboard.press('Escape');
		// Search in normal words.
		await page.getByRole('searchbox', { name: 'Fotos durchsuchen' }).fill('Hund am Strand');
		await page.getByText('1 Treffer').waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/3-search.png` });
		await page.getByRole('searchbox', { name: 'Fotos durchsuchen' }).fill('');
		// Albums.
		await page.getByRole('tab', { name: 'Alben' }).click();
		await page.getByRole('button', { name: /Urlaub 2026/ }).click();
		await page.getByRole('heading', { name: 'Urlaub 2026' }).waitFor();
		await page.getByRole('button', { name: 'Teilen' }).click();
		await page.locator('dialog[open]').getByLabel('maria').check();
		await page.locator('dialog[open]').getByRole('button', { name: 'Speichern' }).click();
		await page.waitForTimeout(200);
		assert.deepEqual(seen.puts.at(-1).body.members, ['maria'], 'album shared');
		await page.screenshot({ path: `${out}/4-album.png` });
		// Map: off (list by place), then on (tiles from OpenStreetMap).
		await page.getByRole('tab', { name: 'Karte' }).click();
		await page.getByText('3 Fotos mit Ort, 10 ohne').waitFor();
		assert.equal(await page.locator('img[src^="https://tile.openstreetmap.org"]').count(), 0, 'no tiles while the map is off');
		await page.screenshot({ path: `${out}/5-places.png` });
		await page.getByRole('button', { name: 'Karte einschalten' }).click();
		await page.getByRole('application', { name: 'Karte' }).waitFor();
		assert.equal(seen.puts.at(-1).body.map, true);
		await page.locator('img[src^="https://tile.openstreetmap.org"]').first().waitFor();
		await page.getByRole('button', { name: /2 Fotos an diesem Ort/ }).click();
		await page.getByRole('heading', { name: /Fotos an diesem Ort/ }).waitFor();
		await page.waitForTimeout(300);
		await page.screenshot({ path: `${out}/6-map.png` });
		// Photo folders and AI settings.
		await page.getByRole('button', { name: 'Foto-Ordner' }).click();
		const panel = page.getByRole('complementary', { name: 'Foto-Ordner und KI-Beschreibungen' });
		await panel.waitFor();
		await panel.getByText('Nachts und wenn frei').click();
		await panel.getByRole('button', { name: '+ Ordner wählen …' }).click();
		await panel.getByRole('button', { name: 'Documents' }).click();
		await panel.getByRole('button', { name: 'Diesen Ordner wählen' }).click();
		await page.screenshot({ path: `${out}/7-folders.png` });
		await panel.getByRole('button', { name: 'Speichern' }).click();
		await page.waitForTimeout(200);
		const prefs = seen.puts.at(-1).body;
		assert.equal(prefs.describe, 'idle');
		assert.equal(prefs.folders.length, 2);
		await page.getByRole('button', { name: 'Jetzt beschreiben' }).click();
		await page.waitForTimeout(200);
		assert.ok(seen.posts.includes('/photos/describe'), 'describe by the button');
		// Back to the folders.
		await page.getByRole('button', { name: 'Ordner', exact: true }).click();
		await page.getByRole('button', { name: 'Hochladen' }).waitFor();
		await ctx.close();
	}
	// No folders yet: a start in words.
	{
		const { ctx, page } = await open(1440, 900, { folders: [] });
		await page.goto(base + '/files');
		await page.getByRole('button', { name: 'Zeitleiste' }).click();
		await page.getByRole('heading', { name: 'Deine Fotos an einem Ort' }).waitFor();
		await ctx.close();
	}
	// Phone.
	{
		const { ctx, page } = await open(390, 844);
		await page.goto(base + '/files');
		await page.locator('.places-toggle').click();
		await page.getByRole('button', { name: 'Zeitleiste' }).click();
		await page.getByRole('heading', { name: /Oktober 2026/ }).waitFor();
		await page.waitForTimeout(300);
		await noOverflow(page, 'phone timeline');
		await page.screenshot({ path: `${out}/8-phone.png` });
		await page.locator('.cell').first().click();
		await page.getByRole('dialog', { name: 'IMG_4800.jpg' }).waitFor();
		await page.waitForTimeout(200);
		await page.screenshot({ path: `${out}/9-phone-viewer.png` });
		await ctx.close();
	}
	const real = errors.filter((e) => !/favicon|ERR_ABORTED/.test(e));
	assert.deepEqual(real, [], 'console errors');
	console.log('photos UI ok:', out);
} finally {
	await browser.close();
}
