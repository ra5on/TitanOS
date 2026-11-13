// PLAN 86: a hit of the settings search leads to the setting itself – tab
// chosen, rows on the way opened, the setting marked and focused.
import { beforeAll, expect, test } from 'vitest';
import { render } from '@testing-library/svelte';
import JumpProbe from '../components/fixtures/JumpProbe.svelte';
import { jumpToSetting } from '#lib/settings-jump.ts';

beforeAll(() => {
	// The page has the keyboard (the search on the same page, as on the phone).
	document.hasFocus = () => true;
	// jsdom has neither scrolling nor media queries.
	Element.prototype.scrollIntoView = () => {};
	window.matchMedia ??= ((query: string) => ({
		matches: false,
		media: query,
		addEventListener() {},
		removeEventListener() {},
	})) as unknown as typeof window.matchMedia;
});

const at = (href: string) => new URL(href, 'http://nas.test');
// The anchor the search writes (findHash in settings-find.ts).
const findHash = (titles: string[]) => 'find=' + titles.map(encodeURIComponent).join('|');

test('a part inside a closed row on another tab: tab, row, part', async () => {
	const { container } = render(JumpProbe);
	const el = await jumpToSetting(
		at(`/settings/x?tab=b#${findHash(['Verbindung', 'Mit Teilen', 'Ein Teil'])}`),
		container,
	);
	expect(container.querySelector('[data-tab="b"]')?.getAttribute('aria-selected')).toBe('true');
	expect(container.querySelector('#row-parts')?.classList.contains('open')).toBe(true);
	expect(el?.matches('.srow-sub')).toBe(true);
	expect(el?.classList.contains('found')).toBe(true);
	// The keyboard is at the part's own control.
	expect(document.activeElement?.getAttribute('aria-label')).toBe('Ein Teil');
	// Opened by the search, not by the user: the browser does not keep it open.
	expect(localStorage.getItem('titanos.settings.rows') ?? '').not.toContain('row-parts');
});

test('content that comes later is waited for', async () => {
	const { container } = render(JumpProbe, { late: true });
	const el = await jumpToSetting(
		at(`/settings/x?tab=b#${findHash(['Verbindung', 'Mit Teilen'])}`),
		container,
	);
	expect(el?.id).toBe('row-parts');
	expect(el?.classList.contains('open')).toBe(true);
});

test('a plain anchor marks its element; a tab alone only chooses the tab', async () => {
	const { container } = render(JumpProbe);
	expect(await jumpToSetting(at('/settings/x?tab=b'), container)).toBeNull();
	expect(container.querySelector('[data-tab="b"]')?.getAttribute('aria-selected')).toBe('true');
	const el = await jumpToSetting(at('/settings/x?tab=b#row-parts'), container);
	expect(el?.id).toBe('row-parts');
	expect(el?.classList.contains('found')).toBe(true);
});

test('a search in another document (the side bar of a window) keeps the keyboard', async () => {
	document.hasFocus = () => false;
	try {
		const { container } = render(JumpProbe);
		const before = document.activeElement;
		const el = await jumpToSetting(
			at(`/settings/x?tab=b#${findHash(['Verbindung', 'Mit Teilen', 'Ein Teil'])}`),
			container,
		);
		expect(el?.classList.contains('found')).toBe(true);
		expect(document.activeElement).toBe(before);
	} finally {
		document.hasFocus = () => true;
	}
});
