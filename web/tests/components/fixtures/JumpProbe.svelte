<script lang="ts">
	// PLAN 86: a settings page in small – two tabs, a group with a row that
	// opens to its parts – for the jump of the settings search.
	import { onMount } from 'svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';

	let tab = $state<'a' | 'b'>('a');
	let { late = false }: { late?: boolean } = $props();
	let shown = $state(false);
	// Some pages draw their rows only once their data is there.
	onMount(() => {
		if (late) setTimeout(() => (shown = true), 250);
		else shown = true;
	});
</script>

<Tabs
	label="Reiter"
	value={tab}
	onchange={(next) => (tab = next)}
	tabs={[
		{ id: 'a', label: 'Erster' },
		{ id: 'b', label: 'Zweiter' },
	]}
/>
{#if tab === 'a'}
	<SettingGroup title="Allgemein">
		<SettingRow title="Einfach" />
	</SettingGroup>
{:else if shown}
	<SettingGroup title="Verbindung">
		<SettingRow id="row-parts" title="Mit Teilen">
			<SettingSub title="Ein Teil">
				{#snippet control()}<input aria-label="Ein Teil" />{/snippet}
			</SettingSub>
		</SettingRow>
	</SettingGroup>
{/if}
