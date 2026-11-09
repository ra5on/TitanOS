<script lang="ts">
	// PLAN 75-21 (owner 9.11.2026): where the programs lie – one row per
	// mounted volume with its RAID level and free space; the current one
	// (when moving) is shown but cannot be chosen.
	import { onMount } from 'svelte';
	import { api, type StorageStatus } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		volumes: string[];
		value: string;
		/** The volume the programs lie on now (moving): shown, not choosable. */
		current?: string;
		disabled?: boolean;
		name?: string;
	};
	let {
		volumes,
		value = $bindable(''),
		current = '',
		disabled = false,
		name = 'webapps-volume',
	}: Props = $props();

	let status = $state<StorageStatus | null>(null);
	onMount(() => {
		api
			.get<StorageStatus>('/storage')
			.then((s) => (status = s))
			.catch(() => {});
	});
	const info = (v: string) => status?.volumes.find((x) => x.name === v);
	// Preselect the first choosable volume.
	$effect(() => {
		if (!value || value === current) value = volumes.find((v) => v !== current) ?? '';
	});
</script>

<div class="volume-choice" role="radiogroup" aria-label={t('webapps.volume.choose')}>
	{#each volumes as v (v)}
		{@const vol = info(v)}
		{@const here = v === current}
		<label class="opt" class:on={value === v && !here} class:here>
			<input type="radio" {name} value={v} bind:group={value} disabled={disabled || here} />
			<span class="what">
				<b>{v}{#if vol} · {t(`level.${vol.level}`)}{/if}</b>
				<span class="muted small"
					>{here
						? t('webapps.volume.here')
						: vol && vol.sizeBytes > 0
							? t('webapps.volume.free', {
									free: formatBytes(Math.max(0, vol.sizeBytes - vol.usedBytes)),
									total: formatBytes(vol.sizeBytes),
								})
							: ''}</span
				>
				{#if vol && vol.sizeBytes > 0 && !here}<span class="bar" aria-hidden="true"
						><i style:width="{Math.min(100, (vol.usedBytes / vol.sizeBytes) * 100)}%"></i></span
					>{/if}
			</span>
		</label>
	{/each}
</div>

<style>
	.volume-choice {
		display: grid;
		gap: var(--space-2);
	}
	.opt {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		cursor: pointer;
	}
	.opt.on {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 10%, transparent);
	}
	.opt.here {
		cursor: default;
		opacity: 0.75;
	}
	.opt input {
		flex: none;
		accent-color: var(--accent);
	}
	.what {
		display: grid;
		flex: 1;
		min-width: 0;
		gap: 2px;
	}
	.what b {
		overflow-wrap: anywhere;
	}
	.bar {
		display: block;
		height: 4px;
		margin-top: 4px;
		border-radius: 2px;
		background: var(--border);
	}
	.bar i {
		display: block;
		height: 100%;
		border-radius: 2px;
		background: var(--accent);
	}
</style>
