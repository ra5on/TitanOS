import type { Settings } from './api.ts';
export const colorKeys = [
	'background',
	'navigation',
	'card',
	'userBubble',
	'agentBubble',
	'text',
	'button',
	'accent',
	'dots',
	'trail',
	'orb',
] as const;
export type ColorKey = (typeof colorKeys)[number];
export const defaultAppearance = {
	theme: 'system',
	motion: 'full',
	dotSize: 10,
	colors: {},
} as const;
export function validColor(value: unknown): value is string {
	return typeof value === 'string' && /^#[0-9a-f]{6}$/i.test(value);
}
export function colorStyle(settings: Pick<Settings, 'colors' | 'dotSize'> | undefined): string {
	if (!settings) return '';
	const entries = colorKeys
		.filter((k) => validColor(settings.colors?.[k]))
		.map((k) => '--pa-' + k + ':' + settings.colors[k]);
	const accent = settings.colors?.accent;
	if (validColor(accent))
		entries.push(
			'--pa-on-accent:' +
				(contrast('#000000', accent) > contrast('#ffffff', accent) ? '#000000' : '#ffffff'),
		);
	entries.push('--ag-dot-size:' + Math.max(4, Math.min(48, settings.dotSize || 10)) + 'px');
	return entries.join(';');
}
export function rgb(value: string): number[] {
	const v = value.trim();
	if (/^#[0-9a-f]{6}$/i.test(v)) return [1, 3, 5].map((i) => parseInt(v.slice(i, i + 2), 16));
	const normalized = /^color\(srgb\s+([^)]+)\)$/.exec(v);
	const parts =
		(normalized?.[1] ?? v.replace(/^rgba?\(/, ''))
			.split('/')[0]
			.match(/[\d.]+%?/g)
			?.slice(0, 3) ?? [];
	return parts.map((c) =>
		Math.round(
			Math.min(255, Math.max(0, parseFloat(c) * (c.endsWith('%') ? 2.55 : normalized ? 255 : 1))),
		),
	);
}
export function contrast(a: string, b: string): number {
	const luminance = (color: string) => {
		const match = rgb(color);
		if (match.length !== 3) return NaN;
		const s = match
			.map((v) => v / 255)
			.map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
		return s[0] * 0.2126 + s[1] * 0.7152 + s[2] * 0.0722;
	};
	const l = luminance(a),
		r = luminance(b);
	return (Math.max(l, r) + 0.05) / (Math.min(l, r) + 0.05);
}
