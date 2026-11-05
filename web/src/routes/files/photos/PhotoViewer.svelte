<script lang="ts">
	// One photo full screen (PLAN 52): arrows and swiping, beside it the
	// description of the KI-Agent and what the file tells (date, place,
	// camera, folder, size), with "Zu Album", "Im Ordner zeigen" and "Mit
	// KI-Agent". Pictures come as large thumbnails, videos as a stream.
	import { goto } from '$app/navigation';
	import { apiPath } from '#lib/cluster-node.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { openAgent } from '#lib/agent-app.ts';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { streamableVideo, swipeDirection, thumbnailURL } from '#lib/file-actions.ts';
	import { photoDate, photoName, placeText, type PhotoAlbum, type PhotoItem } from './photos.ts';

	let {
		items,
		index = $bindable(),
		albums = [],
		onalbum,
		onfolder,
		onclose,
	}: {
		items: PhotoItem[];
		index: number;
		albums?: PhotoAlbum[];
		/** Adds the photo to an album (id "new" with a name makes one). */
		onalbum: (p: PhotoItem, id: string, name?: string) => void;
		onfolder: (root: string, dir: string) => void;
		onclose: () => void;
	} = $props();

	const p = $derived(items[index]);
	let picking = $state(false);
	let newName = $state('');
	let touch: { x: number; y: number } | null = null;
	function step(by: number) {
		if (!items.length) return;
		index = (index + by + items.length) % items.length;
		picking = false;
	}
	function key(e: KeyboardEvent) {
		if (e.key === 'Escape') onclose();
		if (e.key === 'ArrowLeft') step(-1);
		if (e.key === 'ArrowRight') step(1);
	}
	const dir = $derived(p ? p.path.split('/').slice(0, -1).join('/') : '');
	const root = $derived(p ? p.root.replace(/^share\//, '') : '');
	const ownAlbums = $derived(albums.filter((a) => !a.owner));
</script>

<svelte:window onkeydown={key} />

{#if p}
	<div class="viewer" role="dialog" aria-modal="true" aria-label={photoName(p)}>
		<div
			class="stage"
			role="presentation"
			ontouchstart={(e) => (touch = { x: e.touches[0].clientX, y: e.touches[0].clientY })}
			ontouchend={(e) => {
				if (!touch) return;
				const d = swipeDirection(
					e.changedTouches[0].clientX - touch.x,
					e.changedTouches[0].clientY - touch.y,
				);
				touch = null;
				if (d) step(-d);
			}}
		>
			{#if p.kind === 'video' && streamableVideo(p.path)}
				<!-- svelte-ignore a11y_media_has_caption -->
				<video
					controls
					preload="metadata"
					poster={thumbnailURL(p.root, p.path, 1600, p.modified)}
					src={apiPath(
						'/files/download?' + new URLSearchParams({ root: p.root, path: p.path, media: '1' }),
					)}
				></video>
			{:else}
				<img src={thumbnailURL(p.root, p.path, 1600, p.modified)} alt={p.desc ?? photoName(p)} />
			{/if}
			<button type="button" class="nav prev" aria-label={t('photos.prev')} onclick={() => step(-1)}
				><Icon name="chevronLeft" size={26} /></button
			>
			<button type="button" class="nav next" aria-label={t('photos.next')} onclick={() => step(1)}
				><Icon name="chevronRight" size={26} /></button
			>
			<button type="button" class="close" aria-label={t('common.close')} onclick={onclose}
				><Icon name="close" size={22} /></button
			>
		</div>
		<aside>
			<h2>{photoName(p)}</h2>
			<span class="muted">{index + 1} / {items.length}</span>
			{#if p.desc}
				<div class="ai">
					<strong><Icon name="sparkle" size={14} /> {t('photos.aiDescription')}</strong>
					<p>{p.desc}</p>
				</div>
			{:else if p.kind === 'image'}
				<p class="muted small">{p.failed ? t('photos.aiFailed') : t('photos.aiNone')}</p>
			{/if}
			<dl>
				<dt>{p.taken ? t('photos.taken') : t('photos.changed')}</dt>
				<dd>{formatDateTime(photoDate(p))}</dd>
				{#if p.gps && p.lat !== undefined && p.lon !== undefined}
					<dt>{t('photos.place')}</dt>
					<dd>{placeText(p.lat, p.lon, i18n.locale)}</dd>
				{/if}
				{#if p.camera}<dt>{t('photos.camera')}</dt>
					<dd>{p.camera}</dd>{/if}
				<dt>{t('photos.folder')}</dt>
				<dd>{root}{dir ? ' › ' + dir.replaceAll('/', ' › ') : ''}</dd>
				<dt>{t('photos.size')}</dt>
				<dd>{formatBytes(p.size)}</dd>
			</dl>
			<div class="acts">
				<button type="button" class="btn" onclick={() => (picking = !picking)}
					><Icon name="plus" size={15} /> {t('photos.toAlbum')}</button
				>
				<button type="button" class="btn" onclick={() => onfolder(p.root, dir)}
					><Icon name="folder" size={15} /> {t('photos.inFolder')}</button
				>
				<button
					type="button"
					class="btn"
					onclick={() =>
						openAgent(
							'/agent?' +
								new URLSearchParams({ attach: `${p.root}:${p.path}`, ask: t('photos.askAgent') }),
							goto,
						)}><Icon name="sparkle" size={15} /> {t('photos.withAgent')}</button
				>
			</div>
			{#if picking}
				<div class="pick">
					{#each ownAlbums as a (a.id)}
						<button
							type="button"
							class="btn"
							onclick={() => {
								onalbum(p, a.id);
								picking = false;
							}}>{a.name}</button
						>
					{/each}
					<form
						class="new"
						onsubmit={(e) => {
							e.preventDefault();
							if (!newName.trim()) return;
							onalbum(p, 'new', newName.trim());
							newName = '';
							picking = false;
						}}
					>
						<input
							class="field"
							bind:value={newName}
							maxlength="80"
							placeholder={t('photos.newAlbum')}
							aria-label={t('photos.newAlbum')}
						/>
						<button type="submit" class="btn btn-primary">{t('photos.create')}</button>
					</form>
				</div>
			{/if}
		</aside>
	</div>
{/if}

<style>
	.viewer {
		position: fixed;
		inset: 0;
		z-index: 80;
		display: grid;
		grid-template-columns: minmax(0, 1fr) 360px;
		background: rgb(0 0 0 / 0.94);
	}
	.stage {
		position: relative;
		display: grid;
		place-items: center;
		min-height: 0;
	}
	.stage img,
	.stage video {
		max-width: 94%;
		max-height: 90dvh;
		border-radius: 8px;
		object-fit: contain;
	}
	.nav,
	.close {
		position: absolute;
		border: 0;
		border-radius: 50%;
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		background: rgb(255 255 255 / 0.12);
		color: #fff;
		cursor: pointer;
	}
	.prev {
		left: 12px;
		top: 50%;
	}
	.next {
		right: 12px;
		top: 50%;
	}
	.close {
		top: 12px;
		right: 12px;
	}
	aside {
		background: color-mix(in srgb, var(--surface) 94%, transparent);
		color: var(--text);
		padding: 18px;
		display: flex;
		flex-direction: column;
		gap: 10px;
		overflow-y: auto;
	}
	h2 {
		margin: 0;
		font-size: var(--text-md);
		overflow-wrap: anywhere;
	}
	.ai {
		padding: 10px 12px;
		border-radius: 12px;
		border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
		background: color-mix(in srgb, var(--accent) 10%, transparent);
	}
	.ai strong {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: var(--text-sm);
	}
	.ai p {
		margin: 6px 0 0;
		white-space: pre-wrap;
		line-height: 1.5;
	}
	.small {
		font-size: var(--text-sm);
		margin: 0;
	}
	dl {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: 4px 12px;
		margin: 0;
		font-size: var(--text-sm);
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.acts,
	.pick {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.new {
		display: flex;
		gap: 6px;
		width: 100%;
	}
	.new input {
		flex: 1;
		min-width: 0;
	}
	@media (max-width: 760px) {
		.viewer {
			grid-template-columns: minmax(0, 1fr);
			grid-template-rows: minmax(0, 1fr) auto;
		}
		aside {
			max-height: 42dvh;
			padding: 12px 14px calc(14px + env(safe-area-inset-bottom, 0px));
		}
		.stage img,
		.stage video {
			max-width: 100%;
			max-height: 56dvh;
			border-radius: 0;
		}
		.close {
			top: calc(env(safe-area-inset-top, 0px) + 10px);
		}
	}
</style>
