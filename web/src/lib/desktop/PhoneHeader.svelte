<script lang="ts">
	import { tick } from 'svelte';
	import { goto } from '$app/navigation';
	import Icon from '#lib/components/Icon.svelte';
	import PowerDialog from '#lib/components/PowerDialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { phoneChrome } from './immersive-phone.svelte.ts';
	import { programs, vmScreens } from './programs.svelte.ts';
	import { openPhoneProgram } from './phone-open.ts';
	import { watchSources } from './sources.svelte.ts';
	import Launcher from './Launcher.svelte';

	let { showHome = false }: { showHome?: boolean } = $props();
	let dialog: HTMLDialogElement;
	let search: HTMLInputElement;
	let launcher = $state<Launcher>();
	let query = $state('');
	let power = $state<'restart' | 'shutdown' | null>(null);
	let open = $state(false);

	$effect(() => {
		if (open) return watchSources();
	});
	export function showMenu(focusSearch = false) {
		open = true;
		dialog.showModal();
		if (focusSearch) search.focus();
		else void tick().then(() => dialog.querySelector<HTMLElement>('.filters button')?.focus());
	}
	function close() {
		dialog.close();
	}
	function choose(key: string, href?: string) {
		close();
		const program = [...programs(), ...vmScreens()].find((p) => p.key === key);
		if (href) void goto(href);
		else if (program) void openPhoneProgram(program);
	}
</script>

<header class="nord-header">
	<button
		class="pill menu-button"
		type="button"
		data-menu-button
		aria-haspopup="dialog"
		onclick={() => showMenu()}
	>
		<Icon name="menu" size={16} /><span>{t('desktop.menu.title')}</span>
	</button>
	{#if showHome}
		<a
			class="pill brand home-link"
			href="/"
			aria-label={t('desktop.phone.home')}
			title={t('desktop.phone.home')}
		>
			<Icon name="home" size={21} />TitanOS
		</a>
	{:else}
		<span class="brand"><img src="/favicon.svg" alt="" width="27" height="27" />TitanOS</span>
	{/if}
	<span class="pill tools">
		<button type="button" aria-label={t('desktop.menu.search')} onclick={() => showMenu(true)}
			><Icon name="search" size={17} /></button
		>
		<button
			type="button"
			aria-label={t('phone.nord.customize')}
			onclick={() => {
				phoneChrome.customizeDock = false;
				phoneChrome.customize = true;
			}}
		>
			<svg
				width="18"
				height="18"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="1.7"
				stroke-linecap="round"
				aria-hidden="true"
				><path d="M4 8h5m5 0h6M4 16h9m5 0h2" /><circle cx="11" cy="8" r="2.3" /><circle
					cx="15.5"
					cy="16"
					r="2.3"
				/></svg
			>
		</button>
	</span>
</header>
<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dialog}
	class="library"
	aria-label={t('desktop.menu.title')}
	onclose={() => {
		open = false;
		query = '';
	}}
	onclick={(e) => {
		if (
			e.target === dialog &&
			!e.currentTarget.querySelector('.library-top')?.contains(e.target as Node)
		)
			close();
	}}
>
	<div class="library-top">
		<label
			><Icon name="search" size={18} /><input
				bind:this={search}
				bind:value={query}
				type="search"
				placeholder={t('desktop.menu.search')}
				aria-label={t('desktop.menu.search')}
				onkeydown={(e) => launcher?.searchKey(e)}
			/></label
		>
		<button type="button" onclick={close} aria-label={t('common.close')}
			><Icon name="close" size={20} /></button
		>
	</div>
	{#if open}
		<Launcher
			bind:this={launcher}
			phone
			{query}
			onsearch={() => search.focus()}
			onopen={choose}
			onclose={close}
			onpower={(act) => {
				close();
				power = act;
			}}
		/>
	{/if}
</dialog>
{#if power}<PowerDialog action={power} onclose={() => (power = null)} />{/if}

<style>
	.nord-header {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
		align-items: center;
		gap: 6px;
		padding: calc(12px + env(safe-area-inset-top)) max(14px, env(safe-area-inset-right)) 5px
			max(14px, env(safe-area-inset-left));
		color: white;
		position: relative;
		z-index: 5;
		flex: none;
	}
	.pill {
		border: 1px solid rgb(224 244 255 / 0.23);
		background: var(--nord-chrome);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		box-shadow: inset 0 1px rgb(255 255 255 / 0.08);
		border-radius: 99px;
		color: inherit;
		height: 46px;
	}
	.menu-button {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		justify-self: start;
		width: 92px;
		font: inherit;
		font-size: 0.91rem;
		font-weight: 600;
		cursor: pointer;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 7px;
		font-size: 1.13rem;
		font-weight: 700;
		letter-spacing: -0.04em;
		text-shadow: 0 1px 8px #06151c;
	}
	.home-link {
		justify-self: center;
		justify-content: center;
		padding-inline: 9px;
		min-width: 44px;
		text-decoration: none;
	}
	.home-link:focus-visible {
		outline: 2px solid currentColor;
		outline-offset: 2px;
	}

	.tools {
		display: flex;
		justify-self: end;
		width: 92px;
		align-items: center;
		justify-content: center;
	}
	.tools button,
	.library-top button {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		color: inherit;
		background: none;
		border: 0;
		border-radius: 99px;
		cursor: pointer;
	}
	.library {
		box-sizing: border-box;
		position: fixed;
		inset: calc(10px + env(safe-area-inset-top)) max(8px, env(safe-area-inset-right)) auto
			max(8px, env(safe-area-inset-left));
		width: auto;
		max-width: 700px;
		max-height: calc(100dvh - 20px - env(safe-area-inset-top) - env(safe-area-inset-bottom));
		margin: 0 auto;
		padding: 8px;
		border-radius: 24px;
		border: 1px solid var(--border);
		background: var(--nord-surface);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		color: var(--text);
		overflow: auto;
	}
	.library::backdrop {
		background: rgb(3 14 19 / 0.46);
	}
	.library-top {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.library-top label {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: 1;
		min-width: 0;
		background: var(--chip);
		padding: 0 12px;
		border-radius: 16px;
	}
	.library input {
		flex: 1;
		min-width: 0;
		min-height: 44px;
		font: inherit;
		font-size: 16px;
		color: inherit;
		background: none;
		border: 0;
	}
	.library :global(.launcher.phone) {
		position: relative;
		inset: auto;
		margin-top: 6px;
		width: 100%;
		max-width: 100%;
		max-height: calc(100dvh - 92px - env(safe-area-inset-top) - env(safe-area-inset-bottom));
		border: 0;
		box-shadow: none;
		background: transparent;
		backdrop-filter: none;
	}
	@media (max-width: 350px) {
		.nord-header {
			padding-inline: 8px;
		}
		.brand {
			font-size: 1rem;
			gap: 4px;
		}
		.menu-button,
		.tools {
			width: 89px;
		}
	}
</style>
