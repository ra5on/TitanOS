// PLAN 76 (F-12-7): a page inside a window of the desktop tells the window
// that it holds work that closing would lose (changed fields, a running
// upload). The window then asks before it closes, like for a web app.
import { inWindow, type FrameMessage } from './frame.ts';

const sources = new Map<symbol, 'edits' | 'busy'>();
let told = '';

function tell() {
	if (!inWindow) return;
	const kinds = [...sources.values()];
	const state = kinds.includes('edits') ? 'edits' : kinds.includes('busy') ? 'busy' : '';
	if (state === told) return;
	told = state;
	window.parent.postMessage(
		{ type: 'titanos-unsaved', state } satisfies FrameMessage,
		location.origin,
	);
}

/**
 * Call while a component is set up. `unsaved` is read reactively: 'edits'
 * for changes that are not saved, 'busy' for work that would be cut off.
 */
export function guardWindowClose(unsaved: () => 'edits' | 'busy' | '' | boolean) {
	if (!inWindow) return;
	const key = Symbol();
	$effect(() => {
		const value = unsaved();
		const kind = value === true ? 'edits' : value || '';
		if (kind) sources.set(key, kind);
		else sources.delete(key);
		tell();
		return () => {
			sources.delete(key);
			tell();
		};
	});
}
