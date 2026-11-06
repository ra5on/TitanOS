<script lang="ts">
	// A view of a function opened from the agent (menu, settings), PLAN 54:
	// each group renders its own ids. paper, deadlines: Docs; guard,
	// handbook, emergency: Protect; advisor: Foresight; buddy, catalog: Family.
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import DocsViews from './DocsViews.svelte';
	import ProtectViews from './ProtectViews.svelte';
	import ForesightViews from './ForesightViews.svelte';
	import FamilyViews from './FamilyViews.svelte';

	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean };
	let { id, onclose, calm = false }: Props = $props();
</script>

{#if id === 'paper' || id === 'deadlines'}
	<DocsViews {id} {onclose} {calm} />
{:else if id === 'guard' || id === 'handbook' || id === 'emergency'}
	<ProtectViews {id} {onclose} {calm} />
{:else if id === 'advisor'}
	<ForesightViews {id} {onclose} {calm} />
{:else}
	<FamilyViews {id} {onclose} {calm} />
{/if}
