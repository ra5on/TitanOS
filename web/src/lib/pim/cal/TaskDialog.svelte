<script lang="ts">
	// 1.13 (Baustand step 11): a reminder of Kalender – a new one or one to
	// change: title, list, due date (optional, with a time or without),
	// priority, a reminder at the due time, notes. Deleting asks in a dialog
	// of its own (the button left of "Abbrechen", never on Enter). A
	// reminder changed elsewhere meanwhile (task_conflict) says so and can
	// be read again. Everything else the file holds (X-APPLE-…, other
	// alarms, a rule) stays (pim/tasks edits the component tree).
	// A draft of the KI-Agent (`draft`, its fields) is put into the form
	// once the reminder is read: text to check, saved only with "Sichern".
	import { ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { wallOf, viewZone } from '../caldates.ts';
	import {
		createTask,
		deleteTask,
		loadTask,
		updateTask,
		type DueTask,
		type Task,
		type TaskInput,
		type TaskList,
	} from './tasks.ts';
	import { taskDraftForm } from '#lib/agent/drafts.ts';

	type Props = {
		/** The reminder to change; none = a new one. */
		task?: Pick<DueTask, 'list' | 'id'>;
		lists: TaskList[];
		/** The list of a new one. */
		list?: string;
		/** A first due date of a new one. */
		due?: string;
		/** The fields of a draft of the KI-Agent to put in. */
		draft?: Record<string, unknown>;
		onclose: () => void;
		onchanged: () => void;
	};
	let { task, lists, list = '', due = '', draft, onclose, onchanged }: Props = $props();

	type Form = {
		title: string;
		list: string;
		hasDue: boolean;
		date: string;
		hasTime: boolean;
		time: string;
		priority: number;
		alarm: boolean;
		notes: string;
	};
	const today = wallOf(new Date(), viewZone()).date;
	// svelte-ignore state_referenced_locally
	const editing = !!task;
	// svelte-ignore state_referenced_locally
	let form = $state<Form>({
		title: '',
		list: list || lists[0]?.id || '',
		hasDue: !!due,
		date: due || today,
		hasTime: false,
		time: '09:00',
		priority: 0,
		alarm: false,
		notes: '',
	});
	let initial = $state('');
	let full = $state<Task | null>(null);
	let loading = $state(editing);
	let busy = $state(false);
	let error = $state('');
	let conflict = $state(false);
	let askDelete = $state(false);

	function formOf(x: Task): Form {
		const timed = !!x.due && !x.dueAllDay && x.due.length > 10;
		return {
			title: x.title,
			list: x.list,
			hasDue: !!x.due,
			date: x.due ? x.due.slice(0, 10) : today,
			hasTime: timed,
			time: timed ? x.due.slice(11, 16) : '09:00',
			priority: x.priority,
			alarm: x.alarms.length > 0,
			notes: x.notes,
		};
	}
	/** The draft goes in once, over what was there (the change is the difference). */
	let drafted = false;
	function putDraft() {
		if (!draft || drafted) return;
		drafted = true;
		form = taskDraftForm(
			form,
			draft,
			lists.map((l) => l.id),
		);
	}
	async function read() {
		if (!task) {
			initial = JSON.stringify(form);
			putDraft();
			return;
		}
		loading = true;
		try {
			full = await loadTask(task.list, task.id);
			form = formOf(full);
			initial = JSON.stringify(form);
			putDraft();
			error = '';
			conflict = false;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void read();
	});

	const dueValue = $derived(
		form.hasDue ? (form.hasTime ? `${form.date}T${form.time}` : form.date) : null,
	);
	function input(): TaskInput {
		const base = initial ? (JSON.parse(initial) as Form) : null;
		const out: TaskInput = {};
		const changed = (k: keyof Form) => !full || !base || form[k] !== base[k];
		if (changed('title')) out.title = form.title.trim();
		if (changed('notes')) out.notes = form.notes;
		if (changed('priority')) out.priority = form.priority;
		if (changed('hasDue') || changed('date') || changed('hasTime') || changed('time'))
			out.due = dueValue;
		if (changed('alarm') || (form.alarm && out.due !== undefined))
			out.alarms = form.alarm && dueValue ? [dueValue] : null;
		if (form.list && (!full || form.list !== full.list)) out.list = form.list;
		return out;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!form.title.trim()) {
			error = t('calendar.tasks.titleMissing');
			return;
		}
		busy = true;
		error = '';
		conflict = false;
		try {
			if (full) await updateTask(full.list, full.id, full.etag, input());
			else await createTask({ ...input(), list: form.list });
			onchanged();
			onclose();
		} catch (err) {
			conflict = err instanceof ApiError && err.code === 'task_conflict';
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	async function remove() {
		if (!full) return;
		busy = true;
		try {
			await deleteTask(full);
			onchanged();
			onclose();
		} catch (err) {
			askDelete = false;
			conflict = err instanceof ApiError && err.code === 'task_conflict';
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	const priorities = $derived([
		{ value: '0', label: t('calendar.tasks.prio.none') },
		{ value: '9', label: t('calendar.tasks.prio.low') },
		{ value: '5', label: t('calendar.tasks.prio.medium') },
		{ value: '1', label: t('calendar.tasks.prio.high') },
	]);
	const priority = $derived(
		String(form.priority === 0 ? 0 : form.priority < 5 ? 1 : form.priority > 5 ? 9 : 5),
	);
</script>

{#if askDelete && full}
	<Dialog
		title={t('calendar.tasks.delTitle', { title: full.title })}
		locked={busy}
		onclose={() => (askDelete = false)}
	>
		<p class="lead">{t('calendar.tasks.delText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (askDelete = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				><Icon name="trash" size={15} />{t('calendar.tasks.delete')}</button
			>
		</div>
	</Dialog>
{:else}
	<Dialog
		title={task ? t('calendar.tasks.editTitle') : t('calendar.tasks.newTitle')}
		locked={busy}
		{onclose}
	>
		{#if loading}
			<p class="muted" role="status">{t('common.loading')}</p>
		{:else}
			<form class="form" onsubmit={save} data-task-dialog>
				{#if draft}<p class="notice-warn" role="note" data-agent-draft>
						{t('agentpim.draft.taskNote')}
					</p>{/if}
				<label class="row">
					<span class="lbl">{t('calendar.tasks.title')}</span>
					<input class="field" type="text" bind:value={form.title} maxlength="2000" required />
				</label>
				{#if lists.length > 1}
					<label class="row">
						<span class="lbl">{t('calendar.tasks.list')}</span>
						<select class="field" bind:value={form.list}>
							{#each lists as l (l.id)}<option value={l.id}>{l.name}</option>{/each}
						</select>
					</label>
				{/if}
				<div class="row">
					<label class="check"
						><input type="checkbox" bind:checked={form.hasDue} />{t('calendar.tasks.hasDue')}</label
					>
					{#if form.hasDue}
						<span class="when">
							<input
								class="field"
								type="date"
								bind:value={form.date}
								aria-label={t('calendar.tasks.dueDate')}
							/>
							<label class="check"
								><input type="checkbox" bind:checked={form.hasTime} />{t(
									'calendar.tasks.hasTime',
								)}</label
							>
							{#if form.hasTime}<input
									class="field"
									type="time"
									bind:value={form.time}
									aria-label={t('calendar.tasks.dueTime')}
								/>{/if}
						</span>
						<label class="check"
							><input type="checkbox" bind:checked={form.alarm} />{t('calendar.tasks.alarm')}</label
						>
					{/if}
				</div>
				<div class="row">
					<span class="lbl" id="task-prio">{t('calendar.tasks.priority')}</span>
					<Segments
						compact
						label={t('calendar.tasks.priority')}
						value={priority}
						options={priorities}
						onchange={(v) => (form.priority = Number(v))}
					/>
				</div>
				<label class="row">
					<span class="lbl">{t('calendar.tasks.notes')}</span>
					<textarea class="field" rows="3" bind:value={form.notes}></textarea>
				</label>
				{#if error}
					<p class="notice-error" role="alert">
						{error}
						{#if conflict}<button type="button" class="btn" onclick={read}
								>{t('calendar.reload')}</button
							>{/if}
					</p>
				{/if}
				<div class="dialog-actions">
					{#if full}
						<button
							type="button"
							class="btn btn-danger"
							disabled={busy}
							onclick={() => (askDelete = true)}
							><Icon name="trash" size={15} />{t('calendar.tasks.deleteAsk')}</button
						>
					{/if}
					<button type="button" class="btn" disabled={busy} onclick={onclose}
						>{t('common.cancel')}</button
					>
					<button type="submit" class="btn btn-primary" disabled={busy}
						>{t('calendar.tasks.save')}</button
					>
				</div>
			</form>
		{/if}
	</Dialog>
{/if}

<style>
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: min(28rem, 100%);
	}
	.row {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.lbl {
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.when {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.check {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 36px;
	}
	.check input {
		width: 16px;
		height: 16px;
		accent-color: var(--accent);
	}
	textarea {
		resize: vertical;
	}
</style>
