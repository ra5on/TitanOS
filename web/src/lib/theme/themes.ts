// Themes, accents and the appearance settings of an account.
// Values follow docs/design/Appearance.dc.html. The identifiers (graphit,
// blau, kompakt, …) are part of the exported theme files; do not rename them.

import { contrast, darkenFor, isHex, luminance, rotateHue, shade, withAlpha, type Hex } from './color.ts';

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

/**
 * Optional effects (Settings › Appearance › Effects). `enabled` is the main
 * switch; the others only count while it is on. Movement always stops for
 * `prefers-reduced-motion`, whatever is set here.
 */
export type Effects = {
	enabled: boolean;
	/** Content fades over when the page changes; header and navigation stay. */
	transitions: boolean;
	/** Placeholder bars instead of "Loading…" while a page fetches its data. */
	skeleton: boolean;
	/** Small movements: pressed buttons, opening dialogs and menus, busy bars. */
	micro: boolean;
	/** Warnings and errors pulse gently (they always say so in text as well). */
	pulse: boolean;
	/** Accent as a gradient of two neighbouring hues instead of one color. */
	gradient: boolean;
	/** Drawings on pages that have nothing to show yet. */
	illustrations: boolean;
	/** "?" opens a list of the keyboard shortcuts. */
	shortcuts: boolean;
};

export const EFFECT_IDS = ['transitions', 'skeleton', 'micro', 'pulse', 'gradient', 'illustrations', 'shortcuts'] as const;
export type EffectId = (typeof EFFECT_IDS)[number];

/**
 * Pop-up surfaces (Settings › Appearance › Menus and windows). With `own`
 * off they follow the tiles; with it on they use their own glass values.
 * Only used with the glass style; flat and outline stay opaque.
 */
export type FloatingSurface = {
	own: boolean;
	/** Transparency in percent, 0–90 (limited like tiles, see floatingAlpha). */
	glass: number;
	/** Blur in px, 0–40. */
	blur: number;
	/** Shine edge in percent, 0–60. */
	shine: number;
};

export type DialogSurface = FloatingSurface & {
	/** Dimming of the page behind a dialog in percent, 0–90; also without own values. */
	dim: number;
};

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
	/** Glow around tiles in percent, 0–100; 0 = none. */
	glow: number;
	/** Color of the glow; empty = the accent color. */
	glowColor: Hex | '';
	/** Whether bars, rings and histories move smoothly to new values. */
	motion: boolean;
	/** Length of that movement in ms, 150–1000. */
	duration: number;
	fx: Effects;
	/** Menus: account menu, dropdowns, context menus, mobile navigation, palette, bell, terminal "+". */
	menus: FloatingSurface;
	/** Dialogs (Dialog.svelte) and the dimming behind them. */
	dialogs: DialogSurface;
};

export const LIMITS = {
	glass: { min: 0, max: 90, step: 5 },
	blur: { min: 0, max: 40, step: 2 },
	shine: { min: 0, max: 60, step: 5 },
	rTile: { min: 0, max: 32, step: 2 },
	rCtl: { min: 0, max: 22, step: 1 },
	font: { min: 90, max: 120, step: 5 },
	dim: { min: 0, max: 80, step: 5 },
	glow: { min: 0, max: 100, step: 5 },
	duration: { min: 150, max: 1000, step: 50 },
	dialogDim: { min: 0, max: 90, step: 5 }
} as const;

/**
 * Menus and dialogs that follow the tiles stay at least this opaque: they
 * lie over text, not only over the background (the rule before 0.36.0).
 */
export const FLOATING_INHERIT_MIN = 0.7;

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
		custom: { bg: '#101418', surface: '#1A2027', header: '#13181D', text: '#ECEFF3', border: '#2A333D' },
		glow: 0,
		glowColor: '',
		motion: true,
		duration: 450,
		fx: defaultEffects(),
		menus: defaultFloating(),
		dialogs: { ...defaultFloating(), dim: 70 }
	};
}

/** Values a pop-up surface starts with; `own` off means "like the tiles". */
export function defaultFloating(): FloatingSurface {
	return { own: false, glass: 30, blur: 24, shine: 20 };
}

function sanitizeFloating(input: unknown): FloatingSurface {
	const d = defaultFloating();
	const v = (typeof input === 'object' && input !== null ? input : {}) as Record<string, unknown>;
	return {
		own: typeof v.own === 'boolean' ? v.own : d.own,
		glass: inRange(v.glass, LIMITS.glass, d.glass),
		blur: inRange(v.blur, LIMITS.blur, d.blur),
		shine: inRange(v.shine, LIMITS.shine, d.shine)
	};
}

function sanitizeDialogs(input: unknown): DialogSurface {
	const v = (typeof input === 'object' && input !== null ? input : {}) as Record<string, unknown>;
	return { ...sanitizeFloating(input), dim: inRange(v.dim, LIMITS.dialogDim, 70) };
}

export function defaultEffects(): Effects {
	return {
		enabled: true,
		transitions: true,
		skeleton: true,
		micro: true,
		pulse: false,
		gradient: false,
		illustrations: true,
		shortcuts: true
	};
}

