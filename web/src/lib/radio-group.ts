// Keyboard handling for groups with one choice (WAI-ARIA radio group and
// tabs patterns): the arrow keys, Home and End move focus to the next
// enabled item and select it. Pair with a roving tabindex on the items
// (`tabindex={chosen ? 0 : -1}`, the first one when none is chosen) so Tab
// enters the group only once.
const STEP: Record<string, number> = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };
const ACROSS: Record<string, number> = { ArrowRight: 1, ArrowLeft: -1 };

function roving(event: KeyboardEvent, role: 'radio' | 'tab', steps: Record<string, number>) {
	const group = event.currentTarget as HTMLElement;
	const items = [...group.querySelectorAll<HTMLElement>(`[role="${role}"]`)].filter(
		(item) =>
			!(item as HTMLButtonElement).disabled && item.getAttribute('aria-disabled') !== 'true',
	);
	if (items.length === 0) return;
	const at = items.indexOf(document.activeElement as HTMLElement);
	// Only from an item of the group (a field inside a tab panel keeps its keys).
	if (at < 0) return;
	let next: number;
	if (event.key === 'Home') next = 0;
	else if (event.key === 'End') next = items.length - 1;
	else if (event.key in steps) next = (at + steps[event.key] + items.length) % items.length;
	else return;
	event.preventDefault();
	items[next].focus();
	const state = role === 'radio' ? 'aria-checked' : 'aria-selected';
	if (items[next].getAttribute(state) !== 'true') items[next].click();
}

/** For role="radiogroup": all four arrow keys, Home and End. */
export function radioKeys(event: KeyboardEvent): void {
	roving(event, 'radio', STEP);
}

/** For a horizontal role="tablist": left and right, Home and End (the tab is chosen at once). */
export function tabKeys(event: KeyboardEvent): void {
	roving(event, 'tab', ACROSS);
}
