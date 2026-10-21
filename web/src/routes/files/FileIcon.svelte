<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { fileKind } from '#lib/file-actions.ts';

	let { name, dir, size = 16 }: { name: string; dir: boolean; size?: number } = $props();
	const kind = $derived(fileKind(name, dir));
	const icon = $derived(kind === 'text' ? 'fileText' : kind);
</script>

<span class="file-icon" data-kind={kind}><Icon name={icon} {size} /></span>

<style>
	.file-icon {
		display: inline-flex;
		flex: none;
		color: var(--muted);
	}
	.file-icon[data-kind='folder'] {
		color: var(--accent);
	}
</style>
