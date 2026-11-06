<script lang="ts">
	// Stub of the skeleton (PLAN 54): the views of this group opened from
	// the agent. Render only for the ids of this group; others are not yours.
	import type { FeatureViewId } from '#lib/agent-features.ts';
	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean };
	const props: Props = $props();
	void props;
</script>
