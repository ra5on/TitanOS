import type { MessageKey } from '#lib/i18n/index.svelte.ts';
import type { Operations } from '#lib/operations.ts';
import { folderRestoreProgress } from '#lib/snapshot-restore.ts';

export type TaskState =
	| 'running'
	| 'paused'
	| 'waiting'
	| 'scheduled'
	| 'done'
	| 'failed'
	| 'blocked'
	| 'cancelled'
	| 'unknown';
export type TaskKind =
	| 'files'
	| 'backup'
	| 'maintenance'
	| 'docker'
	| 'vm'
	| 'disk'
	| 'assistant'
	| 'automation'
	| 'upload'
	| 'recovery'
	| 'restore'
	| 'system';
export type TaskAction = {
	label: 'pause' | 'resume' | 'stop' | 'retry';
	path: string;
	body: Record<string, unknown>;
};
export type TaskItem = {
	titleKey?: MessageKey;
	id: string;
	name: string;
	kind: TaskKind;
	state: TaskState;
	href: string;
	detail?: string;
	at?: string | null;
	progress?: number;
	actions?: TaskAction[];
	wait?: string;
};
const path = encodeURIComponent;
const percent = (done: number, total: number) =>
	total > 0 ? Math.max(0, Math.min(100, (done / total) * 100)) : undefined;
export const needsAttention = (task: TaskItem) =>
	task.state === 'failed' || task.state === 'blocked' || task.state === 'unknown';
export const isActive = (task: TaskItem) => ['running', 'paused', 'waiting'].includes(task.state);

