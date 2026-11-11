<script lang="ts">
	// The dialogs of the app's page: removing the app, the tailnet, a new
	// entry, discarding changes, the logs and leaving with unsaved changes.
	import Dialog from '#lib/components/Dialog.svelte';
	import AddEntryDialog from '#lib/components/appform/AddEntryDialog.svelte';
	import { tailnetPort } from '#lib/app-proxy.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import RemoveAppDialog from '../../RemoveAppDialog.svelte';
	import TailnetDialog from '../../TailnetDialog.svelte';
	import type { AppEditor } from './app-editor.svelte.ts';

	let { ed }: { ed: AppEditor } = $props();
</script>

{#if ed.removing}<RemoveAppDialog
		app={ed.name}
		volume={ed.status?.settings.volume ?? ''}
		busy={ed.removeBusy}
		error={ed.removeError}
		onclose={() => (ed.removing = false)}
		onremove={ed.removeApp}
	/>{/if}

{#if ed.tailnetDialog}<TailnetDialog
		app={ed.name}
		mode={ed.tailnetDialog}
		port={tailnetPort(ed.members, ed.primary)}
		onclose={() => (ed.tailnetDialog = null)}
		onchanged={() => {
			ed.proxyVersion += 1;
			void ed.loadProxy();
			void ed.loadStatus();
		}}
	/>{/if}

{#if ed.adding}<AddEntryDialog onadd={ed.addEntry} onclose={() => (ed.adding = false)} />{/if}

{#if ed.confirmDiscard}<Dialog
		title={t('editor.discardTitle')}
		onclose={() => (ed.confirmDiscard = false)}
	>
		<p>{t('editor.discardText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (ed.confirmDiscard = false)}
				>{t('shares.keepEditing')}</button
			><button type="button" class="btn btn-warn" onclick={ed.discardChanges}
				>{t('editor.discard')}</button
			>
		</div>
	</Dialog>{/if}

{#if ed.logs}
	{@const open = ed.logs}
	<Dialog
		title={t('docker.logsTitle', { name: dockerName(open.container, 'distinct') })}
		wide
		onclose={() => (ed.logs = null)}
	>
		{#if open.error}<div class="notice-error row-note" role="alert">
				<span>{open.error}</span><button
					type="button"
					class="btn small"
					onclick={() => ed.showLogs(open.container)}>{t('common.retry')}</button
				>
			</div>{/if}
		<pre class="logs">{open.text === null ? '…' : open.text || t('docker.logsEmpty')}</pre>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => ed.showLogs(open.container)}
				>{t('docker.logsRefresh')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => (ed.logs = null)}
				>{t('common.done')}</button
			>
		</div>
	</Dialog>
{/if}

{#if ed.leaving}<Dialog
		title={t('shares.unsaved')}
		locked={ed.applying}
		onclose={() => {
			ed.leaving = false;
			ed.pendingURL = '';
		}}
	>
		<p>{t('editor.unsavedHint')}</p>
		<div class="dialog-actions">
			<button
				class="btn"
				disabled={ed.applying}
				onclick={() => {
					ed.leaving = false;
					ed.pendingURL = '';
				}}>{t('shares.keepEditing')}</button
			><button class="btn btn-warn" disabled={ed.applying} onclick={ed.discard}
				>{t('shares.discard')}</button
			><button class="btn btn-primary" disabled={ed.blocked} onclick={ed.apply}
				>{t('editor.apply')}</button
			>
		</div>
		{#if ed.result && !ed.result.ok}<p class="notice-error" role="alert">{ed.result.text}</p>{/if}
	</Dialog>{/if}

<style>
	.row-note {
		flex: 1 1 100%;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
	.logs {
		max-height: 60vh;
		margin: 0;
		padding: var(--space-3) 14px;
		overflow: auto;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		line-height: 1.5;
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
