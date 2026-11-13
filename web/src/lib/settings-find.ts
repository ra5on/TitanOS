// PLAN 86 (owner 13.11.2026): the search of the control panel finds every
// single setting – the pages, their tabs, groups, rows and the parts of a
// row – and leads to the row itself (tab chosen, row opened, row marked).
//
// The rows come from settings-index.gen.ts, which scripts/settings-index.mjs
// writes from the settings pages (no hand list); the words people use for a
// setting that are not in its title ("Glocke", "Knoten", "eingeloggt") stand
// in `synonyms` below. The menu of the window, "Alle Einstellungen" on the
// phone and the command palette all search through `findSettings`.
import type { IconName } from './components/Icon.svelte';
import type { MessageKey } from './i18n/index.svelte.ts';
import {
	matchesSetting,
	settingsAreas,
	settingsGroupLabel,
	type SettingsArea,
	type SettingsGroup,
} from './settings-search.ts';

/** One place of the index: a tab, group, row or part of a row on a page. */
export type SettingsIndexEntry = {
	/** The page ("/settings/appearance", "/settings/backups/data", "/account"). */
	p: string;
	/** The title as translation key … */
	k?: MessageKey;
	/** … or as text that is the same in every language ("Tailscale"). */
	x?: string;
	kind: 'page' | 'tab' | 'group' | 'row' | 'sub';
	/** The tab of the page it stands on (`?tab=`). */
	tab?: string;
	/** The groups and rows around it, outermost first (keys, or "=text"). */
	in?: string[];
	/** Its anchor, when it is on the page without opening a row. */
	id?: string;
	/** Only owners and administrators see it (the page shows it only to them). */
	admin?: true;
	/** Only accounts with the role "user" see it (Mein Konto: their own devices). */
	user?: true;
};

/**
 * Words for a setting that its title does not have, in both languages.
 * Keyed by the title's translation key (or "=text"), or by a page id.
 */
export const synonyms: Record<string, string> = {
	// Darstellung
	'lang.label': 'sprache language deutsch englisch german english locale',
	'appearance.fontSize': 'schriftgröße schrift größer kleiner font size text zoom lesbar',
	'appearance.accent': 'akzent akzentfarbe accent colour color farbe',
	'look.mode': 'hell dunkel dark light modus theme nachtmodus night',
	'look.liquid': 'transparenz transparency glas glass durchsichtig deckend opacity',
	'appearance.density': 'dichte kompakt compact abstand spacing',
	'look.motion': 'bewegung animation motion reduzieren',
	'desktop.wallpaper.title': 'hintergrund wallpaper bild picture',
	// Zugang
	'access.sshEnable': 'ssh remote shell konsole',
	'access.cert': 'https tls ssl zertifikat certificate',
	'stay.title':
		'angemeldet bleiben stay logged in dauerhaft eingeloggt abmelden ausgeloggt logout sitzung session',
	'idleLock.title': 'sperren sperre leerlauf lock idle bildschirmsperre',
	'settings.consoles': 'terminal shell konsole console bestätigung confirmation',
	'webdav.enabled': 'webdav netzlaufwerk network drive finder',
	// Netzwerk
	'netx.tab.remote': 'fernzugriff remote access vpn tailscale netbird',
	'=Tailscale': 'tailscale tailnet vpn fernzugriff remote',
	'=NetBird': 'netbird vpn fernzugriff remote',
	'netcfg.hostname': 'hostname name nas-name rechnername umbenennen rename',
	'netx.hostname': 'hostname name nas-name rechnername umbenennen rename',
	'netcfg.dns': 'dns namensauflösung nameserver',
	// Updates
	'update.kernel.title': 'kernel linux stabil neu backports',
	'update.auto.title': 'automatisch automatic',
	// Benachrichtigungen
	'push.title': 'handy phone push mitteilungen glocke',
	'notify.email': 'email e-mail mail smtp',
	// Cluster
	'cluster.join': 'beitreten join knoten mehrere nas',
	'cluster.create': 'anlegen create knoten mehrere nas verbund',
	// Mein Konto
	'account.group.bell': 'glocke bell töne sounds mitteilungen',
	// Firewall
	'firewall.tab.zones': 'zone zonen matrix vlan eigene own docker netz network ziel destination',
	'firewall.tab.networks':
		'netz netze network networks vlan bridge docker br0 docker0 virbr subnetz subnet',
	'firewall.tab.policies':
		'richtlinie richtlinien policy policies regel rule regeln rules erlauben allow verbieten deny docker apps port ports',
	'firewall.tab.autoblock':
		'auto block automatisch sperren blockieren fehlgeschlagen failed login anmeldung brute force',
	'firewall.tab.recent': 'gesperrt blocked verworfen dropped pakete packets zuletzt recent',
	// Hardware
	'intelGuc.title': 'intel igpu grafik graphics guc huc firmware quick sync qsv i915 xe',
	// Systemdaten
	'sysdata.common': 'ablage volume speicherort storage location ruhemodus sleep wecken wake',
	'sysdata.group.config':
		'konfigurationssicherung configuration backup stick einstellungen settings passwort password',
	'sysdata.group.logs':
		'protokoll protokolle logs journal aktivität activity meldungen notifications smart monitoring verlauf history',
	// HTTPS-Namen für Apps
	'appaccess.caTitle':
		'zertifizierungsstelle certificate authority ca root stammzertifikat installieren install iphone ipad android windows macos linux',
	// Sicherungen
	'coverage.title': 'abdeckung coverage geschützt protected ungesichert unprotected',
	'applications.title': 'anwendung app application docker vm wiederherstellung restore',
	'safeguard.data': 'backup datensicherung dateien files restic usb smb s3 kopie copy',
	'safeguard.config': 'stick config konfiguration configuration sicherung',
	'safeguard.restore': 'wiederherstellung wiederherstellen restore ersetzen replace',
};

