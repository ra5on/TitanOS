<script lang="ts">
	// PLAN 61: the bar above a program on the phone – back to the home
	// screen, the program's name and its pages.
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { appForPath, sideActive } from './apps.ts';

	const app = $derived(appForPath(page.url.pathname));
	const items = $derived((app?.side ?? []).filter((i) => 'href' in i));
	let open = $state(false);
</script>

<header class="phone-bar">
	<button type="button" class="back" onclick={() => goto('/')} aria-label={t('desktop.phone.home')}
		><Icon name="chevronLeft" size={22} /></button
	>
	{#if app}<DesktopIcon
			glyph={app.glyph}
			gradient={app.gradient}
			size={26}
			name={t(app.label)}
		/>{/if}
	<b>{app ? t(app.label) : ''}</b>
	<span class="space"></span>
	{#if items.length > 1}
		<button
			type="button"
			class="back"
			aria-haspopup="menu"
			aria-expanded={open}
			onclick={() => (open = !open)}
			aria-label={t('desktop.phone.pages')}><Icon name="more" size={20} /></button
		>
		{#if open}
			<div class="pages dropdown-panel" role="menu">
				{#each items as item, i (i)}
					{#if 'label' in item}
						<button
							type="button"
							role="menuitemradio"
							aria-checked={sideActive(item, page.url.pathname, app?.side ?? [])}
							onclick={() => ((open = false), goto(item.href))}><span>{t(item.label)}</span></button
						>
					{/if}
				{/each}
			</div>
		{/if}
	{/if}
</header>

<style>
	.phone-bar {
		position: sticky;
		top: 0;
		z-index: 30;
		display: flex;
		align-items: center;
		gap: 8px;
		height: calc(52px + env(safe-area-inset-top));
		padding: env(safe-area-inset-top) 8px 0;
		background: var(--header);
		border-bottom: 1px solid var(--border);
	}
	.back {
		width: 40px;
		height: 40px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 10px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	b {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.space {
		flex: 1;
	}
	.pages {
		position: absolute;
		right: 8px;
		top: calc(100% + 4px);
		min-width: 14rem;
	}
</style>
