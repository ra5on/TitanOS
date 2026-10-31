// The 3-2-1 rule of backups (PLAN 13j): at least three copies of the data
// (the original and two backups), on at least two kinds of storage, at
// least one of them away from home. Worked out from the backup jobs only:
// a job counts while it is switched on, two jobs to the same place count as
// one copy. Whether every share is in a job shows the coverage page.

export type BackupDestination = {
	type: string;
	deviceId?: string;
	targetId?: string;
	volume?: string;
	folder?: string;
	endpoint?: string;
	bucket?: string;
	prefix?: string;
	host?: string;
	port?: number;
	user?: string;
	path?: string;
	url?: string;
};

export type BackupJobLike = {
	job: { id: string; enabled: boolean; destination?: BackupDestination };
};

/** Kinds of storage; the original lies on the disks of this NAS ("nas"). */
export type Medium = 'nas' | 'usb' | 'share' | 'titanos' | 'cloud' | 'server';

const MEDIUM: Record<string, Medium> = {
	local: 'nas',
	'local-copy': 'nas',
	usb: 'usb',
	'usb-copy': 'usb',
	smb: 'share',
	'smb-copy': 'share',
	titanos: 'titanos',
	s3: 'cloud',
	sftp: 'server',
	rest: 'server',
};

/** Targets away from home: the internet or another TitanOS NAS. */
const OFFSITE = new Set(['s3', 'sftp', 'rest', 'titanos']);

export function mediumOf(type: string): Medium | null {
	return MEDIUM[type] ?? null;
}

export function isOffsite(type: string): boolean {
	return OFFSITE.has(type);
}

/** One place a job writes to; jobs with the same place are one copy. */
export function placeOf(d: BackupDestination): string {
	const where = (() => {
		switch (d.type) {
			case 'local':
			case 'local-copy':
				return `${d.volume ?? ''}/${d.folder ?? ''}`;
			case 'usb':
			case 'usb-copy':
				return `${d.deviceId ?? ''}/${d.folder ?? ''}`;
			case 'smb':
			case 'smb-copy':
			case 'titanos':
				return `${d.targetId ?? ''}/${d.folder ?? ''}`;
			case 's3':
				return `${d.endpoint ?? ''}/${d.bucket ?? ''}/${d.prefix ?? ''}`;
			case 'sftp':
				return `${d.user ?? ''}@${d.host ?? ''}:${d.port ?? ''}/${d.path ?? ''}`;
			case 'rest':
				return d.url ?? '';
		}
		return '';
	})();
	return `${d.type}:${where.toLowerCase()}`;
}

export type Rule321 = {
	/** The original plus one per place backed up to. */
	copies: number;
	/** Kinds of storage, the original's "nas" first. */
	media: Medium[];
	/** Places away from home. */
	offsite: number;
	/** Jobs that are switched on and were counted. */
	jobs: number;
	three: boolean;
	two: boolean;
	one: boolean;
};

export function rule321(jobs: BackupJobLike[]): Rule321 {
	const places = new Map<string, BackupDestination>();
	let counted = 0;
	for (const { job } of jobs) {
		const d = job.destination;
		if (!job.enabled || !d || !mediumOf(d.type)) continue;
		counted++;
		places.set(placeOf(d), d);
	}
	const media = new Set<Medium>(['nas']);
	let offsite = 0;
	for (const d of places.values()) {
		media.add(mediumOf(d.type)!);
		if (isOffsite(d.type)) offsite++;
	}
	const copies = 1 + places.size;
	return {
		copies,
		media: [...media],
		offsite,
		jobs: counted,
		three: copies >= 3,
		two: media.size >= 2,
		one: offsite >= 1,
	};
}
