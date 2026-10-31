import { sourceId, type TileId } from './tiles.ts';
import { systemFields, type SystemField } from './system-fields.ts';
import { tailscaleFields, type TailscaleField } from './tailscale.ts';
import { netbirdFields, type NetbirdField } from './netbird.ts';
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
	'surfaceOverride',
	'blur',
	'glow',
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
	'iconSize',
	'entryWidth',
	'nameLines',
	'showSummary',
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
	iconSize: number;
	entryWidth: number;
	nameLines: number;
	showSummary: boolean;
	collapsed: boolean;
	details: boolean;
	history: boolean;
	temperature: boolean;
	ipv4: boolean;
	ipv6: boolean;
	link: boolean;
	rates: boolean;
	interface: string;
	device: string;
	sensor: string;
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
	surfaceOverride: boolean;
	blur: number;
	glow: number;
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
	systemFields: SystemField[];
	tailscaleFields: TailscaleField[];
	netbirdFields: NetbirdField[];
	systemLayout: 'rows' | 'stacked';
	showLabels: boolean;
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
		iconSize: number('iconSize', 32, 20, 64),
		entryWidth: number('entryWidth', idSource === 'vms' ? 200 : 170, 120, 320),
		nameLines: number('nameLines', 2, 1, 2),
		showSummary: bool('showSummary', true),
		collapsed: bool('collapsed', false),
		details: bool(
			'details',
			!['network', 'system', 'igpu', 'npu', 'nvidia', 'accelerators'].includes(idSource),
		),
		history: bool('history', false),
		temperature: bool('temperature', true),
		ipv4: bool('ipv4', true),
		ipv6: bool('ipv6', true),
		link: bool('link', false),
		rates: bool('rates', true),
		interface: /^[a-zA-Z0-9_.:@-]{1,15}$/.test(text('interface', 15)) ? text('interface', 15) : '',
		device: /^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$/i.test(text('device', 12))
			? text('device', 12)
			: '',
		sensor: /^[a-f0-9]{32}$/.test(text('sensor', 32)) ? text('sensor', 32) : '',
		title: text('title', 80),
		showTitle: bool('showTitle', true),
		showIcon: bool('showIcon', true),
		icon: choice(
			'icon',
			EDITOR_ICONS,
			(
				{
					system: 'monitor',
					docker: 'grid',
					vms: 'monitor',
					memory: 'chip',
					volumes: 'disk',
					shares: 'folder',
					network: 'globe',
					tailscale: 'globe',
					netbird: 'globe',
					temperatures: 'activity',
				} as Record<string, (typeof EDITOR_ICONS)[number]>
			)[idSource] ?? 'chip',
		),
		titleSize: number('titleSize', 12, 11, 36),
		valueSize: number(
			'valueSize',
			[
				'system',
				'docker',
				'vms',
				'shares',
				'volumes',
				'disks',
				'temperatures',
				'tailscale',
				'netbird',
			].includes(idSource)
				? 14
				: 18,
			11,
			96,
		),
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
		opacity: number('opacity', 100, 10, 100),
		surfaceOverride: bool('surfaceOverride', typeof v.opacity === 'number' && v.opacity !== 100),
		blur: number('blur', 14, 0, 40),
		glow: number('glow', 18, 0, 100),
		radius: number('radius', 4, 0, 40),
		padding: number('padding', 12, 8, 40),
		gap: number('gap', 10, 2, 30),
		borderWidth: number('borderWidth', 1, 0, 4),
		shadow: number('shadow', 0, 0, 40),
		align: choice('align', align, 'left'),
		titleAlign: choice('titleAlign', align, 'left'),
		labelAlign: choice('labelAlign', align, 'left'),
		density: choice('density', ['compact', 'normal', 'comfortable'], 'normal'),
		display: choice('display', ['auto', 'value', 'bar', 'ring', 'history'], 'auto'),
		list: choice(
			'list',
			['list', 'grid'],
			['docker', 'vms', 'temperatures'].includes(idSource) ? 'grid' : 'list',
		),
		valueFont: choice(
			'valueFont',
			['mono', 'sans'],
			['docker', 'vms', 'shares'].includes(idSource) ? 'sans' : 'mono',
		),
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
		rows: number('rows', 100, 1, 100),
		systemFields: systemFields(v.systemFields),
		tailscaleFields: tailscaleFields(v.tailscaleFields),
		netbirdFields: netbirdFields(v.netbirdFields),
		systemLayout: choice('systemLayout', ['rows', 'stacked'], 'rows'),
		showLabels: bool('showLabels', true),
	};
}
/**
 * The CSS variables of a tile. Since 0.30.0 a tile has no design of its
 * own: corners, surface, glow, fonts and spacing come from Settings ›
 * Appearance like every other tile in the UI. Sizes are in rem, so the
 * font size chosen there scales them. Design values stored by older
 * versions stay in the preferences and are ignored here.
 */
export function tileStyle(o: TileOptions, id: TileId): string {
	const d = tileOptions(null, id);
	const rem = (px: number) => `${px / 16}rem`;
	const font = (v: string) =>
		v === 'mono' ? "'IBM Plex Mono',monospace" : "'IBM Plex Sans',sans-serif";
	return `--tile-icon-size:${o.iconSize}px;--tile-entry-width:${o.entryWidth}px;--tile-name-lines:${o.nameLines};--tile-title-size:${rem(d.titleSize)};--tile-value-size:${rem(d.valueSize)};--tile-label-size:${rem(d.labelSize)};--tile-title-weight:${d.titleWeight};--tile-value-weight:${d.valueWeight};--tile-label-weight:${d.labelWeight};--tile-title-color:var(--text);--tile-value-color:var(--text);--tile-label-color:var(--muted);--tile-value-font:${font(d.valueFont)};--tile-title-font:${font(d.titleFont)};--tile-label-font:${font(d.labelFont)};--tile-align:left;--tile-title-align:left;--tile-label-align:left;--tile-padding:var(--tile-pad);--tile-gap:var(--gap);--row-pad:var(--tile-row-gap);`;
}
