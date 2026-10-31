<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { onDestroy } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { api, type VMStatus } from '#lib/api.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import { mediaActive, verifyMediaUpload, type MediaJob } from '#lib/media.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	let jobs = $state<MediaJob[]>([]),
		loading = $state(true),
		error = $state(''),
		busy = $state(false),
		adding = $state(false),
		source = $state<'url' | 'upload'>('url'),
		url = $state(''),
		volume = $state(''),
		name = $state(''),
		sha256 = $state(''),
		compression = $state('auto'),
		picked = $state<File | null>(null),
		chunk = $state(8 << 20),
		uploading = $state(''),
		verifyProgress = $state('');
	const volumes = $derived((storage.status?.volumes ?? []).filter((v) => v.mounted));
	const files = new Map<string, File>();
	const paused = new Set<string>();
	let confirm = $state<{ job: MediaJob; action: 'cancel' | 'remove' } | null>(null),
		confirmBusy = $state(false),
		confirmError = $state('');
	// The list could not be refreshed; the last state stays visible.
	let loadError = $state('');
	// Jobs with a request on its way, so a second click does not send it again.
	const controlling = new SvelteSet<string>();
	// Media already on the volumes and the VMs that use them.
	let media = $state<{ path: string; sizeBytes: number }[] | null>(null),
		mediaError = $state(''),
		vms = $state<VMStatus['vms']>([]);
	const usedBy = (path: string) =>
		vms.filter((vm) => vm.iso === path || vm.virtioISO === path).map((vm) => vmLabel(vm));
	async function loadMedia() {
		try {
			const [found, status] = await Promise.all([
				api.get<{ media: { path: string; sizeBytes: number }[] }>('/vms/media'),
				api.get<VMStatus>('/vms'),
			]);
			media = found.media ?? [];
			vms = status.vms;
			mediaError = '';
		} catch (e) {
			mediaError = apiErrorText(e);
		}
	}
	const phases: Record<string, MessageKey> = {
		uploading: 'media.phase.uploading',
		downloading: 'media.phase.downloading',
		extracting: 'media.phase.extracting',
		verifying: 'media.phase.verifying',
		publishing: 'media.phase.publishing',
		paused: 'media.phase.paused',
		pausing: 'media.phase.pausing',
		failed: 'media.phase.failed',
		review: 'media.phase.review',
		cancelled: 'media.phase.cancelled',
		done: 'media.phase.done',
	};
	async function load() {
		try {
			const res = await api.get<{ jobs: MediaJob[]; chunkBytes: number }>('/vms/imports');
			const finished = (res.jobs ?? []).some(
				(job) =>
					job.phase === 'done' && jobs.some((old) => old.id === job.id && old.phase !== 'done'),
			);
			jobs = res.jobs ?? [];
			chunk = res.chunkBytes || 8 << 20;
			loadError = '';
			// A newly published medium belongs into the list below.
			if (finished) void loadMedia();
		} catch (e) {
			loadError = apiErrorText(e);
		} finally {
			loading = false;
		}
	}
	// Quick while something is moving, slow when every job rests.
	const active = $derived(
		!!uploading || jobs.some((job) => mediaActive(job) || job.phase === 'uploading'),
	);
	$effect(() => {
		void load();
		void loadMedia();
	});
	$effect(() => {
		return poll(load, { interval: active ? 1500 : 15000, immediate: false });
	});
	function outputName(source: string) {
		let result = source.split('/').at(-1) ?? '';
		result = result.replace(/\.(gz|xz|zip)$/i, '');
		if (!/\.(iso|qcow2|raw|img|vmdk|vhdx|vdi)$/i.test(result)) result += '.iso';
		return result;
	}
	function add() {
		source = 'url';
		url = '';
		name = '';
		sha256 = '';
		compression = 'auto';
		picked = null;
		volume = volumes[0]?.name ?? '';
		error = '';
		adding = true;
	}
	onDestroy(() => {
		if (uploading) {
			paused.add(uploading);
			void api.post(`/vms/imports/${uploading}/pause`, {}).catch(() => {});
		}
	});
	async function send(job: MediaJob, file: File) {
		if (file.name !== job.uploadName || file.size !== job.uploadSize) {
			error = t('media.originalFile');
			return;
		}
		files.set(job.id, file);
		paused.delete(job.id);
		uploading = job.id;
		try {
			if (job.bytes) {
				verifyProgress = t('media.verifyPrefix');
				await verifyMediaUpload(
					job,
					file,
					chunk,
					(done) =>
						(verifyProgress = `${t('media.verifyPrefix')} ${formatBytes(done)} / ${formatBytes(job.bytes)}`),
				);
				verifyProgress = '';
			}
			if (['paused', 'failed'].includes(job.phase))
				await api.post(`/vms/imports/${job.id}/resume`, {});
			for (let at = job.bytes; at < file.size; at += chunk) {
				if (paused.has(job.id)) break;
				await api.put(
					`/vms/imports/${job.id}/data?offset=${at}`,
					file.slice(at, Math.min(file.size, at + chunk)),
				);
				await load();
			}
			if (!paused.has(job.id)) await api.post(`/vms/imports/${job.id}/finish`, {});
		} catch (e) {
			error = apiErrorText(e, { detail: true });
		} finally {
			uploading = '';
			verifyProgress = '';
			await load();
		}
	}
	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const req = {
				volume,
				name,
				sha256,
				compression,
				...(source === 'url' ? { url } : { uploadName: picked?.name, uploadSize: picked?.size }),
			};
			const job = await api.post<MediaJob>('/vms/imports', req);
			adding = false;
			if (source === 'upload' && picked) void send(job, picked);
			await load();
		} catch (e) {
			error = apiErrorText(e, { detail: true });
		} finally {
			busy = false;
		}
	}
	async function control(job: MediaJob, action: string) {
		if (controlling.has(job.id)) return;
		controlling.add(job.id);
		error = '';
		if (action === 'pause' || action === 'cancel') paused.add(job.id);
		try {
			await api.post(`/vms/imports/${job.id}/${action}`, {});
			await load();
		} catch (e) {
			error = apiErrorText(e, { detail: true });
		} finally {
			controlling.delete(job.id);
		}
	}
	// From the confirmation: an error stays in the dialog, which stays open.
	async function confirmed() {
		if (!confirm || confirmBusy) return;
		const { job, action } = confirm;
		confirmBusy = true;
		confirmError = '';
		controlling.add(job.id);
		if (action === 'cancel') paused.add(job.id);
		try {
			await api.post(`/vms/imports/${job.id}/${action}`, {});
			confirm = null;
			await load();
		} catch (e) {
			confirmError = apiErrorText(e, { detail: true });
		} finally {
			confirmBusy = false;
			controlling.delete(job.id);
		}
	}
	function ask(job: MediaJob, action: 'cancel' | 'remove') {
		confirmError = '';
		confirm = { job, action };
	}
