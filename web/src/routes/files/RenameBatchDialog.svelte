<script lang="ts">
	import { api, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	let {
		root,
		dir,
		names,
		onclose,
		ondone,
	}: { root: FileRoot; dir: string; names: string[]; onclose: () => void; ondone: () => void } =
		$props();
	type Change = {
		from: string;
		to: string;
		version: string;
		problem: '' | 'invalid' | 'exists' | 'duplicate' | 'protected';
	};
	let find = $state(''),
		replace = $state(''),
		prefix = $state(''),
		suffix = $state(''),
		number = $state(false),
		start = $state(1),
		digits = $state(3);
	let changes = $state<Change[] | null>(null),
		previewKey = $state(''),
		busy = $state(false),
		error = $state('');
	const key = $derived(JSON.stringify([find, replace, prefix, suffix, number, start, digits]));
	const valid = $derived(
		changes &&
			previewKey === key &&
			!changes.some((c) => c.problem) &&
			changes.some((c) => c.from !== c.to),
	);
	async function preview(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			changes = await api.post<Change[]>('/files/rename-preview', {
				root: root.id,
				dir,
				names,
				find,
				replace,
				prefix,
				suffix,
				number,
				start,
				digits,
			});
			previewKey = key;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	async function apply() {
		if (!valid) return;
		busy = true;
		error = '';
		try {
			const result = await api.post<{ done: number; error: string }>('/files/rename-batch', {
				root: root.id,
				changes,
			});
			if (result.error) {
				error = t('files.renamePartial', { count: result.done });
				changes = null;
			} else ondone();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('files.renameBatch')} {onclose} locked={busy} wide>
	<p>{t('files.renameBatchHint')}</p>
	<form onsubmit={preview}>
		<fieldset disabled={busy}>
			<label>{t('files.renameFind')}<input class="field" bind:value={find} /></label><label
				>{t('files.renameReplace')}<input class="field" bind:value={replace} /></label
			>
			<label>{t('files.renamePrefix')}<input class="field" bind:value={prefix} /></label><label
				>{t('files.renameSuffix')}<input class="field" bind:value={suffix} /></label
			>
			<label class="number"
				><input type="checkbox" bind:checked={number} />{t('files.renameNumber')}</label
			>
			{#if number}<label
					>{t('files.renameStart')}<input
						class="field"
						type="number"
						min="0"
						max="1000000000"
						bind:value={start}
						required
					/></label
				><label
					>{t('files.renameDigits')}<input
						class="field"
						type="number"
						min="1"
						max="9"
						bind:value={digits}
						required
					/></label
				>{/if}
		</fieldset>
		<button class="btn" disabled={busy}>{t('files.renamePreview')}</button>
	</form>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if changes && previewKey === key}<div class="results">
			<table>
				<thead
					><tr
						><th>{t('files.renameOld')}</th><th>{t('files.newName')}</th><th
							>{t('files.renameCheck')}</th
						></tr
					></thead
				><tbody>
					{#each changes as item (item.from)}<tr
							><td class="mono">{item.from}</td><td class="mono">{item.to}</td><td
								>{item.problem
									? t(`files.renameProblem.${item.problem}`)
									: item.from === item.to
										? t('files.renameUnchanged')
										: t('files.renameReady')}</td
							></tr
						>{/each}
				</tbody>
			</table>
		</div>{/if}
	<div class="dialog-actions">
		<button class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button><button
			class="btn btn-primary"
			disabled={busy || !valid}
			onclick={apply}>{t('files.renameApply')}</button
		>
	</div>
</Dialog>

<style>
	fieldset {
		border: 0;
		padding: 0;
		margin: 0 0 12px;
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.number {
		flex-direction: row;
		align-items: center;
	}
	.results {
		max-height: 45vh;
		overflow: auto;
		margin-top: 12px;
	}
	table {
		width: 100%;
		border-collapse: collapse;
	}
	td,
	th {
		padding: 8px;
		text-align: left;
		border-bottom: 1px solid var(--border);
		overflow-wrap: anywhere;
	}
	@media (max-width: 600px) {
		fieldset {
			grid-template-columns: 1fr;
		}
	}
</style>
