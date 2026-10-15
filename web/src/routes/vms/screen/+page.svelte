<script lang="ts">
	import { vmLabel } from '#lib/vm-name.ts';
	// The screen of a VM in a window of its own: nothing but the picture.
	import { page } from '$app/state';
	import { api, type VMStatus } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import VncScreen, { type ScaleMode } from '../VncScreen.svelte';

	const name = $derived(page.url.searchParams.get('vm') ?? '');
	let running = $state<boolean | null>(null);
	let displayName = $state('');
	let mode = $state<ScaleMode>('scale');
	let screen = $state<VncScreen>();
	const MODES: ScaleMode[] = ['scale', 'remote', 'full'];

	$effect(() => {
		const check = () => api.get<VMStatus>('/vms').then((res) => { const vm = res.vms.find((vm) => vm.name === name); running = vm?.state === 'running'; displayName = vm ? vmLabel(vm) : name; }, () => {});
		check();
		const timer = setInterval(check, 3000);
		return () => clearInterval(timer);
	});
</script>

<svelte:head><title>{displayName || name} · TitanOS</title></svelte:head>

<div class="window">
	<div class="frame">
		{#if running}
			{#key name}<VncScreen bind:this={screen} vm={name} {mode} />{/key}
		{:else if running === false}
			<p class="muted off">{t('vms.off', { name: displayName || name })}</p>
		{/if}
	</div>
	<div class="bar">
		<span class="mono">{displayName || name}</span>
		<span class="grow"></span>
		<button type="button" onclick={() => screen?.ctrlAltDel()} disabled={!running}>{t('vms.ctrlAltDel')}</button>
		{#each MODES as entry (entry)}
			<button type="button" aria-pressed={mode === entry} onclick={() => (mode = entry)}>{t(`vms.mode.${entry}`)}</button>
		{/each}
	</div>
</div>

<style>
	/* Over everything, the header of the page included. */
	.window {
		position: fixed;
		inset: 0;
		z-index: 40;
		display: flex;
		flex-direction: column;
		background: var(--bg);
	}
	.bar {
		flex-wrap: wrap;
		flex: none;
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 4px 12px;
		border-top: 1px solid var(--border);
		font-size: 0.8125rem;
	}
	.grow {
		flex: 1 1 auto;
	}
	.bar button {
		height: 2rem;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.bar button:hover,
	.bar button[aria-pressed='true'] {
		background: var(--chip);
	}
	.frame {
		position: relative;
		flex: 1 1 auto;
		min-height: 0;
	}
	.off {
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		justify-content: center;
	}
</style>
