<script lang="ts">
	// Search, terminal, notifications and "edit dashboard" (in exactly this
	// order), in the head line or at the bottom of the sidebar.
	import { page } from '$app/state';
	import { dashboardControl } from '#lib/dashboard/control.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { terminal, toggleTerminal } from '#lib/terminal.svelte.ts';
	import { togglePalette } from '#lib/palette.svelte.ts';
	import Icon from './Icon.svelte';
	import Notifications from './Notifications.svelte';

	/** `side`: in the sidebar (menus open to its right); `stacked`: one below the other (folded). */
	let { side = false, stacked = false }: { side?: boolean; stacked?: boolean } = $props();

	const role = $derived(session.data?.user?.role ?? 'user');
</script>

<div class="quick-access" class:side class:stacked role="group" aria-label={t('nav.quickActions')}>
	<button
		class="terminal"
		type="button"
		onclick={togglePalette}
		aria-label={t('palette.title')}
		title={t('palette.shortcut')}><Icon name="search" size={18} /></button
	>
	{#if role !== 'user'}
		<button
			type="button"
			class="terminal"
			aria-pressed={terminal.open}
			aria-label={t('terminal.title')}
			title={t('terminal.shortcut')}
			onclick={toggleTerminal}
		>
			<svg
				width="18"
				height="18"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				stroke-linecap="round"
				stroke-linejoin="round"
				aria-hidden="true"><path d="m5 7 5 5-5 5M12 17h7"></path></svg
			>
		</button>
	{/if}
	{#if role !== 'user'}<Notifications compact {side} />{/if}
	{#if page.url.pathname === '/' && dashboardControl.edit}<button
			class="terminal"
			type="button"
			aria-label={t('dashboard.editTitle')}
			title={t('dashboard.editTitle')}
			aria-pressed={dashboardControl.editing}
			disabled={dashboardControl.editing}
			onclick={() => dashboardControl.edit?.()}><Icon name="edit" size={18} /></button
		>{/if}
</div>

<style>
	.quick-access {
		display: flex;
		align-items: center;
		gap: 0;
		height: 44px;
		padding: 3px 4px;
		box-sizing: border-box;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--chip);
		--quick-button-size: 36px;
	}
	/* In the sidebar the row fills its width, the tools spread out. */
	.quick-access.side {
		flex: none;
		justify-content: space-around;
	}
	.quick-access.stacked {
		flex-direction: column;
		width: 46px;
		height: auto;
		padding: 4px 3px;
		gap: 2px;
		--quick-button-size: 38px;
	}
	.terminal {
		width: var(--quick-button-size);
		height: 36px;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.terminal:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.terminal:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.terminal[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		font-weight: 500;
	}
	@media (max-width: 480px) {
		.quick-access {
			--quick-button-size: 32px;
		}
	}
	@media (max-width: 360px) {
		.quick-access {
			--quick-button-size: 28px;
		}
	}
</style>
