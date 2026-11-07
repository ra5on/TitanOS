<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	import { apiFetch } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { previewKind, thumbnailURL } from '#lib/file-actions.ts';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import FileIcon from './FileIcon.svelte';
	// PLAN 70, quick look: Space opens it on any file and closes it again, the
	// arrows go to the previous/next file of the folder, the head names size
	// and date with "Öffnen" (standard app) and "Herunterladen". Office files
	// show the picture they carry; files without a preview get an info card.
	let {
		root,
		path,
		size,
		modified,
		onclose,
		onprevious,
		onnext,
		onopen,
	}: {
		root: string;
		path: string;
		size: number;
		modified?: string;
		onclose: () => void;
		onprevious?: () => void;
		onnext?: () => void;
		/** Opens the file in its standard app (only when it has one). */
		onopen?: () => void;
	} = $props();
	const name = $derived(path.split('/').pop() ?? path);
	const office = $derived(/\.(docx|xlsx|pptx|odt|ods|odp)$/i.test(name));
	let officeFailed = $state(false);
	$effect(() => {
		void path;
		officeFailed = false;
	});
	let text = $state(''),
		url = $state(''),
		failed = $state(false),
		loading = $state(true);
	const stream = $derived(
		apiPath('/files/download?' + new URLSearchParams({ root, path, media: '1' })),
	);
	function keyboard(event: KeyboardEvent) {
		if (event.altKey || event.ctrlKey || event.metaKey || event.defaultPrevented) return;
		// Keys inside a player, a button or a text belong to them.
		const target = event.target instanceof Element ? event.target : null;
		if (target?.closest('video, audio, input, textarea, select, object')) return;
		if ((event.key === 'ArrowLeft' || event.key === 'ArrowUp') && onprevious) {
			event.preventDefault();
			onprevious();
		} else if ((event.key === 'ArrowRight' || event.key === 'ArrowDown') && onnext) {
			event.preventDefault();
			onnext();
		} else if (event.key === ' ' && !target?.closest('button, a')) {
			event.preventDefault();
			onclose();
		}
	}
	const kind = $derived(previewKind(path, size));
	$effect(() => {
		loading = true;
		failed = false;
		url = '';
		text = '';
		const controller = new AbortController();
		let objectURL = '';
		const limit = kind === 'image' || kind === 'pdf' ? 20 * 1024 * 1024 : 512 * 1024;
		async function read() {
			try {
				if (kind === 'audio' || kind === 'video') {
					loading = false;
					return;
				}
				if (!kind) {
					loading = false;
					return;
				}
				const res = await apiFetch('/files/download?' + new URLSearchParams({ root, path }), {
					signal: controller.signal,
				});
				if (!res.ok || Number(res.headers.get('content-length')) > limit || !res.body)
					throw new Error('unavailable');
				const reader = res.body.getReader(),
					chunks: Uint8Array[] = [];
				let length = 0;
				for (;;) {
					const { done, value } = await reader.read();
					if (done) break;
					length += value.byteLength;
					if (length > limit) {
						await reader.cancel();
						throw new Error('too large');
					}
					chunks.push(value);
				}
				const bytes = new Uint8Array(length);
				let offset = 0;
				for (const chunk of chunks) {
					bytes.set(chunk, offset);
					offset += chunk.length;
				}
				if (controller.signal.aborted) return;
				if (kind === 'text') text = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
				else {
					const ext = path.split('.').pop()!.toLowerCase();
					objectURL = URL.createObjectURL(
						new Blob([bytes], {
							type: kind === 'pdf' ? 'application/pdf' : 'image/' + (ext === 'jpg' ? 'jpeg' : ext),
						}),
					);
					url = objectURL;
				}
			} catch {
				if (!controller.signal.aborted) failed = true;
			} finally {
				if (!controller.signal.aborted) loading = false;
			}
		}
		read();
		return () => {
			controller.abort();
			if (objectURL) URL.revokeObjectURL(objectURL);
		};
	});
