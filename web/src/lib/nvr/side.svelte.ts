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
 * The entries of the NVR's side bar for these rights. Unknown rights (not
 * read yet, or an administrator) leave the list as it is; without any right
 * "Live" stays, which then says that no camera is shared.
 */
export function nvrSideFor(side: SideItem[], rights: NvrRights | null): SideItem[] {
	if (!rights) return side;
	const out = side.filter((item) => {
		if (!('href' in item)) return true;
		const path = item.href.split('?')[0];
		if (path === '/nvr') return rights.live || !rights.recordings;
		if (RECORDING_PAGES.includes(path)) return rights.recordings;
		return true;
	});
	return out;
}

/** The rights of the signed-in account, read once in a while for the desktop's side bar. */
export const nvrRights = $state<{ value: NvrRights | null }>({ value: null });
let readAt = 0;
let reading = false;

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
