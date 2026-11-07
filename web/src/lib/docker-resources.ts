// PLAN 67-1: Docker images, volumes and the clean-up page (docs/DOCKER-CLEANUP.md).
// titand decides what may go: images in use or protected (remote view,
// the agent's sandbox) and volumes of TitanOS are never offered here, and
// the volume clean-up takes only unused anonymous volumes by default.
import { api } from './api.ts';

export type UsedBy = { app?: string; container?: string; running?: boolean };

export type DockerImage = {
	id: string;
	repoTags: string[];
	/** With the layers shared with other images. */
	size: number;
	/** The shared part, -1 unknown; deleting frees about size - sharedSize. */
	sharedSize: number;
	/** ISO date (or Unix seconds from an older answer). */
	created: number | string;
	usedBy: UsedBy[];
	dangling: boolean;
	/** Why TitanOS keeps it ("viewer", "sandbox"); empty when it may go. */
	protected: string;
};

export type ImageTotals = {
	count: number;
	size: number;
	unusedCount: number;
	unusedSize: number;
	danglingCount: number;
	danglingSize: number;
};

export type DockerVolume = {
	name: string;
	driver: string;
	/** Bytes; undefined while Docker does not know (still counting). */
	size?: number;
	usedBy: UsedBy[];
	/** The Compose project (app) it was made for. */
	app: string;
	anonymous: boolean;
	/** Managed by TitanOS: never deleted here. */
	titanos: boolean;
};

/** One kind of thing in the disk usage: how much there is and what can go. */
export type DfPart = { count: number; size: number; reclaimable: number };
export type DockerDf = {
	images?: DfPart;
	containers?: DfPart;
	volumes?: DfPart;
	buildCache?: DfPart;
	/** The part of the volumes' reclaimable space in unused anonymous volumes. */
	anonymousReclaimable: number;
	/** A count is running: ask again a little later. */
	measuring: boolean;
};

export type PruneResult = {
	deleted?: string[] | number;
	failed?: { id: string; error: string }[];
	reclaimed?: number;
};

const num = (v: unknown, fallback = 0) =>
	typeof v === 'number' && Number.isFinite(v) ? v : fallback;

function list<T>(data: unknown, key: string): T[] {
	if (Array.isArray(data)) return data as T[];
	if (data && typeof data === 'object' && Array.isArray((data as Record<string, unknown>)[key]))
		return (data as Record<string, T[]>)[key];
	return [];
}

function normalImage(i: Partial<DockerImage>): DockerImage {
	return {
		id: String(i.id ?? ''),
		repoTags: Array.isArray(i.repoTags) ? i.repoTags.filter((x) => x && x !== '<none>:<none>') : [],
		size: num(i.size),
		sharedSize: num(i.sharedSize, -1),
		created: i.created ?? 0,
		usedBy: Array.isArray(i.usedBy) ? i.usedBy : [],
		dangling: !!i.dangling,
		protected: typeof i.protected === 'string' ? i.protected : '',
	};
}

function normalVolume(v: Partial<DockerVolume>): DockerVolume {
	const size = typeof v.size === 'number' && v.size >= 0 ? v.size : undefined;
	return {
		name: String(v.name ?? ''),
		driver: String(v.driver ?? 'local'),
		size,
		usedBy: Array.isArray(v.usedBy) ? v.usedBy : [],
		app: typeof v.app === 'string' ? v.app : '',
		anonymous: !!v.anonymous,
		titanos: !!v.titanos,
	};
}

export async function loadImages(): Promise<{ images: DockerImage[]; totals: ImageTotals | null }> {
	const data = await api.get<unknown>('/docker/images');
	const images = list<Partial<DockerImage>>(data, 'images').map(normalImage);
	const raw = (data as { totals?: Partial<ImageTotals> } | null)?.totals;
	const totals = raw
		? {
				count: num(raw.count),
				size: num(raw.size),
				unusedCount: num(raw.unusedCount),
				unusedSize: num(raw.unusedSize),
				danglingCount: num(raw.danglingCount),
				danglingSize: num(raw.danglingSize),
			}
		: null;
	return { images, totals };
}

export async function loadVolumes(): Promise<DockerVolume[]> {
	const data = await api.get<unknown>('/docker/volumes');
	return list<Partial<DockerVolume>>(data, 'volumes').map(normalVolume);
}

function part(value: unknown): DfPart | undefined {
	if (!value || typeof value !== 'object') return undefined;
	const v = value as Record<string, unknown>;
	return { count: num(v.count), size: num(v.size), reclaimable: num(v.reclaimable) };
}

export async function loadDf(fresh = false): Promise<DockerDf> {
	const data =
		(await api.get<Record<string, unknown>>('/docker/df' + (fresh ? '?fresh=1' : ''))) ?? {};
	return {
		images: part(data.images),
		containers: part(data.containers),
		volumes: part(data.volumes),
		buildCache: part(data.buildCache),
		anonymousReclaimable: num(data.anonymousReclaimable),
		measuring: data.measuring === true,
	};
}

