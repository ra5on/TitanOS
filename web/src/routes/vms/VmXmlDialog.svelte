<script lang="ts">
	// The libvirt configuration of a VM, to read.
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let { text, onclose }: { text: string; onclose: () => void } = $props();
</script>

<Dialog title={t('vms.showXml')} wide {onclose}>
	<pre class="xml">{text}</pre>
	<div class="dialog-actions">
		<button type="button" class="btn btn-primary" onclick={onclose}>{t('common.done')}</button>
	</div>
</Dialog>

<style>
	.xml {
		max-height: 60vh;
		margin: 0;
		padding: 12px;
		overflow: auto;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
