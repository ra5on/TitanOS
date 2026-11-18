<script lang="ts">
	// 1.11 (step 16): "Importieren (.vcf)" into the address book "Kontakte":
	// a file, what to do with contacts it already has (keep or replace,
	// like T-Kalender's import), then a summary with what was not taken.
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { apiErrorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	import { importVCF, type ImportResult } from '../contacts-api.ts';

	type Props = { onclose: () => void; onchanged: () => void };
	let { onclose, onchanged }: Props = $props();

	let file = $state<File | null>(null);
	let onDuplicate = $state<'skip' | 'replace'>('skip');
	let busy = $state(false);
	let error = $state('');
	let result = $state<ImportResult | null>(null);

	async function run() {
		if (!file) return;
		busy = true;
		error = '';
		result = null;
		try {
			result = await importVCF(file, onDuplicate);
			onchanged();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('contacts.imp.title')} locked={busy} {onclose}>
	<div class="imp" data-contact-import>
		<p class="lead">{t('contacts.imp.text')}</p>
		<input
			class="field"
			type="file"
			accept=".vcf,text/vcard,text/x-vcard"
			aria-label={t('contacts.imp.file')}
			onchange={(e) => {
				file = e.currentTarget.files?.[0] ?? null;
				result = null;
			}}
		/>
		<div class="dup">
			<span class="muted">{t('contacts.imp.onDuplicate')}</span>
			<Segments
				compact
				label={t('contacts.imp.onDuplicate')}
				bind:value={onDuplicate}
				options={[
					{ value: 'skip', label: t('calendar.cal.skip') },
					{ value: 'replace', label: t('calendar.cal.replace') },
				]}
			/>
		</div>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if result}
			<div class="result" role="status" data-import-result>
				<p>
					{t('contacts.imp.result', {
						added: result.added,
						updated: result.updated,
						skipped: result.skipped,
					})}
				</p>
				{#if result.failed.length}
					<p>{t('calendar.cal.failed', { n: result.failed.length })}</p>
					<ul>
						{#each result.failed as f, i (i)}
							<li>
								<span class="who">{f.name || f.uid || '–'}</span>:
								{tOr(`contacts.reason.${f.reason}`, t('contacts.reason.invalid'))}
							</li>
						{/each}
					</ul>
				{/if}
			</div>
		{/if}
	</div>
	<div class="dialog-actions">
		<button type="button" class="btn" disabled={busy} onclick={onclose}
			>{result ? t('common.close') : t('common.cancel')}</button
		>
		<button type="button" class="btn btn-primary" disabled={busy || !file} onclick={run}
			><Icon name="upload" size={15} />{t('calendar.cal.importRun')}</button
		>
	</div>
</Dialog>

<style>
	.imp {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-3);
	}
	.imp p {
		margin: 0;
		overflow-wrap: break-word;
	}
	.imp .field {
		max-width: 100%;
	}
	.dup {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.result {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.result ul {
		margin: 0;
		padding-left: var(--space-4);
	}
	.who {
		overflow-wrap: anywhere;
	}
</style>
