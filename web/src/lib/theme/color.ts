// Color math for the theme system. Colors are "#rrggbb" strings.

export type Hex = string;

const HEX = /^#[0-9a-f]{6}$/i;

export function isHex(value: unknown): value is Hex {
	return typeof value === 'string' && HEX.test(value);
}

function toRgb(hex: Hex): [number, number, number] {
	const n = parseInt(hex.slice(1), 16);
	return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function toHex(rgb: number[]): Hex {
	return '#' + rgb.map((v) => Math.round(v).toString(16).padStart(2, '0')).join('');
}

/** Relative luminance as defined by WCAG. */
export function luminance(hex: Hex): number {
	const [r, g, b] = toRgb(hex).map((v) => {
		const c = v / 255;
		return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
	});
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/** WCAG contrast ratio, 1 (none) to 21 (black on white). */
export function contrast(a: Hex, b: Hex): number {
	const x = luminance(a);
	const y = luminance(b);
	return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

/** Moves a color towards white (amount > 0) or black (amount < 0), amount in -1..1. */
export function shade(hex: Hex, amount: number): Hex {
	return toHex(toRgb(hex).map((v) => v + (amount > 0 ? (255 - v) * amount : v * amount)));
}

/** Darkens a color until it has at least the wanted contrast against a background. */
export function darkenFor(hex: Hex, background: Hex, minContrast: number): Hex {
	let color = hex;
	for (let i = 0; i < 40 && contrast(color, background) < minContrast; i++) {
		color = shade(color, -0.05);
	}
	return color;
}

/** "rgba(r, g, b, a)" for a hex color. */
export function withAlpha(hex: Hex, alpha: number): string {
	const [r, g, b] = toRgb(hex);
	return `rgba(${r}, ${g}, ${b}, ${alpha.toFixed(2)})`;
}

/** The same color with its hue turned by `degrees` (HSL), for a two-color gradient. */
export function rotateHue(hex: Hex, degrees: number): Hex {
	const [r, g, b] = toRgb(hex).map((v) => v / 255);
	const max = Math.max(r, g, b);
	const min = Math.min(r, g, b);
	const l = (max + min) / 2;
	const d = max - min;
	if (d === 0) return hex;
	const s = d / (1 - Math.abs(2 * l - 1));
	let h = max === r ? ((g - b) / d) % 6 : max === g ? (b - r) / d + 2 : (r - g) / d + 4;
	h = (h * 60 + degrees + 360) % 360;
	const c = (1 - Math.abs(2 * l - 1)) * s;
	const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
	const m = l - c / 2;
	const [r1, g1, b1] = h < 60 ? [c, x, 0] : h < 120 ? [x, c, 0] : h < 180 ? [0, c, x] : h < 240 ? [0, x, c] : h < 300 ? [x, 0, c] : [c, 0, x];
	return toHex([r1, g1, b1].map((v) => (v + m) * 255));
}
