// Types and small helpers of the NVR page (docs/rules/NVR.md).
export type CameraStatus = {
	recording: boolean;
	since?: string;
	restarts: number;
	lastError?: string;
};

export type Camera = {
	id: string;
	name: string;
	host: string;
	user: string;
	vendor?: string;
	mainUrl: string;
	subUrl?: string;
	record: boolean;
	audio: boolean;
	keepDays?: number;
	onvifPort?: number;
	qualityOriginal?: Record<string, QualityChange>;
	timeSync: boolean;
	detect: boolean;
	sensitivity?: number;
	watch: { watching: boolean; motion: boolean };
	status: CameraStatus;
	segments: number;
	bytes: number;
	latest?: number;
};

export type NvrSettings = {
	enabled: boolean;
	volume?: string;
	keepDays: number;
	reserveGB: number;
	armed: boolean;
};

export type NvrState = {
	settings: NvrSettings;
	volumes: string[];
	cameras: Camera[];
	root?: string;
	freeBytes: number;
	usedBytes: number;
	vendors: string[];
	views: View[];
};

export type FoundStream = {
	role: 'main' | 'sub';
	url: string;
	width?: number;
	height?: number;
	codec?: string;
	source: string;
};

export type Discovery = {
	found: boolean;
	streams: FoundStream[];
	onvifPort?: number;
	rejected: number;
	locked: boolean;
	clockOffsetSeconds?: number;
};

export type Recordings = {
	days: string[];
	segments: { name: string; start: string; size: number }[];
};

/** Why a search found nothing, as a message key. */
export function discoveryProblem(d: Discovery): 'locked' | 'rejected' | 'none' | '' {
	if (d.found) return '';
	if (d.locked) return 'locked';
	if (d.rejected > 0) return 'rejected';
	return 'none';
}

/** "2560 × 1440" or an empty string when the camera did not tell. */
export function resolution(s: { width?: number; height?: number }): string {
	return s.width && s.height ? `${s.width} × ${s.height}` : '';
}

/** The time of a recording file name "2026-11-02/14-05-00.mp4" as "14:05:00". */
export function clockOf(name: string): string {
	const m = /\/(\d{2})-(\d{2})-(\d{2})\.mp4$/.exec(name);
	return m ? `${m[1]}:${m[2]}:${m[3]}` : name;
}

/** The file part of a recording name, for the address. */
export function fileOf(name: string): { day: string; file: string } {
	const [day = '', file = ''] = name.split('/');
	return { day, file };
}

export type NvrEvent = {
	id: string;
	camera: string;
	kind: string;
	start: string;
	end?: string;
	labels?: string[];
	snapshot: boolean;
};

/** Sensitivity choices of the motion check. */
export const SENSITIVITY = [
	{ value: 25, key: 'nvr.sensLow' },
	{ value: 50, key: 'nvr.sensMid' },
	{ value: 80, key: 'nvr.sensHigh' },
] as const;

/** The nearest choice for a stored value (0 means the default, medium). */
export function sensitivityChoice(value: number | undefined): number {
	if (!value) return 50;
	return SENSITIVITY.reduce((best, c) =>
		Math.abs(c.value - value) < Math.abs(best.value - value) ? c : best,
	).value;
}

/** Address of an event's picture. */
export function pictureUrl(camera: string, id: string): string {
	const [day = '', file = ''] = id.split('/');
	return `/api/v1/nvr/cameras/${camera}/events/${day}/${file}.jpg`;
}

export const VIEW_COLUMNS = 6;
export const VIEW_ROWS = 12;

export type Tile = { camera: string; x: number; y: number; w: number; h: number };
export type View = { id: string; name: string; tiles: Tile[] };

const overlaps = (a: Tile, b: Tile) =>
	a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** Whether the tile fits the grid and touches none of the others. */
export function tileFits(tile: Tile, others: Tile[]): boolean {
	return (
		tile.w >= 1 &&
		tile.h >= 1 &&
		tile.x >= 0 &&
		tile.y >= 0 &&
		tile.x + tile.w <= VIEW_COLUMNS &&
		tile.y + tile.h <= VIEW_ROWS &&
		!others.some((o) => overlaps(tile, o))
	);
}

/** The first free place of the given size, row by row; null if the grid is full. */
export function freeSpot(tiles: Tile[], w: number, h: number): { x: number; y: number } | null {
	for (let y = 0; y + h <= VIEW_ROWS; y++)
		for (let x = 0; x + w <= VIEW_COLUMNS; x++)
			if (tileFits({ camera: '', x, y, w, h }, tiles)) return { x, y };
	return null;
}

export type Resolution = { width: number; height: number };
export type Quality = {
	token: string;
	name: string;
	codec: string;
	width: number;
	height: number;
	fps: number;
	bitrateKb: number;
	resolutions?: Resolution[];
	fpsMin?: number;
	fpsMax?: number;
	bitrateMin?: number;
	bitrateMax?: number;
};
export type QualityChange = {
	token: string;
	width: number;
	height: number;
	fps: number;
	bitrateKb: number;
};

/** Gigabytes a day at a bit rate in kbit/s (one camera, recording all day). */
export function gbPerDay(bitrateKb: number): number {
	return (bitrateKb * 1000 * 86400) / 8 / 1e9;
}
