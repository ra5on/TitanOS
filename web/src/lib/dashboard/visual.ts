export const BACKGROUND_IDS = [
	'nordlicht',
	'nebelwald',
	'bergsee',
	'kueste',
	'wueste',
	'weltraum',
	'graphit',
	'mitternacht',
	'wellen',
	'geometrie',
] as const;
export const MAX_IMAGES = 5;
export type PersonalImage = {
	id: string;
	name: string;
	width: number;
	height: number;
	bytes: number;
};
export type DashboardVisual = {
	transparency: number;
	blur: number;
	glow: number;
	glowColor: string;
	dim: number;
	motion: boolean;
	duration: number;
	images: PersonalImage[];
	background: {
		source: 'inherit' | 'builtin' | 'custom' | 'none';
		id: string;
		fit: 'contain' | 'cover';
		x: number;
		y: number;
		zoom: number;
		fixed: boolean;
		// Shown behind every page, not just the dashboard. Older app
		// versions drop the field and show it on the dashboard only.
		everywhere: boolean;
	};
};
const record = (v: unknown): Record<string, unknown> =>
	typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {};
const number = (v: unknown, fallback: number, min: number, max: number) =>
	typeof v === 'number' && Number.isFinite(v)
		? Math.max(min, Math.min(max, Math.round(v)))
		: fallback;
const hash = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{64}$/.test(v);
export function dashboardVisual(input: unknown): DashboardVisual {
	const v = record(input),
		b = record(v.background);
	const images: PersonalImage[] = [];
	for (const candidate of Array.isArray(v.images) ? v.images : []) {
		const i = record(candidate);
		if (hash(i.id) && !images.some((n) => n.id === i.id) && images.length < MAX_IMAGES)
			images.push({
				id: i.id,
				name:
					typeof i.name === 'string' ? i.name.replace(/[\x00-\x1f\x7f]/g, '').slice(0, 120) : '',
				width: number(i.width, 0, 0, 50000),
				height: number(i.height, 0, 0, 50000),
				bytes: number(i.bytes, 0, 0, 10 * 1024 * 1024),
			});
	}
	let source: DashboardVisual['background']['source'] = [
		'inherit',
		'builtin',
		'custom',
		'none',
	].includes(String(b.source))
		? (b.source as DashboardVisual['background']['source'])
		: 'inherit';
	let id = typeof b.id === 'string' ? b.id : '';
	if (source === 'builtin' && !BACKGROUND_IDS.includes(id as (typeof BACKGROUND_IDS)[number])) {
		source = 'inherit';
		id = '';
	}
	if (source === 'custom' && !images.some((i) => i.id === id)) {
		source = 'inherit';
		id = '';
	}
	return {
		transparency: number(v.transparency, 48, 0, 90),
		blur: number(v.blur, 14, 0, 40),
		glow: number(v.glow, 18, 0, 100),
		glowColor:
			typeof v.glowColor === 'string' && /^#[a-f0-9]{6}$/i.test(v.glowColor) ? v.glowColor : '',
		dim: number(v.dim, 15, 0, 80),
		motion: typeof v.motion === 'boolean' ? v.motion : true,
		duration: number(v.duration, 450, 150, 1000),
		images,
		background: {
			source,
			id,
			fit: b.fit === 'cover' ? 'cover' : 'contain',
			x: number(b.x, 50, 0, 100),
			y: number(b.y, 50, 0, 100),
			zoom: number(b.zoom, 100, 100, 200),
			fixed: b.fixed !== false,
			everywhere: b.everywhere === true,
		},
	};
}
export function backgroundURL(v: DashboardVisual, urls: Record<string, string> = {}): string {
	if (v.background.source === 'builtin') return '/backgrounds/' + v.background.id + '.webp';
	if (v.background.source === 'custom')
		return urls[v.background.id] || '/api/v1/background/' + v.background.id;
	return '';
}
