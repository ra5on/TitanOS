<script lang="ts" module>
	export type VmDialogKind = 'remove' | 'poweroff' | 'paste';
	export type VmDialogDraft = { deleteDisks: boolean; text: string };
</script>

<script lang="ts">
	// Removing a VM, forcing it off, or pasting text into its screen.
	import type { VM } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { problem, type Problem } from './vm-page.ts';

	type Props = {
		kind: VmDialogKind;
		vm: VM;
		onclose: () => void;
		/** Does the work; the dialog shows what fails. */
		onsubmit: (draft: VmDialogDraft) => Promise<void>;
	};
	let { kind, vm, onclose, onsubmit }: Props = $props();

	let draft = $state<VmDialogDraft>({ deleteDisks: false, text: '' });
	let busy = $state(false);
	let error = $state<Problem | null>(null);

	// Runs only from the dialog's button, never from Enter in the form.
	async function submit() {
		if (busy) return;
		busy = true;
		error = null;
		try {
			await onsubmit(draft);
		} catch (err) {
			error = problem(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t(`vms.dialog.${kind}`, { name: vmLabel(vm) })} locked={busy} {onclose}>
	<form onsubmit={(event) => event.preventDefault()}>
		{#if kind === 'remove'}
			<p class="text">{t('vms.removeText')}</p>
			<label class="check"
				><input type="checkbox" bind:checked={draft.deleteDisks} disabled={busy} /><span
					>{t('vms.deleteDisks')}</span
				></label
			>
			{#if draft.deleteDisks}
				<p class="notice-error">
					{t('vms.deleteDisksWarning', {
						disks:
							vm.disks
								.filter((disk) => !disk.path)
								.map((disk) => disk.file)
								.join(', ') || '–',
					})}
				</p>
			{/if}
		{:else if kind === 'poweroff'}
			<p class="text">{t('vms.poweroffText')}</p>
		{:else if kind === 'paste'}
			<p class="text">{t('vms.pasteText')}</p>
			<!-- svelte-ignore a11y_autofocus -->
			<textarea class="field mono" rows="5" bind:value={draft.text} spellcheck="false" autofocus
			></textarea>
		{/if}
		{#if error}
			<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={onclose}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn {kind === 'paste'
					? 'btn-primary'
					: kind === 'remove' && draft.deleteDisks
						? 'btn-danger'
						: 'btn-warn'}"
				disabled={busy}
				onclick={submit}>{t(`vms.submit.${kind}`)}</button
			>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: 0.875rem;
	}
	textarea {
		height: auto;
		padding: 10px 12px;
		font-size: 0.8125rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
