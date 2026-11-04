// Settings › Appearance › Navigation › Sidebar against isolated fixtures:
// the side navigation has its own look ("Like the tiles" by default, opaque,
// own values) on the desktop (1440 px) and in the phone's drawer (390 px),
// header and status bar keep theirs; the setting sits at the "Top/Side"
// choice with a live preview; one "Apply" bar at the bottom saves every
// changed area of the page (appearance, app symbol, login page) and names
// a failing area.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs UI_URL=http://127.0.0.1:5413 node scripts/test-ui-sidebar-look.mjs
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || 'http://127.0.0.1:5173';
const out = process.env.UI_OUTPUT || 'out/validation/seitenleiste';
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

// Graphit's header color #12161A.
const HEADER = [18, 22, 26];
const rgba = (alpha) =>
	alpha === 1 ? `rgb(${HEADER.join(', ')})` : `rgba(${HEADER.join(', ')}, ${alpha})`;

// Tiles at 60 % transparency and 12 px blur, so that "Like the tiles" is visible.
const TILES = { theme: 'graphit', style: 'glass', glass: 60, blur: 12, shine: 20 };
const MODES = {
	tiles: { look: { mode: 'tiles' }, bg: rgba(0.4), backdrop: 'blur(12px) saturate(1.6)' },
	opaque: { look: { mode: 'opaque' }, bg: rgba(1), backdrop: 'none' },
	own: {
		look: { mode: 'own', own: true, glass: 50, blur: 6, shine: 40 },
		bg: rgba(0.5),
		backdrop: 'blur(6px) saturate(1.6)',
	},
};

async function open({ appearance, width = 1440, path = '/', role = 'owner' }) {
	const { ctx, page, data } = await context(role, 'graphit', width);
	data.prefs.appearance = appearance;
	await ctx.addInitScript(
		(value) => localStorage.setItem('titanos.appearance', JSON.stringify(value)),
		appearance,
	);
	if (width < 761) await page.setViewportSize({ width, height: 844 });
	await page.goto(base + path);
	await page.locator('main').first().waitFor();
	await page.waitForLoadState('networkidle');
	return { ctx, page, data };
}

/** Background and backdrop filter of an element's ::before layer. */
const layer = (locator) =>
	locator.evaluate((el) => {
		const s = getComputedStyle(el, '::before');
		return { bg: s.backgroundColor, backdrop: s.backdropFilter, edge: s.boxShadow };
	});

async function openDrawer(page) {
	await page.locator('button.fab').click();
	const drawer = page.locator('dialog.drawer');
	await drawer.waitFor();
	await drawer.evaluate((d) => Promise.all(d.getAnimations().map((a) => a.finished)));
	return drawer;
}

