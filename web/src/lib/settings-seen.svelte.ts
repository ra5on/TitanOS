// Which settings areas with a "new" badge were already opened in this
// browser: the badge goes away after the first visit. Kept like the seen
// marker of the notifications, per browser and without the NAS. Without a
// visit it ends by itself after two minor versions or 30 days (stillNew).
import { settingsAreaOf, stillNew, type NewSince } from './settings-search.ts';
import { system } from './system.svelte.ts';

const SEEN_KEY = 'titanos.settings.seen';

function readSeen(): string[] {
	try {
		const value = JSON.parse(localStorage.getItem(SEEN_KEY) ?? '[]');
		return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string') : [];
	} catch {
		return [];
	}
}

export const seenAreas = $state<{ ids: string[] }>({ ids: readSeen() });

/** Whether an area still shows its "new" badge. */
export function showsNew(area: { id: string; newSince?: NewSince }): boolean {
	return (
		!!area.newSince &&
		!seenAreas.ids.includes(area.id) &&
		stillNew(area.newSince, system.info?.version, Date.now())
	);
}

/** Marks the area of a settings path as opened. */
export function markAreaSeen(pathname: string) {
	const id = settingsAreaOf(pathname);
	if (!id || seenAreas.ids.includes(id)) return;
	seenAreas.ids = [...seenAreas.ids, id];
	try {
		localStorage.setItem(SEEN_KEY, JSON.stringify(seenAreas.ids));
	} catch {
		// Without storage the badge simply stays.
	}
}
