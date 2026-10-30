<script lang="ts">
	import type { DockerContainer } from '#lib/api.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isRunning, type ContainerActions } from './container.ts';

	type Props = ContainerActions & {
		container: DockerContainer;
		/** Name for the button label: "Actions for …". */
		name: string;
		busy: boolean;
	};
	let {
		container,
		name,
		busy,
		onaction,
		onlogs,
		onremove,
		onconsole,
		onupdate,
		ontailnet,
		tailnet = false,
	}: Props = $props();

	const running = $derived(isRunning(container));
	// The menu lies in the browser's top layer, so no tile or list clips it.
	let at = $state<{ x: number; y: number } | null>(null);
	let button: HTMLButtonElement | undefined = $state();
	// A press on the button closes an open menu (outside the menu); the
	// click that follows must not open it again.
	let wasOpen = false;

	function toggle() {
		if (wasOpen || at) {
			wasOpen = false;
			at = null;
			return;
		}
		const rect = button?.getBoundingClientRect();
		if (!rect) return;
		// Right edges aligned like before; the menu keeps itself on screen.
		at = { x: Math.max(8, rect.right - 224), y: rect.bottom + 4 };
	}
	function choose(action: () => void) {
		at = null;
		action();
	}
</script>

<div class="wrap">
	<button
		type="button"
		class="more"
		bind:this={button}
		aria-label={t('docker.actionsFor', { name })}
		title={t('docker.actionsFor', { name })}
		aria-haspopup="menu"
		aria-expanded={!!at}
		onpointerdown={() => (wasOpen = !!at)}
		onclick={toggle}
	>
		<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"
			><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle
				cx="19"
				cy="12"
				r="1.8"
			/></svg
		>
	</button>
</div>
{#if at}
	<ContextMenu
		label={t('docker.actionsFor', { name })}
		x={at.x}
		y={at.y}
		onclose={() => (at = null)}
	>
		<button type="button" role="menuitem" onclick={() => choose(onlogs)}>{t('docker.logs')}</button>
		<button type="button" role="menuitem" onclick={() => choose(onconsole)} disabled={!running}
			>{t('docker.console')}</button
		>
		{#if container.managed}
			<a role="menuitem" href="/docker/apps/{container.app}" onclick={() => (at = null)}
				>{t('docker.edit')}</a
			>
		{/if}
		<button
			type="button"
			role="menuitem"
			onclick={() => choose(() => onaction('restart'))}
			disabled={busy}>{t('docker.restart')}</button
		>
		{#if container.managed && ontailnet}
			<button type="button" role="menuitem" onclick={() => choose(ontailnet)}
				>{t(tailnet ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish')}</button
			>
		{/if}
		{#if container.managed && container.updateAvailable}
			<button type="button" role="menuitem" onclick={() => choose(onupdate)} disabled={busy}
				>{t('docker.updateNow')}</button
			>
		{/if}
		<hr />
		<button type="button" role="menuitem" class="danger" onclick={() => choose(onremove)}
			>{t('docker.remove')}</button
		>
	</ContextMenu>
{/if}

<style>
	.wrap {
		position: relative;
		flex: none;
	}
	.more {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.more:hover,
	.more[aria-expanded='true'] {
		background: var(--chip);
		border-color: var(--border);
		color: var(--text);
	}
	@media (max-width: 760px) {
		.more {
			width: 2.75rem;
			height: 2.75rem;
		}
	}
</style>
