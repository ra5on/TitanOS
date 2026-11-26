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
	backupProblems: { id: string; label: string; missingUSB: boolean }[];
	loaded: boolean;
	partial: boolean;
};

/** Unknown totals stay indeterminate. Never fabricate completion percentages. */
export function taskPercent(done: number, total: number): number | undefined {
	if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) return undefined;
	return Math.min(100, Math.max(0, (done / total) * 100));
}
