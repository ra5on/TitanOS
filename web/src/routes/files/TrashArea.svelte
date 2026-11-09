<script lang="ts">
	// The recycle bin of the current place: the share's own bin, or for a
	// volume the bin of one of its shares (chosen first when there are several).
	import type { FileRoot } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import TrashView from './TrashView.svelte';

	let {
		root,
		binRoot,
		bins,
		openBin,
		trashBin = $bindable(),
		user,
		admin,
		onback,
		onchanged,
	}: {
		root: FileRoot;
		binRoot: FileRoot | undefined;
		bins: string[];
		openBin: string;
		trashBin: string;
		user: string;
		admin: boolean;
		/** Leaves the bin for the folder list. */
		onback: () => void;
		onchanged: () => void;
	} = $props();
</script>

{#if root.recycle}
	<TrashView {root} {user} {admin} onclose={onback} {onchanged} />
{:else if binRoot}
	<TrashView
		root={binRoot}
		bin={openBin}
		{user}
		{admin}
		onclose={() => {
			if (trashBin && bins.length > 1) trashBin = '';
			else onback();
		}}
		{onchanged}
	/>
{:else if bins.length}
	<div class="toolbar">
		<button type="button" class="btn" onclick={onback}>{t('files.trashBack')}</button>
	</div>
	<section class="bin-choice" aria-label={t('files.trashChoose')}>
		<h2 class="tile-title">{t('files.trashChoose')}</h2>
		<p class="muted">{t('files.trashChooseHint', { name: root.name })}</p>
		{#each bins as bin (bin)}<button type="button" class="btn bin" onclick={() => (trashBin = bin)}
				><Icon name="trash" size={16} /><span class="mono">{root.name}/{bin}</span></button
			>{/each}
	</section>
{:else}
	<div class="toolbar">
		<button type="button" class="btn" onclick={onback}>{t('files.trashBack')}</button>
	</div>
	<EmptyState title={t('files.trashOf', { name: root.name })} text={t('files.trashDisabled')} />
	{#if admin}<a class="btn trash-configure" href="/shares">{t('files.trashConfigure')}</a>{/if}
{/if}

<style>
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2-5) var(--space-3);
		border-bottom: 1px solid var(--panel-border);
	}
	.toolbar .btn {
		height: 2.375rem;
	}
	.bin-choice {
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-4);
	}
	.bin-choice .bin {
		justify-content: flex-start;
		gap: var(--space-2);
	}
	.trash-configure {
		align-self: center;
		margin-bottom: var(--space-4);
	}
	@media (max-width: 760px) {
		.toolbar .btn {
			height: 2.75rem;
		}
	}
</style>
