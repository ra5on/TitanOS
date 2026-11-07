<script lang="ts">
	// PLAN 67-1: Docker › Images. Which images are on the NAS, how big, which
	// app uses them; unused ones can go one by one or all at once. Images in
	// use are never offered for deletion (the NAS refuses them too).
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import {
		createdDate,
		deletedCount,
		freed,
		imageName,
		loadImages,
		relativeDate,
		sumSize,
		unusedImages,
		usedByText,
		type DockerImage,
		type ImageTotals,
		type PruneResult,
	} from '#lib/docker-resources.ts';

	let images = $state<DockerImage[]>([]);
	let totals = $state<ImageTotals | null>(null);
	let loading = $state(true);
	let error = $state('');
	let message = $state('');
	let query = $state('');
	let busy = $state(false);
	let remove = $state<DockerImage | null>(null);
	let prune = $state(false);

	async function load() {
		try {
			({ images, totals } = await loadImages());
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
	});

	const unused = $derived(unusedImages(images));
	// What deleting frees, without layers shared with other images (titand's count first).
	const unusedSize = $derived(
		totals?.unusedCount === unused.length
			? totals.unusedSize
			: unused.reduce((s, i) => s + freed(i), 0),
	);
	const total = $derived(sumSize(images));
	// Used first (by name), then the unused ones: they are what one acts on.
	const shown = $derived(
		images
			.filter((i) => {
				const q = query.trim().toLowerCase();
				if (!q) return true;
				return (
					i.repoTags.some((tag) => tag.toLowerCase().includes(q)) ||
					usedByText(i.usedBy).toLowerCase().includes(q) ||
					i.id.includes(q)
				);
			})
			.sort(
				(a, b) =>
					Number(!a.usedBy.length) - Number(!b.usedBy.length) ||
					Number(!!a.protected) - Number(!!b.protected) ||
					(a.repoTags[0] ?? '~').localeCompare(b.repoTags[0] ?? '~'),
			),
	);

	function label(image: DockerImage) {
		const n = imageName(image);
		return n.repo ? `${n.repo}${n.tag ? ':' + n.tag : ''}` : t('dres.noName');
	}

	async function eraseOne() {
		if (!remove) return;
		busy = true;
		error = '';
		message = '';
		const gone = remove;
		try {
			await api.delete(`/docker/images/${encodeURIComponent(gone.id)}`);
			message = t('dres.images.deleted', { size: formatBytes(freed(gone)) });
			remove = null;
		} catch (err) {
			error = apiErrorText(err, { detail: true });
			remove = null;
		} finally {
			busy = false;
			await load();
		}
	}

	async function eraseUnused() {
		busy = true;
		error = '';
		message = '';
		try {
			const res = await api.post<PruneResult>('/docker/images/prune', { unused: true });
			message = t('dres.cleanup.doneCount', {
				count: deletedCount(res) ?? unused.length,
				size: formatBytes(res?.reclaimed ?? unusedSize),
			});
			if (res?.failed?.length)
				error = t('dres.failed', { names: res.failed.map((f) => f.id).join(', ') });
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			prune = false;
			busy = false;
			await load();
		}
	}
</script>

<PageHead title={t('dres.images.title')}>
	<p>{t('dres.images.intro')}</p>
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if message}<p class="notice-ok" role="status">{message}</p>{/if}

