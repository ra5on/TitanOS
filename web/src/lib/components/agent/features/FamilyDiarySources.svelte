<script lang="ts">
	// The sources of a diary answer under it (PLAN 54-G, mockup 13): the
	// diary tool names them in its step (titanos:diary/<source>). ChatView
	// shows this under an answer with the steps of its turn.
	import type { Entry } from '#lib/agent.ts';
	import { diarySources } from '#lib/features/family.ts';

	let { steps }: { steps: Entry[] } = $props();

	const icons: Record<string, string> = {
		chats: '💬',
		reminders: '📅',
		home: '🏠',
		photos: '📷',
		immich: '📷',
		frigate: '🚪',
	};
	const list = $derived(
		steps.filter((s) => s.tool === 'diary').flatMap((s) => diarySources(s.sources)),
	);
</script>

{#if list.length}
	<ul class="src">
		{#each list as s, i (i)}
			<li><span aria-hidden="true">{icons[s.id] ?? '•'}</span> {s.title}</li>
		{/each}
	</ul>
{/if}

<style>
	.src {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		list-style: none;
		margin: 6px 0 0;
		padding: 0;
	}
	.src li {
		font-size: var(--text-xs, 0.75rem);
		color: var(--muted);
		padding: 3px 10px;
		border-radius: 999px;
		border: 1px solid var(--border);
	}
</style>