function sanitizeEffects(input: unknown): Effects {
	const d = defaultEffects();
	if (typeof input !== 'object' || input === null) return d;
	const v = input as Record<string, unknown>;
	const out = { ...d };
	for (const key of ['enabled', ...EFFECT_IDS] as const) {
		if (typeof v[key] === 'boolean') out[key] = v[key] as boolean;
	}
	return out;
}

/** Whether an effect is in use: switched on and the main switch on too. */
export function effectOn(a: Appearance, id: EffectId): boolean {
	return a.fx.enabled && a.fx[id];
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
export function sanitizeAppearance(input: unknown, legacy?: unknown): Appearance {
	const d = defaultAppearance();
	if (typeof input !== 'object' || input === null) return d;
	// Glow and animation were set in the dashboard editor before 0.30.0
	// (dashboard preferences, "visual"); they count until the appearance
	// has its own.
	const old = (typeof legacy === 'object' && legacy !== null ? legacy : {}) as Record<string, unknown>;
	const v: Record<string, unknown> = { glow: old.glow, glowColor: old.glowColor, motion: old.motion, duration: old.duration, ...(input as Record<string, unknown>) };
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
		},
		glow: inRange(v.glow, LIMITS.glow, d.glow),
		glowColor: isHex(v.glowColor) ? v.glowColor.toUpperCase() : '',
		motion: typeof v.motion === 'boolean' ? v.motion : d.motion,
		duration: inRange(v.duration, LIMITS.duration, d.duration),
		fx: sanitizeEffects(v.fx),
		menus: sanitizeFloating(v.menus),
		dialogs: sanitizeDialogs(v.dialogs)
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

/** The glass values a menu or dialog uses: its own, or those of the tiles. */
export function floatingValues(a: Appearance, s: FloatingSurface): { glass: number; blur: number; shine: number } {
	return s.own ? { glass: s.glass, blur: s.blur, shine: s.shine } : { glass: a.glass, blur: a.blur, shine: a.shine };
}

/**
 * Opacity of a glass menu or dialog. Unlike tiles they always lie over
 * other content, so the limit holds on a plain background as well: own
 * values at least 35 % (dark) or 50 % (light), like tiles; following the
 * tiles at least FLOATING_INHERIT_MIN as before.
 */
export function floatingAlpha(a: Appearance, s: FloatingSurface, palette: Palette): number {
	const { glass } = floatingValues(a, s);
	const alpha = 1 - glass / 100;
	if (!s.own) return Math.max(alpha, FLOATING_INHERIT_MIN);
	return Math.max(alpha, palette.dark ? 0.35 : 0.5);
}

function shineColor(shine: number, dark: boolean): string {
	return `rgba(255, 255, 255, ${(dark ? shine * 0.5 : 0.4 + shine * 0.5).toFixed(2)})`;
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
	const menu = floatingValues(a, a.menus);
	const menuAlpha = floatingAlpha(a, a.menus, p);
	const dialog = floatingValues(a, a.dialogs);
	const dialogAlpha = floatingAlpha(a, a.dialogs, p);
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
		'--glass-border': p.dark ? 'rgba(255, 255, 255, 0.12)' : 'rgba(255, 255, 255, 0.7)',
		'--glass-shine-color': shineColor(shine, p.dark),
		// Menus and dialogs (app.css builds --menu-*/--dialog-* surfaces from these).
		'--menu-alpha': menuAlpha.toFixed(2),
		'--menu-blur': `${menu.blur}px`,
		'--menu-shine': (menu.shine / 100).toFixed(2),
		'--glass-menu': withAlpha(p.surface, menuAlpha),
		'--menu-shine-color': shineColor(menu.shine / 100, p.dark),
		'--dialog-alpha': dialogAlpha.toFixed(2),
		'--dialog-blur': `${dialog.blur}px`,
		'--dialog-shine': (dialog.shine / 100).toFixed(2),
		'--glass-dialog': withAlpha(p.surface, dialogAlpha),
		'--dialog-shine-color': shineColor(dialog.shine / 100, p.dark),
		'--glass-shadow': `rgba(0, 0, 0, ${p.dark ? 0.3 : 0.08})`,
		'--outline-border': p.dark ? shade(p.border, 0.12) : shade(p.border, -0.15),
		'--overlay': withAlpha(p.dark ? p.bg : p.text, a.dialogs.dim / 100),
		'--bg-dim': (a.dim / 100).toFixed(2),
		// A shadow list entry even when off, so it can follow --panel-shadow.
		'--panel-glow': a.glow
			? `0 0 24px color-mix(in srgb, ${a.glowColor || accent} ${a.glow}%, transparent)`
			: '0 0 0 transparent',
		'--metric-duration': `${a.motion ? a.duration : 0}ms`,
		'--accent-2': rotateHue(accent, 38),
		'--accent-fill': effectOn(a, 'gradient') ? `linear-gradient(135deg, ${accent}, ${rotateHue(accent, 38)})` : accent
	};
}

/** The data attributes that go with cssVariables(). */
export function dataAttributes(a: Appearance, systemLight: boolean): Record<string, string> {
	return {
		'data-theme': a.theme,
		'data-mode': resolvePalette(a, systemLight).dark ? 'dark' : 'light',
		'data-surface': a.style,
		'data-density': a.density === 'kompakt' ? 'compact' : 'normal',
		...Object.fromEntries(EFFECT_IDS.map((id) => [`data-fx-${id}`, effectOn(a, id) ? 'on' : 'off']))
	};
}
