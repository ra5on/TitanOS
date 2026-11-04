// Tools with a sidebar like the settings (decision of the owner of 4 Nov
// 2026, variant B) against isolated fixtures: every tools page is in the
// sidebar and marked when open, "At a glance" shows values from the
// fixtures' answers, old anchors lead to the new pages, the phone shows the
// grouped list and a way back, no sideways scrolling, users get no access.
// Screenshots: desktop 1440 and phone 390, Graphit and Hell.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5199 node scripts/test-ui-tools-sidebar.mjs
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || 'http://127.0.0.1:5173';
const out = process.env.UI_OUTPUT || 'out/validation/werkzeuge';
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

// The sidebar in its order: group, entry, address and page title.
const entries = [
	['Beobachten', 'Aufträge', '/tools/tasks', 'Aufträge'],
	['Beobachten', 'Monitoring', '/tools/monitoring', 'Monitoring'],
	['Beobachten', 'Aktivität', '/tools/activity', 'Aktivität'],
	['Beobachten', 'Protokoll', '/tools/logs', 'Protokoll'],
	['Pflegen', 'Wartung', '/tools/maintenance', 'Wartung'],
	['Pflegen', 'Skripte', '/tools/scripts', 'Skripte'],
	['Pflegen', 'Hardware prüfen', '/tools/hardware', 'Hardware prüfen'],
	['Pflegen', 'Platten vorbereiten', '/tools/disks', 'Platten vorbereiten'],
	['Hilfe', 'Diagnosepaket', '/tools/bundle', 'Diagnosepaket'],
];

let lastPage;
async function open(role, theme, width, path = '/tools', prepare) {
	const made = await context(role, theme, width);
	lastPage = made.page;
	prepare?.(made.data);
	if (width < 761) await made.page.setViewportSize({ width, height: 844 });
	await made.page.goto(base + path);
	await made.page.locator('main').waitFor();
	await made.page.waitForLoadState('networkidle');
	return made;
}
// Page changes cross-fade (view transition); screenshots wait until that is over.
const settle = (page) =>
	page
		.waitForTimeout(400)
		.then(() =>
			page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => {})))),
		);
const noSideScroll = (page) =>
	page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1);

