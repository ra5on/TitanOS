<script lang="ts">
	// The failure of a plugin action, with the NAS's message where there is one.
	import type { Problem } from './plugins.ts';

	let { problem }: { problem: Problem } = $props();
</script>

<div class="notice-error" role="alert">
	{problem.text}{#if problem.detail}<pre>{problem.detail}</pre>{/if}
</div>

<style>
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