/** Pages outside the control panel whose rows the search knows too. */
export const otherPages: { path: string; label: MessageKey; icon: IconName }[] = [
	{ path: '/account', label: 'desktop.app.account', icon: 'users' },
];

let loaded: SettingsIndexEntry[] | null = null;
let loading: Promise<SettingsIndexEntry[]> | null = null;

/** The index, loaded once on the first search (it is not in the main chunk). */
export function loadSettingsIndex(): Promise<SettingsIndexEntry[]> {
	if (loaded) return Promise.resolve(loaded);
	loading ??= import('./settings-index.gen.ts').then((m) => (loaded = m.settingsIndex));
	return loading;
}
/** The index if it is loaded already. */
export function settingsIndexNow(): SettingsIndexEntry[] | null {
	return loaded;
}

/** One hit of the search. */
export type SettingsHit = {
	href: string;
	/** The title in the page language. */
	label: string;
	/** Where it stands: "Darstellung › Farben und Schrift › Sprache und Schrift". */
	where: string;
	icon: IconName;
	/** The area of its page (none for pages of other programs). */
	group?: SettingsGroup;
	/** The page id ("appearance"), for states and "new" badges. */
	page: string;
	kind: SettingsIndexEntry['kind'];
};

function normalize(text: string): string {
	return text.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();
}

/** "find=" anchors: the titles to open one after another (see settings-jump.ts). */
export function findHash(titles: string[]): string {
	return 'find=' + titles.map((title) => encodeURIComponent(title)).join('|');
}

type Translate = (key: MessageKey) => string;

function titleOf(ref: string, translate: Translate): string {
	return ref.startsWith('=') ? ref.slice(1) : translate(ref as MessageKey);
}

/** Where a hit leads: the page, its tab, and the row by anchor or by its titles. */
export function settingsHitHref(entry: SettingsIndexEntry, translate: Translate): string {
	const base = entry.p + (entry.tab ? `?tab=${entry.tab}` : '');
	if (entry.kind === 'page' || entry.kind === 'tab') return base;
	if (entry.id) return `${base}#${entry.id}`;
	const own = entry.k ? translate(entry.k) : (entry.x ?? '');
	return `${base}#${findHash([...(entry.in ?? []).map((ref) => titleOf(ref, translate)), own])}`;
}

