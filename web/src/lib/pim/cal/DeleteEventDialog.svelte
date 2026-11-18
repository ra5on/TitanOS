<script lang="ts">
	// 1.11 (step 15): deleting an event (draft 07). A series asks which
	// events are meant (only this one, this and all following, the whole
	// series – "Alle N Termine" when titand counts them); the request
	// carries scope, occurrence and etag. "Löschen" stands left of
	// "Abbrechen" (Dialog.svelte orders it), "Abbrechen" comes first in the
	// markup and gets the focus; Enter never deletes. A changed event
	// (calendar_conflict) says so and can be read again.
	import { ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { CalEvent } from '../calendar.ts';
	import { deleteEvent, loadEvent, type Scope } from '../calendar-api.ts';
	import { eventTitle, ruleText, startText } from '../calendar-text.ts';
	import ScopeChoices from './ScopeChoices.svelte';

	type Props = {
		event: CalEvent;
		onclose: () => void;
		/** Deleted; `scope` as titand applied it. */
		ondone: (scope: Scope) => void;
	};
	let { event, onclose, ondone }: Props = $props();

	// svelte-ignore state_referenced_locally
	let ev = $state(event);
	let scope = $state<Scope>('this');
	let busy = $state(false);
	let error = $state('');
	let conflict = $state(false);

	async function remove() {
		busy = true;
		error = '';
		conflict = false;
		try {
			const res = await deleteEvent(ev.calendar, ev.id, {
				scope: ev.recurring ? scope : 'all',
				occurrence: ev.occurrence,
				etag: ev.etag,
			});
			ondone(res?.scope ?? scope);
		} catch (err) {
			conflict = err instanceof ApiError && err.code === 'calendar_conflict';
			error = conflict ? t('calendar.conflict') : apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	async function reload() {
		busy = true;
		try {
			ev = await loadEvent(ev.calendar, ev.id, ev.occurrence);
			error = '';
			conflict = false;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	const rule = $derived(ruleText(ev));
</script>

<Dialog title={t('calendar.del.title', { title: eventTitle(ev) })} locked={busy} {onclose}>
	{#if ev.recurring}
		<p class="lead">
			{rule
				? t('calendar.del.series', { when: startText(ev), rule })
				: t('calendar.del.seriesPlain', { when: startText(ev) })}
		</p>
		<ScopeChoices
			bind:value={scope}
			mode="delete"
			total={ev.occurrences}
			label={t('calendar.del.which')}
		/>
	{:else}
		<p class="lead">{t('calendar.del.single', { when: startText(ev) })}</p>
	{/if}
	<p class="muted">{t('calendar.del.sync')}</p>
	{#if error}
		<p class="notice-error" role="alert">
			{error}
			{#if conflict}<button type="button" class="btn reload" onclick={reload}
					>{t('calendar.reload')}</button
				>{/if}
		</p>
	{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button
		>
		<button type="button" class="btn btn-danger" disabled={busy || conflict} onclick={remove}
			>{t('calendar.del.confirm')}</button
		>
	</div>
</Dialog>

<style>
	.lead {
		margin: 0;
		overflow-wrap: break-word;
	}
	.muted {
		margin: 0;
	}
	.lead + .muted {
		margin-top: var(--space-3);
	}
	.notice-error {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.reload {
		min-height: 36px;
	}
</style>
