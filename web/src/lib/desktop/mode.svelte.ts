// PLAN 61: which frame TitanOS shows. From 900 px width the desktop with
// windows; below the phone's home grid (a tablet held upright, too). A page
// inside a window of the desktop shows only itself.
import { inWindow } from './frame.ts';

const QUERY = '(min-width: 900px)';

export const mode = $state<{ wide: boolean }>({
	wide: typeof matchMedia === 'function' ? matchMedia(QUERY).matches : true,
});

if (typeof matchMedia === 'function') {
	matchMedia(QUERY).addEventListener('change', (e) => (mode.wide = e.matches));
}

export { inWindow };
