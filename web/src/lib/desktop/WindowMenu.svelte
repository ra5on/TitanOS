<script lang="ts">
	// PLAN 66: the ⋯ menu in the title bar of a web app's or a Docker app's
	// window. The remote view puts its controls into it (remoteHost).
	import { tick } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import { focusFirstItem, moveFocus } from '../components/menu.ts';
	import { t } from '../i18n/index.svelte.ts';
	import type { WebApp } from '../webapps/protocol.ts';
	import type WebAppView from '../webapps/WebAppView.svelte';

	let {
		moreOpen = $bindable(),
		remoteHost = $bindable(),
		external,
		webapp,
		view,
		ownStream,
		direct,
		directOn,
		directFailed,
		switchDirect,
		openTab,
		restartApp,
	}: {
		moreOpen: boolean;
		/** Where the remote view puts its part of the menu. */
		remoteHost: HTMLElement | null;
		external: boolean;
		webapp: WebApp | undefined;
		view: WebAppView | undefined;
		ownStream: boolean;
		direct: boolean;
		directOn: boolean;
		directFailed: boolean;
		switchDirect: (on: boolean) => void;
		openTab: () => void;
		restartApp: () => void;
	} = $props();

	let moreButton = $state<HTMLButtonElement>();
	let menuEl = $state<HTMLElement>();
	// The ⋯ menu by keyboard like every menu of TitanOS: arrows, Escape back to ⋯.
	async function toggleMore() {
		moreOpen = !moreOpen;
		if (!moreOpen) return;
		await tick();
		focusFirstItem(menuEl);
	}
	function menuKey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			e.stopPropagation();
			moreOpen = false;
			moreButton?.focus();
			return;
		}
		if ((e.target as HTMLElement).matches?.('[role^="menuitem"]')) moveFocus(menuEl, e);
	}
</script>

<span class="more-wrap">
	<button
		type="button"
		class="ctl"
		class:on={moreOpen}
		aria-haspopup="menu"
		aria-expanded={moreOpen}
		bind:this={moreButton}
		onclick={toggleMore}
		title={t('webapps.menu.more')}
		aria-label={t('webapps.menu.more')}><Icon name="more" size={16} /></button
	>
	{#if moreOpen}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="win-menu dropdown-panel floating-menu"
			role="menu"
			tabindex="-1"
			bind:this={menuEl}
			onkeydown={menuKey}
		>
			{#if external}
				<!-- PLAN 66: the remote view puts its controls here (RemoteView, inTitle). -->
				<div class="remote-host" bind:this={remoteHost}></div>
				<hr />
				{#if ownStream}
					<button
						type="button"
						role="menuitemcheckbox"
						aria-checked={direct}
						onclick={() => {
							moreOpen = false;
							switchDirect(!directOn || directFailed);
						}}
						><Icon name={direct ? 'monitor' : 'zap'} size={16} /><span
							>{t(direct ? 'desktop.direct.useRemote' : 'desktop.direct.useDirect')}</span
						></button
					>
					{#if directFailed && directOn}<p class="menu-note">
							{t('desktop.direct.notReached')}
						</p>{/if}
				{/if}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						moreOpen = false;
						openTab();
					}}><Icon name="external" size={16} /><span>{t('desktop.window.openTab')}</span></button
				>
			{:else}
				<button
					type="button"
					role="menuitem"
					disabled={!webapp}
					onclick={() => {
						moreOpen = false;
						view?.openPicker();
					}}><Icon name="folder" size={16} /><span>{t('webapps.menu.open')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={!webapp || webapp.bridge !== 'host'}
					onclick={() => {
						moreOpen = false;
						view?.saveAs();
					}}><Icon name="download" size={16} /><span>{t('webapps.menu.saveAs')}</span></button
				>
				<hr />
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						moreOpen = false;
						openTab();
					}}><Icon name="external" size={16} /><span>{t('desktop.window.openTab')}</span></button
				>
				<button type="button" role="menuitem" disabled={!webapp} onclick={restartApp}
					><Icon name="restart" size={16} /><span>{t('webapps.restart')}</span></button
				>
				{#if webapp}<p class="menu-status muted">
						{t('webapps.menu.status', { version: webapp.version })}
					</p>{/if}
			{/if}
		</div>
	{/if}
</span>

<style>
	.ctl:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.more-wrap {
		position: relative;
		flex: none;
	}
	.ctl.on {
		background: color-mix(in srgb, var(--text) 12%, transparent);
		color: var(--text);
	}
	.win-menu {
		position: absolute;
		right: 0;
		top: calc(100% + 6px);
		z-index: 20;
		min-width: 260px;
		max-width: min(340px, calc(100vw - 24px));
		/* Over a busy app (a remote picture) the glass alone would not keep the text readable. */
		background:
			linear-gradient(var(--menu-bg), var(--menu-bg)),
			color-mix(in srgb, var(--surface) 88%, transparent);
		display: flex;
		flex-direction: column;
		padding: var(--space-1-5);
	}
	.menu-status {
		margin: var(--space-1) var(--space-2-5) 2px;
		font-size: var(--text-xs);
	}
	.ctl {
		width: 32px;
		height: 28px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: calc(7px * var(--radius-scale, 1));
		background: none;
		color: var(--muted);
		cursor: pointer;
		flex: none;
	}
	.ctl:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
		color: var(--text);
	}
	.menu-note {
		margin: 0;
		padding: var(--space-1) var(--space-3) var(--space-2);
		font-size: var(--text-sm);
		color: var(--muted);
		max-width: 18rem;
	}
</style>
