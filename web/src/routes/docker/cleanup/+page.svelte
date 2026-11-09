<script lang="ts">
	// PLAN 67-1: Docker › Aufräumen. What Docker takes on the NAS and what can
	// be freed, then one row per kind of clean-up with its size. Unused
	// anonymous volumes take their data with them: their own confirmation
	// says so. While Docker still counts (measuring), the page asks again
	// every 3 s.
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import {
		anonymousUnused,
		deletedCount,
		freed,
		loadDf,
		loadImages,
		loadNetworks,
		loadVolumes,
		unusedNetworks,
		type DockerNetwork,
		sumSize,
		unusedImages,
		type ImageTotals,
		type DockerDf,
		type DockerImage,
		type DockerVolume,
		type PruneResult,
	} from '#lib/docker-resources.ts';

	type Kind = 'dangling' | 'images' | 'volumes' | 'cache' | 'networks';

	let df = $state<DockerDf | null>(null);
	let images = $state<DockerImage[]>([]);
	let totals = $state<ImageTotals | null>(null);
	let volumes = $state<DockerVolume[]>([]);
	let networks = $state<DockerNetwork[]>([]);
	let loading = $state(true);
	let error = $state('');
	let message = $state('');
	let busy = $state<Kind | null>(null);
	let ask = $state<Kind | null>(null);

	async function load(fresh = false) {
		const [d, i, v, n] = await Promise.allSettled([
			loadDf(fresh),
			loadImages(),
			loadVolumes(),
			loadNetworks(),
		]);
		networks = n.status === 'fulfilled' ? n.value : [];
		df = d.status === 'fulfilled' ? d.value : null;
		images = i.status === 'fulfilled' ? i.value.images : [];
		totals = i.status === 'fulfilled' ? i.value.totals : null;
		volumes = v.status === 'fulfilled' ? v.value : [];
		const failed = [d, i, v].find((r) => r.status === 'rejected');
		error = failed && failed.status === 'rejected' ? apiErrorText(failed.reason) : '';
		loading = false;
	}
	$effect(() => {
		void load();
	});
	// Docker counts in the background: ask again until it is done.
	async function remeasure() {
		try {
			df = await loadDf();
		} catch {
			// The next round tries again.
		}
	}
	$effect(() => {
		if (!df?.measuring) return;
		const timer = setInterval(remeasure, 3000);
		return () => clearInterval(timer);
	});

	const unusedImg = $derived(unusedImages(images));
	const dangling = $derived(unusedImg.filter((i) => i.dangling));
	const unusedVol = $derived(anonymousUnused(volumes));
	const spareNetworks = $derived(unusedNetworks(networks));
	const freedOf = (list: DockerImage[]) => list.reduce((s, i) => s + freed(i), 0);
	const cache = $derived(df?.buildCache?.reclaimable ?? df?.buildCache?.size ?? 0);

	const rows = $derived<
		{ kind: Kind; title: MessageKey; hint: MessageKey; count: number; size: number }[]
	>([
		{
			kind: 'dangling',
			title: 'dres.cleanup.dangling',
			hint: 'dres.cleanup.danglingHint',
			count: dangling.length,
			size: totals?.danglingCount === dangling.length ? totals.danglingSize : freedOf(dangling),
		},
		{
			kind: 'images',
			title: 'dres.cleanup.unusedImages',
			hint: 'dres.cleanup.unusedImagesHint',
			count: unusedImg.length,
			size: totals?.unusedCount === unusedImg.length ? totals.unusedSize : freedOf(unusedImg),
		},
		{
			kind: 'volumes',
			title: 'dres.cleanup.anonymous',
			hint: 'dres.cleanup.anonymousHint',
			count: unusedVol.length,
			size: df?.anonymousReclaimable || sumSize(unusedVol),
		},
		{
			kind: 'cache',
			title: 'dres.cleanup.cache',
			hint: 'dres.cleanup.cacheHint',
			count: cache > 0 ? 1 : 0,
			size: cache,
		},
		{
			kind: 'networks',
			title: 'dnet.cleanup',
			hint: 'dnet.cleanupHint',
			count: spareNetworks.length,
			size: 0,
		},
	]);

	const parts = $derived(
		df
			? (
					[
						['dres.cleanup.images', df.images],
						['dres.cleanup.containers', df.containers],
						['dres.cleanup.volumes', df.volumes],
						['dres.cleanup.buildCache', df.buildCache],
					] as const
				).filter(([, p]) => p)
			: [],
	);
	const total = $derived(parts.reduce((s, [, p]) => s + (p?.size ?? 0), 0));
	const free = $derived(parts.reduce((s, [, p]) => s + (p?.reclaimable ?? 0), 0));

	const PATHS: Record<Kind, [string, Record<string, unknown>]> = {
		dangling: ['/docker/images/prune', { unused: false }],
		images: ['/docker/images/prune', { unused: true }],
		volumes: ['/docker/volumes/prune', { anonymousOnly: true }],
		cache: ['/docker/builder/prune', {}],
		networks: ['/docker/networks/prune', {}],
	};

	async function run(kind: Kind) {
		const row = rows.find((r) => r.kind === kind);
		ask = null;
		busy = kind;
		error = '';
		message = '';
		try {
			const [path, body] = PATHS[kind];
			const res = await api.post<PruneResult>(path, body);
			const count = kind === 'cache' ? null : deletedCount(res);
			if (kind === 'networks') {
				message = t('dnet.pruned', { count: count ?? spareNetworks.length });
				return;
			}
			const size = formatBytes(res?.reclaimed ?? row?.size ?? 0);
			message =
				count === null
					? t('dres.cleanup.done', { size })
					: t('dres.cleanup.doneCount', { count, size });
			if (res?.failed?.length)
				error = t('dres.failed', { names: res.failed.map((f) => f.id).join(', ') });
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			busy = null;
			await load();
		}
	}

	const asked = $derived(rows.find((r) => r.kind === ask));
