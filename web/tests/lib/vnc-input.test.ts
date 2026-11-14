// PLAN 87: what the phone's keyboard types into the VM screen's hidden field.
import { expect, test } from 'vitest';
import {
	accelerate,
	diffInput,
	fKey,
	KEY_CODE,
	KEYSYM,
	keysymForChar,
	namedKeysym,
} from '../../src/routes/vms/vnc-input.ts';

test('characters become keysyms: Latin-1 as is, the rest as Unicode keysyms', () => {
	expect(keysymForChar('a')).toBe(0x61);
	expect(keysymForChar('Z')).toBe(0x5a);
	expect(keysymForChar(' ')).toBe(0x20);
	expect(keysymForChar('ü')).toBe(0xfc);
	expect(keysymForChar('ß')).toBe(0xdf);
	expect(keysymForChar('€')).toBe(0x20ac);
	expect(keysymForChar('\n')).toBe(KEYSYM.Return);
	expect(keysymForChar('ł')).toBe(0x01000142);
	expect(keysymForChar('😀')).toBe(0x0101f600);
});

test('a changed field is Backspaces and new letters', () => {
	expect(diffInput(' ', ' a')).toEqual({ back: 0, text: 'a' });
	expect(diffInput(' ab', ' a')).toEqual({ back: 1, text: '' });
	expect(diffInput(' ', '')).toEqual({ back: 1, text: '' });
	// Autocorrect rewrites the word.
	expect(diffInput(' teh', ' the ')).toEqual({ back: 2, text: 'he ' });
	// Surrogate pairs count as one character.
	expect(diffInput(' 😀', ' ')).toEqual({ back: 1, text: '' });
});

test('named keys, F-keys and their key codes', () => {
	expect(namedKeysym('Enter')).toBe(0xff0d);
	expect(namedKeysym('Backspace')).toBe(0xff08);
	expect(namedKeysym('ArrowUp')).toBe(0xff52);
	expect(namedKeysym('F12')).toBe(0xffc9);
	expect(namedKeysym('a')).toBeNull();
	expect(namedKeysym('Unidentified')).toBeNull();
	expect(fKey(1)).toBe(0xffbe);
	expect(KEY_CODE[fKey(5)]).toBe('F5');
	expect(KEY_CODE[KEYSYM.Control]).toBe('ControlLeft');
});

test('the trackpad speeds up fast movements only', () => {
	expect(accelerate(2, 16)).toBe(2);
	expect(accelerate(-20, 16)).toBeLessThan(-40);
});
