<script lang="ts">
	// 1.13: ColumnFrame with a side bar of three sortable entries, a list
	// and content (column-frame.test.ts).
	import ColumnFrame from '#lib/components/ColumnFrame.svelte';
	import SortGrip, { sortKey } from '#lib/components/SortGrip.svelte';

	let {
		columns = true,
		withList = true,
		level = 'content',
	}: { columns?: boolean; withList?: boolean; level?: 'side' | 'list' | 'content' } = $props();
	let keys = $state(['eins', 'zwei', 'drei']);
	const move = (next: string[]) => (keys = next);
</script>

{#snippet action(folded: boolean)}
	<button type="button" aria-label={folded ? 'Neu' : undefined}>{folded ? '+' : 'Neu'}</button>
{/snippet}
{#snippet side()}
	<ul data-sort-list>
		{#each keys as key (key)}
			<li data-sort-group="" data-sort-key={key}>
				<button type="button" class="entry" onkeydown={(e) => sortKey(e, keys, key, key, move)}
					>{key}</button
				>
				<SortGrip group="" {key} {keys} name={key} onmove={move} />
			</li>
		{/each}
	</ul>
{/snippet}
{#snippet rail()}<span class="rail-probe">Rail</span>{/snippet}
{#snippet list()}<p>Liste</p>{/snippet}

<main>
	<ColumnFrame
		storage="titanos.probe"
		{columns}
		{level}
		class="probe"
		sideClass="probe-side"
		{action}
		{side}
		{rail}
		list={withList ? list : undefined}
	>
		<p>Inhalt</p>
	</ColumnFrame>
</main>
