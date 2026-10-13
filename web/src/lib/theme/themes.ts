// Themes, accents and the appearance settings of an account.
// Values follow docs/design/Appearance.dc.html. The identifiers (graphit,
// blau, kompakt, …) are part of the exported theme files; do not rename them.

import { contrast, darkenFor, isHex, luminance, shade, withAlpha, type Hex } from './color.ts';

export type Palette = {
	bg: Hex;
	surface: Hex;
	header: Hex;
	border: Hex;
	text: Hex;
	muted: Hex;
	track: Hex;
	chip: Hex;
	dark: boolean;
};

export const THEMES = {
	graphit: { bg: '#0E1114', surface: '#161A1F', header: '#12161A', border: '#232A31', text: '#E6E9ED', muted: '#9AA3AD', track: '#232A31', chip: '#1E252C', dark: true },
	schwarz: { bg: '#000000', surface: '#0E0E0F', header: '#080808', border: '#242426', text: '#EDEDED', muted: '#A3A3A9', track: '#1F1F22', chip: '#1A1A1C', dark: true },
	mitternacht: { bg: '#0A0F1E', surface: '#111933', header: '#0D1428', border: '#232D4D', text: '#E6EAF5', muted: '#9DA7C6', track: '#232D4D', chip: '#1A2442', dark: true },
	hell: { bg: '#F3F4F6', surface: '#FFFFFF', header: '#FFFFFF', border: '#DCE0E5', text: '#14181D', muted: '#56606B', track: '#E3E7EC', chip: '#EAEDF1', dark: false }
} satisfies Record<string, Palette>;

export const THEME_IDS = ['graphit', 'schwarz', 'mitternacht', 'hell', 'auto', 'eigen'] as const;
export type ThemeId = (typeof THEME_IDS)[number];

/** Accent colors; `dark` is the variant for light themes (contrast at least 3:1). */
export const ACCENTS = {
	blau: { hex: '#6CB6FF', dark: '#1F6FC4' },
	gruen: { hex: '#4CC38A', dark: '#1E7A4E' },
	tuerkis: { hex: '#3FCFC7', dark: '#127A75' },
	violett: { hex: '#B49CFF', dark: '#6247C9' },
	kupfer: { hex: '#E0895A', dark: '#A8521F' },
	rose: { hex: '#F28CB0', dark: '#B33A68' }
} satisfies Record<string, { hex: Hex; dark: Hex }>;

export const ACCENT_IDS = ['blau', 'gruen', 'tuerkis', 'violett', 'kupfer', 'rose'] as const;
export type AccentId = (typeof ACCENT_IDS)[number] | 'custom';

export const SURFACE_STYLES = ['flat', 'glass', 'outline'] as const;
export type SurfaceStyle = (typeof SURFACE_STYLES)[number];

export const BACKGROUNDS = ['solid', 'shapes', 'image'] as const;
export type Background = (typeof BACKGROUNDS)[number];

export const DENSITIES = ['normal', 'kompakt'] as const;
export type Density = (typeof DENSITIES)[number];

export type CustomColors = { bg: Hex; surface: Hex; header: Hex; text: Hex; border: Hex };

export type Appearance = {
	theme: ThemeId;
	accent: AccentId;
	customAccent: Hex;
	style: SurfaceStyle;
	/** Transparency of glass surfaces in percent, 0–90. */
	glass: number;
	/** Blur behind glass surfaces in px, 0–40. */
	blur: number;
	/** Strength of the bright top edge of glass surfaces in percent, 0–60. */
	shine: number;
	/** Corner radius of tiles and windows in px, 0–32. */
	rTile: number;
	/** Corner radius of buttons, fields and menus in px, 0–22. */
	rCtl: number;
	/** Font size in percent, 90–120. */
	font: number;
	density: Density;
	bg: Background;
	/** Darkening of the background image in percent, 0–80. */
	dim: number;
	/** Changes whenever a new background image is uploaded; 0 = none uploaded. */
	bgVersion: number;
	custom: CustomColors;
};

