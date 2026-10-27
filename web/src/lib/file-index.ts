import type { MessageKey } from '#lib/i18n/index.svelte.ts';

// Settings of the opt-in full-text index (/config/files/index.json).
export type IndexSettings = {
	enabled: boolean;
	volumes: string[];
	shares: string[];
	ocr: boolean;
	time: string;
};
export type IndexState = 'idle' | 'running' | 'unavailable';
export type VolumeIndex = {
	volume: string;
	state: IndexState;
	files: number;
	known: boolean;
	scanned: number;
	updated: number;
	bytes: number;
	lastRun?: string;
	lastError?: string;
	truncated?: boolean;
};
export type IndexStatus = Partial<IndexSettings> & {
	indexes: VolumeIndex[];
	ocrAvailable: boolean;
};

const names = (value: unknown): string[] =>
	Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];

/** A clean editable copy of the settings; missing fields mean "off". */
export function indexSettings(value: Partial<IndexSettings> | null): IndexSettings {
	return {
		enabled: value?.enabled === true,
		volumes: names(value?.volumes),
		shares: names(value?.shares),
		ocr: value?.ocr === true,
		time:
			typeof value?.time === 'string' && /^([01]\d|2[0-3]):[0-5]\d$/.test(value.time)
				? value.time
				: '',
	};
}

export function indexStateLabel(state: IndexState): MessageKey {
	return state === 'running'
		? 'fileIndex.state.running'
		: state === 'unavailable'
			? 'fileIndex.state.unavailable'
			: 'fileIndex.state.idle';
}