try {
	{
		// Desktop: the sidebar lists every tool in its group and walks through them.
		const { ctx, page } = await open('owner', 'graphit', 1440, '/tools', (data) => {
			data.maintenance.settings.enabled = true;
			// Real answers carry it; without it the page counts as changed.
			data.maintenance.settings.backupsFirst = false;
			data.monitor = { enabled: true, volume: 'Data', days: 14, maxMiB: 8 };
		});
		const side = page.getByRole('navigation', { name: 'Werkzeugbereiche' });
		await side.waitFor();
		const overview = side.getByRole('link', { name: 'Übersicht' });
		assert.equal(await overview.getAttribute('aria-current'), 'page');
		const links = side.getByRole('link');
		assert.deepEqual(await links.allInnerTexts(), ['Übersicht', ...entries.map((e) => e[1])]);
		for (const group of ['Beobachten', 'Pflegen', 'Hilfe']) {
			const list = side.getByRole('list', { name: group });
			const expected = entries.filter((e) => e[0] === group).map((e) => e[1]);
			assert.deepEqual(await list.getByRole('link').allInnerTexts(), expected, group);
		}
		// At a glance: values from the answers, nothing made up.
		const glance = page.getByRole('region', { name: 'Auf einen Blick' });
		await glance.waitFor();
		await glance.getByText('nichts läuft').waitFor();
		const text = await glance.innerText();
		for (const part of ['Aufträge', 'Wartung', 'an · 0 Aufgaben', 'Skripte', 'eingerichtet', 'Monitoring', 'an · Data, 14 Tage', 'keine Prüfung läuft'])
			assert.ok(text.includes(part), `glance shows ${part}: ${text}`);
		assert.ok(!text.includes('Protokoll'), 'no journal count without an API for it');
		assert.equal(await page.locator('.phone-only').isVisible(), false, 'no phone list on the desktop');
		assert.ok(await noSideScroll(page));
		await settle(page);
		await page.screenshot({ path: `${out}/uebersicht-graphit-1440.png`, fullPage: true });
		for (const [, label, href, title] of entries) {
			await side.getByRole('link', { name: label, exact: true }).click();
			await page.waitForURL(`**${href}`);
			await page.getByRole('heading', { name: title, level: 1 }).waitFor();
			const current = side.locator('a[aria-current="page"]');
			assert.equal(await current.count(), 1, `one current entry on ${href}`);
			assert.equal((await current.innerText()).trim(), label);
			// The sidebar says where one is: no crumb and no way back on the desktop.
			assert.equal(await page.locator('.page-head .crumbs').isVisible().catch(() => false), false, href);
			assert.equal(await page.getByRole('link', { name: '‹ Werkzeuge' }).isVisible(), false);
			assert.ok(await noSideScroll(page), `no sideways scrolling on ${href}`);
		}
		await page.goto(base + '/tools/disks');
		await page.getByRole('region', { name: 'Platten vorbereiten' }).waitFor();
		await settle(page);
		await page.screenshot({ path: `${out}/platten-graphit-1440.png`, fullPage: true });
		await page.goto(base + '/tools/bundle');
		await page.getByRole('button', { name: 'Erstellen und herunterladen' }).waitFor();
		await settle(page);
		await page.screenshot({ path: `${out}/diagnosepaket-graphit-1440.png`, fullPage: true });
		await ctx.close();
	}
	{
		// Saved links to the old sections lead to their pages.
		const { ctx, page } = await open('owner', 'graphit', 1440, '/tools#tools-disks');
		await page.waitForURL('**/tools/disks');
		await page.goto(base + '/tools#tools-bundle');
		await page.waitForURL('**/tools/bundle');
		await page.goto(base + '/tools#tools-logs');
		await page.waitForURL('**/tools/logs');
		await ctx.close();
	}
	{
		const { ctx, page } = await open('owner', 'hell', 1440);
		await page.getByRole('region', { name: 'Auf einen Blick' }).getByText('nichts läuft').waitFor();
		await settle(page);
		await page.screenshot({ path: `${out}/uebersicht-hell-1440.png`, fullPage: true });
		await page.goto(base + '/tools/maintenance');
		await page.getByRole('heading', { name: 'Wartung', level: 1 }).waitFor();
		await page.waitForLoadState('networkidle');
		await settle(page);
		await page.screenshot({ path: `${out}/wartung-hell-1440.png`, fullPage: true });
		await ctx.close();
	}
	for (const theme of ['graphit', 'hell']) {
		// Phone: no sidebar; the overview is the grouped list, pages have a way back.
		const { ctx, page } = await open('owner', theme, 390);
		assert.equal(await page.getByRole('navigation', { name: 'Werkzeugbereiche' }).isVisible(), false);
		assert.equal(await page.getByRole('region', { name: 'Auf einen Blick' }).isVisible(), false);
		for (const group of ['Beobachten', 'Pflegen', 'Hilfe'])
			assert.ok(await page.getByRole('heading', { name: group, level: 2 }).isVisible(), group);
		const list = page.locator('.phone-only');
		const names = await list.locator('a .name').allInnerTexts();
		assert.deepEqual(names, entries.map((e) => e[1]));
		assert.ok(await noSideScroll(page), 'no sideways scrolling at 390 px');
		await settle(page);
		await page.screenshot({ path: `${out}/uebersicht-${theme}-390.png`, fullPage: true });
		await list.getByRole('link', { name: /Platten vorbereiten/ }).click();
		await page.waitForURL('**/tools/disks');
		await page.getByRole('heading', { name: 'Platten vorbereiten', level: 1 }).waitFor();
		await page.waitForLoadState('networkidle');
		assert.ok(await noSideScroll(page));
		await settle(page);
		await page.screenshot({ path: `${out}/platten-${theme}-390.png`, fullPage: true });
		await page.getByRole('link', { name: '‹ Werkzeuge' }).click();
		await page.waitForURL(/\/tools$/);
		await list.getByRole('link', { name: /Diagnosepaket/ }).click();
		await page.waitForURL('**/tools/bundle');
		assert.ok(await noSideScroll(page));
		await settle(page);
		await page.screenshot({ path: `${out}/diagnosepaket-${theme}-390.png`, fullPage: true });
		await ctx.close();
	}
	{
		// Accounts without admin rights see no tools.
		const { ctx, page } = await open('user', 'graphit', 1440);
		await page.getByRole('alert').waitFor();
		assert.equal(await page.getByRole('navigation', { name: 'Werkzeugbereiche' }).count(), 0);
		await ctx.close();
	}
	assert.deepEqual(errors, []);
	console.log('tools sidebar UI ok');
} catch (err) {
	console.error(errors.join('\n').slice(0, 3000));
	await lastPage?.screenshot({ path: `${out}/fehler.png` }).catch(() => {});
	throw err;
} finally {
	await browser.close();
}
