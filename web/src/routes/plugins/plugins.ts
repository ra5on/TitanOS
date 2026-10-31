// Shapes of GET /plugins and what the plugin page works out from them.

import { ApiError } from '#lib/api.ts';
import type { DriverStatus } from '#lib/components/DriverUpdates.svelte';
import { errorText, t, type Locale, type MessageKey } from '#lib/i18n/index.svelte.ts';

export type Plugin = {
	name: string;
	title: string;
	description: Record<string, string> | null;
	version: string;
	size: number;
	kernel: string;
	contains: Record<string, string[]> | null;
	modules: string[] | null;
	services: string[] | null;
	installed: boolean;
	enabled: boolean;
	active: boolean;
	path: string;
	official: boolean;
	local: boolean;
	download: boolean;
	licenseUrl?: string;
	problem: string;
	update?: { path: string; version: string; official: boolean; download?: boolean } | null;
	category?: string;
	license?: string;
	upstream?: { name?: string; url?: string; version?: string } | null;
	details?: Record<string, Details> | null;
	sources?: string[] | null;
	devices?: Device[] | null;
	// A community plugin of a source (its id) or behind a single link.
	source?: string;
	url?: string;
};
export type Source = {
	id: string;
	name: string;
	url: string;
	added: string;
	plugins: number;
	error?: string;
};
export type Device = { key: string; bus: string; address?: string; name: string; driver?: string };
export type Details = {
	when?: string;
	hardware?: string[] | null;
	changes?: string;
	limits?: string[] | null;
	footprint?: string;
};
export type Recommendation = {
	plugin: string;
	title: string;
	category?: string;
	devices: Device[];
};
export type Status = {
	driverUpdate: DriverStatus;
	volume: string;
	kernel: string;
	plugins: Plugin[];
	catalog: { release: string; state: string; error?: string };
	recommendations?: Recommendation[] | null;
	dismissed?: number;
	sources?: Source[] | null;
};
export type Problem = { text: string; detail: string };

/** The text in the interface language, else English, else any. */
export const localized = (texts: Record<string, string> | null, locale: Locale) =>
	texts?.[locale] ?? texts?.en ?? Object.values(texts ?? {})[0] ?? '';
export const containsOf = (plugin: Plugin, locale: Locale) =>
	plugin.contains?.[locale] ?? plugin.contains?.en ?? [];
export const aboutOf = (plugin: Plugin, locale: Locale): Details | null =>
	plugin.details?.[locale] ?? plugin.details?.en ?? null;

/** Two letters of the title for the plugin's tile. */
export const monogram = (plugin: Plugin) =>
	plugin.title
		.replace(/[^a-zA-Z0-9]/g, '')
		.slice(0, 2)
		.replace(/^./, (c) => c.toUpperCase()) || '?';
/** A stable hue per plugin name. */
export const hue = (plugin: Plugin) =>
	[...plugin.name].reduce((sum, char) => (sum * 31 + char.charCodeAt(0)) % 360, 7);

// Areas of the catalog, in this order; unknown categories are "other".
export const CATEGORIES = [
	'network',
	'graphics',
	'sensors',
	'storage',
	'tools',
	'remote',
	'other',
] as const;
export type Category = (typeof CATEGORIES)[number];
export const categoryKey: Record<Category, MessageKey> = {
	network: 'plugins.category.network',
	graphics: 'plugins.category.graphics',
	sensors: 'plugins.category.sensors',
	storage: 'plugins.category.storage',
	tools: 'plugins.category.tools',
	remote: 'plugins.category.remote',
	other: 'plugins.category.other',
};
export const categoryOf = (plugin: { category?: string }): Category =>
	(CATEGORIES as readonly string[]).includes(plugin.category ?? '')
		? (plugin.category as Category)
		: 'other';

/** Whether every word (lower case) is in the plugin's title, name, description, hardware or devices. */
export function matches(plugin: Plugin, words: string, locale: Locale) {
	if (!words) return true;
	const info = aboutOf(plugin, locale);
	const haystack = [
		plugin.title,
		plugin.name,
		localized(plugin.description, locale),
		...(info?.hardware ?? []),
		info?.when ?? '',
		...(plugin.devices ?? []).map((device) => device.name),
	]
		.join(' ')
		.toLowerCase();
	return words.split(/\s+/).every((word) => haystack.includes(word));
}

/** A web link that may be shown as one. */
export const isLink = (value: string) => /^https:\/\/\S+$/.test(value);

export function badge(plugin: Plugin): { text: string; kind: string } {
	if (!plugin.installed && plugin.problem === 'kernel')
		return { text: t('plugins.incompatible'), kind: 'warn' };
	if (!plugin.installed) return { text: t('plugins.available'), kind: '' };
	if (plugin.problem) return { text: t('plugins.notLoaded'), kind: 'warn' };
	if (plugin.active) return { text: t('plugins.active'), kind: 'ok' };
	return { text: t(plugin.enabled ? 'plugins.waiting' : 'plugins.off'), kind: '' };
}

/** The error text; the NAS's own message only where it helps (a failing plugin or download). */
export function pluginProblem(err: unknown): Problem {
	const code = err instanceof ApiError ? err.code : 'internal';
	return {
		text: errorText(code),
		detail:
			(code === 'plugin_failed' ||
				code === 'invalid_plugin' ||
				code === 'plugin_download_failed') &&
			err instanceof ApiError
				? err.message
				: '',
	};
}
