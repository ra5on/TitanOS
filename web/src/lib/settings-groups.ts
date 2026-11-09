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
	const id = () => groupId(path(), title.textContent ?? '');
	const set = (closed: boolean) => {
		tile.toggleAttribute('data-closed', closed);
		button.setAttribute('aria-expanded', String(!closed));
		button.setAttribute('aria-label', t(closed ? 'settings.groupOpen' : 'settings.groupClose'));
	};
	title.append(button);
	set(closedSet().has(id()));
	const toggle = () => {
		const closed = !tile.hasAttribute('data-closed');
		set(closed);
		remember(id(), closed);
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
