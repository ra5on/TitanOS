<script lang="ts" module>
	// 1.07: the photos a turn found in Immich (photos_search), as a grid of
	// small pictures under the answer; a tap shows one large. The pictures
	// come through TitanOS's own route, which checks that the photo belongs
	// to the account (api/agent_photos.go); nothing goes to Immich directly.
	import type { Entry } from '#lib/agent.ts';

	export type PhotoHit = { id: string; date: string; place: string; video: boolean };

	const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

	/** The photos of the newest photos_search of a turn that could be read. */
	export function photoHits(steps: Entry[]): PhotoHit[] {
		const step = steps.findLast((s) => s.role === 'tool' && s.tool === 'photos_search' && s.ok);
		if (!step?.result) return [];
		let data: unknown;
		try {
			data = JSON.parse(step.result.slice(step.result.indexOf('{')));
		} catch {
			return [];
		}
		const list = (data as { photos?: unknown })?.photos;
		if (!Array.isArray(list)) return [];
		return list
			.filter((p): p is Record<string, unknown> => !!p && typeof p === 'object')
			.filter((p) => typeof p.id === 'string' && ID.test(p.id))
			.map((p) => ({
				id: p.id as string,
				date: typeof p.date === 'string' ? p.date : '',
				place: typeof p.place === 'string' ? p.place : '',
				video: p.video === true,
			}));
	}

	/** The URL of a picture through TitanOS. */
	export const pictureURL = (id: string, size: 'thumbnail' | 'preview' = 'thumbnail') =>
		`/api/v1/agent/photos/${encodeURIComponent(id)}/picture?size=${size}`;
</script>

<script lang="ts">
	import Dialog from '#lib/components/Dialog.svelte';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';

	let { photos }: { photos: PhotoHit[] } = $props();

	// Immich gives the local time of the picture without a zone: shown as it is.
	function day(s: string): string {
		const d = new Date(s);
		if (!s || Number.isNaN(d.getTime())) return '';
		return new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(d);
	}
	const label = (p: PhotoHit) =>
		[p.video ? t('agent.photos.video') : t('agent.photos.photo'), day(p.date), p.place]
			.filter(Boolean)
			.join(' · ');

	let all = $state(false);
	const shown = $derived(all ? photos : photos.slice(0, 12));
	let open = $state(-1);
	const current = $derived(open >= 0 ? photos[open] : undefined);
</script>

{#if photos.length}
	<section class="card" aria-label={t('agent.photos.card')}>
		<header>
			<strong>{t('agent.photos.card')}</strong>
			<span class="muted">{t('agent.photos.count', { count: photos.length })}</span>
		</header>
		<ul class="grid">
			{#each shown as p, i (p.id)}
				<li>
					<button type="button" class="tile" aria-label={label(p)} onclick={() => (open = i)}>
						<img src={pictureURL(p.id)} alt="" loading="lazy" decoding="async" />
						{#if p.video}<span class="badge">{t('agent.photos.video')}</span>{/if}
					</button>
				</li>
			{/each}
		</ul>
		{#if photos.length > 12 && !all}
			<button type="button" class="more" onclick={() => (all = true)}
				>{t('agent.photos.all', { count: photos.length })}</button
			>
		{/if}
	</section>
{/if}

{#if current}
	<Dialog title={label(current)} wide onclose={() => (open = -1)}>
		<figure class="big">
			<img src={pictureURL(current.id, 'preview')} alt={label(current)} />
		</figure>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (open = -1)}>{t('common.close')}</button>
			<button
				type="button"
				class="btn"
				disabled={open <= 0}
				onclick={() => (open = Math.max(0, open - 1))}>{t('agent.photos.prev')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={open >= photos.length - 1}
				onclick={() => (open = Math.min(photos.length - 1, open + 1))}
				>{t('agent.photos.next')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.card {
		margin-top: var(--space-3);
		border: 1px solid var(--border);
		border-radius: 18px;
		background: var(--surface);
		overflow: hidden;
	}
	header {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-2);
	}
	.muted {
		color: var(--muted);
		font-size: 0.9em;
	}
	.grid {
		list-style: none;
		margin: 0;
		padding: 0 var(--space-3) var(--space-3);
		display: grid;
		/* Three columns on the phone (two on very narrow ones), more beside. */
		grid-template-columns: repeat(auto-fill, minmax(104px, 1fr));
		gap: var(--space-1);
	}
	@media (max-width: 340px) {
		.grid {
			grid-template-columns: repeat(2, 1fr);
		}
	}
	.tile {
		position: relative;
		display: block;
		width: 100%;
		min-height: 44px;
		aspect-ratio: 1;
		padding: 0;
		border: 0;
		border-radius: 8px;
		overflow: hidden;
		background: var(--chip);
		cursor: zoom-in;
	}
	.tile:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.tile img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.badge {
		position: absolute;
		left: 4px;
		bottom: 4px;
		padding: 1px 6px;
		border-radius: 999px;
		font-size: var(--text-xs);
		background: rgb(0 0 0 / 0.65);
		color: #fff;
	}
	.more {
		width: 100%;
		min-height: 44px;
		border: 0;
		border-top: 1px solid var(--border);
		background: none;
		color: var(--ag-link, var(--accent));
		font: inherit;
		cursor: pointer;
	}
	.big {
		margin: 0;
		display: flex;
		justify-content: center;
	}
	.big img {
		max-width: 100%;
		max-height: 70vh;
		border-radius: 8px;
		object-fit: contain;
	}
	.dialog-actions .btn {
		min-height: 44px;
	}
</style>
