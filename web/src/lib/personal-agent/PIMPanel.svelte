<script lang="ts">
	import Session from './PIMSession.svelte';
	import type { Source } from './api.ts';
	let {
		app,
		sources,
		phone = false,
		onclose,
	}: {
		app: 'mail' | 'calendar' | 'contacts';
		sources: Source[];
		phone?: boolean;
		onclose: () => void;
	} = $props();
</script>

{#key JSON.stringify([app, sources.map((s) => [s.kind, s.id, s.ref])])}
	<Session {app} {sources} {phone} {onclose} />
{/key}