{#if loading}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else if !images.length && !error}
	<EmptyState title={t('dres.images.empty')} text={t('dres.images.emptyText')} />
{:else if images.length}
	{#if unused.length}
		<section class="tile ui-card banner" aria-labelledby="images-unused">
			<div class="banner-text">
				<strong id="images-unused"
					>{unused.length === 1
						? t('dres.images.unusedOne')
						: t('dres.images.unusedTitle', { count: unused.length })}</strong
				>
				<span>{t('dres.images.unusedText', { size: formatBytes(unusedSize) })}</span>
			</div>
			<div class="banner-actions">
				<button type="button" class="btn btn-primary" disabled={busy} onclick={() => (prune = true)}
					>{t('dres.images.deleteUnused', { size: formatBytes(unusedSize) })}</button
				>
			</div>
		</section>
	{/if}

	<section class="tile ui-card" aria-labelledby="images-list">
		<div class="card-head">
			<h2 id="images-list">{t('dres.images.title')}</h2>
			<span class="card-meta"
				>{t('dres.images.count', { count: images.length, size: formatBytes(total) })}</span
			>
			<div class="card-tools">
				<label class="search">
					<Icon name="search" size={14} />
					<span class="sr-only">{t('dres.images.search')}</span>
					<input
						class="field"
						type="search"
						placeholder={t('dres.images.search')}
						bind:value={query}
					/>
				</label>
			</div>
		</div>
		{#if shown.length}
			<div class="table-wrap">
				<table class="data-table resources">
					<thead>
						<tr>
							<th scope="col">{t('dres.col.name')}</th>
							<th scope="col" class="num">{t('dres.col.size')}</th>
							<th scope="col" class="used">{t('dres.col.usedBy')}</th>
							<th scope="col" class="created">{t('dres.col.created')}</th>
							<th scope="col"><span class="sr-only">{t('ui.more')}</span></th>
						</tr>
					</thead>
					<tbody>
						{#each shown as image (image.id)}
							{@const n = imageName(image)}
							{@const used = usedByText(image.usedBy)}
							{@const free = !used && !image.protected}
							<tr class:unused={free}>
								<td class="name">
									{#if n.repo}<strong>{n.repo}</strong>
										{#if n.tag}<span class="tag">{n.tag}</span>{/if}
									{:else}<strong>{t('dres.noName')}</strong>
										<span class="tag mono">{n.tag}</span>{/if}
									{#if image.protected}<span
											class="chip info"
											title={t(
												`dres.protected.${image.protected === 'sandbox' ? 'sandbox' : 'viewer'}`,
											)}><Icon name="lock" size={11} />{t('dres.protectedChip')}</span
										>{/if}
									<span class="sub narrow">{used || t('dres.unused')}</span>
								</td>
								<td class="num">{formatBytes(image.size)}</td>
								<td class="used" class:muted={!used}
									>{used ||
										(image.protected
											? t(`dres.protected.${image.protected === 'sandbox' ? 'sandbox' : 'viewer'}`)
											: t('dres.unused'))}</td
								>
								<td class="created">{relativeDate(createdDate(image.created), i18n.locale)}</td>
								<td class="actions">
									{#if free}
										<button
											type="button"
											class="btn btn-danger-outline small"
											disabled={busy}
											aria-label={t('dres.deleteNamed', { name: label(image) })}
											onclick={() => (remove = image)}>{t('dres.delete')}</button
										>
									{:else}
										<span class="sr-only">{t('dres.inUse')}</span>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="muted none">{t('dres.images.noMatch')}</p>
		{/if}
	</section>
{/if}

{#if remove}
	<Dialog title={t('dres.images.deleteTitle')} locked={busy} onclose={() => (remove = null)}>
		<p>{t('dres.images.deleteText', { name: label(remove), size: formatBytes(freed(remove)) })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (remove = null)}
				>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" disabled={busy} onclick={eraseOne}
				>{t('dres.delete')}</button
			>
		</div>
	</Dialog>
{/if}

{#if prune}
	<Dialog title={t('dres.images.pruneTitle')} locked={busy} onclose={() => (prune = false)}>
		<p>
			{t('dres.images.pruneText', { count: unused.length, size: formatBytes(unusedSize) })}
		</p>
		<ul class="names">
			{#each unused as image (image.id)}<li>
					<span class="mono">{label(image)}</span>
					<span class="muted">{formatBytes(freed(image))}</span>
				</li>{/each}
		</ul>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (prune = false)}
				>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" disabled={busy} onclick={eraseUnused}
				>{t('dres.delete')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.search {
		position: relative;
		display: flex;
		align-items: center;
		color: var(--muted);
	}
	.search :global(svg) {
		position: absolute;
		left: 10px;
		pointer-events: none;
	}
	.search input {
		width: min(260px, 60vw);
		min-height: 32px;
		padding-left: 30px;
		font-size: var(--text-base);
	}
	.name .chip {
		margin-left: 6px;
	}
	.name strong {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.tag {
		color: var(--muted);
		margin-left: 4px;
		overflow-wrap: anywhere;
	}
	.unused .num,
	.unused .created {
		color: var(--muted);
	}
	.actions {
		width: 1%;
		white-space: nowrap;
		text-align: right;
	}
	.small {
		min-height: 30px;
		padding: 0 10px;
		font-size: var(--text-base);
	}
	.none {
		margin: 0;
		padding: 16px;
	}
	.narrow {
		display: none;
	}
	.names {
		margin: 8px 0 0;
		padding: 0;
		list-style: none;
		max-height: 220px;
		overflow: auto;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.names li {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		font-size: var(--text-base);
	}
	.names .mono {
		overflow-wrap: anywhere;
	}
	.dialog-actions {
		margin-top: 16px;
	}
	@media (max-width: 639px) {
		.resources .created {
			display: none;
		}
	}
	@media (max-width: 479px) {
		.resources .used {
			display: none;
		}
		.narrow {
			display: block;
		}
	}
</style>
