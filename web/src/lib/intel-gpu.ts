// GuC/HuC of Intel graphics: what GET /api/v1/hardware/intel-gpu returns
// and how the hardware page reads it.
export type GucMode = 'auto' | 'huc' | 'guc';

export type UCFirmware = {
	file: string;
	present: boolean;
	loaded: string;
	version: string;
	state: string;
	authenticated: boolean;
};

export type IntelGPU = {
	address: string;
	device: string;
	revision: number;
	platform: string;
	name: string;
	generation: string;
	driver: string;
	renderNode: string;
	card: string;
	applies: boolean;
	submission: boolean;
	submissionUsed: boolean | null;
	guc: UCFirmware;
	huc: UCFirmware;
	source: '' | 'debugfs' | 'journal';
	messages: string[];
};

export type IntelGucStatus = {
	mode: GucMode;
	managed: boolean;
	active: string;
	param: string;
	entries: Record<string, string>;
	deferred: boolean;
	restartNeeded: boolean;
	writable: boolean;
	gpus: IntelGPU[];
	problem: string;
};

export const gucModes: GucMode[] = ['auto', 'huc', 'guc'];

// The value of i915.enable_guc each mode writes on the kernel command line.
export const gucValue: Record<GucMode, string> = { auto: '', huc: '2', guc: '3' };

// activeMode names the value the NAS started with; "other" is a value that
// TitanOS does not offer (set by hand, for example 1).
export function activeMode(active: string): GucMode | 'other' {
	const mode = gucModes.find((m) => gucValue[m] === active);
	return mode ?? 'other';
}

// The setting is offered only for Gen9 to Gen11 on the i915 driver; xe and
// Gen12 and later load GuC/HuC themselves.
export function applicableGPUs(status: IntelGucStatus | null): IntelGPU[] {
	return status?.gpus.filter((gpu) => gpu.applies) ?? [];
}

// hucState sums up the HuC of a GPU for a chip: "loaded" once it is
// authenticated, "off" if the kernel does not want it, "failed" for a load
// error, "unknown" without debugfs or kernel messages.
export function hucState(gpu: IntelGPU): 'loaded' | 'off' | 'failed' | 'unknown' {
	if (gpu.huc.authenticated) return 'loaded';
	const state = gpu.huc.state.toUpperCase();
	if (state === 'DISABLED') return 'off';
	if (['MISSING', 'ERROR', 'LOAD FAIL', 'INIT FAIL'].includes(state)) return 'failed';
	if (gpu.source !== '' && gpu.huc.loaded === '' && state === '') return 'off';
	return 'unknown';
}
