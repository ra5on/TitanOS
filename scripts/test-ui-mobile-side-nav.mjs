// Navigation "Side" on a phone (390 px) against isolated fixtures: no head
// line and no status bar, a round menu button at the bottom left that slides
// the sidebar in (button, swipe from the left edge, Escape, tap beside it,
// following a link), account menu inside the screen; "Top" and the desktop
// stay as they are.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-mobile-side-nav.mjs
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || 'http://127.0.0.1:5173';
const out = process.env.UI_OUTPUT || 'out/validation/mobile-side-nav';
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

async function open({ layout = 'side', width = 390, theme = 'graphit', path = '/' } = {}) {
	const { ctx, page, data } = await context('owner', theme, width);
	data.prefs.appearance = { theme, nav: { layout } };
	await ctx.addInitScript(
		({ theme, layout }) => localStorage.setItem('titanos.appearance', JSON.stringify({ theme, nav: { layout } })),
		{ theme, layout },
	);
	if (width < 761) await page.setViewportSize({ width, height: 844 });
	await page.goto(base + path);
	await page.locator('main').waitFor();
	await page.waitForLoadState('networkidle');
	return { ctx, page };
}

// A swipe as touch events on the window (the test browser has no touch screen).
async function swipe(page, from, to) {
	await page.evaluate(
		([from, to]) => {
			const at = (x) => new Touch({ identifier: 1, target: document.body, clientX: x, clientY: 400 });
			const fire = (type, x) =>
				window.dispatchEvent(new TouchEvent(type, { touches: type === 'touchend' ? [] : [at(x)], bubbles: true }));
			fire('touchstart', from);
			fire('touchmove', (from + to) / 2);
			fire('touchmove', to);
			fire('touchend', to);
		},
		[from, to],
	);
}

const fab = (page) => page.locator('button.fab');
const drawer = (page) => page.locator('dialog.drawer');

try {
	{
		const { ctx, page } = await open();
		assert.equal(await page.locator('header.bar-surface').count(), 0, 'no head line');
		assert.equal(await page.locator('footer.bar-surface').count(), 0, 'no status bar');
		assert.ok(await fab(page).isVisible(), 'menu button');
		const box = await fab(page).boundingBox();
		assert.ok(box.x < 40 && box.y > 700, `button at the bottom left (${box.x}, ${box.y})`);
		assert.equal(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--fab-clear').trim()), '76px');
		await page.screenshot({ path: `${out}/zu-graphit-390.png` });

		await fab(page).click();
		await drawer(page).waitFor();
		await drawer(page).evaluate((d) => Promise.all(d.getAnimations().map((a) => a.finished)));
		assert.ok(await drawer(page).evaluate((d) => d.open && d.matches(':modal')), 'drawer is modal');
		assert.equal(await fab(page).getAttribute('aria-expanded'), 'true');
		const nav = drawer(page).getByRole('navigation');
		assert.ok(await nav.getByRole('link', { name: 'Speicher' }).isVisible());
		assert.ok(await drawer(page).getByText('TitanOS', { exact: false }).first().isVisible(), 'status with version');
		const panel = await drawer(page).locator('.side-nav').boundingBox();
		assert.ok(panel.x === 0 && panel.width <= 330, `drawer from the left (${panel.x}, ${panel.width})`);
		await page.screenshot({ path: `${out}/offen-graphit-390.png` });

		// The account menu stays inside the screen.
		await drawer(page).locator('.account .trigger').click();
		await drawer(page).locator('.account .menu').waitFor();
		const menu = await drawer(page).locator('.account .menu').boundingBox();
		assert.ok(menu.x >= 0 && menu.x + menu.width <= 390, `account menu inside (${menu.x}, ${menu.width})`);
		await page.screenshot({ path: `${out}/konto-graphit-390.png` });
		await page.keyboard.press('Escape');
		if (await drawer(page).evaluate((d) => d.open)) await page.keyboard.press('Escape');
		assert.equal(await drawer(page).evaluate((d) => d.open), false, 'Escape closes');
		assert.equal(await fab(page).getAttribute('aria-expanded'), 'false');

		// Following a link closes it.
		await fab(page).click();
		await nav.getByRole('link', { name: 'Speicher' }).click();
		await page.waitForURL('**/storage');
		await page.waitForFunction(() => !document.querySelector('dialog.drawer')?.open);

		// A tap beside the drawer closes it.
		await fab(page).click();
		await page.mouse.click(370, 400);
		assert.equal(await drawer(page).evaluate((d) => d.open), false, 'tap beside closes');

		// Swipe from the left edge opens, swipe to the left closes; not from the middle.
		await swipe(page, 150, 300);
		assert.equal(await drawer(page).evaluate((d) => d.open), false, 'swipe from the middle does nothing');
		await swipe(page, 8, 140);
		assert.equal(await drawer(page).evaluate((d) => d.open), true, 'swipe from the edge opens');
		await swipe(page, 250, 100);
		assert.equal(await drawer(page).evaluate((d) => d.open), false, 'swipe left closes');
		await ctx.close();
	}
	{
		const { ctx, page } = await open({ theme: 'hell' });
		await fab(page).click();
		await drawer(page).waitFor();
		await drawer(page).evaluate((d) => Promise.all(d.getAnimations().map((a) => a.finished)));
		await page.screenshot({ path: `${out}/offen-hell-390.png` });
		await ctx.close();
	}
	{
		// "Top" on a phone: the head line with its menu, no round button.
		const { ctx, page } = await open({ layout: 'top' });
		assert.ok(await page.locator('.mobile-nav-trigger').isVisible());
		assert.equal(await fab(page).count(), 0);
		await ctx.close();
	}
	{
		// "Side" on the desktop: the sidebar, no round button.
		const { ctx, page } = await open({ width: 1280 });
		assert.ok(await page.locator('aside.side-nav').isVisible());
		assert.equal(await fab(page).count(), 0);
		await ctx.close();
	}
	assert.deepEqual(errors, [], 'no console errors');
	console.log('mobile side nav: ok');
} finally {
	await browser.close();
}
