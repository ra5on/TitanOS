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

<p class="drop" class:active={dragging}>
	<Icon name="upload" size={18} />{dragging ? t('files.dropNow', { path }) : t('files.dropHint')}
</p>

<style>
	.drop {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		margin: 12px;
		padding: 12px;
		border: 1px dashed var(--outline-border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
		color: var(--muted);
	}
	.drop.active {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
		color: var(--text);
	}
</style>
