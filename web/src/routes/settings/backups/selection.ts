// What a backup job saves, as the page shows it: one list of folders, each a
// volume and a path on it ('' = the whole volume). Shares are folders at the
// top of their volume. The job stores shares (sources, with folders inside
// them), whole volumes and places (folders outside shares), as before.

export type Chosen = { volume: string; path: string };
export type Share = { name: string; volume: string };

/** The share a folder belongs to, and the rest of its path inside it. */
export function shareOfIn(shares: Share[], c: Chosen): { name: string; rest: string } | null {
	if (!c.path) return null;
	const [top, ...rest] = c.path.split('/');
	return shares.some((s) => s.volume === c.volume && s.name === top)
		? { name: top, rest: rest.join('/') }
		: null;
}

/** The chosen folders as the job stores them. */
export function selectionOfIn(shares: Share[], list: Chosen[]) {
	const picked = list.filter((c) => c.volume);
	const sources: string[] = [];
	const folders: Record<string, string[]> = {};
	const volumes: string[] = [];
	const places: string[] = [];
	const whole = new Set<string>();
	for (const c of picked) {
		const share = shareOfIn(shares, c);
		if (!c.path) {
			if (!volumes.includes(c.volume)) volumes.push(c.volume);
		} else if (share && !share.rest) whole.add(share.name);
	}
	for (const c of picked) {
		// What a whole volume or a whole share already covers is not listed twice.
		if (!c.path || volumes.includes(c.volume)) continue;
		const share = shareOfIn(shares, c);
		if (share) {
			if (!sources.includes(share.name)) sources.push(share.name);
			if (share.rest && !whole.has(share.name)) {
				const list = (folders[share.name] ??= []);
				if (!list.includes(share.rest)) list.push(share.rest);
			}
		} else {
			const place = `${c.volume}/${c.path}`;
			if (!places.includes(place)) places.push(place);
		}
	}
	return { sources, folders, volumes, places };
}
