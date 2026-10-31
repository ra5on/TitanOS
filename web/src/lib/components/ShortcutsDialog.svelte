<script lang="ts">
	// The keyboard shortcuts of the whole UI ("?", Settings › Appearance › Effects).
	import Dialog from './Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	type Props = { admin: boolean; onclose: () => void };
	let { admin, onclose }: Props = $props();

	const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
	const mod = mac ? '⌘' : 'Strg';

	const rows = $derived(
		(
			[
				{ keys: [mod, 'K'], label: 'shortcuts.palette', show: true },
				{ keys: ['Strg', '`'], label: 'shortcuts.terminal', show: admin },
				{ keys: ['?'], label: 'shortcuts.help', show: true },
				{ keys: [mod, 'Z'], label: 'shortcuts.dashboardUndo', show: true },
				{ keys: [mod, 'S'], label: 'shortcuts.dashboardSave', show: true },
				{ keys: ['F1'], label: 'shortcuts.files', show: true },
				{ keys: ['Esc'], label: 'shortcuts.escape', show: true },
			] satisfies { keys: string[]; label: MessageKey; show: boolean }[]
		).filter((row) => row.show),
	);
</script>

<Dialog title={t('shortcuts.title')} {onclose}>
	<dl class="keys">
		{#each rows as row (row.label)}
			<dt>
				{#each row.keys as key, index (index)}{#if index}<span class="plus">+</span>{/if}<kbd
						>{key === 'Strg' ? t('shortcuts.ctrl') : key}</kbd
					>{/each}
			</dt>
			<dd>{t(row.label)}</dd>
		{/each}
	</dl>
	<p class="muted hint">{t('shortcuts.hint')}</p>
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
	</div>
</Dialog>

<style>
	.keys {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 10px 18px;
		margin: 0;
		font-size: 0.875rem;
	}
	dt {
		display: flex;
		align-items: center;
		gap: 4px;
		white-space: nowrap;
	}
	dd {
		margin: 0;
	}
	kbd {
		min-width: 1.75rem;
		padding: 2px 7px;
		background: var(--chip);
		border: 1px solid var(--border);
		border-bottom-width: 2px;
		border-radius: var(--radius-small);
		font-family: 'IBM Plex Mono', monospace;
		font-size: 0.75rem;
		text-align: center;
	}
	.plus {
		color: var(--muted);
		font-size: 0.75rem;
	}
	.hint {
		margin: 0;
		font-size: 0.75rem;
	}
</style>
