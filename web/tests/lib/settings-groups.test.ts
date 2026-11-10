// PLAN 75-20, style E3: a closed settings group says what is set in it.
import { expect, test, vi } from 'vitest';
import { settingsGroups, summaryOf } from '#lib/settings-groups.ts';

function page(html: string): HTMLElement {
	const root = document.createElement('div');
	root.innerHTML = html;
	document.body.append(root);
	return root;
}

test('summaryOf names switches, choices and fields, never passwords', () => {
	const root = page(`<section class="tile"><h2 class="tile-title">Updates</h2>
		<label><input type="checkbox" checked> Automatisch</label>
		<label for="c">Kanal</label><select id="c"><option>Stabil</option><option selected>Test</option></select>
		<label>Passwort <input type="password" value="geheim"></label>
		<label>Name <input type="text" value="nas"></label></section>`);
	const tile = root.querySelector<HTMLElement>('section')!;
	expect(summaryOf(tile)).toBe('Automatisch: On · Kanal: Test · Name: nas');
	tile.dataset.summary = 'Eigene Angabe';
	expect(summaryOf(tile)).toBe('Eigene Angabe');
});

test('closing a group shows the summary on its title row', () => {
	localStorage.clear();
	const root = page(`<section class="tile"><h2 class="tile-title">Dienst</h2>
		<label><input type="checkbox"> SSH</label></section>`);
	settingsGroups(root, () => '/settings/test');
	const toggle = root.querySelector<HTMLButtonElement>('.e3-toggle')!;
	toggle.click();
	expect(root.querySelector('section')!.hasAttribute('data-closed')).toBe(true);
	expect(root.querySelector('.e3-summary')!.textContent).toBe('SSH: Off');
});

test('the summary of a closed group follows its settings (PLAN 76, F-12-13)', async () => {
	localStorage.clear();
	vi.useFakeTimers();
	const root = page(`<section class="tile"><h2 class="tile-title">Dienst</h2>
		<label><input type="checkbox"> SSH</label></section>`);
	settingsGroups(root, () => '/settings/test');
	root.querySelector<HTMLButtonElement>('.e3-toggle')!.click();
	expect(root.querySelector('.e3-summary')!.textContent).toBe('SSH: Off');
	// The data arrives after the group was closed.
	root.querySelector<HTMLInputElement>('input')!.checked = true;
	vi.advanceTimersByTime(1100);
	expect(root.querySelector('.e3-summary')!.textContent).toBe('SSH: On');
	vi.useRealTimers();
});

test('a group with an id keeps its state in another language; long values are left out, not cut', () => {
	localStorage.clear();
	const make = (title: string) =>
		page(`<section class="tile" id="ssh"><h2 class="tile-title">${title}</h2>
		<label>Adresse <input value="${'x'.repeat(60)}"></label>
		<label>Name <input value="nas"></label></section>`);
	const first = make('Zugang');
	settingsGroups(first, () => '/settings/test');
	first.querySelector<HTMLButtonElement>('.e3-toggle')!.click();
	expect(first.querySelector('.e3-summary')!.textContent).toBe('Name: nas');
	const second = make('Access');
	settingsGroups(second, () => '/settings/test');
	expect(second.querySelector('section')!.hasAttribute('data-closed')).toBe(true);
});
