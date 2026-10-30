<script lang="ts">
	import type { FileRoot } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		root: FileRoot | undefined;
		dir: string;
		/** The list of places is unfolded. */
		placesOpen: boolean;
		canBack: boolean;
		onback: () => void;
	};
	let { root, dir, placesOpen = $bindable(), canBack, onback }: Props = $props();
</script>

<!-- On phones: where we are, as a button that unfolds the places. On wider screens only the places are shown. -->
<div class="mobile-location">
	<button
		type="button"
		class="btn places-toggle"
		aria-label={`${root?.name ?? '–'} / ${dir}`}
		title={`${root?.name ?? '–'} / ${dir}`}
		aria-expanded={placesOpen}
		aria-controls="file-places"
		onclick={() => (placesOpen = !placesOpen)}
		><Icon name="folder" size={16} /><span
			>{root?.name ?? '–'}{#if dir}<span class="muted"> / {dir}</span>{/if}</span
		><Icon name="chevronDown" size={14} /></button
	>
	<button
		type="button"
		class="btn mobile-back"
		aria-label={t('files.back')}
		disabled={!canBack}
		onclick={onback}><Icon name="arrowLeft" size={16} /></button
	>
</div>

<style>
	.places-toggle {
		display: none;
	}
	.mobile-location {
		display: contents;
	}
	.mobile-back {
		display: none;
	}
	@media (max-width: 760px) {
		.places-toggle {
			display: flex;
			justify-content: flex-start;
			width: 100%;
		}
		.places-toggle span {
			flex: 1;
			text-align: left;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}
		.mobile-location {
			display: flex;
			gap: 8px;
			align-items: center;
			min-width: 0;
		}
		.mobile-location .places-toggle {
			flex: 1;
			min-width: 0;
			width: auto;
			height: 44px;
			background: var(--panel-bg);
			border-color: var(--panel-border);
			backdrop-filter: var(--panel-backdrop);
		}
		.mobile-location .places-toggle > span {
			min-width: 0;
			flex: 1;
		}
		.mobile-location .places-toggle > span .muted {
			display: inline;
			white-space: nowrap;
		}
		.mobile-back {
			display: inline-flex;
			width: 44px;
			min-width: 44px;
			height: 44px;
			padding: 0;
		}
	}
</style>
