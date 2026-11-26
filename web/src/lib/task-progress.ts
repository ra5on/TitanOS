import type { MessageKey } from './i18n/index.svelte.ts';

export type TaskItem = {
	id: string;
	title: string;
	kind: MessageKey;
	href: string;
	percent?: number;
	attention?: boolean;
	stateLabel?: MessageKey;
};

/** The desktop's existing task poll supplies its status widget too. */
export type WorkspaceActivity = {
	tasks: TaskItem[];
	backupProblems: { id: string; label: string; message: MessageKey }[];
	loaded: boolean;
	partial: boolean;
};

/** Unknown totals stay indeterminate. Never fabricate completion percentages. */
export function taskPercent(done: number, total: number): number | undefined {
	if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) return undefined;
	return Math.min(100, Math.max(0, (done / total) * 100));
}

/** A scheduled USB job retries on insertion (backup.Manager.Plugged). A
 * disabled job or a different fault still needs attention from the owner. */
export function backupProblemMessage(
	job: { enabled?: boolean; destination?: { type?: string; deviceId?: string } },
	state: { lastErrorCode?: string; failingSince?: string },
): MessageKey {
	if (state.lastErrorCode !== 'usb_not_found') return 'desktop.status.backupFailed';
	const dest = job.destination;
	if (
		job.enabled &&
		state.failingSince &&
		dest?.deviceId &&
		(dest.type === 'usb' || dest.type === 'usb-copy')
	) {
		return 'desktop.status.waitingUSB';
	}
	return 'desktop.status.missingUSB';
}
