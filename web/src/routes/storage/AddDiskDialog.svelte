<script lang="ts">
	import { api, ApiError, type Disk, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import DiskPicker from './DiskPicker.svelte';
	import { isUsable } from './labels.ts';

	type Props = { volume: Volume; disks: Disk[]; onclose: () => void; ondone: () => void };
	let { volume, disks, onclose, ondone }: Props = $props();

	let selected = $state<string[]>([]);
	let confirmed = $state(false);
	let working = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);

	const usable = $derived(disks.filter(isUsable));
	const working_members = $derived(volume.members.filter((member) => member.state === 'active' || member.state === 'rebuilding').length);
	const incomplete = $derived(working_members < volume.raidDisks);

	$effect(() => {
		selected;
		confirmed = false;
	});

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (selected.length !== 1 || !confirmed) return;
		working = true;
		error = null;
		try {
			await api.post(`/storage/volumes/${volume.name}/disks`, { disk: selected[0], erase: true });
			ondone();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = { text: errorText(code), detail: code === 'storage_failed' && err instanceof ApiError ? err.message : '' };
			working = false;
		}
	}
</script>

<Dialog title={t('addDisk.title', { volume: volume.name })} wide locked={working} {onclose}>
	<form onsubmit={submit}>
		<p class="muted">{t(incomplete ? 'addDisk.rebuild' : 'addDisk.spare')}</p>
		<DiskPicker disks={usable} bind:selected single name="add-disk" />
		{#if selected.length === 1}
			<label class="confirm">
				<input type="checkbox" bind:checked={confirmed} />
				<span>{t('addDisk.eraseConfirm', { disk: selected[0] })}</span>
			</label>
		{/if}
		{#if error}
			<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-warn" disabled={selected.length !== 1 || !confirmed || working}>{t('addDisk.submit')}</button>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	p {
		font-size: 0.875rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
