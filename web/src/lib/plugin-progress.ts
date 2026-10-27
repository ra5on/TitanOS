import type { MessageKey } from '#lib/i18n/index.svelte.ts';
import { taskPercent } from '#lib/task-progress.ts';

/** Steps the NAS reports for a plugin installation, update or removal (RAM only). */
export type PluginStepId =
	| 'signature'
	| 'download'
	| 'checksum'
	| 'write'
	| 'stop'
	| 'sysext'
	| 'modules'
	| 'services'
	| 'health'
	| 'remove'
	| 'rollback';
export type PluginStep = {
	id: PluginStepId;
	state: 'pending' | 'running' | 'done' | 'failed' | 'skipped';
	error?: string;
};
export type PluginProgress = {
	id: string;
	plugin: string;
	title?: string;
	action: 'install' | 'update' | 'remove';
	state: 'running' | 'done' | 'failed';
	steps: PluginStep[];
	bytes: number;
	total: number;
	speedBytesPerSecond: number;
	error?: string;
	started: string;
	finished?: string;
};

export const pluginStepKey = (id: string) => `plugins.step.${id}` as MessageKey;
export const pluginActionKey = (action: string) => `plugins.action.${action}` as MessageKey;

/** The newest operation for a plugin, running ones first. */
export function latestFor(
	list: PluginProgress[] | undefined,
	plugin: string,
): PluginProgress | undefined {
	const mine = (list ?? []).filter((entry) => entry.plugin === plugin);
	return mine.findLast((entry) => entry.state === 'running') ?? mine.at(-1);
}

export const currentStep = (progress: PluginProgress) =>
	progress.steps.find((step) => step.state === 'running') ??
	progress.steps.find((step) => step.state === 'failed');

/** Download percentage only while the download runs with a known size. */
export function downloadPercent(progress: PluginProgress): number | undefined {
	const step = progress.steps.find((entry) => entry.id === 'download');
	if (step?.state !== 'running') return undefined;
	return taskPercent(progress.bytes, progress.total);
}
