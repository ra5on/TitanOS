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

const clean = (text: string | null | undefined, max = 28) => {
	const one = (text ?? '').replace(/\s+/g, ' ').trim();
	return one.length > max ? one.slice(0, max - 1).trimEnd() + '…' : one;
};

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
	if (tile.dataset.summary) return clean(tile.dataset.summary, 90);
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
				const value = clean(control.value, 24);
				parts.push(label ? `${label}: ${value}` : value);
			}
		}
	}
	return clean(parts.filter(Boolean).join(' · '), 90);
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
	const id = () => groupId(path(), name);
	const set = (closed: boolean) => {
		if (closed) summary.textContent = summaryOf(tile);
		tile.toggleAttribute('data-closed', closed);
		button.setAttribute('aria-expanded', String(!closed));
		button.setAttribute('aria-label', t(closed ? 'settings.groupOpen' : 'settings.groupClose'));
	};
	title.append(summary, button);
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
