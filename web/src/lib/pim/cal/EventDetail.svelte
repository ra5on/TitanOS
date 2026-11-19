<script lang="ts">
	// 1.11 (step 15): an event opened from a view, with every field: when,
	// calendar (color and name), repetition in words, reminders, place,
	// notes. All of it is text from devices: shown as text, never as HTML;
	// in notes only http(s), mailto and tel become links, the link's text is
	// its real target, they open with rel="noopener noreferrer". Birthdays
	// come from the contacts and are read-only.
	// 1.15 (draft 35): on the phone the fields stand as rows of grouped
	// cards with their word ("Kalender", "Hinweis", …) instead of a sign,
	// the place and the time under the title.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { calColor, linkPieces, safeHref, type Calendar, type CalEvent } from '../calendar.ts';
	import { loadEvent } from '../calendar-api.ts';
	import {
		alarmText,
		calName,
		detailTitle,
		ruleText,
		whenText,
		zoneName,
	} from '../calendar-text.ts';
	import { viewZone } from '../caldates.ts';

	type Props = {
		event: CalEvent;
		calFor: (id: string) => Calendar | undefined;
		onclose: () => void;
		onedit: (ev: CalEvent) => void;
		ondelete: (ev: CalEvent) => void;
		/** 1.13: asks the KI-Agent about this event (its panel, the event as reference). */
		onagent?: (ev: CalEvent) => void;
	};
	let { event, calFor, onclose, onedit, ondelete, onagent }: Props = $props();
	const phone = phoneView();

	// svelte-ignore state_referenced_locally
	let ev = $state<CalEvent>(event);
	let full = $state(false);
	let error = $state('');
	$effect(() => {
		const e = event;
		loadEvent(e.calendar, e.id, e.occurrence)
			.then((got) => {
				ev = got;
				full = true;
			})
			.catch((err) => (error = apiErrorText(err)));
	});

	const cal = $derived(calFor(ev.calendar));
	const rule = $derived(ruleText(ev));
	const zone = $derived(!ev.allDay && ev.timezone && ev.timezone !== viewZone() ? ev.timezone : '');
	const birthday = $derived(ev.kind === 'birthday');
	/** Minutes after midnight of the start on the event's own clock. */
	const startOf = (e: CalEvent) =>
		e.allDay ? 0 : Number(e.start.slice(11, 13)) * 60 + Number(e.start.slice(14, 16));
</script>