export const LIMITS = {
	glass: { min: 0, max: 90, step: 5 },
	blur: { min: 0, max: 40, step: 2 },
	shine: { min: 0, max: 60, step: 5 },
	rTile: { min: 0, max: 32, step: 2 },
	rCtl: { min: 0, max: 22, step: 1 },
	font: { min: 90, max: 120, step: 5 },
	dim: { min: 0, max: 80, step: 5 }
} as const;

export function defaultAppearance(): Appearance {
	return {
		theme: 'graphit',
		accent: 'blau',
		customAccent: '#E5B92E',
		style: 'glass',
		glass: 45,
		blur: 18,
		shine: 20,
		rTile: 14,
		rCtl: 10,
		font: 100,
		density: 'normal',
		bg: 'shapes',
		dim: 30,
		bgVersion: 0,
		custom: { bg: '#101418', surface: '#1A2027', header: '#13181D', text: '#ECEFF3', border: '#2A333D' }
	};
}

function oneOf<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
	return allowed.includes(value as T) ? (value as T) : fallback;
}

function inRange(value: unknown, limit: { min: number; max: number }, fallback: number): number {
	if (typeof value !== 'number' || !Number.isFinite(value)) return fallback;
	return Math.min(limit.max, Math.max(limit.min, Math.round(value)));
}

function hex(value: unknown, fallback: Hex): Hex {
	return isHex(value) ? value.toUpperCase() : fallback;
}

/**
 * Turns anything (stored settings, an imported theme file) into valid
 * settings. Unknown or broken values fall back to the defaults.
 */
export function sanitizeAppearance(input: unknown): Appearance {
	const d = defaultAppearance();
	if (typeof input !== 'object' || input === null) return d;
	const v = input as Record<string, unknown>;
	const custom = (typeof v.custom === 'object' && v.custom !== null ? v.custom : {}) as Record<string, unknown>;
	return {
		theme: oneOf(v.theme, THEME_IDS, d.theme),
		accent: oneOf(v.accent, [...ACCENT_IDS, 'custom'] as const, d.accent),
		customAccent: hex(v.customAccent, d.customAccent),
		style: oneOf(v.style, SURFACE_STYLES, d.style),
		glass: inRange(v.glass, LIMITS.glass, d.glass),
		blur: inRange(v.blur, LIMITS.blur, d.blur),
		shine: inRange(v.shine, LIMITS.shine, d.shine),
		rTile: inRange(v.rTile, LIMITS.rTile, d.rTile),
		rCtl: inRange(v.rCtl, LIMITS.rCtl, d.rCtl),
		font: inRange(v.font, LIMITS.font, d.font),
		density: oneOf(v.density, DENSITIES, d.density),
		bg: oneOf(v.bg, BACKGROUNDS, d.bg),
		dim: inRange(v.dim, LIMITS.dim, d.dim),
		bgVersion: typeof v.bgVersion === 'number' && v.bgVersion > 0 ? Math.floor(v.bgVersion) : 0,
		custom: {
			bg: hex(custom.bg, d.custom.bg),
			surface: hex(custom.surface, d.custom.surface),
			header: hex(custom.header, d.custom.header),
			text: hex(custom.text, d.custom.text),
			border: hex(custom.border, d.custom.border)
		}
	};
}

/** The colors of a theme. `systemLight` decides what "auto" means right now. */
export function resolvePalette(a: Appearance, systemLight: boolean): Palette {
	if (a.theme === 'eigen') {
		const c = a.custom;
		const dark = luminance(c.bg) < 0.4;
		return {
			bg: c.bg,
			surface: c.surface,
			header: c.header,
			border: c.border,
			text: c.text,
			// Muted text and chips are derived, not picked.
			muted: dark ? shade(c.text, -0.32) : shade(c.text, 0.38),
			track: c.border,
			chip: dark ? shade(c.surface, 0.06) : shade(c.surface, -0.06),
			dark
		};
	}
	if (a.theme === 'auto') return systemLight ? THEMES.hell : THEMES.graphit;
	return THEMES[a.theme];
}

