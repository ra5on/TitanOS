// Short states next to some settings areas (sidebar and overview). Read once
// when the settings open, only from answers titand keeps in RAM (no polling,
// no stick or volume access); overdue backups come from the messages the bell
// has loaded anyway.
import { api } from '#lib/api.ts';
import type { MessageKey } from '#lib/i18n/index.svelte.ts';
import { notifications } from '#lib/notifications.svelte.ts';

export type AreaState = { text: MessageKey; tone: 'warn' | 'error' | 'info' };

const read = $state<{ states: Record<string, AreaState> }>({ states: {} });

/** Reads the states once; call when the settings open (not on every page). */
export function loadSettingsStates(admin: boolean) {
	if (!admin) {
		read.states = {};
		return;
	}
	void Promise.allSettled([
		api.get<{ available: unknown; pending?: unknown }>('/update'),
		api.get<{ status?: { onBattery?: boolean; lowBattery?: boolean } }>('/ups'),
		api.get<{ notAfter: string }>('/system/certificate'),
	]).then(([update, ups, certificate]) => {
		const next: Record<string, AreaState> = {};
		if (update.status === 'fulfilled' && update.value?.available)
			next.updates = { text: 'settings.state.update', tone: 'info' };
		const power = ups.status === 'fulfilled' ? ups.value?.status : undefined;
		if (power?.onBattery)
			next.ups = {
				text: power.lowBattery ? 'settings.state.lowBattery' : 'settings.state.onBattery',
				tone: power.lowBattery ? 'error' : 'warn',
			};
		if (certificate.status === 'fulfilled' && certificate.value?.notAfter) {
			const days = (Date.parse(certificate.value.notAfter) - Date.now()) / 86_400_000;
			if (days < 0) next.access = { text: 'settings.state.certExpired', tone: 'error' };
			else if (days < 14) next.access = { text: 'settings.state.certExpiring', tone: 'warn' };
		}
		read.states = next;
	});
}

/** The current states by area id; reactive when used inside $derived. */
export function settingsStates(): Record<string, AreaState> {
	const states = { ...read.states };
	// Per backup job, the newest of its messages tells whether it is overdue.
	const newest = new Map<string, string>();
	for (const notice of notifications.list)
		if (notice.kind.startsWith('backup_') && !newest.has(notice.subject))
			newest.set(notice.subject, notice.kind);
	if ([...newest.values()].includes('backup_overdue'))
		states.backups = { text: 'settings.state.backupOverdue', tone: 'warn' };
	return states;
}

/** What the settings search holds; shared by the sidebar and the overview. */
export const settingsSearch = $state({ query: '', active: 0 });
