<script lang="ts">
	// The keyboard shortcuts of the whole UI ("?", Settings › Appearance › Effects).
	import Dialog from './Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	/** desktop: also the keys of the desktop and its menu bar (PLAN 67-1). */
	type Props = { admin: boolean; desktop?: boolean; onclose: () => void };
	let { admin, desktop = false, onclose }: Props = $props();

	const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
	const mod = mac ? '⌘' : 'Strg';

	const rows = $derived(
		(
			[
				{ keys: [mod, 'K'], label: 'shortcuts.palette', show: true },
				{ keys: ['Strg', '`'], label: 'shortcuts.terminal', show: admin },
				{ keys: ['?'], label: 'shortcuts.help', show: true },
				{ keys: ['F1'], label: 'shortcuts.files', show: true },
				{ keys: ['Esc'], label: 'shortcuts.escape', show: true },
				{ keys: ['Alt'], label: 'menubar.short.menubar', show: desktop },
				{ keys: ['F10'], label: 'menubar.short.menubar', show: desktop },
				{ keys: ['Strg', 'Leertaste'], label: 'menubar.short.launcher', show: desktop },
				{ keys: ['Alt', 'Q'], label: 'menubar.short.switcher', show: desktop },
				{ keys: ['Alt', 'W'], label: 'menubar.short.close', show: desktop },
				{ keys: ['Alt', 'M'], label: 'menubar.short.minimize', show: desktop },
				{ keys: ['Alt', '← → ↑'], label: 'menubar.short.snap', show: desktop },
				{ keys: ['Alt', 'K'], label: 'menubar.short.showDesktop', show: desktop },
			] satisfies { keys: string[]; label: MessageKey; show: boolean }[]
		).filter((row) => row.show),
	);
</script>

<Dialog title={t('shortcuts.title')} {onclose}>
	<dl class="keys">
		{#each rows as row (row.label + row.keys.join())}
			<dt>
				{#each row.keys as key, index (index)}{#if index}<span class="plus">+</span>{/if}<kbd
						>{key === 'Strg'
							? t('shortcuts.ctrl')
							: key === 'Leertaste'
								? t('shortcuts.space')
								: key}</kbd
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
		gap: var(--space-2-5) 18px;
		margin: 0;
		font-size: var(--text-md);
	}
	dt {
		display: flex;
		align-items: center;
		gap: var(--space-1);
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
		font-size: var(--text-sm);
		text-align: center;
	}
	.plus {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.hint {
		margin: 0;
		font-size: var(--text-sm);
	}
</style>