/** The accent as picked, before it is adapted to the theme. */
export function pickedAccent(a: Appearance): Hex {
	return a.accent === 'custom' ? a.customAccent : ACCENTS[a.accent].hex;
}

/** The accent as used: darkened on light themes until it is visible (3:1). */
export function resolveAccent(a: Appearance, palette: Palette): Hex {
	if (palette.dark) return pickedAccent(a);
	if (a.accent !== 'custom') return ACCENTS[a.accent].dark;
	return darkenFor(a.customAccent, palette.bg, 3);
}

/** Opacity of glass surfaces. Limited so that text stays readable. */
export function glassAlpha(a: Appearance, palette: Palette): number {
	const alpha = 1 - a.glass / 100;
	// On a plain background there is nothing behind the glass that could
	// disturb the text, so no limit is needed there.
	if (a.bg === 'solid') return alpha;
	return Math.max(alpha, palette.dark ? 0.35 : 0.5);
}

/** Contrast of normal text on surfaces; below 4.5 the editor warns. */
export function textContrast(palette: Palette): number {
	return contrast(palette.text, palette.surface);
}

const ON_ACCENT_DARK: Hex = '#0B1622';
const ON_ACCENT_LIGHT: Hex = '#FFFFFF';

/**
 * All CSS variables for the given settings. Set them on <html> for the whole
 * UI, or on any element to preview settings inside it.
 */
export function cssVariables(a: Appearance, systemLight: boolean): Record<string, string> {
	const p = resolvePalette(a, systemLight);
	const accent = resolveAccent(a, p);
	const alpha = glassAlpha(a, p);
	const shine = a.shine / 100;
	return {
		'--bg': p.bg,
		'--surface': p.surface,
		'--header': p.header,
		'--border': p.border,
		'--text': p.text,
		'--muted': p.muted,
		'--track': p.track,
		'--chip': p.chip,
		'--accent': accent,
		'--on-accent': contrast(accent, ON_ACCENT_DARK) >= 4.5 ? ON_ACCENT_DARK : ON_ACCENT_LIGHT,
		'--surface-alpha': alpha.toFixed(2),
		'--glass-blur': `${a.blur}px`,
		'--glass-shine': shine.toFixed(2),
		'--radius-tile': `${a.rTile}px`,
		'--radius-ctl': `${a.rCtl}px`,
		'--font-scale': (a.font / 100).toFixed(2),
		// Derived values the surface styles in app.css are built from.
		'--glass-surface': withAlpha(p.surface, alpha),
		'--glass-menu': withAlpha(p.surface, Math.max(alpha, 0.7)),
		'--glass-border': p.dark ? 'rgba(255, 255, 255, 0.12)' : 'rgba(255, 255, 255, 0.7)',
		'--glass-shine-color': `rgba(255, 255, 255, ${(p.dark ? shine * 0.5 : 0.4 + shine * 0.5).toFixed(2)})`,
		'--glass-shadow': `rgba(0, 0, 0, ${p.dark ? 0.3 : 0.08})`,
		'--outline-border': p.dark ? shade(p.border, 0.12) : shade(p.border, -0.15),
		'--overlay': withAlpha(p.dark ? p.bg : p.text, 0.7),
		'--bg-dim': (a.dim / 100).toFixed(2)
	};
}

/** The data attributes that go with cssVariables(). */
export function dataAttributes(a: Appearance, systemLight: boolean): Record<string, string> {
	return {
		'data-theme': a.theme,
		'data-mode': resolvePalette(a, systemLight).dark ? 'dark' : 'light',
		'data-surface': a.style,
		'data-density': a.density === 'kompakt' ? 'compact' : 'normal'
	};
}
