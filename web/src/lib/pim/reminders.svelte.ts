// 1.13 (docs/KALENDER.md "Hinweise zu Erinnerungen"): the reminders of the
// account's events as hints in TitanOS. titand plans them (backend
// api/calendar_reminders.go) and names the newest in /system
// (remindersLatest, polled by the shell anyway); when it changes the
// shell reads GET /calendar/reminders and shows each hint once as a
// toast – also when Kalender is not open. Seen ones are kept per browser
// (localStorage), so a reload or a second tab does not show them again;
// one due more than 15 minutes ago is not shown any more.
import { untrack } from 'svelte';
import { api } from '../api.ts';
import { t } from '../i18n/index.svelte.ts';
import { system } from '../system.svelte.ts';
import { showToast } from '../toast.svelte.ts';
import { startText } from './calendar-text.ts';

const SEEN = 'titanos.calendar.remindersSeen';
const SHOW_FOR = 60_000;
const STALE = 15 * 60_000;

type Hint = {
	seq: number;
	at: string;
	title: string;
	start: string;
	allDay: boolean;
	location?: string;
};

function readSeen(): { epoch: string; seq: number } {
	try {
		const v = JSON.parse(localStorage.getItem(SEEN) ?? 'null');
		if (v && typeof v.epoch === 'string' && typeof v.seq === 'number') return v;
	} catch {
		// Shown again after a reload: the least harm.
	}
	return { epoch: '', seq: 0 };
}
function saveSeen(epoch: string, seq: number) {
	try {
		localStorage.setItem(SEEN, JSON.stringify({ epoch, seq }));
	} catch {
		// Kept for this page only.
	}
}

let reading = false;
async function show(latest: string) {
	const [epoch, n] = latest.split('-');
	const seen = readSeen();
	const after = seen.epoch === epoch ? seen.seq : 0;
	if (reading || Number(n) <= after) return;
	reading = true;
	try {
		const res = await api.get<{ epoch?: string; reminders?: Hint[] }>('/calendar/reminders');
		const list = Array.isArray(res?.reminders) ? res.reminders : [];
		let last = after;
		for (const r of list) {
			if (typeof r?.seq !== 'number' || r.seq <= after) continue;
			last = Math.max(last, r.seq);
			if (Date.now() - Date.parse(r.at) > STALE) continue;
			const title = (typeof r.title === 'string' && r.title.trim()) || t('calendar.untitled');
			const when = startText({ start: String(r.start ?? ''), allDay: r.allDay === true });
			showToast(
				t('calendar.reminder.toast', {
					title,
					when: r.location ? `${when} · ${r.location}` : when,
				}),
				'info',
				SHOW_FOR,
			);
		}
		saveSeen(epoch, last);
	} catch {
		// Not read now: the next change of remindersLatest tries again.
	} finally {
		reading = false;
	}
}

/** Follows remindersLatest of /system; returns the function that stops. */
export function watchReminders(): () => void {
	return $effect.root(() => {
		$effect(() => {
			const latest = system.info?.remindersLatest ?? '';
			if (!latest) return;
			untrack(() => void show(latest));
		});
	});
}
