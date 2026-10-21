/** Keyboard handling shared by the dropdowns and the context menu of TitanOS. */
const ITEMS = '[role^="menuitem"]:not(:disabled)';

export function focusFirstItem(menu: HTMLElement | undefined) {
	menu?.querySelector<HTMLElement>(ITEMS)?.focus({ preventScroll: true });
}

/** Arrow keys, Home and End move between the items; returns true when the key was used. */
export function moveFocus(menu: HTMLElement | undefined, event: KeyboardEvent): boolean {
	if (!menu || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return false;
	const items = [...menu.querySelectorAll<HTMLElement>(ITEMS)];
	if (!items.length) return false;
	event.preventDefault();
	const index = items.indexOf(document.activeElement as HTMLElement);
	const next =
		event.key === 'Home' ? 0
		: event.key === 'End' ? items.length - 1
		: (index + (event.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length;
	items[next]?.focus({ preventScroll: true });
	return true;
}
