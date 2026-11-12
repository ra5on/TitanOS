// Settings › Data and backup › System data (PLAN 79-8): where TitanOS keeps
// its own data. The answer of GET /api/v1/system/data and what the page
// derives from it.

export type SysDataVolume = { name: string; mounted: boolean; sleeps: boolean };

export type SysDataFunctionId = 'config' | 'logs' | 'activity' | 'notify' | 'smart' | 'monitoring';

export type SysDataFunction = {
	id: SysDataFunctionId;
	/** The volume chosen for this function, "" while it follows the common one. */
	choice: string;
	/** Where its data go now, "" while there is no volume. */
	volume: string;
	source: 'own' | 'common' | 'automatic';
	enabled: boolean;
	moving: boolean;
	sizeBytes: number | null;
	sizeAt: string | null;
	/**
	 * Entries a move put next to the current ones as "<name>.from-<volume>"
	 * (both volumes held the name with other content): kept, not used.
	 */
	aside?: string[] | null;
};

/** Where an entry of a function's folder lies on the NAS. */
export function sysDataPath(f: Pick<SysDataFunction, 'id' | 'volume'>, name: string): string {
	return `/mnt/${f.volume}/.titanos/${f.id === 'config' ? 'backups' : f.id}/${name}`;
}

export type SysDataState = {
	/** The common choice, "" for the automatic one. */
	volume: string;
	/** The volume in use for everything without its own choice. */
	resolved: string;
	sleepKnown: boolean;
	volumes: SysDataVolume[];
	functions: SysDataFunction[];
};

export type SysDataHint =
	| { tone: 'none' }
	| { tone: 'unknown' }
	/** The common volume never sleeps; `spared` are sleeping volumes nothing writes to any more. */
	| { tone: 'ok'; volume: string; spared: string[] }
	| { tone: 'warn'; volume: string };

/** What to say below the common choice: praise, a warning, or nothing yet. */
export function sysDataHint(state: SysDataState): SysDataHint {
	if (!state.resolved) return { tone: 'none' };
	if (!state.sleepKnown) return { tone: 'unknown' };
	const sleeps = new Set(state.volumes.filter((v) => v.sleeps).map((v) => v.name));
	if (sleeps.has(state.resolved)) return { tone: 'warn', volume: state.resolved };
	const used = new Set(state.functions.map((f) => f.volume));
	return {
		tone: 'ok',
		volume: state.resolved,
		spared: [...sleeps].filter((name) => !used.has(name)).sort(),
	};
}

/** Functions whose own volume has disks that may sleep. */
export function sleepingChoices(state: SysDataState): SysDataFunction[] {
	if (!state.sleepKnown) return [];
	const sleeps = new Set(state.volumes.filter((v) => v.sleeps).map((v) => v.name));
	return state.functions.filter((f) => f.choice !== '' && sleeps.has(f.choice));
}

/** The volumes a select offers: mounted ones, and the stored one even when it is not. */
export function offeredVolumes(state: SysDataState, current: string): SysDataVolume[] {
	return state.volumes.filter((v) => v.mounted || v.name === current);
}
