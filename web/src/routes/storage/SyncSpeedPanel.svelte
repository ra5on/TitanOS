<script lang="ts">
	// Rebuild speed of an mdadm volume (storage/syncspeed.go): how fast the
	// kernel rebuilds, first syncs and checks the array. Applies at once.
	import { api, type SyncSpeed, type Volume } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { radioKeys } from '#lib/radio-group.ts';
	import { refreshStorage } from '#lib/storage.svelte.ts';

	type Props = { volume: Volume; busy?: boolean };
	let { volume, busy = false }: Props = $props();

	const presets: SyncSpeed[] = ['gentle', 'normal', 'fast'];
	let saving = $state(false),
		error = $state(''),
		// What was chosen, until the next status shows it.
		chosen = $state<SyncSpeed | null>(null);
	const current = $derived(chosen ?? volume.syncSpeed ?? 'normal');
	const running = $derived(['rebuilding', 'syncing', 'checking'].includes(volume.state));

	async function choose(speed: SyncSpeed) {
		if (saving || speed === current) return;
		saving = true;
		error = '';
		chosen = speed;
		try {
			await api.put(`/storage/volumes/${encodeURIComponent(volume.name)}/sync-speed`, { speed });
			await refreshStorage();
		} catch (err) {
			error = apiErrorText(err);
		}
		chosen = null;
		saving = false;
	}
</script>

<div class="sync-speed">
	<div class="text">
		<span class="label" id="sync-speed-{volume.name}">{t('syncSpeed.title')}</span>
		<small class="muted">{t(`syncSpeed.${current}Desc`)}</small>
		{#if running}<small class="muted">{t('syncSpeed.running')}</small>{/if}
		{#if error}<small class="problem" role="alert">{error}</small>{/if}
	</div>
	<div
		class="segments"
		role="radiogroup"
		tabindex="-1"
		aria-labelledby="sync-speed-{volume.name}"
		onkeydown={radioKeys}
	>
		{#each presets as speed (speed)}
			<button
				type="button"
				role="radio"
				aria-checked={current === speed}
				tabindex={current === speed ? 0 : -1}
				disabled={busy || saving}
				onclick={() => choose(speed)}>{t(`syncSpeed.${speed}`)}</button
			>
		{/each}
	</div>
</div>

<style>
	.sync-speed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 16px;
		font-size: var(--text-md);
	}
	.text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		flex: 1 1 240px;
		min-width: 0;
	}
	.label {
		font-weight: 600;
	}
	.problem {
		color: var(--error-text);
		overflow-wrap: anywhere;
	}
	.segments {
		display: inline-flex;
		gap: 4px;
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		min-height: 2.25rem;
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		font-weight: 500;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.segments button:disabled {
		cursor: default;
	}
</style>
