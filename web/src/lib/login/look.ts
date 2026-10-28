// The look of the login page: one setting for the whole NAS (Settings ›
// Appearance › Login page), read without login from /api/v1/login-appearance.
// titand checks every value again; this mirrors its rules so that the page
// never renders anything it would not accept.

import { BACKGROUND_IDS } from '../dashboard/visual.ts';
import { contrast, isHex, type Hex } from '../theme/color.ts';
import {
	ACCENT_IDS,
	defaultAppearance,
	glassAlpha,
	SURFACE_STYLES,
	type AccentId,
	type Appearance,
	type Palette,
	type SurfaceStyle
} from '../theme/themes.ts';

export const LOGIN_THEMES = ['inherit', 'graphit', 'schwarz', 'mitternacht', 'hell', 'auto'] as const;
export type LoginTheme = (typeof LOGIN_THEMES)[number];
export const LOGIN_SOURCES = ['theme', 'builtin', 'custom'] as const;
export type LoginSource = (typeof LOGIN_SOURCES)[number];
export const LOGIN_POSITIONS = ['center', 'left', 'right'] as const;
export type LoginPosition = (typeof LOGIN_POSITIONS)[number];
export const LOGIN_WIDTHS = ['normal', 'wide'] as const;
export type LoginWidth = (typeof LOGIN_WIDTHS)[number];
export const LOGIN_LOCALES = ['de', 'en'] as const;
export type LoginLocale = (typeof LOGIN_LOCALES)[number];

export const LOGIN_TITLE_MAX = 60;
export const LOGIN_NOTICE_MAX = 300;
export const LOGIN_IMAGE_MAX = 5 * 1024 * 1024;
export const LOGIN_IMAGE_TYPES = ['image/jpeg', 'image/png', 'image/webp'];

export type LoginTexts = { title: string; notice: string };
export type LoginImage = { id: string; width: number; height: number; bytes: number };

export type LoginLook = {
	theme: LoginTheme;
	accent: AccentId;
	customAccent: Hex;
	style: SurfaceStyle;
	/** Transparency of a glass card in percent, 0–90 (limited like elsewhere). */
	glass: number;
	blur: number;
	background: {
		source: LoginSource;
		/** Gallery motif for source "builtin". */
		id: string;
		fit: 'cover' | 'contain';
		x: number;
		y: number;
		zoom: number;
		dim: number;
	};
	/** The stored own image; only titand sets it. */
	image: LoginImage | null;
	position: LoginPosition;
	width: LoginWidth;
	texts: Partial<Record<LoginLocale, LoginTexts>>;
	hideHost: boolean;
	hideLogo: boolean;
	clock: boolean;
};

export function defaultLoginLook(): LoginLook {
	return {
		theme: 'inherit',
		accent: 'blau',
		customAccent: '#E5B92E',
		style: 'glass',
		glass: 45,
		blur: 18,
		background: { source: 'theme', id: '', fit: 'cover', x: 50, y: 50, zoom: 100, dim: 30 },
		image: null,
		position: 'center',
		width: 'normal',
		texts: {},
		hideHost: false,
		hideLogo: false,
		clock: false
	};
}

const record = (v: unknown): Record<string, unknown> => (typeof v === 'object' && v !== null ? (v as Record<string, unknown>) : {});
const oneOf = <T extends string>(v: unknown, allowed: readonly T[], fallback: T): T => (allowed.includes(v as T) ? (v as T) : fallback);
const number = (v: unknown, fallback: number, min: number, max: number) =>
	typeof v === 'number' && Number.isFinite(v) ? Math.max(min, Math.min(max, Math.round(v))) : fallback;

/** Removes what a text line may not contain and cuts it to its limit. */
export function cleanLoginText(value: unknown, most: number): string {
	if (typeof value !== 'string') return '';
	return [...value.replace(/[\p{Cc}\u2028\u2029]/gu, ' ').trim()].slice(0, most).join('');
}

