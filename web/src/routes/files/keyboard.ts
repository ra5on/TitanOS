// Keyboard control of the file manager: arrows move through the list,
// Enter and Space open, the usual shortcuts copy, cut, paste, rename and delete.

import { tick } from 'svelte';
import type { FileEntry } from '#lib/api.ts';
import { previewKind } from '#lib/file-actions.ts';
import type { FileCommand } from './commands.ts';

/** What the keys need from the page. */
export type KeyboardHost = {
	/** Keys do nothing: no place, the trash or a menu is open. */
	ignored: () => boolean;
	/** The entries on screen, in their order. */
	entries: () => FileEntry[];
	selected: () => string[];
	anchor: () => string;
	setAnchor: (name: string) => void;
	/** Selects only this entry, which also becomes the anchor. */
	pick: (name: string) => void;
	/** Selects from the anchor to this entry. */
	selectRange: (name: string) => void;
	list: () => { reveal: (index: number) => void; getColumns: () => number } | undefined;
	writable: () => boolean;
	stick: () => boolean;
	editable: () => boolean;
	hasClipboard: () => boolean;
	menuAt: (entry: FileEntry, x: number, y: number) => void;
	run: (command: FileCommand, entry?: FileEntry) => void;
};

async function focusEntry(host: KeyboardHost, index: number) {
	host.list()?.reveal(index);
	await tick();
	const name = host.entries()[index]?.name;
	if (name)
		document
			.querySelector<HTMLElement>(`.browser [data-file-name="${CSS.escape(name)}"]`)
			?.focus({ preventScroll: true });
}

export function fileKeys(event: KeyboardEvent, host: KeyboardHost) {
	if (host.ignored() || document.querySelector('dialog') || event.altKey || event.defaultPrevented)
		return;
	if (
		event.target instanceof Element &&
		event.target.closest(
			'input:not([type=checkbox]):not([type=radio]),textarea,select,[contenteditable="true"],[role="menu"],.dropdown-panel',
		)
	)
		return;
	const key = event.key.toLowerCase(),
		command = event.ctrlKey || event.metaKey;
	if (key === 'f1') {
		event.preventDefault();
		host.run('shortcuts');
		return;
	}
	if (event.target instanceof Element && event.target.closest('.pane')) return;
	const entries = host.entries(),
		selected = host.selected();
	const listTarget =
		!(event.target instanceof Element) ||
		event.target === document.body ||
		!!event.target.closest('.file-entries');
	if (
		!command &&
		listTarget &&
		['arrowup', 'arrowdown', 'arrowleft', 'arrowright', 'home', 'end'].includes(key) &&
		entries.length
	) {
		event.preventDefault();
		const focused =
			event.target instanceof Element
				? event.target.closest<HTMLElement>('[data-file-name]')?.dataset.fileName
				: undefined;
		const old = entries.findIndex((e) => e.name === (focused ?? selected.at(-1)));
		const columns = host.list()?.getColumns() ?? 1;
		const step =
			key === 'arrowup' ? -columns : key === 'arrowdown' ? columns : key === 'arrowleft' ? -1 : 1;
		const index =
			key === 'home'
				? 0
				: key === 'end'
					? entries.length - 1
					: Math.max(0, Math.min(entries.length - 1, old < 0 ? 0 : old + step));
		const name = entries[index].name;
		if (event.shiftKey) {
			if (!host.anchor()) host.setAnchor(entries[Math.max(0, old)].name);
			host.selectRange(name);
		} else host.pick(name);
		void focusEntry(host, index);
		return;
	}
	if (!command && listTarget && (key === 'enter' || key === ' ') && selected.length === 1) {
		if (event.target instanceof Element && event.target.closest('button,input,a')) return;
		const entry = entries.find((e) => e.name === selected[0]);
		if (!entry) return;
		event.preventDefault();
		if (entry.dir && key === 'enter') host.run('openFolder', entry);
		else if (!entry.dir && previewKind(entry.name, entry.size)) host.run('preview', entry);
		// Like a double click: a file without a preview opens in the editor.
		else if (!entry.dir && key === 'enter' && host.editable()) host.run('edit', entry);
		return;
	}
	// Shift+F10 and the menu key open the menu of the focused entry.
	if ((key === 'contextmenu' || (key === 'f10' && event.shiftKey)) && selected.length) {
		const row = document.querySelector<HTMLElement>(
			`.browser [data-file-name="${CSS.escape(selected.at(-1) ?? '')}"]`,
		);
		const entry = entries.find((e) => e.name === selected.at(-1));
		if (row && entry) {
			event.preventDefault();
			const rect = row.getBoundingClientRect();
			host.menuAt(entry, rect.left + 40, rect.bottom);
		}
		return;
	}
	const writable = host.writable();
	if (command && key === 'a') {
		event.preventDefault();
		host.run('selectAll');
	} else if (
		command &&
		(key === 'c' || key === 'x') &&
		selected.length &&
		!host.stick() &&
		(key !== 'x' || writable)
	) {
		event.preventDefault();
		host.run(key === 'x' ? 'cut' : 'clipboardCopy');
	} else if (command && key === 'v' && host.hasClipboard() && writable) {
		event.preventDefault();
		host.run('paste');
	} else if (!command && key === 'f2' && selected.length === 1 && writable) {
		event.preventDefault();
		host.run('rename');
	} else if (!command && key === 'delete' && selected.length && writable) {
		event.preventDefault();
		host.run('delete');
	} else if (!command && key === 'escape' && selected.length) {
		host.run('clearSelection');
	}
}
