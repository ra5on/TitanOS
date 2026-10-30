<script lang="ts">
	import { api, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	let {
		root,
		path,
		locked = false,
		onclose,
	}: { root: FileRoot; path: string; locked?: boolean; onclose: () => void } = $props();
	type Properties = {
		access?: { read: boolean; write: boolean; delete: boolean; reason: string };
		kind: string;
		size: number;
		modified: string;
		changed?: string;
		accessed?: string;
		permissions: string;
		uid: number;
		gid: number;
		sha256?: string;
	};
	let data = $state<Properties | null>(null),
		busy = $state(false),
		error = $state(''),
		expected = $state('');
	let controller: AbortController | undefined;
	async function load(checksum = false) {
		controller?.abort();
		const current = new AbortController();
		controller = current;
		busy = true;
		error = '';
		try {
			const result = await api.get<Properties>(
				'/files/properties?' +
					new URLSearchParams({ root: root.id, path, ...(checksum ? { checksum: 'sha256' } : {}) }),
				current.signal,
			);
			if (!current.signal.aborted) data = result;
		} catch (err) {
			if (!current.signal.aborted) error = apiErrorText(err);
		} finally {
			if (!current.signal.aborted) busy = false;
		}
	}
	$effect(() => {
		root.id;
		path;
		void load();
		return () => controller?.abort();
	});
</script>

<Dialog title={t('files.properties')} {onclose} wide>
	<dl>
		<dt>{t('files.path')}</dt>
		<dd class="mono">{root.name}/{path}</dd>
		{#if data}
			<dt>{t('files.fileType')}</dt>
			<dd>{data.kind === 'directory' ? t('files.folder') : data.kind}</dd>
			<dt>{t('files.size')}</dt>
			<dd>{data.kind === 'directory' ? t('files.folderSizeHint') : formatBytes(data.size)}</dd>
			<dt>{t('files.modified')}</dt>
			<dd>{formatDateTime(new Date(data.modified))}</dd>
			{#if data.changed}<dt>{t('files.metadataChanged')}</dt>
				<dd>{formatDateTime(new Date(data.changed))}</dd>{/if}
			{#if data.accessed}<dt>{t('files.lastAccess')}</dt>
				<dd>{formatDateTime(new Date(data.accessed))}</dd>{/if}
			<dt>{t('files.permissions')}</dt>
			<dd class="mono">{data.permissions} · UID {data.uid} · GID {data.gid}</dd>
		{/if}
	</dl>
	{#if data?.access}
		<section aria-label={t('files.effectiveRights')}>
			<h3>{t('files.effectiveRights')}</h3>
			<p>
				{t('files.rightRead')}: {t(data.access.read ? 'files.allowed' : 'files.denied')} · {t(
					'files.rightWrite',
				)}: {t(data.access.write && !locked ? 'files.allowed' : 'files.denied')} · {t(
					'files.rightDelete',
				)}: {t(data.access.delete && !locked ? 'files.allowed' : 'files.denied')}
			</p>
			{#if locked}<p class="notice-warn">
					{t('files.rightReason.usb')}
				</p>{:else if data.access.reason}<p class="notice-warn">
					{t(('files.rightReason.' + data.access.reason) as 'files.rightReason.share')}
				</p>{/if}
			<p class="muted">{t('files.rightsHint')}</p>
		</section>
	{/if}
	{#if data && data.kind !== 'directory' && data.kind !== 'symlink'}
		<button class="btn" disabled={busy} onclick={() => load(true)}
			>{t('files.checksumCalculate')}</button
		>
		<p class="muted">{t('files.checksumHint')}</p>
		{#if data.sha256}<label class="field-row"
				><span>SHA-256</span><textarea class="field mono" readonly value={data.sha256}
				></textarea></label
			>
			<label class="field-row"
				><span>{t('files.checksumCompare')}</span><input
					class="field mono"
					bind:value={expected}
				/></label
			>
			{#if expected.trim()}<p
					role="status"
					class={expected.trim().toLowerCase() === data.sha256 ? 'notice' : 'notice-warn'}
				>
					{t(
						expected.trim().toLowerCase() === data.sha256
							? 'files.checksumMatch'
							: 'files.checksumMismatch',
					)}
				</p>{/if}
		{/if}{/if}
	{#if busy}<p role="status">{t('files.previewLoading')}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button class="btn" onclick={onclose}>{t('files.editClose')}</button>
	</div>
</Dialog>

<style>
	dl {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: 12px;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	textarea {
		width: 100%;
		min-height: 5rem;
	}
	p {
		font-size: 0.875rem;
	}
</style>