/** Turns anything into a valid look; unknown values fall back to the defaults. */
export function sanitizeLoginLook(input: unknown): LoginLook {
	const d = defaultLoginLook();
	const v = record(input);
	const b = record(v.background);
	let source = oneOf(b.source, LOGIN_SOURCES, d.background.source);
	let id = typeof b.id === 'string' ? b.id : '';
	if (source === 'builtin' && !BACKGROUND_IDS.includes(id as (typeof BACKGROUND_IDS)[number])) source = 'theme';
	if (source !== 'builtin') id = '';
	const i = record(v.image);
	const image =
		typeof i.id === 'string' && /^[a-f0-9]{64}$/.test(i.id)
			? { id: i.id, width: number(i.width, 0, 0, 50000), height: number(i.height, 0, 0, 50000), bytes: number(i.bytes, 0, 0, LOGIN_IMAGE_MAX * 4) }
			: null;
	if (source === 'custom' && !image) source = 'theme';
	const texts: LoginLook['texts'] = {};
	const t = record(v.texts);
	for (const locale of LOGIN_LOCALES) {
		const entry = record(t[locale]);
		const title = cleanLoginText(entry.title, LOGIN_TITLE_MAX);
		const notice = cleanLoginText(entry.notice, LOGIN_NOTICE_MAX);
		if (title || notice) texts[locale] = { title, notice };
	}
	return {
		theme: oneOf(v.theme, LOGIN_THEMES, d.theme),
		accent: oneOf(v.accent, [...ACCENT_IDS, 'custom'] as const, d.accent),
		customAccent: isHex(v.customAccent) ? v.customAccent.toUpperCase() : d.customAccent,
		style: oneOf(v.style, SURFACE_STYLES, d.style),
		glass: number(v.glass, d.glass, 0, 90),
		blur: number(v.blur, d.blur, 0, 40),
		background: {
			source,
			id,
			fit: b.fit === 'contain' ? 'contain' : 'cover',
			x: number(b.x, 50, 0, 100),
			y: number(b.y, 50, 0, 100),
			zoom: number(b.zoom, 100, 100, 200),
			dim: number(b.dim, d.background.dim, 0, 80)
		},
		image,
		position: oneOf(v.position, LOGIN_POSITIONS, d.position),
		width: oneOf(v.width, LOGIN_WIDTHS, d.width),
		texts,
		hideHost: v.hideHost === true,
		hideLogo: v.hideLogo === true,
		clock: v.clock === true
	};
}

/** True when the look is exactly today's login page. */
export function isDefaultLook(look: LoginLook): boolean {
	return JSON.stringify(sanitizeLoginLook(look)) === JSON.stringify(defaultLoginLook());
}

/** What the editor sends to titand: no image metadata, nothing extra. */
export function loginSaveBody(look: LoginLook, removeImage = false): Record<string, unknown> {
	const { image: _image, ...rest } = sanitizeLoginLook(look);
	const background: Record<string, unknown> = { ...rest.background };
	if (rest.background.source !== 'builtin') delete background.id;
	return { ...rest, background, ...(removeImage ? { removeImage: true } : {}) };
}

/** Title and notice in a language; a missing text comes from the other language. */
export function loginTexts(look: LoginLook | null, locale: LoginLocale, fallbackTitle: string): LoginTexts {
	const other: LoginLocale = locale === 'de' ? 'en' : 'de';
	const own = look?.texts[locale];
	const second = look?.texts[other];
	return {
		title: own?.title || second?.title || fallbackTitle,
		notice: own?.notice || second?.notice || ''
	};
}

/**
 * The appearance the login page uses. "inherit" keeps what this browser used
 * last (or the defaults), as before; a fixed theme starts from the defaults.
 * `scoped` is false when nothing differs from today's page.
 */
export function loginAppearance(look: LoginLook | null, inherited: Appearance): { appearance: Appearance; scoped: boolean } {
	if (!look || (look.theme === 'inherit' && look.background.source === 'theme')) return { appearance: inherited, scoped: false };
	const base: Appearance =
		look.theme === 'inherit'
			? { ...inherited }
			: { ...defaultAppearance(), theme: look.theme, accent: look.accent, customAccent: look.customAccent, style: look.style, glass: look.glass, blur: look.blur };
	// A picture behind the card: glass keeps its readable minimum opacity.
	if (look.background.source !== 'theme') base.bg = 'image';
	else if (look.theme !== 'inherit') base.bg = 'shapes';
	return { appearance: base, scoped: true };
}

/** The picture behind the card, or '' for the theme's background. */
export function loginBackgroundURL(look: LoginLook | null, override = ''): string {
	if (!look) return '';
	if (look.background.source === 'builtin') return '/backgrounds/' + look.background.id + '.webp';
	if (look.background.source === 'custom') return override || (look.image ? '/api/v1/login-appearance/background/' + look.image.id : '');
	return '';
}

function rgb(hex: Hex): number[] {
	const n = parseInt(hex.slice(1), 16);
	return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** `top` with opacity `alpha` over `bottom`. */
export function composite(top: Hex, alpha: number, bottom: Hex): Hex {
	const a = rgb(top);
	const b = rgb(bottom);
	return '#' + a.map((v, i) => Math.round(v * alpha + b[i] * (1 - alpha)).toString(16).padStart(2, '0')).join('').toUpperCase();
}

/**
 * The lowest contrast of the card's text (normal and muted) against what the
 * card shows: its surface, as much of `behind` as glass lets through, or
 * `behind` alone for outline cards. `behind` is the (average) color of the
 * background after darkening.
 */
export function cardContrast(palette: Palette, appearance: Appearance, behind: Hex): number {
	const alpha = appearance.style === 'flat' ? 1 : appearance.style === 'outline' ? 0 : glassAlpha(appearance, palette);
	const card = composite(palette.surface, alpha, behind);
	return Math.min(contrast(palette.text, card), contrast(palette.muted, card));
}

/** The background color after the "darken" slider (dim in percent, towards black). */
export function dimmed(color: Hex, dim: number): Hex {
	return composite('#000000', dim / 100, color);
}
