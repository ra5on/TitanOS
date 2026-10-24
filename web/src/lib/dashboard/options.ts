import { sourceId, type TileId } from './tiles.ts';
export const PARTS = ['value', 'history', 'temperature', 'details', 'engines'] as const;
export const STYLE_KEYS = [
	'titleSize',
	'valueSize',
	'labelSize',
	'titleWeight',
	'valueWeight',
	'labelWeight',
	'titleColor',
	'valueColor',
	'labelColor',
	'accent',
	'background',
	'borderColor',
	'opacity',
	'radius',
	'padding',
	'gap',
	'borderWidth',
	'shadow',
	'align',
	'titleAlign',
	'labelAlign',
	'density',
	'display',
	'list',
	'showTitle',
	'showIcon',
	'icon',
	'valueFont',
	'titleFont',
	'labelFont',
] as const;
export const EDITOR_ICONS = [
	'chip',
	'monitor',
	'activity',
	'disk',
	'globe',
	'folder',
	'grid',
	'terminal',
	'palette',
	'archive',
] as const;
export type TileOptions = {
	details: boolean;
	history: boolean;
	temperature: boolean;
	ipv4: boolean;
	ipv6: boolean;
	link: boolean;
	rates: boolean;
	interface: string;
	device: string;
	title: string;
	showTitle: boolean;
	showIcon: boolean;
	icon: (typeof EDITOR_ICONS)[number];
	titleSize: number;
	valueSize: number;
	labelSize: number;
	titleWeight: number;
	valueWeight: number;
	labelWeight: number;
	titleColor: string;
	valueColor: string;
	labelColor: string;
	accent: string;
	background: string;
	borderColor: string;
	opacity: number;
	radius: number;
	padding: number;
	gap: number;
	borderWidth: number;
	shadow: number;
	align: 'left' | 'center' | 'right';
	titleAlign: 'left' | 'center' | 'right';
	labelAlign: 'left' | 'center' | 'right';
	density: 'compact' | 'normal' | 'comfortable';
	display: 'auto' | 'value' | 'bar' | 'ring' | 'history';
	list: 'list' | 'grid';
	valueFont: 'mono' | 'sans';
	titleFont: 'mono' | 'sans';
	labelFont: 'mono' | 'sans';
	parts: (typeof PARTS)[number][];
	hiddenFields: string[];
	items: string[] | null;
	runningOnly: boolean;
	rows: number;
};
const hex = (v: unknown) => (typeof v === 'string' && /^#[0-9a-f]{6}$/i.test(v) ? v : '');
export function tileOptions(input: unknown, id: TileId): TileOptions {
	const raw = (input as Record<string, unknown> | null)?.[id];
	const v = (raw && typeof raw === 'object' ? raw : {}) as Record<string, unknown>;
	const bool = (k: string, d: boolean) => (typeof v[k] === 'boolean' ? (v[k] as boolean) : d);
	const number = (k: string, d: number, min: number, max: number) =>
		typeof v[k] === 'number' && Number.isFinite(v[k])
			? Math.max(min, Math.min(max, Math.round(v[k] as number)))
			: d;
	const choice = <T extends string>(k: string, values: readonly T[], d: T): T =>
		values.includes(v[k] as T) ? (v[k] as T) : d;
	const text = (k: string, max: number) =>
		typeof v[k] === 'string' ? (v[k] as string).replace(/[\x00-\x1f\x7f]/g, '').slice(0, max) : '';
	const color = (k: string) => hex(v[k]);
	const align = ['left', 'center', 'right'] as const;
	const idSource = sourceId(id);
	const parts = [
		...new Set([
			...(Array.isArray(v.parts)
				? v.parts.filter((p): p is (typeof PARTS)[number] =>
						PARTS.includes(p as (typeof PARTS)[number]),
					)
				: []),
			...PARTS,
		]),
	];
	return {
		details: bool(
			'details',
			!['cpu', 'network', 'system', 'igpu', 'npu', 'nvidia'].includes(idSource),
		),
		history: bool('history', ['cpu', 'npu'].includes(idSource)),
		temperature: bool('temperature', true),
		ipv4: bool('ipv4', true),
		ipv6: bool('ipv6', true),
		link: bool('link', false),
		rates: bool('rates', true),
		interface: /^[a-zA-Z0-9_.:@-]{1,15}$/.test(text('interface', 15)) ? text('interface', 15) : '',
		device: /^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$/i.test(text('device', 12))
			? text('device', 12)
			: '',
		title: text('title', 80),
		showTitle: bool('showTitle', true),
		showIcon: bool('showIcon', false),
		icon: choice('icon', EDITOR_ICONS, 'chip'),
		titleSize: number('titleSize', 13, 11, 36),
		valueSize: number('valueSize', 36, 16, 96),
		labelSize: number('labelSize', 12, 11, 28),
		titleWeight: number('titleWeight', 500, 400, 600),
		valueWeight: number('valueWeight', 400, 400, 600),
		labelWeight: number('labelWeight', 400, 400, 600),
		titleColor: color('titleColor'),
		valueColor: color('valueColor'),
		labelColor: color('labelColor'),
		accent: color('accent'),
		background: color('background'),
		borderColor: color('borderColor'),
		opacity: number('opacity', 100, 35, 100),
		radius: number('radius', 12, 0, 40),
		padding: number('padding', 18, 8, 40),
		gap: number('gap', 10, 2, 30),
		borderWidth: number('borderWidth', 1, 0, 4),
		shadow: number('shadow', 0, 0, 40),
		align: choice('align', align, 'left'),
		titleAlign: choice('titleAlign', align, 'left'),
		labelAlign: choice('labelAlign', align, 'left'),
		density: choice('density', ['compact', 'normal', 'comfortable'], 'normal'),
		display: choice('display', ['auto', 'value', 'bar', 'ring', 'history'], 'auto'),
		list: choice('list', ['list', 'grid'], idSource === 'docker' ? 'grid' : 'list'),
		valueFont: choice('valueFont', ['mono', 'sans'], 'mono'),
		titleFont: choice('titleFont', ['mono', 'sans'], 'sans'),
		labelFont: choice('labelFont', ['mono', 'sans'], 'sans'),
		parts,
		hiddenFields: Array.isArray(v.hiddenFields)
			? [
					...new Set(
						v.hiddenFields.filter(
							(f): f is string => typeof f === 'string' && /^[a-zA-Z0-9_-]{1,40}$/.test(f),
						),
					),
				].slice(0, 40)
			: [],
		items: Array.isArray(v.items)
			? [
					...new Set(
						v.items.filter(
							(f): f is string =>
								typeof f === 'string' && f.length <= 128 && !/[\x00-\x1f\x7f]/.test(f),
						),
					),
				].slice(0, 1000)
			: null,
		runningOnly: bool('runningOnly', false),
		rows: number('rows', 12, 1, 100),
	};
}
export function tileStyle(o: TileOptions): string {
	const font = (v: string) =>
		v === 'mono' ? "'IBM Plex Mono',monospace" : "'IBM Plex Sans',sans-serif";
	return `--tile-title-size:${o.titleSize}px;--tile-value-size:${o.valueSize}px;--tile-label-size:${o.labelSize}px;--tile-title-weight:${o.titleWeight};--tile-value-weight:${o.valueWeight};--tile-label-weight:${o.labelWeight};--tile-title-color:${o.titleColor || 'var(--text)'};--tile-value-color:${o.valueColor || 'var(--text)'};--tile-label-color:${o.labelColor || 'var(--muted)'};--tile-value-font:${font(o.valueFont)};--tile-title-font:${font(o.titleFont)};--tile-label-font:${font(o.labelFont)};--tile-align:${o.align};--tile-title-align:${o.titleAlign};--tile-label-align:${o.labelAlign};--tile-padding:${o.padding}px;--tile-gap:${o.gap}px;--tile-row-gap:${o.density === 'compact' ? 6 : o.density === 'comfortable' ? 16 : 10}px;--tile-radius:${o.radius}px;--tile-border:${o.borderWidth}px;--tile-border-color:${o.borderColor || 'var(--border)'};--tile-background:color-mix(in srgb,${o.background || 'var(--surface)'} ${o.opacity}%,transparent);--tile-shadow:0 ${Math.round(o.shadow / 3)}px ${o.shadow}px rgb(0 0 0 / ${o.shadow ? '.2' : '0'});${o.accent ? `--accent:${o.accent};` : ''}`;
}
export function transferStyle(from: TileOptions, to: TileOptions): TileOptions {
	const next = { ...to };
	for (const key of STYLE_KEYS) (next as Record<string, unknown>)[key] = from[key];
	return next;
}