// All consumers use the same state mapping. Missing measurements never imply success.
export function taskItems(data: Operations): TaskItem[] {
	const result: TaskItem[] = [];
	for (const job of data.media ?? []) {
		const state: TaskState =
			job.phase === 'review'
				? 'blocked'
				: ['uploading', 'downloading', 'extracting', 'verifying', 'publishing', 'pausing'].includes(
							job.phase,
					  )
					? 'running'
					: ['paused', 'done', 'failed', 'cancelled'].includes(job.phase)
						? (job.phase as TaskState)
						: 'unknown';
		result.push({
			id: `media:${job.id}`,
			name: job.name,
			kind: 'vm',
			state,
			href: '/vms/media',
			detail: job.error || job.output,
			progress: ['uploading', 'downloading'].includes(job.phase)
				? percent(job.bytes, job.total)
				: undefined,
		});
	}
	for (const job of data.transfers ?? []) {
		const state = job.blocked
			? 'blocked'
			: job.state === 'running'
				? job.paused
					? 'paused'
					: job.waiting
						? 'waiting'
						: 'running'
				: job.state;
		const actions: TaskAction[] = [];
		if (job.state === 'running' && (!job.operation || ['copy', 'move'].includes(job.operation))) {
			actions.push({
				label: job.paused ? 'resume' : 'pause',
				path: `/files/jobs/${path(job.id)}/control`,
				body: { paused: !job.paused, priority: job.priority ?? 0, rate: job.rate ?? 0 },
			});
		}
		if (job.state === 'running')
			actions.push({ label: 'stop', path: `/files/jobs/${path(job.id)}/cancel`, body: {} });
		if (job.retryable && !job.blocked)
			actions.push({ label: 'retry', path: `/files/jobs/${path(job.id)}/retry`, body: {} });
		result.push({
			id: `file:${job.id}`,
			name: `${job.from} → ${job.to}`,
			kind: 'files',
			state,
			href: '/files',
			detail: job.error || job.current,
			progress: percent(job.copied, job.bytes),
			actions,
		});
	}
	const maintenance = data.maintenance;
	if (maintenance?.error || (maintenance?.settings.enabled && maintenance.available === false))
		result.push({
			id: 'maintenance:error',
			name: 'maintenance',
			titleKey: 'ops.maintenance',
			kind: 'maintenance',
			state: 'blocked',
			href: '/tools/maintenance',
			detail: maintenance.error,
		});
	for (const view of maintenance?.jobs ?? []) {
		const { job, state } = view;
		if (state.running || state.blocked || state.error)
			result.push({
				id: `maintenance:${job.id}`,
				name: job.name,
				kind: 'maintenance',
				state: state.running ? 'running' : state.blocked ? 'blocked' : 'failed',
				href: '/tools/maintenance',
				detail: state.error || state.phase,
				actions: state.running
					? [{ label: 'stop', path: `/maintenance/${path(job.id)}/stop`, body: {} }]
					: [],
			});
		if (maintenance?.settings.enabled && job.enabled && view.next)
			result.push({
				id: `schedule:${job.id}`,
				name: job.name,
				kind: 'maintenance',
				state: view.wait && !['window', 'disabled'].includes(view.wait) ? 'waiting' : 'scheduled',
				href: '/tools/maintenance',
				at: view.next,
				wait: view.wait,
			});
	}
	for (const run of maintenance?.history ?? [])
		result.push({
			id: `history:${run.id}:${run.started}`,
			name: run.name,
			kind: 'maintenance',
			state: run.error ? 'failed' : 'done',
			detail: run.error,
			at: run.finished,
			href: '/tools/maintenance',
		});
	for (const view of data.backups ?? []) {
		const { job, state } = view;
		if (view.stateKnown === false && !state.running)
			result.push({
				id: `backup-unknown:${job.id}`,
				name: job.id,
				kind: 'backup',
				state: 'unknown',
				detail: state.statusError,
				href: '/settings/backups/data',
			});
		const error = state.lastError || state.checkError || state.probe?.error;
		if (state.running || error || state.lastOK)
			result.push({
				id: `backup:${job.id}`,
				name: job.id,
				kind: 'backup',
				state: state.running ? 'running' : error ? 'failed' : 'done',
				detail: error || state.warning || state.phase,
				at: state.lastRun || state.lastOK,
				href: '/settings/backups/data',
				actions: state.running
					? [{ label: 'stop', path: `/backups/${path(job.id)}/stop`, body: {} }]
					: [{ label: 'retry', path: `/backups/${path(job.id)}/run`, body: {} }],
			});
		if (state.applicationRestore) {
			const restore = state.applicationRestore;
			result.push({
				id: `restore:${job.id}:${restore.at}`,
				name: job.id,
				kind: 'recovery',
				state: restore.verified ? 'done' : 'failed',
				detail: restore.error || restore.target,
				at: restore.at,
				href: '/settings/backups/applications?job=' + encodeURIComponent(job.id),
			});
		}
		if (view.next)
			result.push({
				id: `backup-next:${job.id}`,
				name: job.id,
				kind: 'backup',
				state: 'scheduled',
				at: view.next,
				href: '/settings/backups/data',
			});
	}
	for (const [name, job] of Object.entries(data.docker ?? {}))
		result.push({
			id: `docker:${name}`,
			name,
			kind: 'docker',
			state: job.state === 'running' ? 'running' : job.state === 'failed' ? 'failed' : 'done',
			href: '/docker',
			detail: job.state === 'failed' ? job.output : undefined,
		});
	for (const machine of data.vms ?? []) {
		if (!machine.job) continue;
		result.push({
			id: `vm:${machine.name}`,
			name: machine.displayName || machine.name,
			kind: 'vm',
			state:
				machine.job.state === 'failed'
					? 'failed'
					: machine.job.state === 'running'
						? 'running'
						: 'done',
			href: `/vms?vm=${path(machine.name)}`,
			detail: machine.job.text,
		});
	}
	for (const job of data.diskTests ?? [])
		result.push({
			id: `disk:${job.disk}`,
			name: job.disk,
			kind: 'disk',
			state: !job.result
				? 'running'
				: job.result === 'passed'
					? 'done'
					: job.result === 'cancelled'
						? 'cancelled'
						: 'failed',
			detail: job.result || job.phase,
			progress: job.percent,
			href: '/tools',
		});
	for (const job of data.snapshotRestores ?? [])
		result.push({
			id: `snapshot-restore:${job.id}`,
			name: `${job.volume}/${job.path}`,
			kind: 'restore',
			state: job.state,
			detail: job.error || (job.state === 'done' ? `${job.volume}/${job.target}` : undefined),
			at: job.finished ?? job.started,
			progress: job.state === 'running' ? folderRestoreProgress(job) : undefined,
			href: '/storage/manage?mode=snapshots',
			actions:
				job.state === 'running'
					? [{ label: 'stop', path: `/storage/snapshot-restores/${path(job.id)}/cancel`, body: {} }]
					: [],
		});
	if (data.growth?.pending)
		result.push({
			id: 'growth',
			name: data.growth.operation.volume,
			kind: 'maintenance',
			state: data.growth.active ? 'running' : 'blocked',
			detail: data.growth.error || data.growthError || data.growth.operation.error,
			href: '/storage/manage?mode=grow',
		});
	if (data.applicationRecovery?.operation)
		result.push({
			id: 'application-recovery',
			name: data.applicationRecovery.operation.job,
			kind: 'recovery',
			state: data.applicationRecovery.active ? 'running' : 'blocked',
			href: '/tools/tasks',
		});
	for (const chat of data.assistant ?? []) {
		if (!chat.run) continue;
		const run = chat.run;
		const state: TaskState =
			run.state === 'done'
				? 'done'
				: run.state === 'stopped'
					? 'cancelled'
					: ['confirmation', 'blocked'].includes(run.state)
						? 'blocked'
						: run.state === 'paused'
							? 'paused'
							: run.state === 'running'
								? 'running'
								: 'waiting';
		const actions: TaskAction[] = [];
		if (isActive({ state } as TaskItem))
			actions.push({
				label: state === 'paused' ? 'resume' : 'pause',
				path: `/assistant/chats/${path(chat.id)}/control`,
				body: { action: state === 'paused' ? 'resume' : 'pause' },
			});
		if (['running', 'paused', 'waiting'].includes(state))
			actions.push({
				label: 'stop',
				path: `/assistant/chats/${path(chat.id)}/control`,
				body: { action: 'stop' },
			});
		result.push({
			id: `assistant:${chat.id}`,
			name: chat.title,
			kind: 'assistant',
			state,
			detail: run.detail,
			at: chat.updated,
			href: `/assistant?chat=${path(chat.id)}`,
			actions,
		});
	}
	for (const automation of data.automations ?? []) {
		if (!automation.enabled) continue;
		const next = automation.next && Date.parse(automation.next) > 0 ? automation.next : undefined;
		result.push({
			id: `automation:${automation.id}`,
			name: automation.name,
			kind: 'automation',
			state: automation.pending || !next ? 'waiting' : 'scheduled',
			detail: automation.detail,
			at: next,
			wait: automation.kind === 'event' ? 'event' : undefined,
			href: '/assistant/automations',
		});
	}
	for (const upload of data.uploads ?? [])
		result.push({
			id: `upload:${upload.id}`,
			name: upload.name,
			kind: 'upload',
			state: upload.busy ? 'running' : 'waiting',
			progress: percent(upload.offset, upload.size),
			href: '/files',
		});
	for (const clone of data.clones ?? [])
		result.push({
			id: `clone:${clone.id}`,
			name: clone.vm.name,
			kind: 'recovery',
			state: data.vms?.some(
				(v) => v.name === clone.source && v.job?.state === 'running' && v.job.operation === 'clone',
			)
				? 'running'
				: 'blocked',
			detail: clone.error || clone.phase,
			href: '/tools/tasks',
		});
	if (data.storageActive)
		result.push({
			id: 'storage-active',
			name: 'storage',
			titleKey: 'tasks.storageAction',
			kind: 'maintenance',
			state: 'running',
			href: '/storage',
		});
	const sources: Record<string, MessageKey> = {
		assistant: 'assistant.title',
		automations: 'tasks.kind.automation',
		docker: 'nav.docker',
		'application-recovery': 'ops.appRecover',
		'vm-clones': 'ops.cloneRecovery',
	};
	for (const source of data.unavailable ?? [])
		result.push({
			id: `unavailable:${source}`,
			name: source,
			titleKey: sources[source],
			kind: 'system',
			state: 'blocked',
			href: '/tools/tasks',
		});
	return result;
}
