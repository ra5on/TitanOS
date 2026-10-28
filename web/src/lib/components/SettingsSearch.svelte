<script lang="ts">
	// The search over all settings areas and sections. Arrow keys move through
	// the results shown next to it, Enter opens the highlighted one (its section
	// anchor if a section matched), Escape clears.
	import { goto } from '$app/navigation';
	import Icon from './Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { SettingsEntry } from '#lib/settings-search.ts';
	import { settingsSearch } from '#lib/settings-states.svelte.ts';

	type Props = {
		/** The results in the order they are shown. */
		results: SettingsEntry[];
		/** Prefix of the ids of the result links (aria-activedescendant). */
		idPrefix: string;
		/** Shows the label above the field instead of only to screen readers. */
		visibleLabel?: boolean;
	};
	let { results, idPrefix, visibleLabel = false }: Props = $props();

	const searching = $derived(settingsSearch.query.trim() !== '');
	const active = $derived(
		searching && results.length ? Math.min(settingsSearch.active, results.length - 1) : -1,
	);

	function keydown(event: KeyboardEvent) {
		if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
			if (!results.length) return;
			event.preventDefault();
			const step = event.key === 'ArrowDown' ? 1 : -1;
			settingsSearch.active = (Math.max(active, 0) + step + results.length) % results.length;
			document.getElementById(`${idPrefix}-${settingsSearch.active}`)?.scrollIntoView({ block: 'nearest' });
		} else if (event.key === 'Enter') {
			const entry = results[active];
			if (!entry) return;
			event.preventDefault();
			settingsSearch.query = '';
			settingsSearch.active = 0;
			void goto(entry.href);
		} else if (event.key === 'Escape' && settingsSearch.query) {
			event.preventDefault();
			settingsSearch.query = '';
			settingsSearch.active = 0;
		}
	}
</script>

<label class="search" class:visible={visibleLabel}>
	<span class:sr-only={!visibleLabel}>{t('ui.searchSettings')}</span>
	<span class="field-wrap">
		<span class="glass" aria-hidden="true"><Icon name="search" size={15} /></span>
		<input
			class="field"
			type="search"
			autocomplete="off"
			placeholder={visibleLabel ? t('ui.searchHint') : t('ui.searchSettings')}
			aria-controls="{idPrefix}-results"
			aria-activedescendant={active >= 0 ? `${idPrefix}-${active}` : undefined}
			bind:value={settingsSearch.query}
			oninput={() => (settingsSearch.active = 0)}
			onkeydown={keydown}
		/>
	</span>
</label>

<style>
	.search {
		display: grid;
		gap: 6px;
		width: 100%;
	}
	.search.visible {
		gap: 8px;
	}
	.field-wrap {
		position: relative;
		display: block;
	}
	.glass {
		position: absolute;
		left: 10px;
		top: 50%;
		transform: translateY(-50%);
		display: flex;
		color: var(--muted);
		pointer-events: none;
	}
	input {
		padding-left: 32px;
	}
</style>
