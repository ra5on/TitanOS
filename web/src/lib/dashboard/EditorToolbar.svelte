<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	import Icon from '#lib/components/Icon.svelte';

	// The sticky bar with all editor controls.
	let {
		adding,
		saving,
		columns,
		minimumWidth,
		actualColumns,
		canUndo,
		canRedo,
		ontoggleadd,
		oncolumns,
		onminimumwidth,
		onstep,
		ontemplate,
		onrestore,
		oncancel,
		onsave,
	}: {
		adding: boolean;
		saving: boolean;
		columns: number;
		minimumWidth: number;
		actualColumns: number;
		canUndo: boolean;
		canRedo: boolean;
		ontoggleadd: () => void;
		oncolumns: (value: number) => void;
		onminimumwidth: (value: number) => void;
		onstep: (direction: 'undo' | 'redo') => void;
		ontemplate: () => void;
		onrestore: () => void;
		oncancel: () => void;
		onsave: () => void;
	} = $props();
</script>

<div class="editbar" role="toolbar" aria-label={t('dashboard.editor.toolbar')}>
	<div class="flow-controls">
		<button class="btn" aria-pressed={adding} disabled={saving} onclick={ontoggleadd}
			><Icon name="plus" size={16} />{t('dashboard.hidden')}</button
		><a class="btn" href="/settings/appearance"
			><Icon name="palette" size={16} />{t('dashboard.visual.all')}</a
		>
		<label
			>{t('dashboard.columns')}<select
				class="field"
				value={columns}
				onchange={(e) => oncolumns(Number(e.currentTarget.value))}
				>{#each [1, 2, 3, 4] as n}<option value={n}>{n}</option>{/each}</select
			></label
		><label
			>{t('dashboard.minimumWidth')}<input
				class="field"
				type="number"
				min="280"
				max="700"
				step="10"
				value={minimumWidth}
				onchange={(e) => onminimumwidth(Number(e.currentTarget.value))}
			/></label
		>{#if actualColumns !== columns}<span
				>{t('dashboard.visibleColumns', { count: actualColumns })}</span
			>{/if}
	</div>
	<div class="editor-actions">
		<button
			class="btn icon-btn"
			disabled={!canUndo || saving}
			title={`${t('dashboard.editor.undo')} (${t('shortcuts.ctrl')}+Z)`}
			aria-label={t('dashboard.editor.undo')}
			onclick={() => onstep('undo')}><Icon name="undo" size={15} /></button
		><button
			class="btn icon-btn"
			disabled={!canRedo || saving}
			title={`${t('dashboard.editor.redo')} (${t('shortcuts.ctrl')}+Y)`}
			aria-label={t('dashboard.editor.redo')}
			onclick={() => onstep('redo')}><Icon name="redo" size={15} /></button
		><button class="btn" disabled={saving} onclick={ontemplate}
			>{t('dashboard.templates.button')}</button
		><button class="btn" disabled={saving} onclick={onrestore}>{t('dashboard.restore')}</button
		><button class="btn" disabled={saving} onclick={oncancel}>{t('common.cancel')}</button><button
			class="btn btn-primary"
			disabled={saving}
			title={`${t('dashboard.editor.save')} (${t('shortcuts.ctrl')}+S)`}
			onclick={onsave}>{saving ? t('dashboard.editor.saving') : t('dashboard.editor.save')}</button
		>
	</div>
</div>

<style>
	/* One bar for all editor controls; it stays below the header while scrolling. */
	.editbar {
		position: sticky;
		top: 60px;
		z-index: 15;
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		border: 1px solid var(--border);
		background: var(--surface);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
		margin-bottom: 12px;
		font-size: 0.8rem;
		color: var(--muted);
	}
	.flow-controls {
		display: flex;
		gap: 14px;
		flex-wrap: wrap;
		align-items: center;
	}
	.flow-controls label {
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.flow-controls .field {
		width: 80px;
		min-height: 32px;
	}
	.editor-actions {
		display: flex;
		justify-content: flex-end;
		flex-wrap: wrap;
		gap: 8px;
	}
	.icon-btn {
		padding-inline: 9px;
	}
	@media (max-width: 760px) {
		.editbar {
			font-size: 0.75rem;
		}
	}
</style>
