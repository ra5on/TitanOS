<script lang="ts">
	// PLAN 67-1: Docker › Volumes. The data stores of containers, who uses
	// them and how big they are. Deleting one deletes its data for good: a
	// plain confirmation that says so (CLAUDE.md, no name to type), the
	// dangerous button left of "Cancel", never the focused one.
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	import {
		anonymousUnused,
		deletableVolumes,
		deletedCount,
		loadVolumes,
		sumSize,
		usedByText,
		type DockerVolume,
		type PruneResult,
	} from '#lib/docker-resources.ts';

	let volumes = $state<DockerVolume[]>([]);
	let loading = $state(true);
	let error = $state('');
	let message = $state('');
	let query = $state('');
	let busy = $state(false);
	let remove = $state<DockerVolume | null>(null);
	let prune = $state(false);

	async function load() {
		try {
			volumes = await loadVolumes();
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
	});

	// Unused and not TitanOS's own: deletable one by one. The clean-up takes the anonymous ones.
	const unused = $derived(deletableVolumes(volumes));
	const anonymous = $derived(anonymousUnused(volumes));
	const known = $derived(volumes.some((v) => v.size !== undefined));
	const shown = $derived(
		volumes
			.filter((v) => {
				const q = query.trim().toLowerCase();
				return (
					!q || v.name.toLowerCase().includes(q) || usedByText(v.usedBy).toLowerCase().includes(q)
				);
			})
			.sort(
				(a, b) =>
					Number(!a.usedBy.length) - Number(!b.usedBy.length) || a.name.localeCompare(b.name),
			),
	);
	const size = (v: DockerVolume) =>
		v.size === undefined ? t('dres.volumes.sizeUnknown') : formatBytes(v.size);

	async function eraseOne() {
		if (!remove) return;
		busy = true;
		error = '';
		message = '';
		try {
			await api.delete(`/docker/volumes/${encodeURIComponent(remove.name)}`);
			message = remove.size
				? t('dres.cleanup.done', { size: formatBytes(remove.size) })
				: t('dres.cleanup.doneCount', { count: 1, size: formatBytes(0) });
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			remove = null;
			busy = false;
			await load();
		}
	}

	async function eraseUnused() {
		busy = true;
		error = '';
		message = '';
		try {
			const res = await api.post<PruneResult>('/docker/volumes/prune', { anonymousOnly: true });
			message = t('dres.cleanup.doneCount', {
				count: deletedCount(res) ?? anonymous.length,
				size: formatBytes(res?.reclaimed ?? sumSize(anonymous)),
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

<PageHead title={t('dres.volumes.title')}>
	<p>{t('dres.volumes.intro')}</p>
</PageHead>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if message}<p class="notice-ok" role="status">{message}</p>{/if}

{#if loading}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else if !volumes.length && !error}
	<EmptyState title={t('dres.volumes.empty')} text={t('dres.volumes.emptyText')} />
{:else if volumes.length}
	<!-- PLAN 78-5: what can go is one row with its button; the list stands
	     under a plain heading and keeps the table's own surface. -->
	{#if unused.length}
		<SettingGroup>
			<SettingRow
				icon="trash"
				id="volumes-unused"
				title={unused.length === 1
					? t('dres.volumes.unusedOne')
					: t('dres.volumes.unusedTitle', { count: unused.length })}
				desc="{t('dres.volumes.unusedText')} {anonymous.length
					? t('dres.volumes.anonymousText', { count: anonymous.length })
					: t('dres.volumes.namedOnly')}"
			>
				{#snippet control()}{#if anonymous.length}<button
							type="button"
							class="btn btn-danger-outline"
							disabled={busy}
							onclick={() => (prune = true)}
							>{t('dres.volumes.deleteAnonymous', { count: anonymous.length })}</button
						>{/if}{/snippet}
			</SettingRow>
		</SettingGroup>
	{/if}

	<SettingGroup
		title={t('dres.volumes.title')}
		id="volumes-list"
		desc={known
			? t('dres.images.count', { count: volumes.length, size: formatBytes(sumSize(volumes)) })
			: String(volumes.length)}
	>
		{#snippet tools()}
			<label class="search">
				<Icon name="search" size={14} />
				<span class="sr-only">{t('dres.volumes.search')}</span>
				<input
					class="field"
					type="search"
					placeholder={t('dres.volumes.search')}
					bind:value={query}
				/>
			</label>
		{/snippet}
		{#if shown.length}
			<div class="tile ui-card">
				<div class="table-wrap">
					<table class="data-table resources">
						<thead>
							<tr>
								<th scope="col">{t('dres.col.name')}</th>
								<th scope="col" class="num">{t('dres.col.size')}</th>
								<th scope="col" class="used">{t('dres.col.usedBy')}</th>
								<th scope="col" class="driver">{t('dres.col.driver')}</th>
								<th scope="col"><span class="sr-only">{t('ui.more')}</span></th>
							</tr>
						</thead>
						<tbody>
							{#each shown as volume (volume.name)}
								{@const used = usedByText(volume.usedBy)}
								{@const free = !used && !volume.titanos}
								<tr class:unused={free}>
									<td class="name">
										<span class="mono vname">{volume.name}</span>
										{#if volume.titanos}<span
												class="chip info"
												title={t('dres.volumes.titanosHint')}
												><Icon name="lock" size={11} />{t('dres.volumes.titanos')}</span
											>{/if}
										{#if volume.anonymous}<span class="chip">{t('dres.volumes.anonymousChip')}</span
											>{/if}
										<span class="sub narrow">{used || t('dres.unused')}</span>
									</td>
									<td class="num" class:muted={volume.size === undefined}>{size(volume)}</td>
									<td class="used" class:muted={!used}>{used || t('dres.unused')}</td>
									<td class="driver muted">{volume.driver}</td>
									<td class="actions">
										{#if free}
											<button
												type="button"
												class="btn btn-danger-outline small"
												disabled={busy}
												aria-label={t('dres.deleteNamed', { name: volume.name })}
												onclick={() => (remove = volume)}>{t('dres.delete')}</button
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
			</div>
		{:else}
			<p class="muted none">{t('dres.volumes.noMatch')}</p>
		{/if}
	</SettingGroup>
	<StatusBar label={t('dres.statusBar')}>
		<span
			>{known
				? t('dres.images.count', { count: volumes.length, size: formatBytes(sumSize(volumes)) })
				: volumes.length}</span
		>
		<span class:warn={anonymous.length > 0}>{t('dres.statUnused')} <b>{anonymous.length}</b></span>
	</StatusBar>
{/if}

{#if remove}
	<Dialog title={t('dres.volumes.deleteTitle')} locked={busy} onclose={() => (remove = null)}>
		<p>{t('dres.volumes.deleteText', { name: remove.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (remove = null)}
				>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" disabled={busy} onclick={eraseOne}
				>{t('dres.volumes.deleteButton')}</button
			>
		</div>
	</Dialog>
{/if}

{#if prune}
	<Dialog title={t('dres.volumes.anonymousTitle')} locked={busy} onclose={() => (prune = false)}>
		<p>{t('dres.volumes.anonymousPrune', { count: anonymous.length })}</p>
		<ul class="names">
			{#each anonymous as volume (volume.name)}<li>
					<span class="mono">{volume.name}</span>
					<span class="muted">{size(volume)}</span>
				</li>{/each}
		</ul>
		<p class="notice-warn">{t('dres.volumes.pruneWarn')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (prune = false)}
				>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" disabled={busy} onclick={eraseUnused}
				>{t('dres.volumes.deleteButton')}</button
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
	.vname {
		overflow-wrap: anywhere;
	}
	.name .chip {
		margin-left: var(--space-1-5);
	}
	.actions {
		width: 1%;
		white-space: nowrap;
		text-align: right;
	}
	.small {
		min-height: 30px;
		padding: 0 var(--space-2-5);
		font-size: var(--text-base);
	}
	.none {
		margin: 0;
		padding: var(--space-2) 0;
	}
	.narrow {
		display: none;
	}
	.names {
		margin: var(--space-2) 0;
		padding: 0;
		list-style: none;
		max-height: 220px;
		overflow: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.names li {
		display: flex;
		justify-content: space-between;
		gap: var(--space-3);
		font-size: var(--text-base);
	}
	.names .mono {
		overflow-wrap: anywhere;
	}
	.dialog-actions {
		margin-top: var(--space-4);
	}
	@media (max-width: 639px) {
		.resources .driver {
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
