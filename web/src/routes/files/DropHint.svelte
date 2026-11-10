<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		/** Files are being dragged over the folder. */
		dragging: boolean;
		/** Where they will go: place and folder. */
		path: string;
	};
	let { dragging, path }: Props = $props();
</script>

<!-- Only while something is dragged over the folder (owner 10.11.2026): a
     standing hint below the list is in the way. -->
{#if dragging}
	<p class="drop active">
		<Icon name="upload" size={18} />{t('files.dropNow', { path })}
	</p>
{/if}

<style>
	.drop {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		margin: var(--space-3);
		padding: var(--space-3);
		border: 1px dashed var(--accent);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
		color: var(--text);
	}
</style>
