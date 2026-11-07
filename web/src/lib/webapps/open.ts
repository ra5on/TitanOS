// PLAN 66: opening a file of the NAS in a web app – from the file manager in a
// window (the desktop opens the app's window), on the phone (the app full
// screen) or on the desktop itself.
import { goto } from '$app/navigation';
import { page } from '$app/state';
import { inWindow } from '../desktop/frame.ts';
import { mode } from '../desktop/mode.svelte.ts';
import { openProgram } from '../desktop/programs.svelte.ts';
import { webAppById } from './webapps.svelte.ts';

/** Opens the file in the web app `app` ("webapp:<id>"). */
export function openInWebApp(app: string, root: string, path: string) {
	if (!app.startsWith('webapp:')) return;
	if (inWindow) {
		window.parent.postMessage({ type: 'titanos-open-file', root, path, app }, location.origin);
		return;
	}
	if (!mode.wide) {
		const id = app.slice(7);
		if (!webAppById(id)) return;
		void goto(page.url.pathname + page.url.search, {
			state: { ...page.state, webapp: { id, root, path } },
			shallow: true,
		});
		return;
	}
	void openProgram(app, { doc: { root, path } });
}
