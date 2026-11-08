<script lang="ts">
	// PLAN 72: the view of a function as a page in the agent's main area
	// (computer); the sheets read the context and drop their panel.
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import FeatureView from './FeatureView.svelte';
	import { setFeaturePage } from './page-mode.ts';

	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean };
	let { id, onclose, calm = false }: Props = $props();
	setFeaturePage(true);
</script>

<FeatureView {id} {onclose} {calm} />
