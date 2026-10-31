import { expect, test } from 'vitest';
import { radioKeys } from '#lib/radio-group.ts';

// A group of role="radio" buttons with the handler, like the pages use it.
function group(disabled: number[] = []) {
	const box = document.createElement('div');
	box.setAttribute('role', 'radiogroup');
	const chosen: string[] = [];
	for (const name of ['a', 'b', 'c', 'd']) {
		const radio = document.createElement('button');
		radio.setAttribute('role', 'radio');
		radio.setAttribute('aria-checked', name === 'a' ? 'true' : 'false');
		radio.textContent = name;
		radio.disabled = disabled.includes(box.children.length);
		radio.addEventListener('click', () => chosen.push(name));
		box.append(radio);
	}
	box.addEventListener('keydown', radioKeys);
	document.body.replaceChildren(box);
	const radios = [...box.children] as HTMLButtonElement[];
	const press = (key: string) => {
		const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true });
		(document.activeElement ?? box).dispatchEvent(event);
		return event;
	};
	return { radios, chosen, press };
}

test('arrow keys move focus and select, wrapping around', () => {
	const { radios, chosen, press } = group();
	radios[0].focus();
	expect(press('ArrowRight').defaultPrevented).toBe(true);
	expect(document.activeElement).toBe(radios[1]);
	expect(chosen).toEqual(['b']);
	press('ArrowUp');
	press('ArrowUp');
	expect(document.activeElement).toBe(radios[3]);
	press('ArrowDown');
	expect(document.activeElement).toBe(radios[0]);
	// a is already checked: focus only, no click.
	expect(chosen).toEqual(['b', 'd']);
});

test('Home and End jump to the ends; disabled radios are skipped', () => {
	const { radios, press } = group([3]);
	radios[1].focus();
	press('End');
	expect(document.activeElement).toBe(radios[2]);
	press('Home');
	expect(document.activeElement).toBe(radios[0]);
	press('ArrowLeft');
	expect(document.activeElement).toBe(radios[2]);
});

test('other keys are left alone', () => {
	const { radios, chosen, press } = group();
	radios[0].focus();
	expect(press('Tab').defaultPrevented).toBe(false);
	expect(press('Enter').defaultPrevented).toBe(false);
	expect(chosen).toEqual([]);
});
