<script lang="ts">
	// 1.11 (step 15): an event opened from a view, with every field: when,
	// calendar (color and name), repetition in words, reminders, place,
	// notes. All of it is text from devices: shown as text, never as HTML;
	// in notes only http(s), mailto and tel become links, the link's text is
	// its real target, they open with rel="noopener noreferrer". Birthdays
	// come from the contacts and are read-only.
	// 1.15 (draft 35): on the phone a page of its own over the program (a
	// modal <dialog>: Escape and the way back close it): on top "‹ Do., 19.
	// Nov." (back) and "Bearbeiten", the title large, the place and the time
	// below it, the fields as rows of grouped cards with their word
	// ("Kalender", "Hinweis", …), the notes in a card, the KI-Agent as a row,
	// at the end the red row "Ereignis löschen" (its dialog asks as before).
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { compactView } from '#lib/components/compact.svelte.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { calColor, linkPieces, safeHref, type Calendar, type CalEvent } from '../calendar.ts';
	import { loadEvent } from '../calendar-api.ts';
	import {
		alarmText,
		calName,
		detailTitle,
		ruleText,
		shortDay,
		whenText,
		zoneName,
	} from '../calendar-text.ts';
	import { viewZone, wallOf } from '../caldates.ts';
	import { onMount } from 'svelte';

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
	/** Compact (the phone, windows under 620 px): the event's own page. */
	const phone = compactView();
	const underBar = phoneView();

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
	/** The day the event stands on in the views (the way back names it). */
	const day = $derived(
		event.allDay ? event.start.slice(0, 10) : wallOf(new Date(event.start), viewZone()).date,
	);
	const changeable = $derived(!birthday && !ev.readOnly);

	// The phone's page: a modal dialog of its own (focus kept inside, Escape closes).
	let sheet = $state<HTMLDialogElement>();
	onMount(() => {
		if (phone && sheet && !sheet.open) sheet.showModal();
	});
</script>

{#snippet fields()}
	<dl class="detail" class:phone data-event-detail>
		{#if !phone}<div class="row">
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
			</div>{/if}
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
		{#if ev.location && !phone}
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
		{#if birthday && !phone}
			<p class="muted note">{t('calendar.detail.birthday')}</p>
		{/if}
	</dl>
{/snippet}

{#if phone}
	<dialog
		class="evpage"
		class:under-bar={underBar}
		bind:this={sheet}
		aria-label={detailTitle(ev)}
		oncancel={(e) => {
			e.preventDefault();
			onclose();
		}}
	>
		<div class="evtop">
			<button type="button" class="link-btn back" onclick={onclose}
				><Icon name="chevronLeft" size={20} /><span>{shortDay(day)}</span></button
			>
			{#if changeable}
				<button type="button" class="link-btn" onclick={() => onedit(ev)}
					>{t('calendar.detail.edit')}</button
				>
			{/if}
		</div>
		<h2 class="evtitle">{detailTitle(ev)}</h2>
		{#if ev.location}<p class="evplace">{ev.location}</p>{/if}
		<p class="evwhen">
			{whenText(ev)}
			{#if zone}<small class="muted">{t('calendar.zone', { zone: zoneName(zone) })}</small>{/if}
			{#if ev.floating && !ev.allDay}<small class="muted">{t('calendar.detail.floating')}</small
				>{/if}
			{#if ev.tzGuess}<small class="muted">{t('calendar.detail.tzGuess')}</small>{/if}
		</p>
		{@render fields()}
		{#if birthday}<p class="muted note">{t('calendar.detail.birthday')}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if onagent && !birthday}
			<div class="card">
				<button type="button" class="act" onclick={() => onagent(ev)}
					><Icon name="sparkle" size={17} />{t('agentpim.panel.toggle')}</button
				>
			</div>
		{/if}
		{#if changeable}
			<div class="card">
				<button type="button" class="act danger" disabled={!full} onclick={() => ondelete(ev)}
					>{t('calendar.detail.deleteEvent')}</button
				>
			</div>
		{/if}
	</dialog>
{:else}
	<Dialog title={detailTitle(ev)} {onclose}>
		{@render fields()}
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
{/if}

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

	/* ---- The phone's page (draft 35) ---- */
	.evpage {
		position: fixed;
		inset: 0;
		width: 100%;
		max-width: none;
		height: auto;
		max-height: none;
		margin: 0;
		padding: var(--space-2) calc(var(--space-4) + env(safe-area-inset-right, 0px))
			calc(var(--space-5) + env(safe-area-inset-bottom, 0px))
			calc(var(--space-4) + env(safe-area-inset-left, 0px));
		border: 0;
		background: var(--bg);
		color: var(--text);
		overflow-y: auto;
		overscroll-behavior: contain;
	}
	.evpage.under-bar {
		inset: calc(52px + env(safe-area-inset-top, 0px)) 0 0;
	}
	.evpage[open] {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.evpage::backdrop {
		background: transparent;
	}
	.evtop {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
	.link-btn {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		min-height: 44px;
		padding: 0 var(--space-1);
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.link-btn.back {
		margin-left: calc(-1 * var(--space-1));
	}
	.link-btn:focus-visible,
	.act:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.evtitle {
		margin: 0;
		font-size: 28px;
		font-weight: 700;
		overflow-wrap: anywhere;
	}
	.evplace {
		margin: calc(-1 * var(--space-2)) 0 0;
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.evwhen {
		display: flex;
		flex-direction: column;
		gap: 2px;
		margin: 0;
		overflow-wrap: break-word;
	}
	.card {
		display: flex;
		flex-direction: column;
		border-radius: 12px;
		background: var(--surface);
	}
	.act {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		min-height: 50px;
		padding: var(--space-2) var(--space-3);
		border: 0;
		border-radius: 12px;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		text-align: center;
		cursor: pointer;
	}
	/* Red as the draft, ≥ 4.5:1 on the card in both modes. */
	.act.danger {
		color: var(--cal-delete, #c00012);
	}
	:global([data-mode='dark']) .act.danger {
		--cal-delete: #ff6b60;
	}
	.act:disabled {
		opacity: 0.6;
		cursor: default;
	}
</style>
