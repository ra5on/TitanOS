<script lang="ts">
	// Create or change a script: name, trigger, schedule and the script
	// itself. Deleting is in the danger zone at the end, with a plain
	// confirmation.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import DangerZone from '#lib/components/DangerZone.svelte';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import ScheduleField from '#lib/components/ScheduleField.svelte';
	import { parseCron } from '#lib/schedule.ts';
	import { TRIGGERS, draftBody, draftOf, newDraft, type Draft, type Script } from './scripts.ts';

	type Props = { id: string; timezone?: string; onclose: () => void; onsaved: () => void };
	let { id, timezone, onclose, onsaved }: Props = $props();

	let draft = $state<Draft | null>(null);
	let initial = $state('');
	let error = $state('');
	let busy = $state(false);
	let discard = $state(false);
	let confirmDelete = $state(false);
	const dirty = $derived(!!draft && JSON.stringify(draft) !== initial);
	const tooLarge = $derived(!!draft && new TextEncoder().encode(draft.content).length > 256 * 1024);

	$effect(() => {
		if (!id) {
			// The plain object, not the state: reading draft here would make
			// this effect depend on what it writes.
			const fresh = newDraft();
			initial = JSON.stringify(fresh);
			draft = fresh;
			return;
		}
		api
			.get<Script & { content: string }>(`/scripts/${id}`)
			.then((body) => {
				draft = draftOf(body, body.content);
				initial = JSON.stringify(draft);
			})
			.catch((err) => (error = apiErrorText(err)));
	});

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft || busy || tooLarge) return;
		busy = true;
		error = '';
		try {
			const body = draftBody(draft);
			if (id) await api.put(`/scripts/${id}`, body);
			else await api.post('/scripts', body);
			onsaved();
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			busy = false;
		}
	}

	async function remove() {
		busy = true;
		error = '';
		try {
			await api.delete(`/scripts/${id}`);
			confirmDelete = false;
			onsaved();
		} catch (err) {
			error = apiErrorText(err);
			confirmDelete = false;
		} finally {
			busy = false;
		}
	}

	function close() {
		if (busy) return;
		if (dirty) discard = true;
		else onclose();
	}
</script>

<Dialog title={t(id ? 'scripts.edit' : 'scripts.add')} wide locked={dirty || busy} onclose={close}>
	{#if draft}<form class="editor" onsubmit={save}>
			<label class="stack"
				>{t('scripts.name')}<input
					class="field"
					bind:value={draft.name}
					required
					maxlength="64"
					autocomplete="off"
				/></label
			>
			<label class="stack"
				>{t('scripts.description')}<input
					class="field"
					bind:value={draft.description}
					maxlength="200"
					autocomplete="off"
				/></label
			>
			<label class="stack"
				><span>{t('scripts.trigger')}<HelpTip term="scriptTrigger" /></span><select
					class="field"
					bind:value={draft.trigger}
					>{#each TRIGGERS as trigger (trigger)}<option value={trigger}
							>{t(`scripts.trigger.${trigger}` as MessageKey)}</option
						>{/each}</select
				></label
			>
			{#if draft.trigger === 'schedule'}<fieldset class="schedule">
					<legend>{t('scripts.schedule')}</legend>
					<ScheduleField bind:cron={draft.cron} {timezone} />
				</fieldset>{/if}
			{#if draft.trigger === 'shutdown'}<p class="muted">{t('scripts.shutdownHint')}</p>{/if}
			{#if draft.trigger === 'start'}<p class="muted">{t('scripts.startHint')}</p>{/if}
			<label class="stack"
				><span>{t('scripts.content')}<HelpTip term="shebang" /></span><textarea
					class="field mono"
					rows="16"
					bind:value={draft.content}
					spellcheck="false"
					autocomplete="off"
					autocapitalize="off"></textarea></label
			>
			{#if tooLarge}<p class="notice-error" role="alert">{t('scripts.tooLarge')}</p>{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if discard}
				<p class="notice-warn">{t('scripts.discard')}</p>
				<div class="dialog-actions">
					<button type="button" class="btn" onclick={() => (discard = false)}
						>{t('scripts.keepEditing')}</button
					>
					<button type="button" class="btn" onclick={onclose}>{t('scripts.discardButton')}</button>
				</div>
			{:else}
				<div class="dialog-actions">
					<button type="button" class="btn" onclick={close} disabled={busy}
						>{t('common.cancel')}</button
					>
					<button
						type="submit"
						class="btn btn-primary"
						disabled={busy ||
							!draft.name.trim() ||
							tooLarge ||
							(draft.trigger === 'schedule' && !parseCron(draft.cron)) ||
							(!!id && !dirty)}>{t('ops.save')}</button
					>
				</div>
			{/if}
			{#if id}<DangerZone hint={t('scripts.deleteHint')}
					><button
						type="button"
						class="btn btn-danger"
						disabled={busy}
						onclick={() => (confirmDelete = true)}>{t('scripts.delete')}</button
					></DangerZone
				>{/if}
		</form>{:else if error}<p class="notice-error" role="alert">{error}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
		</div>{:else}<p class="loading">{t('scripts.loading')}</p>{/if}
</Dialog>

{#if confirmDelete && draft}<Dialog
		title={t('scripts.deleteTitle')}
		locked={busy}
		onclose={() => (confirmDelete = false)}
	>
		<p>{t('scripts.deleteText', { name: draft.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirmDelete = false)}
				>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				>{t('scripts.delete')}</button
			>
		</div>
	</Dialog>{/if}

<style>
	.editor {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.editor p {
		margin: 0;
	}
	.schedule {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		margin: 0;
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.schedule legend {
		padding: 0 var(--space-1);
		font-weight: 600;
	}
	textarea {
		font-family: var(--font-mono);
		width: 100%;
		box-sizing: border-box;
		min-height: 14rem;
		resize: vertical;
		font-size: var(--text-sm);
		line-height: 1.45;
		tab-size: 4;
	}
</style>