/** The name an image is shown with: its first tag, else its short id. */
export function imageName(image: DockerImage): { repo: string; tag: string } {
	const first = image.repoTags[0];
	if (!first) return { repo: '', tag: shortId(image.id) };
	const at = first.lastIndexOf(':');
	// A port in the registry ("host:5000/x") is no tag.
	if (at > 0 && !first.slice(at).includes('/'))
		return { repo: first.slice(0, at), tag: first.slice(at + 1) };
	return { repo: first, tag: '' };
}

export function shortId(id: string): string {
	const bare = id.replace(/^sha256:/, '');
	return 'sha256:' + bare.slice(0, 10);
}

export function createdDate(value: number | string): Date | null {
	if (typeof value === 'number') return value > 0 ? new Date(value * 1000) : null;
	const d = new Date(value);
	return Number.isNaN(d.getTime()) || d.getTime() <= 0 ? null : d;
}

/** "immich", "immich (postgres)" – the app with the container when they differ. */
export function usedByText(used: UsedBy[]): string {
	const names = used.map((u) =>
		u.app && u.container && u.container !== u.app
			? `${u.app} (${u.container})`
			: u.app || u.container || '',
	);
	return [...new Set(names.filter(Boolean))].join(', ');
}

/** About the space deleting an image frees (without layers shared with others). */
export const freed = (image: DockerImage) =>
	image.sharedSize > 0 ? Math.max(0, image.size - image.sharedSize) : image.size;

/** Images "Ungenutzte löschen" takes: not used and not protected. */
export const unusedImages = (images: DockerImage[]) =>
	images.filter((i) => !i.usedBy.length && !i.protected);
/** Volumes that can be deleted one by one: unused and not TitanOS's own. */
export const deletableVolumes = (volumes: DockerVolume[]) =>
	volumes.filter((v) => !v.usedBy.length && !v.titanos);
/** What the volume clean-up takes by default: unused anonymous volumes. */
export const anonymousUnused = (volumes: DockerVolume[]) =>
	deletableVolumes(volumes).filter((v) => v.anonymous);
export const sumSize = (items: { size?: number }[]) =>
	items.reduce((sum, i) => sum + (i.size ?? 0), 0);

/** How many a clean-up deleted (a list of ids/names or a number). */
export function deletedCount(result: PruneResult | null | undefined): number | null {
	if (!result) return null;
	if (Array.isArray(result.deleted)) return result.deleted.length;
	return typeof result.deleted === 'number' ? result.deleted : null;
}

/** "vor 2 Tagen", "3 months ago" – the largest whole unit. */
export function relativeDate(date: Date | null, locale: string, now = Date.now()): string {
	if (!date) return '–';
	const seconds = Math.round((date.getTime() - now) / 1000);
	const units: [Intl.RelativeTimeFormatUnit, number][] = [
		['year', 365 * 86400],
		['month', 30 * 86400],
		['week', 7 * 86400],
		['day', 86400],
		['hour', 3600],
		['minute', 60],
	];
	const format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
	for (const [unit, size] of units)
		if (Math.abs(seconds) >= size) return format.format(Math.round(seconds / size), unit);
	return format.format(0, 'minute');
}

/** A network of the engine for Docker › Netzwerke (GET /docker/networks/all). */
export type DockerNetwork = {
	id: string;
	name: string;
	driver: string;
	scope: string;
	/** user: made here (deletable); app: of a TitanOS app; lan: LAN network; system/titanos: kept. */
	kind: 'user' | 'app' | 'lan' | 'system' | 'titanos';
	subnets: string[];
	gateways: string[];
	internal: boolean;
	usedBy: UsedBy[];
	app: string;
	protected: boolean;
};

const KINDS = ['user', 'app', 'lan', 'system', 'titanos'] as const;

export async function loadNetworks(): Promise<DockerNetwork[]> {
	const data = await api.get<unknown>('/docker/networks/all');
	return list<Partial<DockerNetwork>>(data, 'networks').map((n) => ({
		id: String(n.id ?? ''),
		name: String(n.name ?? ''),
		driver: String(n.driver ?? ''),
		scope: String(n.scope ?? ''),
		kind: KINDS.includes(n.kind as DockerNetwork['kind'])
			? (n.kind as DockerNetwork['kind'])
			: 'system',
		subnets: Array.isArray(n.subnets) ? n.subnets.map(String) : [],
		gateways: Array.isArray(n.gateways) ? n.gateways.map(String) : [],
		internal: !!n.internal,
		usedBy: Array.isArray(n.usedBy) ? n.usedBy : [],
		app: typeof n.app === 'string' ? n.app : '',
		protected: n.protected !== false && n.kind !== 'user' ? true : !!n.protected,
	}));
}

/** Networks the clean-up removes: made here and used by no container. */
export const unusedNetworks = (networks: DockerNetwork[]) =>
	networks.filter((n) => n.kind === 'user' && !n.protected && !n.usedBy.length);

/** A private IPv4 subnet /8–/29 ("" = Docker chooses). */
export function validSubnet(value: string): boolean {
	if (!value.trim()) return true;
	const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/.exec(value.trim());
	if (!m) return false;
	const [a, b, c, d, bits] = m.slice(1).map(Number);
	if ([a, b, c, d].some((x) => x > 255) || bits < 8 || bits > 29) return false;
	return a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168);
}
