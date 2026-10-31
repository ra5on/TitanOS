<script lang="ts" module>
	export type ShareChoice = 'details' | 'connect' | 'edit' | 'delete';
</script>

<script lang="ts">
	// The menu of a share in the list, below its "⋯" button.
	import type { Share } from '#lib/api.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { shareFilesURL } from '#lib/share-view.ts';

	type Props = {
		share: Share;
		x: number;
		y: number;
		canAdminister: boolean;
		accountReady: boolean;
		onclose: () => void;
		onchoose: (choice: ShareChoice) => void;
	};
	let { share, x, y, canAdminister, accountReady, onclose, onchoose }: Props = $props();
</script>

<ContextMenu label={t('shares.actionsFor', { name: share.name })} {x} {y} {onclose}>
	<button role="menuitem" onclick={() => onchoose('details')}>{t('shares.details')}</button>
	<button role="menuitem" onclick={() => onchoose('connect')}>{t('shares.connect')}</button>
	<a role="menuitem" href={shareFilesURL(share.name)}>{t('shares.openFiles')}</a>
	{#if canAdminister}<button
			role="menuitem"
			disabled={!accountReady}
			onclick={() => onchoose('edit')}>{t('shares.edit')}</button
		>
		<hr />
		<button role="menuitem" class="danger" onclick={() => onchoose('delete')}
			>{t('shares.delete')}</button
		>{/if}
</ContextMenu>
