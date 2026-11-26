<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { layouts, type ViewPlace, type MobileViews } from './mobile-views.svelte.ts';
	let { views, place, close }: { views: MobileViews; place: ViewPlace; close: () => void } =
		$props();
	const own = $derived(views.folder(place));
	const full = $derived(!own && (views.data?.folders.length ?? 0) >= 200);
</script>

<button
	role="menuitemradio"
	aria-checked={!own}
	disabled={!views.data || !place.root || views.saving}
	onclick={() => {
		close();
		if (own) void views.change('inherit', place);
	}}
>
	<Icon name="restart" size={18} />{t('files.mobile.inherit')}{#if !own}<span class="mark"
			><Icon name="check" size={16} /></span
		>{/if}</button
>
<hr />
{#each layouts as layout}
	<button
		role="menuitemradio"
		aria-checked={own?.view === layout.value}
		disabled={!views.data || !place.root || views.saving || full}
		onclick={() => {
			close();
			void views.change(layout.value, place);
		}}
	>
		<Icon name={layout.icon} size={18} />{t(layout.label)}{#if own?.view === layout.value}<span
				class="mark"><Icon name="check" size={16} /></span
			>{/if}</button
	>
{/each}
{#if full}<p>{t('files.mobile.full')}</p>{/if}
<hr />
<a role="menuitem" href="/settings/files#folder-views" onclick={close}
	><Icon name="gear" size={18} />{t('files.mobile.settings')}</a
>

<style>
	.mark {
		display: inline-flex;
		margin-inline-start: auto;
		color: var(--accent);
	}
</style>
