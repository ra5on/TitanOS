<script lang="ts">
	// 1.12: the keys of T-Mail ("?"), a plain window that only shows.
	import Dialog from '../components/Dialog.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { MAIL_KEYS, type MailKey } from './shortcuts.ts';

	let { onclose }: { onclose: () => void } = $props();

	const label = (a: MailKey) =>
		({
			next: t('mail.keys.next'),
			prev: t('mail.keys.prev'),
			archive: t('mail.keys.archive'),
			reply: t('mail.keys.reply'),
			delete: t('mail.keys.delete'),
			search: t('mail.keys.search'),
			help: t('mail.keys.help'),
		})[a];
</script>

<Dialog title={t('mail.keys.title')} dismiss {onclose}>
	<dl class="keys">
		{#each MAIL_KEYS as k (k.action)}
			<dt>
				{#each k.keys as key, i (key)}{#if i}<span class="or"> {t('mail.keys.or')} </span>{/if}<kbd
						>{key}</kbd
					>{/each}
			</dt>
			<dd>{label(k.action)}</dd>
		{/each}
	</dl>
	<p class="muted note">{t('mail.keys.note')}</p>
	<div class="dialog-actions">
		<!-- svelte-ignore a11y_autofocus -->
		<button type="button" class="btn" autofocus onclick={onclose}>{t('common.close')}</button>
	</div>
</Dialog>

<style>
	.keys {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: var(--space-2) var(--space-4);
		margin: 0 0 var(--space-3);
	}
	dt {
		margin: 0;
	}
	dd {
		margin: 0;
		overflow-wrap: break-word;
	}
	kbd {
		display: inline-block;
		min-width: 1.8em;
		padding: 1px var(--space-1-5);
		border: 1px solid var(--border);
		border-bottom-width: 2px;
		border-radius: var(--radius-small);
		background: var(--chip);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		text-align: center;
	}
	.or {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.note {
		margin: 0 0 var(--space-3);
		overflow-wrap: break-word;
	}
</style>
