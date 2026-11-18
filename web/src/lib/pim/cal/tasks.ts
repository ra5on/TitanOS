// 1.13 (Baustand step 11, docs/KALENDER.md "Erinnerungen (VTODO, seit
// 1.13)"): the reminders in Kalender – their lists in the side bar and the
// open ones that are due on a shown day. Checked like the events (any
// field of a device is text; ids go through encodeURIComponent). No
// Svelte, no DOM.
import { api } from '#lib/api.ts';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { wallOf } from '../caldates.ts';

export type TaskList = {
	id: string;
	name: string;
	color: string;
	order: number;
	count: number;
	open: number;
	virtual: boolean;
};

/** An item of a list (GET /calendar/tasks, /calendar/tasks/due). */
export type DueTask = {
	id: string;
	list: string;
	uid: string;
	title: string;
	clipped: boolean;
	/** A date (dueAllDay) or an RFC 3339 time; '' without. */
	due: string;
	dueAllDay: boolean;
	done: boolean;
	completed: string;
	/** 1 high, 5 middle, 9 low; 0 none. */
	priority: number;
	recurring: boolean;
	alarm: boolean;
	etag: string;
};

/** One reminder with what the form edits (GET /calendar/tasks/{list}/{id}). */
export type Task = DueTask & {
	notes: string;
	timezone: string;
	/** Times of its reminders (absolute, RFC 3339 or dates). */
	alarms: string[];
};

export type TaskInput = {
	list?: string;
	title?: string;
	notes?: string;
	due?: string | null;
	timezone?: string;
	priority?: number;
	alarms?: string[] | null;
	lang?: string;
};

const str = (v: unknown) => (typeof v === 'string' ? v : '');
const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const enc = encodeURIComponent;

export function sanitizeTaskLists(raw: unknown): TaskList[] | null {
	if (!Array.isArray(raw)) return null;
	return raw
		.filter((l): l is Record<string, unknown> => !!l && typeof l === 'object')
		.map((l) => ({
			id: str(l.id),
			name: str(l.name),
			color: /^#[0-9a-fA-F]{6}/.test(str(l.color))
				? str(l.color).slice(0, 7).toLowerCase()
				: '#c2410c',
			order: num(l.order),
			count: Math.max(0, Math.floor(num(l.count))),
			open: Math.max(0, Math.floor(num(l.open))),
			virtual: l.virtual === true,
		}))
		.filter((l) => l.id !== '');
}

export function sanitizeTask(raw: unknown): Task | null {
	if (!raw || typeof raw !== 'object') return null;
	const t = raw as Record<string, unknown>;
	if (!str(t.id) || !str(t.list)) return null;
	const priority = Math.floor(num(t.priority));
	return {
		id: str(t.id),
		list: str(t.list),
		uid: str(t.uid),
		title: str(t.title),
		clipped: t.clipped === true,
		due: str(t.due),
		dueAllDay: t.dueAllDay === true,
		done: t.done === true,
		completed: str(t.completed),
		priority: priority >= 0 && priority <= 9 ? priority : 0,
		recurring: t.recurring === true,
		alarm: t.alarm === true,
		etag: str(t.etag),
		notes: str(t.notes),
		timezone: str(t.timezone),
		alarms: Array.isArray(t.alarms)
			? t.alarms.filter((a): a is string => typeof a === 'string')
			: [],
	};
}

function sanitizeItems(raw: unknown): DueTask[] {
	const list = (raw as { tasks?: unknown } | null)?.tasks;
	if (!Array.isArray(list)) throw new Error('unexpected answer');
	return list.map(sanitizeTask).filter((t): t is Task => t !== null);
}

const lang = () => (i18n.locale === 'en' ? 'en' : 'de');

export async function loadTaskLists(): Promise<TaskList[]> {
	const list = sanitizeTaskLists(await api.get<unknown>(`/calendar/tasklists?lang=${lang()}`));
	if (!list) throw new Error('unexpected answer');
	return list;
}
export function createTaskList(name: string, color: string) {
	return api.post<unknown>('/calendar/tasklists', { name, color });
}
export function updateTaskList(
	id: string,
	change: { name?: string; color?: string; order?: number },
) {
	return api.put<unknown>(`/calendar/tasklists/${enc(id)}`, change);
}
export function removeTaskList(id: string) {
	return api.delete(`/calendar/tasklists/${enc(id)}`);
}

/** Reminders of a list: open, done or all. */
export async function loadTasks(list: string, status: 'open' | 'done' | 'all'): Promise<DueTask[]> {
	return sanitizeItems(
		await api.get<unknown>(`/calendar/tasks?list=${enc(list)}&status=${status}`),
	);
}
/** Open reminders of all lists due in [from, to) (dates). */
export async function loadDueTasks(from: string, to: string): Promise<DueTask[]> {
	return sanitizeItems(
		await api.get<unknown>(`/calendar/tasks/due?from=${enc(from)}&to=${enc(to)}`),
	);
}
export async function loadTask(list: string, id: string): Promise<Task> {
	const t = sanitizeTask(await api.get<unknown>(`/calendar/tasks/${enc(list)}/${enc(id)}`));
	if (!t) throw new Error('unexpected answer');
	return t;
}
export function createTask(input: TaskInput) {
	return api.post<unknown>('/calendar/tasks', { ...input, lang: lang() });
}
export function updateTask(list: string, id: string, etag: string, input: TaskInput) {
	return api.put<unknown>(`/calendar/tasks/${enc(list)}/${enc(id)}`, { etag, ...input });
}
export function setTaskDone(t: Pick<DueTask, 'list' | 'id' | 'etag'>, done: boolean) {
	return api.post<{ task?: unknown; copy?: unknown }>(
		`/calendar/tasks/${enc(t.list)}/${enc(t.id)}/done`,
		{ etag: t.etag, done },
	);
}
export function deleteTask(t: Pick<DueTask, 'list' | 'id' | 'etag'>) {
	return api.delete(`/calendar/tasks/${enc(t.list)}/${enc(t.id)}?etag=${enc(t.etag)}`);
}

/** The day a reminder is due on in a zone ('' without a due date). */
export function dueDay(t: Pick<DueTask, 'due' | 'dueAllDay'>, zone: string): string {
	if (!t.due) return '';
	if (t.dueAllDay || t.due.length === 10) return t.due.slice(0, 10);
	const d = new Date(t.due);
	return Number.isNaN(d.getTime()) ? t.due.slice(0, 10) : wallOf(d, zone).date;
}
/** Minutes after midnight of a timed due in a zone; null for a date. */
export function dueMinutes(t: Pick<DueTask, 'due' | 'dueAllDay'>, zone: string): number | null {
	if (!t.due || t.dueAllDay || t.due.length === 10) return null;
	const d = new Date(t.due);
	return Number.isNaN(d.getTime()) ? null : wallOf(d, zone).minutes;
}

/** The reminders per day (by time, those without a time first). */
export function tasksByDay(list: DueTask[], zone: string): Map<string, DueTask[]> {
	const out = new Map<string, DueTask[]>();
	for (const t of list) {
		const day = dueDay(t, zone);
		if (!day) continue;
		out.set(day, [...(out.get(day) ?? []), t]);
	}
	for (const l of out.values())
		l.sort(
			(a, b) =>
				(dueMinutes(a, zone) ?? -1) - (dueMinutes(b, zone) ?? -1) || a.title.localeCompare(b.title),
		);
	return out;
}