</script>

<PageHead title={t('media.title')} parent={{ href: '/vms', label: t('nav.vms') }}
	><p class="muted">{t('media.intro')}</p>
	{#snippet actions()}<button class="btn btn-primary" disabled={!volumes.length} onclick={add}
			><Icon name="plus" />{t('media.add')}</button
		>{/snippet}</PageHead
>
{#if error && !adding}<div class="notice-error" role="alert">{error}</div>{/if}
{#if loadError}<div class="notice-warn stale" role="alert">
		<span>{loadError}</span><button class="btn" onclick={load}>{t('common.retry')}</button>
	</div>{/if}{#if verifyProgress}<p role="status">
		{verifyProgress}
	</p>{/if}
{#if loading}<p class="loading" role="status">
		{t('common.loading')}
	</p>{:else if !jobs.length}<EmptyState
		title={t('media.empty')}
	/>{:else}{#each jobs as job (job.id)}<section class="tile job">
			<div class="row">
				<strong>{job.name}</strong><span class="chip"
					>{t(phases[job.phase] ?? 'media.phase.review')}</span
				>
			</div>
			<p class="muted">
				{job.volume} · {formatBytes(job.bytes)}{job.total
					? ` / ${formatBytes(job.total)}`
					: ''}{job.written ? ` · ${t('media.unpacked')}: ${formatBytes(job.written)}` : ''}
			</p>
			{#if job.total > 0 && ['uploading', 'downloading', 'paused'].includes(job.phase)}<progress
					value={job.bytes}
					max={job.total}
					aria-label={job.name}
				></progress>{/if}{#if job.error}<p
					class={job.phase === 'review' ? 'notice-warn' : 'notice-error'}
					role="status"
				>
					{job.error}
				</p>{/if}{#if job.output}<p class="mono path">{job.output}</p>
				<details>
					<summary>SHA-256</summary>
					<p class="mono path">{job.sha256}</p>
				</details>{/if}
			<div class="actions">
				{#if (mediaActive(job) && job.phase !== 'publishing' && job.phase !== 'pausing') || uploading === job.id}<button
						class="btn"
						disabled={controlling.has(job.id)}
						onclick={() => control(job, 'pause')}><Icon name="pause" />{t('media.pause')}</button
					>{/if}
				{#if job.phase === 'review'}<button
						class="btn"
						disabled={controlling.has(job.id)}
						onclick={() => control(job, 'review')}>{t('media.review')}</button
					>{/if}{#if job.url && job.phase === 'paused'}<button
						class="btn btn-primary"
						disabled={controlling.has(job.id)}
						onclick={() => control(job, 'resume')}>{t('media.resume')}</button
					>{/if}{#if job.url && job.phase === 'failed'}<button
						class="btn"
						disabled={controlling.has(job.id)}
						onclick={() => control(job, 'retry')}>{t('common.retry')}</button
					>{/if}
				{#if !job.url && !job.sourceReady && ['uploading', 'paused', 'failed'].includes(job.phase) && uploading !== job.id}<label
						class="resume"
						><span>{t('media.reselect')}</span><input
							class="field"
							type="file"
							onchange={(e) => {
								const file = e.currentTarget.files?.[0];
								if (file) void send(job, file);
							}}
						/></label
					>{/if}
				{#if !job.url && job.sourceReady && ['paused', 'failed'].includes(job.phase)}<button
						class="btn"
						disabled={controlling.has(job.id)}
						onclick={() => control(job, 'resume')}>{t('media.resume')}</button
					>{/if}
				{#if !['done', 'cancelled', 'review', 'publishing', 'pausing'].includes(job.phase)}<button
						class="btn"
						disabled={controlling.has(job.id)}
						onclick={() => ask(job, 'cancel')}>{t('media.cancelImport')}</button
					>{/if}
				{#if !mediaActive(job) && uploading !== job.id}<button
						class="btn"
						disabled={controlling.has(job.id)}
						onclick={() => ask(job, 'remove')}>{t('media.remove')}</button
					>{/if}
			</div>
		</section>{/each}{/if}
<p class="muted">{t('media.background')}</p>
<section class="tile files" aria-labelledby="media-files">
	<div class="row">
		<h2 class="tile-title" id="media-files">{t('media.files')}</h2>
		<button class="btn" onclick={loadMedia}><Icon name="refresh" />{t('media.filesRefresh')}</button
		>
	</div>
	{#if mediaError}<div class="notice-error" role="alert">
			<span>{mediaError}</span>
			<button class="btn" onclick={loadMedia}>{t('common.retry')}</button>
		</div>{/if}
	{#if media === null && !mediaError}<p class="muted loading" role="status">
			{t('common.loading')}
		</p>
	{:else if media && media.length === 0}<p class="muted">{t('media.filesEmpty')}</p>
	{:else if media}<ul class="file-list">
			{#each media as entry (entry.path)}
				{@const users = usedBy(entry.path)}
				<li>
					<span class="mono path">{entry.path}</span>
					<span class="muted mono">{formatBytes(entry.sizeBytes)}</span>
					<span class="muted"
						>{users.length ? t('media.usedBy', { vms: users.join(', ') }) : t('media.unused')}</span
					>
				</li>
			{/each}
		</ul>{/if}
</section>
{#if adding}<Dialog title={t('media.add')} wide locked={busy} onclose={() => (adding = false)}
		><form onsubmit={create}>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="tabs" role="tablist" aria-label={t('media.source')}>
				<button
					type="button"
					class="btn"
					role="tab"
					aria-selected={source === 'url'}
					onclick={() => (source = 'url')}>{t('media.url')}</button
				><button
					type="button"
					class="btn"
					role="tab"
					aria-selected={source === 'upload'}
					onclick={() => (source = 'upload')}>{t('media.upload')}</button
				>
			</div>
			{#if source === 'url'}<label
					><span>{t('media.url')}</span><input
						class="field"
						type="url"
						bind:value={url}
						required
						onblur={() => {
							if (!name) {
								try {
									name = outputName(new URL(url).pathname);
								} catch {}
							}
						}}
					/></label
				>{:else}<label
					><span>{t('media.upload')}</span><input
						class="field"
						type="file"
						required
						accept=".iso,.qcow2,.raw,.img,.vmdk,.vhdx,.vdi,.gz,.xz,.zip"
						onchange={(e) => {
							picked = e.currentTarget.files?.[0] ?? null;
							if (picked) name = outputName(picked.name);
						}}
					/></label
				>{/if}<label
				><span>{t('media.volume')}</span><select class="field" bind:value={volume}
					>{#each volumes as v}<option value={v.name}
							>{v.name} · {formatBytes(v.sizeBytes - v.usedBytes)}</option
						>{/each}</select
				></label
			><label
				><span>{t('media.filename')}</span><input
					class="field mono"
					bind:value={name}
					required
					maxlength="120"
				/></label
			>
			<details>
				<summary>{t('media.options')}</summary><label
					><span>{t('media.sourceHash')}</span><input
						class="field mono"
						bind:value={sha256}
						pattern="[a-fA-F0-9]{64}"
					/></label
				><label
					><span>{t('media.compression')}</span><select class="field" bind:value={compression}
						><option value="auto">{t('netx.automatic')}</option><option value="none"
							>{t('media.none')}</option
						><option value="gz">gzip</option><option value="xz">xz</option><option value="zip"
							>ZIP</option
						></select
					></label
				>
			</details>
			<p class="muted">{t('media.archiveHint')}</p>
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (adding = false)}
					>{t('common.cancel')}</button
				><button
					class="btn btn-primary"
					disabled={busy || !volume || (source === 'upload' && !picked)}>{t('media.start')}</button
				>
			</div>
		</form></Dialog
	>{/if}
{#if confirm}<Dialog
		title={t(confirm.action === 'remove' ? 'media.removeTitle' : 'media.cancelTitle')}
		locked={confirmBusy}
		onclose={() => (confirm = null)}
		><p>
			{t(confirm.action === 'remove' ? 'media.removeHint' : 'media.cancelHint', {
				name: confirm.job.name,
			})}
		</p>
		{#if confirmError}<p class="notice-error" role="alert">{confirmError}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={confirmBusy} onclick={() => (confirm = null)}
				>{t(confirm.action === 'remove' ? 'media.keepJob' : 'media.keepImport')}</button
			><button class="btn btn-danger" disabled={confirmBusy} onclick={confirmed}
				>{t(confirm.action === 'remove' ? 'media.remove' : 'media.cancelImport')}</button
			>
		</div></Dialog
	>{/if}

<style>
	.job {
		padding: var(--tile-pad);
		display: grid;
		gap: var(--gap);
	}
	.row,
	.actions,
	.tabs,
	.dialog-actions {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.row {
		justify-content: space-between;
	}
	.row strong,
	.path {
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.path {
		font-size: var(--text-base);
	}
	form,
	label {
		display: grid;
		gap: var(--gap);
	}
	progress {
		width: 100%;
		accent-color: var(--accent);
	}
	.tabs [aria-selected='true'] {
		color: var(--accent);
		border-bottom: 2px solid var(--accent);
	}
	.dialog-actions {
		justify-content: flex-end;
		margin-top: var(--gap);
	}
	.resume {
		max-width: 100%;
		font-size: var(--text-base);
	}
	details summary {
		cursor: pointer;
		min-height: 44px;
		display: flex;
		align-items: center;
	}
	details label {
		margin: var(--gap) 0;
	}
	.stale,
	.notice-error:has(> .btn) {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}
	.files {
		padding: var(--tile-pad);
		display: grid;
		gap: var(--gap);
		margin-top: var(--gap);
	}
	.file-list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
	}
	.file-list li {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto minmax(8rem, auto);
		gap: 4px 16px;
		align-items: center;
		padding: 8px 0;
		border-top: 1px solid var(--panel-border);
		font-size: var(--text-base);
	}
	@media (max-width: 760px) {
		.file-list li {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
