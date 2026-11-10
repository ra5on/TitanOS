// PLAN 75-19/75-20, style E3 (owner 9.11.2026): every group of a settings
// page – a tile with a title – opens and closes from its title row, like
// the drafted Energie page. Open at first; a closed group stays closed on
// that page in this browser. The pages keep their markup: this only sets
// attributes on the title and its tile, so nothing is moved in the DOM.

import { t } from '#lib/i18n/index.svelte.ts';

const KEY = 'titanos.settings.closed';

function closedSet(): Set<string> {
	try {
		return new Set(JSON.parse(localStorage.getItem(KEY) ?? '[]') as string[]);
	} catch {
		return new Set();
	}
}

function remember(id: string, closed: boolean) {
	try {
		const all = closedSet();
		if (closed) all.add(id);
		else all.delete(id);
		localStorage.setItem(KEY, JSON.stringify([...all].slice(-300)));
	} catch {
		/* private window: only for this visit */
	}
}

/** The identity of a group across visits: page and title text. */
export function groupId(path: string, title: string): string {
	return `${path.replace(/\/$/, '')}#${title.trim().toLowerCase()}`;
}

// Text is never cut off (owner 10.11.2026): the summary wraps in its row; a
// value too long to say in passing is left out instead.
const clean = (text: string | null | undefined) => (text ?? '').replace(/\s+/g, ' ').trim();
const LONG = 40;

function labelOf(control: HTMLElement): string {
	const own =
		(control as HTMLInputElement).labels?.[0]?.textContent ??
		control.getAttribute('aria-label') ??
		control.closest('label')?.textContent;
	return clean(own);
}

/**
 * What a closed group says on its title row (PLAN 75-20, "Aufklappen öfter"):
 * `data-summary` of the tile if the page sets one, else the first settings
 * in it – a switch as "Name: An/Aus", a choice by its chosen entry, a field
 * by its value. Passwords, hidden and search fields never show.
 */
export function summaryOf(tile: HTMLElement): string {
	if (tile.dataset.summary) return clean(tile.dataset.summary);
	const parts: string[] = [];
	const controls = tile.querySelectorAll<HTMLElement>(
		'input:not([type=hidden]):not([type=password]):not([type=search]):not([type=file]), select',
	);
	for (const control of controls) {
		if (parts.length >= 3) break;
		if (control.closest('.tile-title')) continue;
		if (control instanceof HTMLSelectElement) {
			const label = labelOf(control);
			const value = clean(control.selectedOptions[0]?.textContent);
			if (value) parts.push(label ? `${label}: ${value}` : value);
		} else if (control instanceof HTMLInputElement) {
			if (control.type === 'radio') {
				if (control.checked) parts.push(labelOf(control));
			} else if (control.type === 'checkbox') {
				const label = labelOf(control);
				if (label) parts.push(`${label}: ${t(control.checked ? 'appform.on' : 'appform.off')}`);
			} else if (control.value.trim()) {
				const label = labelOf(control);
				const value = clean(control.value);
				if (value.length <= LONG) parts.push(label ? `${label}: ${value}` : value);
			}
		}
	}
	return parts.filter((p) => p && p.length <= 2 * LONG).join(' · ');
}

function wire(title: HTMLElement, path: () => string) {
	if (title.dataset.e3) return;
	const tile = title.parentElement;
	if (!tile || !tile.classList.contains('tile')) return;
	title.dataset.e3 = '1';
	tile.classList.add('e3-group');
	// The title stays a heading; a button of its own opens and closes the
	// group for the keyboard and screen readers, a click on the title too.
	const button = document.createElement('button');
	button.type = 'button';
	button.className = 'e3-toggle';
	const summary = document.createElement('span');
	summary.className = 'e3-summary';
	// The title's own text, without the summary and the toggle.
	const name = (title.textContent ?? '').trim();
	// PLAN 76 (F-12-13): a group with an id of its own keeps its state when the
	// language changes; the title's text is the older identity and still read.
	const byTitle = () => groupId(path(), name);
	const own = tile.id || title.id || tile.dataset.group || '';
	const id = () => (own ? `${path().replace(/\/$/, '')}#id:${own}` : byTitle());
	const isClosed = () => {
		const all = closedSet();
		return all.has(id()) || all.has(byTitle());
	};
	// The settings of a closed group may arrive or change later (loaded data, a
	// save elsewhere): its summary follows instead of staying at the first guess.
	const refresh = () => {
		if (!tile.isConnected) return clearInterval(timer);
		if (!tile.hasAttribute('data-closed') || document.hidden) return;
		const text = summaryOf(tile);
		if (summary.textContent !== text) summary.textContent = text;
	};
	let timer: ReturnType<typeof setInterval> | undefined;
	const set = (closed: boolean) => {
		if (closed) summary.textContent = summaryOf(tile);
		clearInterval(timer);
		if (closed) timer = setInterval(refresh, 1000);
		tile.toggleAttribute('data-closed', closed);
		button.setAttribute('aria-expanded', String(!closed));
		button.setAttribute('aria-label', t(closed ? 'settings.groupOpen' : 'settings.groupClose'));
	};
	title.append(summary, button);
	// The heading keeps its own name: without this, screen readers (and
	// getByRole) would read the summary and the toggle's label as part of it.
	const ownName = () =>
		Array.from(title.childNodes)
			.filter((n) => n !== summary && n !== button)
			.map((n) => n.textContent ?? '')
			.join('')
			.trim();
	const named = () => {
		const own = ownName();
		if (own && title.getAttribute('aria-label') !== own) title.setAttribute('aria-label', own);
	};
	named();
	new MutationObserver(named).observe(title, {
		childList: true,
		characterData: true,
		subtree: true,
	});
	set(isClosed());
	const toggle = () => {
		const closed = !tile.hasAttribute('data-closed');
		set(closed);
		remember(id(), closed);
		if (own) remember(byTitle(), false);
	};
	button.addEventListener('click', (event) => {
		event.stopPropagation();
		toggle();
	});
	title.addEventListener('click', (event) => {
		if ((event.target as Element).closest('a, button, input, select, label')) return;
		toggle();
	});
}

/** Svelte action for the settings' page: wires every group, also later ones. */
export function settingsGroups(node: HTMLElement, path: () => string) {
	const scan = () =>
		node
			.querySelectorAll<HTMLElement>(
				':scope section.tile > .tile-title, :scope form.tile > .tile-title',
			)
			.forEach((title) => wire(title, path));
	scan();
	const observer = new MutationObserver(scan);
	observer.observe(node, { childList: true, subtree: true });
	return { destroy: () => observer.disconnect() };
}
