<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import type { TileOptions } from './options.ts';

	// The head of a dashboard tile: grip and buttons in the editor, collapse otherwise.
	let {
		title,
		summary,
		settings,
		osIcon = 'monitor',
		editing,
		locked,
		saving,
		onpointerdown,
		onkeymove,
		onlock,
		onconfigure,
		onhide,
		oncollapse,
	}: {
		title: string;
		summary: string;
		settings: TileOptions;
		/** The mark for the icon choice "operating system" (tiles of one VM). */
		osIcon?: IconName;
		editing: boolean;
		locked: boolean;
		saving: boolean;
		onpointerdown: (event: PointerEvent) => void;
		onkeymove: (event: KeyboardEvent) => void;
		onlock: () => void;
		onconfigure: () => void;
		onhide: () => void;
		oncollapse: () => void;
	} = $props();
</script>

<!-- svelte-ignore a11y_no_static_element_interactions (the grip is the keyboard handle) -->
<div class="tile-head" class:draggable={editing && !locked} {onpointerdown}>
	{#if editing}<button
			disabled={locked}
			class="grip"
			title={t('dashboard.editor.moveHint')}
			aria-label={t('dashboard.move', { tile: title })}
			onkeydown={onkeymove}><Icon name="grip" size={16} /></button
		>{/if}
	{#if settings.showIcon}<span class="tile-symbol"
			><Icon
				name={settings.icon === 'os' ? osIcon : settings.icon}
				size={settings.titleSize + 3}
			/></span
		>{/if}
	<h2 class="tile-title" data-edit-part="heading">
		{settings.showTitle ? title : ''}
	</h2>
	{#if settings.showSummary}<span class="tile-summary">{summary}</span>{/if}
	{#if editing}<button
			class="tile-action"
			title={t(locked ? 'dashboard.unlockPosition' : 'dashboard.lockPosition')}
			aria-label={t('dashboard.lockPosition')}
			aria-pressed={locked}
			onclick={onlock}><Icon name={locked ? 'lock' : 'unlock'} size={14} /></button
		><button
			class="tile-action"
			title={t('dashboard.tileOptions')}
			aria-label={`${t('dashboard.tileOptions')}: ${title}`}
			onclick={onconfigure}><Icon name="edit" size={14} /></button
		><button
			class="tile-action"
			title={t('dashboard.hide', { tile: title })}
			aria-label={t('dashboard.hide', { tile: title })}
			onclick={onhide}><Icon name="close" size={16} /></button
		>{:else}<button
			class="tile-action collapse"
			class:collapsed={settings.collapsed}
			disabled={saving}
			title={t(settings.collapsed ? 'dashboard.expandTile' : 'dashboard.collapseTile', {
				name: title,
			})}
			aria-label={t(settings.collapsed ? 'dashboard.expandTile' : 'dashboard.collapseTile', {
				name: title,
			})}
			aria-expanded={!settings.collapsed}
			onclick={oncollapse}><Icon name="chevronDown" size={13} /></button
		>{/if}
</div>

<style>
	.tile-head {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: none;
		min-height: 35px;
		padding: 5px 10px;
		border-bottom: 1px solid var(--border);
		background: color-mix(in srgb, var(--text) 3%, transparent);
	}
	.tile-head.draggable {
		cursor: grab;
		touch-action: none;
	}
	/* `.moving` marks the dragged grid item in the dashboard. */
	:global(.moving) .tile-head.draggable {
		cursor: grabbing;
	}
	.tile-title {
		flex: 1;
		min-width: 0;
		font-size: var(--tile-title-size, var(--text-base));
		font-weight: var(--tile-title-weight, 500);
		font-family: var(--tile-title-font);
		color: var(--tile-title-color, var(--text));
		text-align: var(--tile-title-align, left);
		line-height: 1.3;
		letter-spacing: 0;
		text-transform: uppercase;
		overflow-wrap: anywhere;
	}
	.tile-symbol {
		color: var(--accent);
	}
	.tile-summary {
		color: var(--muted);
		font-size: var(--text-xs);
		min-width: 0;
		overflow-wrap: anywhere;
		text-align: right;
	}
	.grip,
	.tile-action {
		border: 0;
		background: transparent;
		color: var(--muted);
		padding: 3px;
		cursor: pointer;
		display: flex;
		align-items: center;
		flex: none;
	}
	.grip {
		cursor: grab;
		touch-action: none;
	}
	.grip:disabled {
		cursor: default;
		opacity: 0.35;
	}
	.collapse :global(svg) {
		transform: rotate(180deg);
	}
	.collapse.collapsed :global(svg) {
		transform: none;
	}
</style>