<Dialog title={detailTitle(ev)} {onclose}>
	<dl class="detail" class:phone data-event-detail>
		<div class="row">
			<dt>
				{#if phone}<span class="word">{t('calendar.detail.when')}</span>{:else}<Icon
						name="clock"
						size={16}
					/><span class="sr-only">{t('calendar.detail.when')}</span>{/if}
			</dt>
			<dd>
				{whenText(ev)}
				{#if zone}<small class="muted">{t('calendar.zone', { zone: zoneName(zone) })}</small>{/if}
				{#if ev.floating && !ev.allDay}<small class="muted">{t('calendar.detail.floating')}</small
					>{/if}
				{#if ev.tzGuess}<small class="muted">{t('calendar.detail.tzGuess')}</small>{/if}
			</dd>
		</div>
		<div class="row">
			<dt>
				{#if phone}<span class="word">{t('calendar.form.calendar')}</span>{:else}<Icon
						name="calendar"
						size={16}
					/><span class="sr-only">{t('calendar.form.calendar')}</span>{/if}
			</dt>
			<dd class="cal" style:--c={calColor(cal?.color ?? '')}>
				<span class="sw" aria-hidden="true"></span>{calName(cal)}
			</dd>
		</div>
		{#if ev.recurring}
			<div class="row">
				<dt>
					{#if phone}<span class="word">{t('calendar.form.repeat')}</span>{:else}<Icon
							name="repeat"
							size={16}
						/><span class="sr-only">{t('calendar.form.repeat')}</span>{/if}
				</dt>
				<dd>
					{rule || t('calendar.repeats')}
					{#if ev.recurUnsupported}<small class="muted"
							>{t('calendar.detail.recurUnsupported')}</small
						>{/if}
					{#if ev.exception}<small class="muted">{t('calendar.detail.exception')}</small>{/if}
				</dd>
			</div>
		{/if}
		{#if ev.alarms.length}
			<div class="row">
				<dt>
					{#if phone}<span class="word">{t('calendar.form.alarm')}</span>{:else}<Icon
							name="bell"
							size={16}
						/><span class="sr-only">{t('calendar.form.alarm')}</span>{/if}
				</dt>
				<dd>
					{#each ev.alarms as m (m)}<span class="line">{alarmText(m, ev.allDay, startOf(ev))}</span
						>{/each}
				</dd>
			</div>
		{/if}
		{#if ev.location}
			<div class="row">
				<dt>
					{#if phone}<span class="word">{t('calendar.form.place')}</span>{:else}<Icon
							name="place"
							size={16}
						/><span class="sr-only">{t('calendar.form.place')}</span>{/if}
				</dt>
				<dd class="text">{ev.location}</dd>
			</div>
		{/if}
		{#if ev.notes}
			<div class="row">
				<dt>
					{#if phone}<span class="word">{t('calendar.form.notes')}</span>{:else}<Icon
							name="note"
							size={16}
						/><span class="sr-only">{t('calendar.form.notes')}</span>{/if}
				</dt>
				<dd class="text notes">
					{#each linkPieces(ev.notes) as piece, i (i)}{#if piece.href && safeHref(piece.href)}<a
								href={piece.href}
								target="_blank"
								rel="noopener noreferrer">{piece.text}</a
							>{:else}{piece.text}{/if}{/each}
				</dd>
			</div>
		{/if}
		{#if birthday}
			<p class="muted note">{t('calendar.detail.birthday')}</p>
		{/if}
	</dl>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('calendar.detail.close')}</button>
		{#if onagent && !birthday}
			<button type="button" class="btn" onclick={() => onagent(ev)}
				><Icon name="sparkle" size={15} />{t('agentpim.panel.toggle')}</button
			>
		{/if}
		{#if !birthday && !ev.readOnly}
			<button type="button" class="btn btn-danger" disabled={!full} onclick={() => ondelete(ev)}
				><Icon name="trash" size={15} />{t('calendar.detail.delete')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => onedit(ev)}
				><Icon name="edit" size={15} />{t('calendar.detail.edit')}</button
			>
		{/if}
	</div>
</Dialog>

<style>
	.detail {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 0 0 var(--space-3);
	}
	.row {
		display: grid;
		grid-template-columns: 24px minmax(0, 1fr);
		gap: var(--space-2);
		align-items: start;
	}
	dt {
		color: var(--muted);
		padding-top: 2px;
	}
	dd {
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		overflow-wrap: break-word;
	}
	dd.cal {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2);
		font-weight: 600;
	}
	.sw {
		flex: none;
		width: 12px;
		height: 12px;
		border-radius: 3px;
		background: var(--c);
	}
	.text {
		overflow-wrap: anywhere;
	}
	.notes {
		display: block;
		white-space: pre-wrap;
	}
	/* The phone: a link in the notes is a 44 px target of its own. */
	.phone .notes a {
		display: inline-block;
		max-width: 100%;
		min-height: 44px;
		line-height: 44px;
		vertical-align: top;
	}
	small {
		font-size: var(--text-sm);
	}
	.note {
		margin: 0;
	}

	/* ---- Phone: grouped cards (draft 35) ---- */
	.detail.phone {
		gap: 0;
		border-radius: 12px;
		background: var(--surface);
	}
	.phone .row {
		grid-template-columns: minmax(5.5rem, auto) minmax(0, 1fr);
		gap: var(--space-3);
		align-items: start;
		padding: var(--space-3);
	}
	.phone .row + .row {
		border-top: 1px solid var(--srow-line);
	}
	.phone dt {
		color: var(--text);
		padding: 0;
	}
	.phone dd {
		color: var(--muted);
		text-align: right;
		align-items: flex-end;
	}
	.phone dd.cal {
		justify-content: flex-end;
		align-items: center;
		font-weight: 400;
	}
	.phone dd.cal .sw {
		border-radius: 50%;
	}
	.phone .row:has(.notes) {
		grid-template-columns: minmax(0, 1fr);
		gap: var(--space-1);
	}
	.phone dd.notes {
		text-align: left;
		color: var(--text);
	}
	.word {
		overflow-wrap: break-word;
	}
</style>