try {
	// 1. Each mode on the desktop and in the drawer; the header and status bar stay opaque.
	for (const [mode, expected] of Object.entries(MODES)) {
		const appearance = { ...TILES, nav: { layout: 'side' }, sidebar: expected.look };
		{
			const { ctx, page } = await open({ appearance });
			const got = await layer(page.locator('aside.side-nav'));
			assert.equal(got.bg, expected.bg, `${mode} desktop background`);
			assert.equal(got.backdrop, expected.backdrop, `${mode} desktop backdrop`);
			if (mode === 'own') assert.match(got.edge, /inset/, 'own shine edge');
			else assert.equal(got.edge, 'none', `${mode} without shine`);
			if (mode === 'tiles') await page.screenshot({ path: `${out}/dashboard-wie-kacheln-1440.png` });
			if (mode === 'own') await page.screenshot({ path: `${out}/dashboard-eigene-werte-1440.png` });
			await ctx.close();
		}
		{
			const { ctx, page } = await open({ appearance, width: 390 });
			const drawer = await openDrawer(page);
			const got = await layer(drawer.locator('.side-nav'));
			assert.equal(got.bg, expected.bg, `${mode} drawer background`);
			assert.equal(got.backdrop, expected.backdrop, `${mode} drawer backdrop`);
			if (mode === 'tiles') await page.screenshot({ path: `${out}/dashboard-wie-kacheln-390.png` });
			await ctx.close();
		}
	}
	{
		// Stored before the field existed: like the tiles. Header/status bar stay opaque.
		const { ctx, page } = await open({ appearance: { ...TILES, nav: { layout: 'side' } } });
		assert.equal((await layer(page.locator('aside.side-nav'))).bg, rgba(0.4), 'old appearance');
		await ctx.close();
		const top = await open({ appearance: { ...TILES, sidebar: { mode: 'own', glass: 50 } } });
		assert.equal((await layer(top.page.locator('header.bar-surface'))).bg, rgba(1), 'header opaque');
		assert.equal((await layer(top.page.locator('footer.bar-surface'))).bg, rgba(1), 'status opaque');
		await top.ctx.close();
		// The bars "like the tiles", the sidebar unaffected by them.
		const bars = await open({
			appearance: { ...TILES, bars: { mode: 'tiles' }, sidebar: { mode: 'opaque' } },
		});
		assert.equal((await layer(bars.page.locator('header.bar-surface'))).bg, rgba(0.4), 'header like tiles');
		await bars.ctx.close();
		// Flat: opaque whatever the sidebar says.
		const flat = await open({
			appearance: { ...TILES, style: 'flat', nav: { layout: 'side' }, sidebar: MODES.own.look },
		});
		const got = await layer(flat.page.locator('aside.side-nav'));
		assert.deepEqual([got.bg, got.backdrop], [rgba(1), 'none'], 'flat stays opaque');
		await flat.ctx.close();
	}

	// 2. The setting in Settings › Appearance, its live preview and the one save bar.
	{
		const icon = { kind: 'default', version: 1 };
		const saves = { appearance: [], icon: [] };
		const { ctx, page } = await open({
			appearance: { ...TILES, nav: { layout: 'side' } },
			path: '/settings/appearance',
		});
		await ctx.route('**/api/v1/preferences/appearance', (route) => {
			saves.appearance.push(route.request().postDataJSON());
			return route.fulfill({ status: 204 });
		});
		let failIcon = true;
		await ctx.route('**/api/v1/app-icon', (route) => {
			if (route.request().method() !== 'PUT') return route.fulfill({ json: icon });
			if (failIcon)
				return route.fulfill({ status: 500, json: { error: { code: 'internal', message: 'Testfehler' } } });
			const body = route.request().postDataJSON();
			saves.icon.push(body);
			return route.fulfill({ json: { ...body, version: 2 } });
		});
		// The editors load once at the start; reload with the routes above in place.
		await page.reload();
		await page.locator('main').first().waitFor();
		const nav = page.locator('section#navigation');
		const sidebar = nav.locator('fieldset#sidebar');
		await sidebar.waitFor();
		// Directly after the "Top/Side" choice.
		assert.ok(
			await sidebar.evaluate((el) => el.previousElementSibling?.classList.contains('layouts')),
			'sidebar right after the layout choice',
		);
		const choice = sidebar.getByRole('radiogroup', { name: 'Seitenleiste' });
		assert.equal(await choice.getByRole('radio', { checked: true }).textContent(), 'Wie die Kacheln');
		assert.equal(await page.locator('.save-bar').count(), 0, 'no bar without changes');

		const sample = page.locator('.preview .side-sample');
		assert.equal((await layer(sample)).bg, rgba(0.4), 'preview like the tiles');
		await choice.getByRole('radio', { name: 'Deckend' }).click();
		assert.equal((await layer(sample)).bg, rgba(1), 'preview opaque right away');
		await choice.getByRole('radio', { name: 'Eigene Werte' }).click();
		for (const name of ['Transparenz', 'Unschärfe', 'Glanzkante'])
			assert.ok(await sidebar.getByText(name, { exact: true }).first().isVisible(), name);
		// The rest of the UI keeps the saved look until "Apply".
		assert.equal((await layer(page.locator('aside.side-nav'))).bg, rgba(0.4));

		const bar = page.locator('.save-bar');
		await bar.getByText('Nicht gespeichert').waitFor();
		assert.equal(await bar.evaluate((el) => getComputedStyle(el).position), 'sticky', 'bar pinned');
		await nav.scrollIntoViewIfNeeded();
		await nav.screenshot({ path: `${out}/einstellungen-navigation-1440.png` });
		await page.screenshot({ path: `${out}/einstellungen-speicherleiste-1440.png` });

		// "Top" hides the entry; the bars entry names only header and status bar.
		await nav.getByText('Oben', { exact: true }).click();
		assert.equal(await sidebar.count(), 0, 'hidden with "Top"');
		await nav.getByText('Seitlich', { exact: true }).click();
		assert.match(
			await page.locator('#menus').textContent(),
			/Kopfzeile und Statusleiste \(Navigation „Oben“\)/,
		);

		// App symbol changed too; it fails, the appearance is saved anyway.
		await page.locator('#appearance-app-icon').getByText('Logo auf eigener Farbe').click();
		await bar.getByRole('button', { name: 'Übernehmen' }).click();
		await bar.getByText(/^App-Symbol: /).waitFor();
		assert.equal(saves.appearance.length, 1);
		assert.equal(saves.appearance[0].sidebar.mode, 'own');
		assert.equal(saves.appearance[0].sidebar.own, true);
		// Own values start from the tiles' values.
		assert.equal(saves.appearance[0].sidebar.glass, 60);
		assert.equal(saves.appearance[0].sidebar.blur, 12);
		const applied = await layer(page.locator('aside.side-nav'));
		assert.deepEqual([applied.bg, applied.backdrop], [rgba(0.4), 'blur(12px) saturate(1.6)'], 'applied');
		assert.match(applied.edge, /inset/, 'applied with shine edge');
		await bar.getByText('Nicht gespeichert').waitFor();
		await page.screenshot({ path: `${out}/einstellungen-fehler-1440.png` });
		// Again: only the app symbol is sent now.
		failIcon = false;
		await bar.getByRole('button', { name: 'Übernehmen' }).click();
		await bar.getByText('Gespeichert', { exact: true }).waitFor();
		assert.equal(saves.appearance.length, 1, 'appearance not sent twice');
		assert.equal(saves.icon.length, 1);
		assert.equal(saves.icon[0].kind, 'color');
		assert.equal(await bar.getByRole('button').count(), 0, 'no buttons without changes');

		// "Reset" drops the changes of every area.
		await choice.getByRole('radio', { name: 'Deckend' }).click();
		await page.locator('#appearance-app-icon').getByText('Standard', { exact: true }).click();
		await bar.getByRole('button', { name: 'Zurücksetzen' }).click();
		assert.equal(await page.locator('.save-bar').count(), 0, 'bar gone after reset');
		assert.equal(await choice.getByRole('radio', { checked: true }).textContent(), 'Eigene Werte');
		assert.ok(
			await page.locator('#appearance-app-icon').locator('input[value="color"]').isChecked(),
		);

		// Flat: a short note instead of the choice.
		await page.getByRole('radio', { name: /^Flach/ }).click();
		await sidebar.getByText(/Mit den Stilen Flach und Kontur ist die Seitenleiste deckend/).waitFor();
		assert.equal(await choice.count(), 0);
		await ctx.close();
	}
	{
		// The phone: the entry and the bar beside the round menu button.
		const { ctx, page } = await open({
			appearance: { ...TILES, nav: { layout: 'side' } },
			path: '/settings/appearance',
			width: 390,
		});
		const sidebar = page.locator('fieldset#sidebar');
		await sidebar.getByRole('radio', { name: 'Eigene Werte' }).click();
		const bar = page.locator('.save-bar');
		await bar.waitFor();
		const box = await bar.boundingBox();
		assert.ok(box.x >= 76, `bar beside the menu button (${box.x})`);
		assert.ok(box.x + box.width <= 390, 'bar inside the screen');
		await sidebar.scrollIntoViewIfNeeded();
		await page.screenshot({ path: `${out}/einstellungen-seitenleiste-390.png` });
		await ctx.close();
	}
	assert.deepEqual(errors, [], 'no console errors');
	console.log('sidebar look: ok');
} finally {
	await browser.close();
}
