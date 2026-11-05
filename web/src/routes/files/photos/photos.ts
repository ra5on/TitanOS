// Photos in the file manager (PLAN 52): types and small helpers.

export type PhotoItem = {
	root: string;
	path: string;
	kind: 'image' | 'video';
	size: number;
	modified: string;
	taken?: string;
	lat?: number;
	lon?: number;
	gps?: boolean;
	camera?: string;
	desc?: string;
	failed?: boolean;
};

export type PhotoPrefs = {
	folders: { root: string; path: string }[];
	describe: 'manual' | 'night' | 'idle';
	from: number;
	to: number;
	map?: boolean;
	started?: boolean;
};

export type PhotosAnswer = {
	prefs: PhotoPrefs;
	items: PhotoItem[];
	described: number;
	scanning: boolean;
	scanned?: string;
	describe: { running: boolean; done: number; total: number; error?: string; model?: string };
	ai: boolean;
};

export type PhotoAlbum = {
	id: string;
	name: string;
	items: string[];
	members?: string[];
	owner?: string;
	created: string;
};

export const photoKey = (p: { root: string; path: string }) => `${p.root}:${p.path}`;

/** The date of the timeline: taken (EXIF), else changed. */
export const photoDate = (p: PhotoItem) => new Date(p.taken || p.modified);

export const photoName = (p: { path: string }) => p.path.split('/').pop() ?? p.path;

/** Groups photos (already newest first) by month: "2026-10" → items. */
export function byMonth(items: PhotoItem[]): { key: string; date: Date; items: PhotoItem[] }[] {
	const groups: { key: string; date: Date; items: PhotoItem[] }[] = [];
	for (const p of items) {
		const d = photoDate(p);
		const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
		const last = groups.at(-1);
		if (last?.key === key) last.items.push(p);
		else groups.push({ key, date: new Date(d.getFullYear(), d.getMonth(), 1), items: [p] });
	}
	return groups;
}

/** Photos with GPS grouped into places of about 10 km, biggest first. */
export function byPlace(items: PhotoItem[]): { lat: number; lon: number; items: PhotoItem[] }[] {
	const map = new Map<string, { lat: number; lon: number; items: PhotoItem[] }>();
	for (const p of items) {
		if (!p.gps || p.lat === undefined || p.lon === undefined) continue;
		const key = `${Math.round(p.lat * 10)}:${Math.round(p.lon * 10)}`;
		const g = map.get(key) ?? { lat: 0, lon: 0, items: [] };
		g.items.push(p);
		g.lat += (p.lat - g.lat) / g.items.length;
		g.lon += (p.lon - g.lon) / g.items.length;
		map.set(key, g);
	}
	return [...map.values()].sort((a, b) => b.items.length - a.items.length);
}

/** "54,96° N · 8,13° O" in the language of the page. */
export function placeText(lat: number, lon: number, locale: 'de' | 'en'): string {
	const n = (v: number) =>
		Math.abs(v)
			.toFixed(2)
			.replace('.', locale === 'de' ? ',' : '.');
	const ns = lat >= 0 ? 'N' : 'S';
	const ew = lon >= 0 ? (locale === 'de' ? 'O' : 'E') : 'W';
	return `${n(lat)}° ${ns} · ${n(lon)}° ${ew}`;
}

// Web Mercator for the map: pixel coordinates at zoom z (256 px tiles).
export function project(lat: number, lon: number, z: number): { x: number; y: number } {
	const scale = 256 * 2 ** z;
	const s = Math.sin((Math.max(-85, Math.min(85, lat)) * Math.PI) / 180);
	return {
		x: ((lon + 180) / 360) * scale,
		y: (0.5 - Math.log((1 + s) / (1 - s)) / (4 * Math.PI)) * scale,
	};
}

/** The highest zoom (up to 16) at which all points fit into w×h pixels. */
export function fitZoom(points: { lat: number; lon: number }[], w: number, h: number): number {
	if (points.length <= 1) return 12;
	for (let z = 16; z > 1; z--) {
		const ps = points.map((p) => project(p.lat, p.lon, z));
		const xs = ps.map((p) => p.x);
		const ys = ps.map((p) => p.y);
		if (Math.max(...xs) - Math.min(...xs) < w * 0.8 && Math.max(...ys) - Math.min(...ys) < h * 0.8)
			return z;
	}
	return 2;
}

/** The reverse of project. */
export function unproject(x: number, y: number, z: number): { lat: number; lon: number } {
	const scale = 256 * 2 ** z;
	const lon = (x / scale) * 360 - 180;
	const n = Math.PI - (2 * Math.PI * y) / scale;
	const lat = (180 / Math.PI) * Math.atan(0.5 * (Math.exp(n) - Math.exp(-n)));
	return { lat, lon };
}
