<script lang="ts">
	// The shared tabs, segments and a dropdown menu, as pages use them.
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	let { tab = 'a', mode = 'x' }: { tab?: string; mode?: string } = $props();
	let chosen = $state<string[]>([]);
</script>

<button type="button">before</button>
<Tabs
	label="tabs"
	tabs={[
		{ id: 'a', label: 'A' },
		{ id: 'b', label: 'B' },
		{ id: 'c', label: 'C' },
	]}
	value={tab}
	onchange={(id) => (tab = id)}
/>
<Segments
	label="modes"
	options={[
		{ value: 'x', label: 'X' },
		{ value: 'y', label: 'Y' },
	]}
	bind:value={mode}
/>
<Dropdown label="more" text="More">
	{#snippet children(close)}
		<button
			type="button"
			role="menuitem"
			onclick={() => {
				chosen.push('one');
				close();
			}}>one</button
		>
		<button type="button" role="menuitem" onclick={close}>two</button>
	{/snippet}
</Dropdown>
<button type="button">after</button>
<p data-chosen>{chosen.join(',')}</p>
