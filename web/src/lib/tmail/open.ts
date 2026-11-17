// 1.05: where writing an e-mail opens. On the desktop it gets a window of
// its own (the mail window stays as it is); on the phone and alone it is a
// page of its own.
import { goto } from '$app/navigation';
import { inWindow, type FrameMessage } from '../desktop/frame.ts';
import { query } from './api.ts';
import type { ComposeMode } from './types.ts';

export type ComposeStart = {
	/** The mailbox to write from. */
	a: string;
	mode?: ComposeMode | 'draft';
	/** The message answered, passed on or the draft continued. */
	f?: string;
	uid?: number;
	/** A first recipient (a link "write to"). */
	to?: string;
	/** The place of T-Mail to go back to on the phone. */
	back?: string;
};

export function composeHref(start: ComposeStart): string {
	return '/mail/compose' + query({ ...start, uid: start.uid ?? undefined });
}

export function openCompose(start: ComposeStart) {
	const href = composeHref(start);
	if (inWindow) {
		window.parent.postMessage(
			{ type: 'titanos-open-task', href } satisfies FrameMessage,
			location.origin,
		);
		return;
	}
	void goto(href);
}

/** The compose window is done (sent or thrown away): closes it, or goes back on the phone. */
export function closeCompose(back: string) {
	if (inWindow) {
		window.parent.postMessage({ type: 'titanos-close' } satisfies FrameMessage, location.origin);
		return;
	}
	void goto(back, { replace: true });
}
