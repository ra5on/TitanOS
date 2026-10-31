<script lang="ts">
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import { onMount } from 'svelte';
	import { api, type VM, type StorageStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { operationError } from '#lib/operations.ts';
	let { source, onclose, onstarted }: { source: VM; onclose: () => void; onstarted: () => void } =
		$props();
	let name = $state(''),
		volume = $state(''),
		volumes = $state<string[]>([]),
		busy = $state(false),
		error = $state('');
	onMount(() => {
		api
			.get<StorageStatus>('/storage')
			.then((s) => {
				volumes = s.volumes.filter((v) => v.mounted).map((v) => v.name);
				volume = volumes.includes(source.volume) ? source.volume : (volumes[0] ?? '');
			})
			.catch((e) => (error = operationError(e, t)));
	});
	async function clone() {
		busy = true;
		error = '';
		try {
			await api.post(`/vms/${encodeURIComponent(source.name)}/clone`, { name, volume });
			onstarted();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
</script>

<NavigationGuard dirty={!!name} />
<Dialog title={t('ops.clone')} locked={busy} {onclose}
	><p>{t('ops.cloneHint')}</p>
	<p class="mono">{source.displayName || source.name}</p>
	<form
		id="clone-vm"
		onsubmit={(e) => {
			e.preventDefault();
			clone();
		}}
	>
		<label
			>{t('ops.name')}<input
				class="field"
				bind:value={name}
				pattern={'[a-z][a-z0-9\\-]{0,31}'}
				maxlength="32"
				required
			/></label
		><label
			>{t('ops.volume')}<select class="field" bind:value={volume} required
				><option value="">{t('ops.choose')}</option>{#each volumes as v (v)}<option value={v}
						>{v}</option
					>{/each}</select
			></label
		>
	</form>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button><button
			form="clone-vm"
			class="btn btn-primary"
			disabled={busy || source.state !== 'stopped'}>{t('ops.clone')}</button
		>
	</div></Dialog
>

<style>
	form {
		display: flex;
		gap: 14px;
		flex-wrap: wrap;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		flex: 1 1 200px;
		font-size: var(--text-base);
	}
</style>
