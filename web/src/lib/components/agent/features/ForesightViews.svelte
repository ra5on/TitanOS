<script lang="ts">
	// The views of the group "Vorausschau" opened from the agent (PLAN 54):
	// 'advisor' is the report of the Hardware-Berater. Was-wäre-wenn opens
	// from the storage page and from the report (ForesightWhatIf).
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import ForesightAdvisor from './ForesightAdvisor.svelte';

	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean };
	let { id, onclose }: Props = $props();
</script>

{#if id === 'advisor'}
	<ForesightAdvisor {onclose} />
{/if}
