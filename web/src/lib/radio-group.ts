// Keyboard handling for custom role="radio" button groups (WAI-ARIA radio
// group pattern): the arrow keys, Home and End move focus to the next enabled
// radio and select it. Pair with a roving tabindex on the radios
// (`tabindex={checked ? 0 : -1}`) so Tab enters the group only once.
const STEP: Record<string, number> = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };

export function radioKeys(event: KeyboardEvent): void {
	const group = event.currentTarget as HTMLElement;
	const radios = [...group.querySelectorAll<HTMLElement>('[role="radio"]')].filter(
		(radio) => !(radio as HTMLButtonElement).disabled && radio.getAttribute('aria-disabled') !== 'true'
	);
	if (radios.length === 0) return;
	const at = radios.indexOf(document.activeElement as HTMLElement);
	let next: number;
	if (event.key === 'Home') next = 0;
	else if (event.key === 'End') next = radios.length - 1;
	else if (event.key in STEP) next = at < 0 ? 0 : (at + STEP[event.key] + radios.length) % radios.length;
	else return;
	event.preventDefault();
	radios[next].focus();
	if (radios[next].getAttribute('aria-checked') !== 'true') radios[next].click();
}