function pageOf(path: string): { area?: SettingsArea; other?: (typeof otherPages)[number] } {
	const id = path.split('/')[2];
	if (path.startsWith('/settings/')) return { area: settingsAreas.find((a) => a.id === id) };
	return { other: otherPages.find((o) => path === o.path || path.startsWith(o.path + '/')) };
}

/**
 * How well the words fit: 0 the title is the query, 1 it starts with it,
 * 2 a word of the title starts with a word of the query, 3 somewhere inside;
 * null when a word is missing or none of them is in the title itself.
 */
function fit(words: string[], query: string, own: string, title: string, around: string) {
	if (!words.every((w) => own.includes(w) || around.includes(w))) return null;
	if (!words.some((w) => own.includes(w))) return null;
	if (title === query) return 0;
	if (title.startsWith(query)) return 1;
	const starts = title.split(/[\s\-/(),.›]+/);
	if (words.some((w) => starts.some((s) => s.startsWith(w)))) return 2;
	return 3;
}

/**
 * The hits for a query, best first: pages, then their tabs, groups, rows and
 * parts of rows; within a fit in the order of the menu and the page. An
 * account sees only what it may open; without the index loaded yet the
 * pages alone.
 */
export function findSettings(
	query: string,
	admin: boolean,
	translate: Translate,
	index: SettingsIndexEntry[] | null = loaded,
	limit = 80,
): SettingsHit[] {
	const q = normalize(query).trim().replace(/\s+/g, ' ');
	if (!q) return [];
	const words = q.split(' ');
	const order = new Map(settingsAreas.map((area, i) => [area.id, i]));
	const hits: (SettingsHit & { score: number; n: number })[] = [];
	settingsAreas.forEach((area, n) => {
		if (area.admin && !admin) return;
		const label = translate(area.label);
		if (!matchesSetting(query, label, area.id)) return;
		const title = normalize(label);
		// A page found only by its keywords comes after rows named like the query.
		const named = words.every((w) => title.includes(w));
		hits.push({
			href: `/settings/${area.id}`,
			label,
			where: translate(settingsGroupLabel(area.group)),
			icon: area.icon,
			group: area.group,
			page: area.id,
			kind: 'page',
			score: !named ? 5 : title === q ? 0 : title.startsWith(q) ? 1 : 2,
			n,
		});
	});
	(index ?? []).forEach((entry, i) => {
		if ((entry.admin && !admin) || (entry.user && admin)) return;
		const { area, other } = pageOf(entry.p);
		if (!area && !other) return;
		if (area?.admin && !admin) return;
		const label = entry.k ? translate(entry.k) : (entry.x ?? '');
		const ref = entry.k ?? `=${entry.x}`;
		const title = normalize(label);
		const own = `${title} ${normalize(synonyms[ref] ?? '')}`;
		const pageLabel = translate(area?.label ?? other!.label);
		const tabEntry = entry.tab
			? index!.find((e) => e.kind === 'tab' && e.p === entry.p && e.tab === entry.tab)
			: undefined;
		// A tab and its one group often share a name ("Fernzugriff"): once.
		const path = [
			pageLabel,
			...(tabEntry?.k && entry.kind !== 'tab' ? [translate(tabEntry.k)] : []),
			...(entry.in ?? []).map((r) => titleOf(r, translate)),
		].filter((part, i, all) => part !== all[i - 1]);
		const around = normalize(path.join(' '));
		const score = fit(words, q, own, title, around);
		if (score === null) return;
		hits.push({
			href: settingsHitHref(entry, translate),
			label,
			where: path.join(' › '),
			icon: area?.icon ?? other!.icon,
			group: area?.group,
			page: area?.id ?? entry.p.split('/')[1],
			kind: entry.kind,
			score: 3 + score,
			n: (order.get(area?.id ?? '') ?? settingsAreas.length) * 10_000 + i,
		});
	});
	const seen = new Set<string>();
	return hits
		.sort((a, b) => a.score - b.score || a.n - b.n)
		.filter((hit) => {
			const key = `${hit.label}\n${hit.where}`;
			if (seen.has(key)) return false;
			seen.add(key);
			return true;
		})
		.slice(0, limit)
		.map(({ score: _score, n: _n, ...hit }) => hit);
}
