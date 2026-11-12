// PLAN 79-7: the NVR's pages an account without the admin role sees in the
// side bar (window) and the phone's bar: "Live" only with the right to watch
// a camera live, "Aufnahmen" and "Ereignisse" only with the right to see
// recordings of one. The server checks every request anyway; this only
// leaves out pages that would say "nothing for you". Administrators see all.
import { api } from '../api.ts';
import type { SideItem } from '../desktop/apps.ts';
import type { CameraList } from './types.ts';

export type NvrRights = { live: boolean; recordings: boolean };

/** What the account may do on any camera; null while unknown. */
export function rightsOf(cameras: { rights?: Partial<NvrRights> }[]): NvrRights {
	return {
		live: cameras.some((c) => c.rights?.live === true),
		recordings: cameras.some((c) => c.rights?.recordings === true),
	};
}

const RECORDING_PAGES = ['/nvr/recordings', '/nvr/events'];

/**
 * Whether a page of the NVR is worth offering for these rights. Unknown
 * rights (not read yet, or an administrator) offer everything; without any
 * right "Live" stays, which then says that no camera is shared.
 */
export function nvrPageAllowed(href: string, rights: NvrRights | null): boolean {
	if (!rights) return true;
	const path = href.split(/[?#]/)[0];
	if (path === '/nvr') return rights.live || !rights.recordings;
	if (RECORDING_PAGES.includes(path)) return rights.recordings;
	return true;
}

/** The entries of the NVR's side bar for these rights (nvrPageAllowed). */
export function nvrSideFor(side: SideItem[], rights: NvrRights | null): SideItem[] {
	return side.filter((item) => !('href' in item) || nvrPageAllowed(item.href, rights));
}

/** The rights of the signed-in account, read once in a while for the desktop's side bar. */
export const nvrRights = $state<{ value: NvrRights | null }>({ value: null });
let readAt = 0;
let reading = false;

/** The rights for the desktop's menus: null for administrators and while unknown. */
export function desktopNvrRights(role: string): NvrRights | null {
	if (role !== 'user') return null;
	wantNvrRights();
	return nvrRights.value;
}

/** Reads the rights again when they are older than a minute (called while the side bar is drawn). */
export function wantNvrRights(): void {
	if (reading || Date.now() - readAt < 60_000) return;
	reading = true;
	readAt = Date.now();
	api.get<CameraList>('/nvr/cameras').then(
		(list) => {
			nvrRights.value = rightsOf(list.cameras ?? []);
			reading = false;
		},
		() => {
			// A NAS without the NVR, or no answer: all entries stay.
			nvrRights.value = null;
			reading = false;
		},
	);
}
