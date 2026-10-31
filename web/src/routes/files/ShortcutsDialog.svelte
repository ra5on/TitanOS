<script lang="ts">
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let { onclose }: { onclose: () => void } = $props();
</script>

<Dialog title={t('files.shortcuts')} {onclose}>
	<p>{t('files.shortcutsHint')}</p>
	<dl class="file-properties">
		{#each [['↑ ↓ ← →', 'navigate'], ['Enter', 'open'], ['Space', 'preview'], ['Shift + ↑ / ↓', 'range'], ['Ctrl / ⌘ + A', 'all'], ['Ctrl / ⌘ + C / X / V', 'clipboard'], ['F2', 'rename'], ['Delete', 'delete'], ['Shift + F10', 'menu'], ['Esc', 'clear'], ['F1', 'help']] as const as [keys, action] (keys)}<dt
				class="mono"
			>
				{keys}
			</dt>
			<dd>{t(`files.key.${action}`)}</dd>{/each}
	</dl>
	<p class="muted">{t('files.key.doubleClick')}</p>
	<div class="dialog-actions">
		<button class="btn" onclick={onclose}>{t('files.editClose')}</button>
	</div>
</Dialog>

<style>
	.file-properties dd {
		margin: 0 0 8px;
		overflow-wrap: anywhere;
	}
	.file-properties dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
</style>
