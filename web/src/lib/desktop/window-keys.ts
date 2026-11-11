// The desktop's keys (Alt+Q switcher, Alt+W/M, Alt+arrows, Ctrl+Space) reach
// the desktop at once while the focus is in the window's page (same origin),
// before the page sees them; releasing Alt ends the switcher there too.
import { deskKey, menuKeyReset, screenOwnsKeys } from './menubar/keys.ts';
import { wm } from './wm.svelte.ts';

/** Hooks the desktop's keys into the page of a window's frame. */
export function hookDeskKeys(inner: Window | null | undefined) {
	if (!inner) return;
	const pass = (type: 'keydown' | 'keyup', e: KeyboardEvent) =>
		window.dispatchEvent(
			new KeyboardEvent(type, {
				key: e.key,
				code: e.code,
				altKey: e.altKey,
				ctrlKey: e.ctrlKey,
				shiftKey: e.shiftKey,
				metaKey: e.metaKey,
			}),
		);
	inner.addEventListener(
		'keydown',
		(e) => {
			// PLAN 67-1: Alt alone and F10 lead into the menu bar (not from a screen or terminal).
			if (e.key === 'Alt' || e.key === 'F10') {
				if (screenOwnsKeys(e.target) || (e.key === 'F10' && (e.shiftKey || e.ctrlKey))) {
					menuKeyReset();
					return;
				}
				if (e.key === 'F10') e.preventDefault();
				pass('keydown', e);
				return;
			}
			menuKeyReset();
			// Alt + K (show the desktop) stays with a screen or terminal (readline, guests).
			if (e.altKey && e.code === 'KeyK' && screenOwnsKeys(e.target)) return;
			const desk = !!deskKey(e) || ((e.ctrlKey || e.metaKey) && e.code === 'Space');
			if (!desk) return;
			e.preventDefault();
			e.stopImmediatePropagation();
			pass('keydown', e);
		},
		true,
	);
	inner.addEventListener(
		'keyup',
		(e) => (wm.switcher || e.key === 'Alt') && pass('keyup', e),
		true,
	);
	inner.addEventListener('pointerdown', menuKeyReset, true);
}
