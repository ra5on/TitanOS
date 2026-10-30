<script lang="ts">
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	let {
		root,
		dir,
		onclose,
		oncreated,
	}: { root: string; dir: string; onclose: () => void; oncreated: (path: string) => void } =
		$props();
	let name = $state(''),
		extension = $state('txt'),
		busy = $state(false),
		error = $state('');
	async function create(event: SubmitEvent) {
		event.preventDefault();
		if (busy) return;
		const file = name.trim();
		if (!file || /[\\/\x00-\x1f]/.test(file) || file === '.' || file === '..') {
			error = t('files.invalidPath');
			return;
		}
		const filename = file.toLowerCase().endsWith('.' + extension) ? file : file + '.' + extension;
		const path = [dir, filename].filter(Boolean).join('/');
		busy = true;
		error = '';
		try {
			await api.post('/files/create', { root, path });
			oncreated(path);
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('files.newFile')} {onclose} locked={busy}>
	<form onsubmit={create}>
		<label
			>{t('files.name')}<input
				class="field"
				bind:value={name}
				required
				maxlength="240"
				disabled={busy}
			/></label
		>
		<label
			>{t('files.fileFormat')}<select class="field" bind:value={extension} disabled={busy}
				><option value="txt">Text (.txt)</option><option value="md">Markdown (.md)</option><option
					value="json">JSON (.json)</option
				></select
			></label
		>
		<p class="muted">{t('files.newFileHint')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={onclose}
				>{t('common.cancel')}</button
			><button class="btn btn-primary" disabled={busy}>{t('files.createAndEdit')}</button>
		</div>
	</form>
</Dialog>
