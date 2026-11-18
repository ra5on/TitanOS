// 1.13 (Baustand step 11): the reminders of the page of Kalender – the
// lists (sorted by their own order, which the iPhone shares), the chosen
// list's open (and on request done) reminders, and the open ones due in
// the shown window for the views. A change of the live stream (kind
// "tasks") reads the lists, the chosen list and the due ones again.
import { apiErrorText } from '#lib/i18n/index.svelte.ts';
import { addDays } from '../caldates.ts';
import {
	loadDueTasks,
	loadTaskLists,
	loadTasks,
	setTaskDone,
	updateTaskList,
	type DueTask,
	type TaskList,
} from './tasks.ts';

const CHOSEN = 'titanos.calendar.taskList';

export class TaskData {
	lists = $state<TaskList[]>([]);
	/** The chosen list ('' until the lists came). */
	chosen = $state('');
	items = $state<DueTask[]>([]);
	showDone = $state(false);
	due = $state<DueTask[]>([]);
	error = $state('');
	/** The lists could be read (false: the section says so, the calendar stays). */
	ready = $state(false);
	#from = '';
	#to = '';
	#timer: ReturnType<typeof setTimeout> | undefined;

	async start() {
		try {
			this.lists = await loadTaskLists();
			let kept = '';
			try {
				kept = localStorage.getItem(CHOSEN) ?? '';
			} catch {
				// A list is chosen anew.
			}
			this.chosen = this.lists.some((l) => l.id === kept) ? kept : (this.lists[0]?.id ?? '');
			this.ready = true;
			await this.loadItems();
		} catch (err) {
			this.error = apiErrorText(err);
		}
	}

	choose(id: string) {
		this.chosen = id;
		try {
			localStorage.setItem(CHOSEN, id);
		} catch {
			// Chosen for this page only.
		}
		void this.loadItems();
	}

	async loadItems() {
		if (!this.chosen) return;
		const list = this.chosen;
		try {
			const got = await loadTasks(list, this.showDone ? 'all' : 'open');
			if (list === this.chosen) this.items = got;
			this.error = '';
		} catch (err) {
			this.error = apiErrorText(err);
		}
	}

	async reloadLists() {
		try {
			this.lists = await loadTaskLists();
			if (!this.lists.some((l) => l.id === this.chosen)) this.chosen = this.lists[0]?.id ?? '';
		} catch (err) {
			this.error = apiErrorText(err);
		}
	}

	/** The open reminders due on the shown days ([first, last], a day around). */
	async showDays(first: string, last: string, again = false) {
		const from = addDays(first, -1);
		const to = addDays(last, 2);
		if (!again && from === this.#from && to === this.#to) return;
		this.#from = from;
		this.#to = to;
		try {
			const got = await loadDueTasks(from, to);
			if (from === this.#from && to === this.#to) this.due = got;
		} catch {
			// The views show the events without them; the side bar says what failed.
		}
	}

	/** Everything again (a change of the stream, or one made here). */
	refresh() {
		clearTimeout(this.#timer);
		this.#timer = setTimeout(() => {
			void this.reloadLists();
			void this.loadItems();
			if (this.#from) void this.showDays(this.#from, addDays(this.#to, -2), true).catch(() => {});
		}, 200);
	}

	/** Ticks a reminder off or opens it again; shown at once. */
	async toggleDone(task: DueTask) {
		const done = !task.done;
		const mark = (list: DueTask[]) =>
			list.map((x) => (x.list === task.list && x.id === task.id ? { ...x, done } : x));
		this.items = mark(this.items);
		this.due = mark(this.due);
		try {
			await setTaskDone(task, done);
			this.error = '';
		} catch (err) {
			this.error = apiErrorText(err);
		}
		this.refresh();
	}

	/** The lists in a new order: each list whose place changed is told its order. */
	async sort(ids: string[]) {
		const before = this.lists;
		this.lists = ids
			.map((id, i) => {
				const l = before.find((x) => x.id === id);
				return l ? { ...l, order: i } : null;
			})
			.filter((l): l is TaskList => l !== null);
		try {
			await Promise.all(
				this.lists
					.filter((l) => before.find((x) => x.id === l.id)?.order !== l.order)
					.map((l) => updateTaskList(l.id, { order: l.order })),
			);
		} catch (err) {
			this.error = apiErrorText(err);
			void this.reloadLists();
		}
	}

	stop() {
		clearTimeout(this.#timer);
	}
}
