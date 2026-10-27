<script lang="ts">
	import type { DockerContainer } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { tick } from 'svelte';
	import { isRunning, type ContainerActions } from './container.ts';

	type Props = ContainerActions & {
		container: DockerContainer;
		/** Name for the button label: "Actions for …". */
		name: string;
		busy: boolean;
	};
	let { container, name, busy, onaction, onlogs, onremove, onconsole, onupdate }: Props = $props();

	const running = $derived(isRunning(container));
	let open = $state(false);
	// Opens upwards when there is no room below, so that the page never grows.
	let up = $state(false);
	let root: HTMLElement | undefined = $state();
	let button: HTMLButtonElement | undefined = $state();

	async function toggle() {
		if (open) return close();
		const rect = button?.getBoundingClientRect();
		up = !!rect && window.innerHeight - rect.bottom < 300 && rect.top > 300;
		open = true;
		await tick();
		root?.querySelector<HTMLElement>('[role="menuitem"]:not(:disabled)')?.focus();
	}
	function close(focus = false) {
		open = false;
		if (focus) button?.focus();
	}
	function choose(action: () => void) {
		close();
		action();
	}
	function keys(event: KeyboardEvent) {
		if (event.key === 'Escape') return close(true);
		if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
		event.preventDefault();
		const items = [...(root?.querySelectorAll<HTMLElement>('[role="menuitem"]:not(:disabled)') ?? [])];
		const index = items.indexOf(document.activeElement as HTMLElement);
		items[(index + (event.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length]?.focus();
	}
</script>

<svelte:window onclick={(event) => open && !root?.contains(event.target as Node) && close()} />

<div class="wrap" class:menu-open={open} bind:this={root}>
	<button type="button" class="more" bind:this={button} aria-label={t('docker.actionsFor', { name })} title={t('docker.actionsFor', { name })} aria-haspopup="menu" aria-expanded={open} onclick={toggle}>
		<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.8" /><circle cx="12" cy="12" r="1.8" /><circle cx="19" cy="12" r="1.8" /></svg>
	</button>
	{#if open}
		<div class="menu" class:up role="menu" tabindex="-1" aria-label={t('docker.actionsFor', { name })} onkeydown={keys}>
			<button type="button" role="menuitem" onclick={() => choose(onlogs)}>{t('docker.logs')}</button>
			<button type="button" role="menuitem" onclick={() => choose(onconsole)} disabled={!running}>{t('docker.console')}</button>
			{#if container.managed}
				<a role="menuitem" href="/docker/apps/{container.app}">{t('docker.edit')}</a>
			{/if}
			<button type="button" role="menuitem" onclick={() => choose(() => onaction('restart'))} disabled={busy}>{t('docker.restart')}</button>
			{#if container.managed && container.updateAvailable}
				<button type="button" role="menuitem" onclick={() => choose(onupdate)} disabled={busy}>{t('docker.updateNow')}</button>
			{/if}
			<hr />
			<button type="button" role="menuitem" class="danger" onclick={() => choose(onremove)}>{t('docker.remove')}</button>
		</div>
	{/if}
</div>

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
	.menu {
		position: absolute;
		right: 0;
		top: calc(100% + 4px);
		z-index: 40;
		width: 13rem;
		display: flex;
		flex-direction: column;
		padding: 6px;
		/* Solid: inside a glass tile a second backdrop blur only sees the rows
		   beneath it, and their text would show through. */
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
	}
	.menu.up {
		top: auto;
		bottom: calc(100% + 4px);
	}
	.menu button,
	.menu a {
		display: flex;
		align-items: center;
		min-height: 2.25rem;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--text);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		text-decoration: none;
		cursor: pointer;
	}
	.menu .danger {
		color: var(--error-text);
	}
	.menu button:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.menu a:hover,
	.menu a:focus-visible,
	.menu button:hover:not(:disabled),
	.menu button:focus-visible {
		background: var(--chip);
		outline: none;
	}
	.menu hr {
		height: 1px;
		margin: 6px 4px;
		border: 0;
		background: var(--border);
	}
	@media (max-width: 760px) {
		.more {
			width: 2.75rem;
			height: 2.75rem;
		}
		.menu button,
		.menu a {
			min-height: 2.75rem;
		}
	}
</style>