</script>

<svelte:window onkeydown={keyboard} />
<Dialog title={t('files.previewTitle', { name })} {onclose} wide>
	<div class="ql-head">
		<span class="ql-meta"
			>{formatBytes(size) + (modified ? ' · ' + formatDateTime(new Date(modified)) : '')}</span
		>
		<span class="ql-actions">
			{#if onprevious || onnext}<button
					type="button"
					class="btn"
					disabled={!onprevious}
					onclick={onprevious}
					aria-label={t('files.quickPrevious')}>‹</button
				><button
					type="button"
					class="btn"
					disabled={!onnext}
					onclick={onnext}
					aria-label={t('files.quickNext')}>›</button
				>{/if}
			{#if onopen}<button type="button" class="btn btn-primary" onclick={onopen}
					>{t('files.quickOpen')}</button
				>{/if}
			<a class="btn" href={apiPath('/files/download?' + new URLSearchParams({ root, path }))}
				>{t('files.download')}</a
			>
		</span>
	</div>
	{#if loading}<p role="status">{t('files.previewLoading')}</p>{:else if failed}<p
			class="notice-error"
			role="alert"
		>
			{t('files.previewFailed')}
		</p>{:else if kind === 'image'}<img
			src={url}
			alt={path}
			onerror={(e) => {
				// A picture of the previous file (its address already freed) does not count.
				if ((e.currentTarget as HTMLImageElement).src === url) failed = true;
			}}
		/>{:else if kind === 'audio'}<audio
			controls
			preload="metadata"
			src={stream}
			onerror={() => (failed = true)}
		></audio>
		<p class="muted">
			{t('files.mediaHint')}
		</p>{:else if kind === 'video'}<!-- Native media controls provide keyboard access; no invented subtitle track. -->
		<!-- svelte-ignore a11y_media_has_caption -->
		<video controls preload="metadata" src={stream} onerror={() => (failed = true)}></video>
		<p class="muted">{t('files.mediaHint')}</p>{:else if kind === 'pdf'}<object
			data={url}
			type="application/pdf"
			aria-label={path}><p>{t('files.pdfFallback')}</p></object
		>{:else if kind === 'text'}<pre>{text}</pre>{:else}<div class="ql-info">
			{#if office && !officeFailed}<img
					class="ql-page"
					src={thumbnailURL(root, path, 640, modified)}
					alt={t('files.quickPage', { name })}
					onerror={() => (officeFailed = true)}
				/>{:else}<span class="ql-icon"><FileIcon {name} dir={false} size={72} /></span>{/if}
			<p class="muted">
				{office
					? officeFailed
						? t('files.quickOfficeNone')
						: t('files.quickOfficePage')
					: t('files.quickNone')}
			</p>
		</div>{/if}
	<p class="muted ql-keys">{t('files.quickKeys')}</p>
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('files.editClose')}</button>
	</div>
</Dialog>

<style>
	video {
		width: 100%;
		max-height: 65vh;
	}
	audio {
		width: 100%;
	}
	object {
		display: block;
		width: 100%;
		height: 65vh;
		border: 0;
	}
	.ql-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin-bottom: var(--space-3);
	}
	.ql-meta {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.ql-actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.ql-info {
		display: grid;
		justify-items: center;
		gap: var(--space-3);
		padding: var(--space-4) 0;
		text-align: center;
	}
	.ql-page {
		max-height: 60vh;
		border: 1px solid var(--border);
		border-radius: var(--radius-small, 6px);
		background: #fff;
	}
	.ql-keys {
		margin: var(--space-3) 0 0;
		font-size: var(--text-sm);
	}
	img {
		max-width: 100%;
		max-height: 65vh;
		object-fit: contain;
	}
	pre {
		max-height: 65vh;
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-family: var(--font-mono, monospace);
		font-size: var(--text-md);
	}
</style>
