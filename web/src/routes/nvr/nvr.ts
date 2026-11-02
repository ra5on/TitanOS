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
	timeSync: boolean;
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
};

export type NvrState = {
	settings: NvrSettings;
	volumes: string[];
	cameras: Camera[];
	root?: string;
	freeBytes: number;
	usedBytes: number;
	vendors: string[];
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
