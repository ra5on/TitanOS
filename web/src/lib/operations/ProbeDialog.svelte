<script lang="ts">
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { operationError } from '#lib/operations.ts';
	let { job, onclose, onstarted }: { job: string; onclose: () => void; onstarted: () => void } =
		$props();
	let mib = $state(64),
		files = $state(20),
		busy = $state(false),
		error = $state('');
	async function start() {
		busy = true;
		error = '';
		try {
			await api.post(`/backups/${encodeURIComponent(job)}/probe`, {
				maxBytes: mib * 1048576,
				maxFiles: files,
			});
			onstarted();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('ops.probe')} locked={busy} {onclose}
	><p>{t('ops.probeHint')}</p>
	<p class="mono">{job}</p>
	<form
		id="backup-probe"
		onsubmit={(e) => {
			e.preventDefault();
			start();
		}}
	>
		<label
			>{t('ops.maxMiB')}<input
				class="field"
				type="number"
				bind:value={mib}
				min="1"
				max="1024"
				required
			/></label
		><label
			>{t('ops.maxFiles')}<input
				class="field"
				type="number"
				bind:value={files}
				min="1"
				max="200"
				required
			/></label
		>
	</form>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button><button
			class="btn btn-primary"
			form="backup-probe"
			disabled={busy}>{t('ops.run')}</button
		>
	</div></Dialog
>

<style>
	form {
		display: flex;
		flex-wrap: wrap;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		flex: 1 1 170px;
		font-size: var(--text-base);
	}
</style>
