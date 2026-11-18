// 1.13: opens a page of a program from the agent's panel or chat – a draft
// in its editor ("Öffnen und bearbeiten"), a mail. In a window of the
// desktop the desktop opens it in the right window: an editor that is a
// task of its own (taskPage: the mail editor, the event window) with
// titanos-open-task, any other page of a program with titanos-open-app
// (checked there against the account's own programs). Outside the desktop
// the page goes there itself.
import { goto } from '$app/navigation';
import { inWindow, ownPath } from '../desktop/frame.ts';
import { taskPage } from '../desktop/apps.ts';

export function openInProgram(href: string) {
	if (!ownPath(href)) return;
	if (inWindow) {
		const type = taskPage(href) ? 'titanos-open-task' : 'titanos-open-app';
		window.parent.postMessage({ type, href }, location.origin);
		return;
	}
	void goto(href);
}
