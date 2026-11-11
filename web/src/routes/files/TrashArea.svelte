<script lang="ts">
	// The recycle bin of the current place: the share's own bin, or for a
	// volume the bin of one of its shares (chosen first when there are several).
	import type { FileRoot } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
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
	<!-- PLAN 78-5: a plain heading; every bin is a row that opens it. -->
	<div class="bin-choice">
		<SettingGroup
			title={t('files.trashChoose')}
			desc={t('files.trashChooseHint', { name: root.name })}
			id="files-bins"
		>
			{#each bins as bin (bin)}<button
					type="button"
					class="srow bin"
					onclick={() => (trashBin = bin)}
					><span class="srow-head"
						><span class="srow-ico" aria-hidden="true"><Icon name="trash" size={19} /></span><span
							class="srow-text"><span class="srow-title mono">{root.name}/{bin}</span></span
						><span class="srow-chev" aria-hidden="true"><Icon name="chevronRight" size={16} /></span
						></span
					></button
				>{/each}
		</SettingGroup>
	</div>
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
		padding: var(--space-3) var(--space-4) var(--space-4);
	}
	.bin-choice .bin {
		width: 100%;
		padding: 0;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.bin-choice .bin:hover {
		background: var(--srow-hover);
	}
	.bin-choice .bin:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
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
