<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	// Photo view of one folder: a grid of thumbnails made by titand, ordered by
	// the date taken (EXIF) or by name, and a full-screen viewer with arrow keys
	// and swiping. Nothing is read recursively; originals stay where they are.
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t, i18n } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import {
		sortPhotos,
		swipeDirection,
		streamableVideo,
		thumbnailURL,
		type Photo,
	} from '#lib/file-actions.ts';
	import FileThumbnail from './FileThumbnail.svelte';
	let { root, dir, onclose }: { root: string; dir: string; onclose: () => void } = $props();
	let list = $state<Photo[] | null>(null),
		truncated = $state(false),
		error = $state('');
	let order = $state<'taken' | 'name'>('taken');
	let current = $state<number | null>(null);
	let viewer = $state<HTMLDivElement>();
	const photos = $derived(sortPhotos(list ?? [], order, i18n.locale));
	const shown = $derived(current === null ? null : photos[current]);
	const join = (name: string) => (dir ? dir + '/' + name : name);
	$effect(() => {
		const controller = new AbortController();
		list = null;
		error = '';
		api
			.get<{ entries: Photo[]; truncated: boolean }>(
				'/files/photos?' + new URLSearchParams({ root, path: dir }),
				controller.signal,
			)
			.then((result) => {
				list = result.entries;
				truncated = result.truncated;
			})
			.catch((err) => {
				if (!controller.signal.aborted) error = apiErrorText(err);
			});
		return () => controller.abort();
	});
	function step(by: number) {
		if (current === null || photos.length === 0) return;
		current = (current + by + photos.length) % photos.length;
	}
	function keyboard(event: KeyboardEvent) {
		if (current === null || event.altKey || event.ctrlKey || event.metaKey) return;
		if (event.key === 'ArrowLeft') {
			event.preventDefault();
			step(-1);
		}
		if (event.key === 'ArrowRight') {
			event.preventDefault();
			step(1);
		}
	}
	let start: { x: number; y: number } | null = null;
	function pointerdown(event: PointerEvent) {
		if (event.pointerType !== 'mouse') start = { x: event.clientX, y: event.clientY };
	}
	function pointerup(event: PointerEvent) {
		if (!start) return;
		const direction = swipeDirection(event.clientX - start.x, event.clientY - start.y);
		start = null;
		if (direction) step(-direction);
	}
	function fullscreen() {
		if (document.fullscreenElement) void document.exitFullscreen();
		else void viewer?.requestFullscreen?.().catch(() => {});
	}
	function closeViewer() {
		if (document.fullscreenElement) void document.exitFullscreen();
		current = null;
	}
</script>

<svelte:window onkeydown={keyboard} />
<Dialog title={t('files.photoView')} {onclose} wide maxWidth={1400}>
	<p class="mono">{dir || '/'}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>
	{:else if list === null}<p role="status">{t('files.previewLoading')}</p>
	{:else if photos.length === 0}<p class="muted">{t('files.photoEmpty')}</p>
	{:else}
		<div class="bar">
			<label
				>{t('files.photoOrder')}
				<select class="field" bind:value={order}
					><option value="taken">{t('files.photoByDate')}</option><option value="name"
						>{t('files.photoByName')}</option
					></select
				></label
			>
			<span class="muted">{t('files.photoCount', { count: photos.length })}</span>
		</div>
		{#if truncated}<p class="notice-warn">{t('files.photoLimited')}</p>{/if}
		<div class="grid">
			{#each photos as photo, index (photo.name)}
				<button type="button" class="cell" title={photo.name} onclick={() => (current = index)}>
					<FileThumbnail {root} path={join(photo.name)} modified={photo.modified} height={150} />
					<span class="caption">{formatDateTime(new Date(photo.taken || photo.modified))}</span>
				</button>
			{/each}
		</div>
	{/if}
	{#if shown && current !== null}
		<!-- The viewer is a dialog of its own: Escape closes it first, then the photo view. -->
		<Dialog title={shown.name} onclose={closeViewer} wide maxWidth={1400}>
			<div
				class="viewer"
				bind:this={viewer}
				role="group"
				aria-label={shown.name}
				onpointerdown={pointerdown}
				onpointerup={pointerup}
			>
				{#if shown.kind === 'video' && streamableVideo(shown.name)}
					<!-- svelte-ignore a11y_media_has_caption -->
					<video
						controls
						preload="metadata"
						poster={thumbnailURL(root, join(shown.name), 1280, shown.modified)}
						src={apiPath(
							'/files/download?' +
								new URLSearchParams({ root, path: join(shown.name), media: '1' }),
						)}
					></video>
				{:else}
					<img src={thumbnailURL(root, join(shown.name), 1280, shown.modified)} alt={shown.name} />
				{/if}
				<div class="controls">
					<button
						type="button"
						class="btn"
						onclick={() => step(-1)}
						aria-label={t('files.previousImage')}>‹</button
					>
					<span class="info"
						><strong>{shown.name}</strong> · {formatDateTime(
							new Date(shown.taken || shown.modified),
						)} · {t('files.photoPosition', { index: current + 1, count: photos.length })}</span
					>
					<button
						type="button"
						class="btn"
						onclick={() => step(1)}
						aria-label={t('files.nextImage')}>›</button
					>
					<a
						class="btn"
						href={apiPath(
							'/files/download?' + new URLSearchParams({ root, path: join(shown.name) }),
						)}>{t('files.download')}</a
					>
					{#if document.fullscreenEnabled}<button type="button" class="btn" onclick={fullscreen}
							>{t('files.photoFullscreen')}</button
						>{/if}
				</div>
			</div>
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={closeViewer}>{t('files.editClose')}</button>
			</div>
		</Dialog>
	{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('files.editClose')}</button>
	</div>
</Dialog>

<style>
	.bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 12px;
	}
	.bar label {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
		gap: 8px;
		max-height: 65vh;
		overflow: auto;
	}
	.cell {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 6px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--surface);
		color: var(--text);
		cursor: pointer;
	}
	.cell:focus-visible {
		outline: 2px solid var(--accent);
	}
	.caption {
		font-size: 0.75rem;
		color: var(--muted);
		text-align: center;
	}
	.viewer {
		display: flex;
		flex-direction: column;
		gap: 12px;
		background: var(--surface);
		touch-action: pan-y;
	}
	.viewer img,
	.viewer video {
		width: 100%;
		height: 70vh;
		object-fit: contain;
	}
	.viewer:fullscreen img,
	.viewer:fullscreen video {
		height: auto;
		flex: 1;
		min-height: 0;
	}
	.controls {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: center;
		gap: 8px;
	}
	.info {
		min-width: 0;
		overflow-wrap: anywhere;
		text-align: center;
	}
</style>
