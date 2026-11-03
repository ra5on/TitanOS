// Settings › Temperatures › Heat notifications against isolated fixtures:
// limits come from titand, muting a sensor sends the whole list.
// PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs node scripts/test-ui-temperature-alarms.mjs
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { fixtureFactory } from './test-ui-fixtures.mjs';
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright');
const browser = await chromium.launch({ headless: true, executablePath: process.env.BROWSER_EXECUTABLE });
const base = process.env.UI_URL || 'http://127.0.0.1:5173';
const out = process.env.UI_OUTPUT || 'out/validation/temperature-alarms';
await mkdir(out, { recursive: true });
const errors = [];
const context = fixtureFactory(browser, errors);

const id = (n) => String(n).repeat(32);
const temperatures = [
	{ id: id(1), chip: 'coretemp', label: 'Package id 0', device: 'devices/platform/coretemp.0', celsius: 48.0, criticalC: 100 },
	{ id: id(2), chip: 'acpitz', label: 'temp1', device: 'devices/virtual/thermal/thermal_zone0', celsius: 27.8, criticalC: 105 },
	{ id: id(3), chip: 'nct6798', label: 'temp8', device: 'devices/platform/nct6775.656', celsius: 126.0, criticalC: null },
];
const limits = { [id(1)]: 95, [id(2)]: 100, [id(3)]: null };

try {
	for (const [name, width, theme] of [['graphit-1440', 1440, 'graphit'], ['hell-390', 390, 'hell']]) {
		const { ctx, page, data } = await context('owner', theme, width);
		data.system = { ...data.system, temperatures };
		let muted = [id(2)];
		const puts = [];
		const answer = () => ({ muted, sensors: temperatures.map((t) => ({ id: t.id, limit: limits[t.id], muted: muted.includes(t.id) })) });
		await page.route('**/api/v1/notifications/temperatures', (route) => {
			if (route.request().method() === 'PUT') {
				puts.push(route.request().postDataJSON());
				muted = puts.at(-1).muted;
			}
			return route.fulfill({ json: answer() });
		});
		await page.route('**/api/v1/system/fans', (route) => route.fulfill({ json: { fans: [], readOnly: false } }));
		await page.goto(base + '/settings/temperatures');
		const panel = page.getByRole('region', { name: 'Meldungen bei Hitze' });
		await panel.getByText('Meldet ab 95 °C').waitFor();
		assert.ok(await panel.getByText('Stummgeschaltet').isVisible());
		assert.ok(await panel.getByText('Meldet nie: Chip ohne eigenen Grenzwert').isVisible());
		const cpu = panel.getByLabel('coretemp Package id 0 melden');
		const board = panel.getByLabel('nct6798 temp8 melden');
		assert.ok(await cpu.isChecked());
		assert.ok(await board.isDisabled());
		assert.equal(await board.isChecked(), false);
		await page.screenshot({ path: `${out}/${name}.png`, fullPage: true, animations: 'disabled' });
		if (width === 1440) {
			await cpu.uncheck();
			await panel.getByText('Meldungen gespeichert.').waitFor();
			assert.deepEqual(puts.at(-1), { muted: [id(2), id(1)] });
			await panel.getByLabel('acpitz temp1 melden').check();
			await panel.getByText('Meldet ab 100 °C').waitFor();
			assert.deepEqual(puts.at(-1), { muted: [id(1)] });
		} else {
			const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
			assert.ok(overflow <= 0, `page scrolls sideways by ${overflow}px`);
		}
		await ctx.close();
	}
	// A plain user sees no heat notifications.
	const { ctx, page, data } = await context('user');
	data.system = { ...data.system, temperatures };
	await page.goto(base + '/settings/temperatures');
	await page.getByText('Temperaturen auf dem Dashboard anzeigen').waitFor();
	assert.equal(await page.getByText('Meldungen bei Hitze').count(), 0);
	await ctx.close();
	assert.deepEqual(errors, []);
	console.log(`ok – screenshots in ${out}`);
} finally {
	await browser.close();
}
