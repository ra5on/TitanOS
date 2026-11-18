<script lang="ts">
	// 1.11 (step 15): a calendar of T-Kalender – create one (name, color),
	// or change one: rename and recolor, import an .ics file (what to do
	// with events it already has, then a summary), export it, and in the
	// danger zone "Kalender löschen …", which asks in its own dialog that
	// names the calendar and how many events go with it (clearly not the
	// deletion of one event). The colors are the fixed program squares,
	// each with its name; the last calendar cannot go (calendar_last, shown
	// as text).
	import Dialog from '#lib/components/Dialog.svelte';
	import DangerZone from '#lib/components/DangerZone.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { apiErrorText, t, tOr, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { SQUARES, squareHex, squareOf, type Calendar, type SquareName } from '../calendar.ts';
	import {
		createCalendar,
		exportICS,
		importICS,
		removeCalendar,
		updateCalendar,
		type ImportResult,
	} from '../calendar-api.ts';

	type Props = {
		/** The calendar to change; none = a new one. */
		calendar?: Calendar;
		onclose: () => void;
		/** Something changed: reload calendars and events. */
		onchanged: () => void;
	};
	let { calendar, onclose, onchanged }: Props = $props();

	// svelte-ignore state_referenced_locally
	let name = $state(calendar?.name ?? '');
	// svelte-ignore state_referenced_locally
	let color = $state<SquareName>(calendar ? squareOf(calendar.color) : 'violet');
	let busy = $state(false);
	let error = $state('');
	let confirmDelete = $state(false);
	let file = $state<File | null>(null);
	let onDuplicate = $state<'skip' | 'replace'>('skip');
	let result = $state<ImportResult | null>(null);
	let importError = $state('');

	const colorName = (c: SquareName) => t(`calendar.color.${c}` as MessageKey);

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!name.trim()) {
			error = t('calendar.cal.nameMissing');
			return;
		}
		busy = true;
		error = '';
		try {
			if (calendar)
				await updateCalendar(calendar.id, { name: name.trim(), color: squareHex(color) });
			else await createCalendar(name.trim(), squareHex(color));
			onchanged();
			onclose();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function runImport() {
		if (!calendar || !file) return;
		busy = true;
		importError = '';
		result = null;
		try {
			result = await importICS(calendar.id, file, onDuplicate);
			onchanged();
		} catch (err) {
			importError = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function runExport() {
		if (!calendar) return;
		error = '';
		try {
			await exportICS(calendar.id);
		} catch (err) {
			error = apiErrorText(err);
		}
	}

	async function remove() {
		if (!calendar) return;
		busy = true;
		error = '';
		try {
			await removeCalendar(calendar.id);
			onchanged();
			onclose();
		} catch (err) {
			// calendar_last: the last calendar stays; said in words.
			error = apiErrorText(err);
			confirmDelete = false;
		} finally {
			busy = false;
		}
	}
</script>

{#if confirmDelete && calendar}
	<Dialog
		title={t('calendar.cal.delTitle', { name: calendar.name })}
		locked={busy}
		onclose={() => (confirmDelete = false)}
	>
		<p class="lead">{t('calendar.cal.delText', { name: calendar.name, n: calendar.count })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirmDelete = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				><Icon name="trash" size={15} />{t('calendar.cal.delConfirm')}</button
			>
		</div>
	</Dialog>
{:else}
	<Dialog
		title={calendar
			? t('calendar.cal.editTitle', { name: calendar.name })
			: t('calendar.cal.newTitle')}
		locked={busy}
		{onclose}
	>
		<form class="form" onsubmit={save} data-calendar-dialog>
			<label class="field-row">
				<span class="lbl">{t('calendar.cal.name')}</span>
				<input class="field" type="text" bind:value={name} maxlength="200" required />
			</label>
			<div class="field-row">
				<span class="lbl" id="cal-color-label">{t('calendar.cal.color')}</span>
				<div class="colors" role="radiogroup" aria-labelledby="cal-color-label">
					{#each SQUARES as [c] (c)}
						<label
							class="swatch"
							class:on={color === c}
							style:--c="var(--icon-square-{c})"
							title={colorName(c)}
						>
							<input
								type="radio"
								name="cal-color"
								value={c}
								bind:group={color}
								aria-label={colorName(c)}
							/>
							{#if color === c}<Icon name="check" size={14} />{/if}
						</label>
					{/each}
				</div>
				<small class="muted chosen">{colorName(color)}</small>
			</div>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={onclose}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={busy}
					>{calendar ? t('calendar.cal.save') : t('calendar.cal.create')}</button
				>
			</div>
		</form>

		{#if calendar}
			<section class="part" aria-labelledby="cal-import-head" data-calendar-import>
				<h3 id="cal-import-head">{t('calendar.cal.import')}</h3>
				<input
					class="field"
					type="file"
					accept=".ics,text/calendar"
					aria-label={t('calendar.cal.file')}
					onchange={(e) => {
						file = (e.currentTarget as HTMLInputElement).files?.[0] ?? null;
						result = null;
					}}
				/>
				<div class="dup">
					<span class="muted">{t('calendar.cal.onDuplicate')}</span>
					<Segments
						compact
						label={t('calendar.cal.onDuplicate')}
						bind:value={onDuplicate}
						options={[
							{ value: 'skip', label: t('calendar.cal.skip') },
							{ value: 'replace', label: t('calendar.cal.replace') },
						]}
					/>
				</div>
				<button type="button" class="btn" disabled={busy || !file} onclick={runImport}
					><Icon name="upload" size={15} />{t('calendar.cal.importRun')}</button
				>
				{#if importError}<p class="notice-error" role="alert">{importError}</p>{/if}
				{#if result}
					<div class="result" role="status" data-import-result>
						<p>
							{t('calendar.cal.result', {
								added: result.added,
								updated: result.updated,
								skipped: result.skipped,
							})}
						</p>
						{#if result.failed.length}
							<p>{t('calendar.cal.failed', { n: result.failed.length })}</p>
							<ul>
								{#each result.failed as f, i (i)}
									<li>
										<span class="uid">{f.uid || '–'}</span>:
										{tOr(`calendar.reason.${f.reason}`, t('calendar.reason.invalid'))}
									</li>
								{/each}
							</ul>
						{/if}
					</div>
				{/if}
			</section>
			<section class="part" aria-labelledby="cal-export-head">
				<h3 id="cal-export-head">{t('calendar.cal.exportHead')}</h3>
				<button type="button" class="btn" onclick={runExport}
					><Icon name="download" size={15} />{t('calendar.cal.export')}</button
				>
			</section>
			<DangerZone hint={t('calendar.cal.dangerHint', { n: calendar.count })}>
				<button
					type="button"
					class="btn btn-danger"
					disabled={busy}
					onclick={() => (confirmDelete = true)}
					><Icon name="trash" size={15} />{t('calendar.cal.delete')}</button
				>
			</DangerZone>
		{/if}
	</Dialog>
{/if}

<style>
	.form {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.lbl {
		color: var(--muted);
	}
	.colors {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.swatch {
		position: relative;
		display: inline-grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border-radius: var(--radius-ctl);
		background: var(--c);
		color: var(--on-icon-square);
		cursor: pointer;
		outline-offset: 2px;
	}
	.swatch.on {
		outline: 2px solid var(--text);
	}
	.swatch:has(input:focus-visible) {
		outline: 2px solid var(--accent);
	}
	.swatch input {
		position: absolute;
		opacity: 0;
		inset: 0;
		margin: 0;
		cursor: pointer;
	}
	.part {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
		margin-top: var(--space-4);
		padding-top: var(--space-3);
		border-top: 1px solid var(--border);
	}
	.part h3 {
		margin: 0;
		font-size: var(--text-base);
	}
	.part .field {
		max-width: 100%;
	}
	.dup {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.result p {
		margin: 0;
	}
	.result ul {
		margin: var(--space-1) 0 0;
		padding-left: var(--space-4);
	}
	.uid {
		font-family: var(--font-mono, monospace);
		overflow-wrap: anywhere;
	}
	.lead {
		margin: 0 0 var(--space-3);
		overflow-wrap: break-word;
	}
</style>
