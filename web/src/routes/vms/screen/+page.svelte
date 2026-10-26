<script lang="ts">
	import { vmLabel } from '#lib/vm-name.ts';
	// The screen of a VM in a window of its own: nothing but the picture.
	import { page } from '$app/state';
	import { api, ApiError, type VMStatus } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import VncScreen, { type ScaleMode, type ConnectionPhase } from '../VncScreen.svelte';

	const name = $derived(page.url.searchParams.get('vm') ?? '');
	let running = $state<boolean | null>(null);
	let displayName = $state('');
	let resizable = $state(false);
	let error = $state('');
	let mode = $state<ScaleMode>('scale');
	let screen = $state<VncScreen>();
	let connectionPhase = $state<ConnectionPhase>('connecting');
	const MODES: ScaleMode[] = ['scale', 'remote', 'full'];

	$effect(() => {
		const vmName = name;
		let disposed = false;
		running = null;
		error = '';
		const check = async () => {
			try {
				const res = await api.get<VMStatus>('/vms');
				if (disposed) return;
				const vm = res.vms.find((vm) => vm.name === vmName);
				running = vm?.state === 'running';
				displayName = vm ? vmLabel(vm) : vmName;
				resizable = vm?.template === 'linux';
				if (!resizable && mode === 'remote') mode = 'scale';
				error = vm ? '' : errorText('vm_not_found');
			} catch (err) {
				if (!disposed) error = errorText(err instanceof ApiError ? err.code : 'internal');
			}
		};
		check();
		const timer = setInterval(check, 3000);
		return () => {
			disposed = true;
			clearInterval(timer);
		};
	});
</script>

<svelte:head><title>{displayName || name} · TitanOS</title></svelte:head>

<div class="window">
	<div class="frame">
		{#if running}
			{#key name}<VncScreen
					bind:this={screen}
					vm={name}
					{mode}
					{resizable}
					onstatus={(phase) => (connectionPhase = phase)}
				/>{/key}
		{/if}
	</div>
	<div class="bar">
		<span class="mono">{displayName || name}</span>
		{#if error}<span class="muted" role="alert">{error}</span>
		{:else if running === null}<span class="muted" role="status">{t('common.loading')}</span>
		{:else if !running}<span class="muted">{t('vms.off', { name: displayName || name })}</span>
		{:else if connectionPhase !== 'connected'}<span class="muted" role="status"
				>{t(connectionPhase === 'disabled' ? 'vms.screenDisabled' : connectionPhase === 'lost' ? 'vms.screenLost' : 'vms.screenConnecting')}</span
			>{/if}
		<span class="grow"></span>
		<button type="button" onclick={() => screen?.ctrlAltDel()} disabled={!running}
			>{t('vms.ctrlAltDel')}</button
		>
		{#each MODES as entry (entry)}
			<button
				type="button"
				aria-pressed={mode === entry}
				disabled={entry === 'remote' && !resizable}
				title={t(
					entry === 'remote' && !resizable
						? 'vms.mode.remote.unsupported'
						: `vms.mode.${entry}.hint`,
				)}
				onclick={() => (mode = entry)}>{t(`vms.mode.${entry}`)}</button
			>
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
</style>