</script>

<PageHead title={t('dres.cleanup.title')}>
	<p>{t('dres.cleanup.intro')}</p>
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if message}<p class="notice-ok" role="status">{message}</p>{/if}

{#if loading}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<section class="tile ui-card" aria-labelledby="cleanup-summary">
		<div class="card-head">
			<h2 id="cleanup-summary">{t('dres.cleanup.summary')}</h2>
			<div class="card-tools">
				<button type="button" class="btn" disabled={df?.measuring} onclick={() => load(true)}
					>{t('dres.cleanup.recount')}</button
				>
			</div>
			{#if df}<span class="card-meta"
					>{formatBytes(total)} · {t('dres.cleanup.reclaimable')} {formatBytes(free)}</span
				>{/if}
		</div>
		{#if df?.measuring}<p class="muted none" role="status">{t('dres.cleanup.measuring')}</p>{/if}
		{#if parts.length}
			<div class="summary">
				{#each parts as [label, p] (label)}
					<div class="part">
						<span class="part-label">{t(label)}</span>
						<strong class="part-size">{formatBytes(p?.size ?? 0)}</strong>
						<span class="part-free"
							>{p?.count ?? 0} · {t('dres.cleanup.free', {
								size: formatBytes(p?.reclaimable ?? 0),
							})}</span
						>
					</div>
				{/each}
			</div>
		{:else}
			<p class="muted none">{t('dres.cleanup.unknown')}</p>
		{/if}
	</section>

	<section class="tile ui-card" aria-labelledby="cleanup-actions">
		<div class="card-head"><h2 id="cleanup-actions">{t('dres.cleanup.actions')}</h2></div>
		{#each rows as row (row.kind)}
			<div class="set-row" data-kind={row.kind}>
				<span class="set-label">{t(row.title)}<span class="set-hint">{t(row.hint)}</span></span>
				<span class="set-control end">
					{#if row.kind !== 'cache'}<span class="count muted">{row.count}</span>{/if}
					<button
						type="button"
						class="btn"
						class:btn-danger-outline={row.kind === 'volumes'}
						disabled={!row.count || busy !== null}
						onclick={() => (ask = row.kind)}
						>{busy === row.kind
							? t('ui.saving')
							: !row.count
								? t('dres.cleanup.nothing')
								: row.kind === 'networks'
									? t('dnet.remove')
									: t('dres.cleanup.runSize', { size: formatBytes(row.size) })}</button
					>
				</span>
			</div>
		{/each}
	</section>
{/if}

{#if asked}
	{@const title =
		asked.kind === 'volumes'
			? t('dres.volumes.anonymousTitle')
			: asked.kind === 'images'
				? t('dres.images.pruneTitle')
				: asked.kind === 'dangling'
					? t('dres.cleanup.danglingTitle')
					: asked.kind === 'networks'
						? t('dnet.pruneTitle')
						: t('dres.cleanup.cacheTitle')}
	<Dialog {title} onclose={() => (ask = null)}>
		{#if asked.kind === 'volumes'}
			<p>{t('dres.volumes.anonymousPrune', { count: unusedVol.length })}</p>
			<ul class="names">
				{#each unusedVol as v (v.name)}<li class="mono">{v.name}</li>{/each}
			</ul>
			<p class="notice-warn">{t('dres.volumes.pruneWarn')}</p>
		{:else if asked.kind === 'images'}
			<p>
				{t('dres.images.pruneText', { count: unusedImg.length, size: formatBytes(asked.size) })}
			</p>
		{:else if asked.kind === 'dangling'}
			<p>{t('dres.cleanup.danglingText')}</p>
		{:else if asked.kind === 'networks'}
			<p>{t('dnet.pruneText', { count: spareNetworks.length })}</p>
			<ul class="names">
				{#each spareNetworks as n (n.name)}<li class="mono">{n.name}</li>{/each}
			</ul>
		{:else}
			<p>{t('dres.cleanup.cacheText')}</p>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (ask = null)}>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" onclick={() => run(asked.kind)}
				>{asked.kind === 'volumes' ? t('dres.volumes.deleteButton') : t('dres.delete')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.summary {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(128px, 1fr));
	}
	.part {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-3) var(--space-4);
		box-shadow: inset 1px 0 0 var(--panel-border, var(--border));
		margin-left: -1px;
		min-width: 0;
	}
	.part-label {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.part-size {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.part-free {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.count {
		min-width: 2ch;
		text-align: right;
		font-size: var(--text-base);
	}
	.none {
		margin: 0;
		padding: var(--space-4);
	}
	.names {
		margin: var(--space-2) 0;
		padding: 0;
		list-style: none;
		max-height: 200px;
		overflow: auto;
		font-size: var(--text-base);
	}
	.dialog-actions {
		margin-top: var(--space-4);
	}
</style>
