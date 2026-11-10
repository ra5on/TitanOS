<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	// The screen of a VM in a window of its own: nothing but the picture.
	import { page } from '$app/state';
	import { api, type VMStatus } from '#lib/api.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
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
		running = null;
		error = '';
		const check = async (signal: AbortSignal) => {
			try {
				const res = await api.get<VMStatus>('/vms', signal);
				if (signal.aborted) return;
				const vm = res.vms.find((vm) => vm.name === vmName);
				running = vm?.state === 'running';
				displayName = vm ? vmLabel(vm) : vmName;
				resizable = vm?.template === 'linux';
				if (!resizable && mode === 'remote') mode = 'scale';
				error = vm ? '' : errorText('vm_not_found');
			} catch (err) {
				if (!signal.aborted) error = apiErrorText(err);
			}
		};
		return poll(check, { interval: 3000 });
	});

	// PLAN 67-1: Bildschirm in the menu bar of the VM screen's window.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import { KEYS, keyLabel } from '../vm-page.ts';
	import { i18n } from '#lib/i18n/index.svelte.ts';
	const live = () => !!running && !!screen;
	registerMenuCommands({
		'vms.cad': { run: () => screen?.ctrlAltDel(), enabled: live },
		'vms.keys': {
			items: () =>
				live() ? KEYS.map((k, i) => ({ id: String(i), text: keyLabel(k.label, i18n.locale) })) : [],
			run: (i) => {
				const entry = KEYS[Number(i)];
				if (entry) screen?.sendKeys(entry.keys);
			},
		},
		'vms.screenshot': {
			run: () => {
				const data = screen?.screenshot();
				if (!data) return;
				const a = document.createElement('a');
				a.href = data;
				a.download = `${name}.png`;
				a.click();
			},
			enabled: live,
		},
		...Object.fromEntries(
			MODES.map((entry) => [
				'vms.mode.' + entry,
				{
					run: () => (mode = entry),
					enabled: () => entry !== 'remote' || resizable,
					checked: () => mode === entry,
				},
			]),
		),
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
	<div class="screen-bar">
		<span class="mono">{displayName || name}</span>
		{#if error}<span class="muted" role="alert">{error}</span>
		{:else if running === null}<span class="muted" role="status">{t('common.loading')}</span>
		{:else if !running}<span class="muted">{t('vms.off', { name: displayName || name })}</span>
		{:else if connectionPhase !== 'connected'}<span class="muted" role="status"
				>{t(
					connectionPhase === 'disabled'
						? 'vms.screenDisabled'
						: connectionPhase === 'lost'
							? 'vms.screenLost'
							: 'vms.screenConnecting',
				)}</span
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
	.screen-bar {
		flex-wrap: wrap;
		flex: none;
		display: flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 28px;
		padding: var(--space-1) var(--space-4);
		border-top: 1px solid var(--border);
		/* Style W4 (0.74): the status bar of the window, as in the other views. */
		background: color-mix(in srgb, var(--surface) 92%, var(--bg));
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.grow {
		flex: 1 1 auto;
	}
	.screen-bar button {
		height: 1.75rem;
		padding: 0 var(--space-2-5);
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.screen-bar button:hover,
	.screen-bar button[aria-pressed='true'] {
		background: var(--chip);
	}
	.frame {
		position: relative;
		flex: 1 1 auto;
		min-height: 0;
	}
</style>
