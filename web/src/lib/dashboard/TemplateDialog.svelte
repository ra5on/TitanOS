<script lang="ts">
	// Dashboard editor › Template: start from a ready-made arrangement. It
	// replaces the tiles of the editor; Undo brings the previous ones back,
	// and nothing is stored before "Save".
	import Dialog from '#lib/components/Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { DASHBOARD_TEMPLATES, type DashboardTemplate } from './tiles.ts';

	let {
		onpick,
		onclose,
	}: { onpick: (template: DashboardTemplate | 'default') => void; onclose: () => void } = $props();
	let chosen = $state<DashboardTemplate | 'default'>('default');
	const all = ['default', ...Object.keys(DASHBOARD_TEMPLATES)] as (DashboardTemplate | 'default')[];
</script>

<Dialog title={t('dashboard.templates.title')} {onclose}>
	<p class="muted">{t('dashboard.templates.intro')}</p>
	<div class="list" role="radiogroup" aria-label={t('dashboard.templates.title')}>
		{#each all as id (id)}
			<label class="choice" class:picked={chosen === id}>
				<input type="radio" name="template" value={id} bind:group={chosen} />
				<span
					><strong>{t(`dashboard.templates.${id}` as MessageKey)}</strong><span class="muted small"
						>{t(`dashboard.templates.${id}Text` as MessageKey)}</span
					></span
				>
			</label>
		{/each}
	</div>
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button type="button" class="btn btn-primary" onclick={() => onpick(chosen)}
			>{t('dashboard.templates.use')}</button
		>
	</div>
</Dialog>

<style>
	.list {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin: 8px 0;
	}
	.choice {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding: 8px 10px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		cursor: pointer;
	}
	.choice.picked {
		border-color: var(--accent);
	}
	.choice > span {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
</style>
