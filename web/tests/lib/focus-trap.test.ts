import { afterEach, expect, test } from 'vitest';
import { trapFocus } from '#lib/focus-trap.ts';

afterEach(() => (document.body.innerHTML = ''));

function sheet(html: string) {
	const node = document.createElement('div');
	node.setAttribute('role', 'dialog');
	node.innerHTML = html;
	document.body.append(node);
	return node;
}
const key = (key: string, shiftKey = false) => {
	const e = new KeyboardEvent('keydown', { key, shiftKey, bubbles: true, cancelable: true });
	(document.activeElement ?? document.body).dispatchEvent(e);
	return e;
};

// 1.01: the sheets of the agent on the phone keep the focus, Escape closes
// the one on top only, and the focus comes back to where it was.
test('Tab stays inside, Escape closes the top sheet, the focus comes back', () => {
	const opener = document.createElement('button');
	document.body.append(opener);
	opener.focus();

	const lower = sheet('<button id="a">A</button><button id="b">B</button>');
	let closedLower = 0;
	const trapLower = trapFocus(lower, { onescape: () => closedLower++, initial: 'first' });
	expect(document.activeElement?.id).toBe('a');
	// Tab at the last control goes to the first, Shift+Tab at the first to the last.
	(lower.querySelector('#b') as HTMLElement).focus();
	expect(key('Tab').defaultPrevented).toBe(true);
	expect(document.activeElement?.id).toBe('a');
	key('Tab', true);
	expect(document.activeElement?.id).toBe('b');

	// A second sheet above: only it answers Escape.
	const upper = sheet('<input id="c" /><button id="d" disabled>D</button>');
	upper.tabIndex = -1;
	let closedUpper = 0;
	const trapUpper = trapFocus(upper, { onescape: () => closedUpper++ });
	expect(document.activeElement).toBe(upper);
	key('Escape');
	expect([closedUpper, closedLower]).toEqual([1, 0]);
	// The focus cannot leave it for the page behind.
	opener.focus();
	expect(upper.contains(document.activeElement)).toBe(true);
	trapUpper.destroy();
	upper.remove();
	expect(lower.contains(document.activeElement)).toBe(true);

	key('Escape');
	expect(closedLower).toBe(1);
	trapLower.destroy();
	lower.remove();
	expect(document.activeElement).toBe(opener);
});

test('Escape inside a <dialog> opened above belongs to the dialog', () => {
	const node = sheet('<button>A</button>');
	let closed = 0;
	const trap = trapFocus(node, { onescape: () => closed++ });
	const dialog = document.createElement('dialog');
	dialog.setAttribute('open', '');
	dialog.innerHTML = '<button id="x">X</button>';
	document.body.append(dialog);
	(dialog.querySelector('#x') as HTMLElement).focus();
	expect(document.activeElement?.id).toBe('x');
	key('Escape');
	expect(closed).toBe(0);
	trap.destroy();
});
