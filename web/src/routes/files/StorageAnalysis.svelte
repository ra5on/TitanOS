<script lang="ts">
	// PLAN 72: storage analysis as a view of the Files window (replaces the
	// old "analyse usage" dialog). Folders as areas, the largest files, files
	// not changed for a long time, and the duplicate search. Marked files go
	// to the recycle bin of their share after a plain confirmation; files
	// without a bin are never deleted from here.
	import { api, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes, formatDate } from '#lib/format.ts';
	import { deletePaths, inTrash } from './commands.ts';
	import { treemap, type Area } from './treemap.ts';

	let {
		root,
		dir,
		showHidden,
		onback,
		onopen,
		onduplicates,
		onchanged,
	}: {
		root: FileRoot;
		dir: string;
		showHidden: boolean;
		onback: () => void;
		onopen: (path: string) => void;
		onduplicates: () => void;
		onchanged: () => void;
	} = $props();

	type Item = {
		path: string;
		bytes: number;
		files: number;
		dir: boolean;
		modified?: string;
		sub?: Item[];
	};
	type Usage = {
		bytes: number;
		files: number;
		folders: number;
		largest: Item[];
		children: Item[];
		old: Item[];
		olderThan: string;
	};
	type Tab = 'areas' | 'large' | 'old';

	// The folder the analysis looks at; starts where the window stands.
	let base = $state((() => dir)());
	let tab = $state<Tab>('areas');
	let data = $state<Usage | null>(null);
	let busy = $state(false);
	let error = $state('');
	let scanned = $state<Date | null>(null);
	let years = $state(3);
	let minOld = $state(0);
	let minLarge = $state(0);
	let selected = $state<string[]>([]);
	let confirm = $state(false);
	let removing = $state(false);
	let controller: AbortController | undefined;
	$effect(() => () => controller?.abort());

	const name = (p: string) => p.split('/').pop() || root.name;
	const parent = (p: string) => p.split('/').slice(0, -1).join('/');

	async function scan() {
		controller?.abort();
		const c = new AbortController();
		controller = c;
		busy = true;
		error = '';
		selected = [];
		try {
			const result = await api.post<Usage>(
				'/files/usage',
				{
					root: root.id,
					path: base,
					showHidden,
					olderThanDays: Math.round(years * 365),
					minOldBytes: minOld,
				},
				c.signal,
			);
			if (!c.signal.aborted) {
				data = result;
				scanned = new Date();
			}
		} catch (e) {
			if (!c.signal.aborted) error = apiErrorText(e);
		} finally {
			if (controller === c) busy = false;
		}
	}
	// Scans on opening and whenever the folder or the old-file filter changes.
	$effect(() => {
		base;
		years;
		minOld;
		scan();
	});

	function into(path: string) {
		base = path;
		tab = 'areas';
	}

	const crumbs = $derived.by(() => {
		const parts = base ? base.split('/') : [];
		return [
			{ label: root.name, path: '' },
			...parts.map((part, i) => ({ label: part, path: parts.slice(0, i + 1).join('/') })),
		];
	});

	let mapWidth = $state(800);
	let mapHeight = $derived(Math.max(260, Math.min(460, Math.round(mapWidth * 0.5))));
	const areas = $derived<Area<Item>[]>(
		data
			? treemap(
					data.children.filter((c) => c.bytes > 0),
					mapWidth,
					mapHeight,
					(c) => c.bytes,
				)
			: [],
	);
	function subAreas(area: Area<Item>): Area<Item>[] {
		const sub = area.item.sub?.filter((s) => s.bytes > 0) ?? [];
		if (!sub.length || area.w < 90 || area.h < 70) return [];
		const rest = area.item.bytes - sub.reduce((sum, s) => sum + s.bytes, 0);
		const items = rest > 0 ? [...sub, { path: '', bytes: rest, files: 0, dir: false }] : sub;
		return treemap(items, area.w - 8, area.h - 30, (s) => s.bytes);
	}

	const list = $derived<Item[]>(
		!data
			? []
			: tab === 'large'
				? data.largest.filter((item) => item.bytes >= minLarge)
				: tab === 'old'
					? data.old
					: [],
	);
	const binned = $derived(selected.filter((p) => inTrash(root, p)));
	const selectedBytes = $derived(
		list.filter((item) => selected.includes(item.path)).reduce((sum, item) => sum + item.bytes, 0),
	);
	function toggle(path: string, on: boolean) {
		selected = on ? [...selected, path] : selected.filter((p) => p !== path);
	}
	async function remove() {
		removing = true;
		error = '';
		try {
			await deletePaths(root, binned, false);
			confirm = false;
			onchanged();
			await scan();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			removing = false;
		}
	}
	const day = (value?: string) => (value ? formatDate(new Date(value)) : '');
</script>

<div class="analysis">
	<div class="toolbar head">
		<button type="button" class="btn" onclick={onback}>← {t('files.analysis.back')}</button>
		<nav class="crumbs" aria-label={t('files.analysis.title')}>
			{#each crumbs as crumb, i (crumb.path)}
				{#if i}<span class="sep" aria-hidden="true">›</span>{/if}
				<button
					type="button"
					class="crumb"
					aria-current={crumb.path === base ? 'page' : undefined}
					onclick={() => into(crumb.path)}>{crumb.label}</button
				>
			{/each}
		</nav>
		<span class="spacer"></span>
		<button type="button" class="btn" disabled={busy} onclick={scan}
			>{t('files.analysis.rescan')}</button
		>
	</div>
	<p class="summary muted" role="status">
		{#if busy}{t('files.analysis.running')}
			<button type="button" class="btn small" onclick={() => controller?.abort()}
				>{t('common.cancel')}</button
			>{:else if data}{t('files.analysis.summary', {
				bytes: formatBytes(data.bytes),
				files: data.files.toLocaleString(),
				folders: data.folders.toLocaleString(),
			})}{#if scanned}
				{' · '}{t('files.analysis.scanned', { time: scanned.toLocaleTimeString() })}{/if}{/if}
	</p>
	<div class="tabs" role="tablist">
		{#each [['areas', t('files.analysis.areas')], ['large', t('files.analysis.large')], ['old', t('files.analysis.old')]] as [id, label] (id)}
			<button
				type="button"
				role="tab"
				class="tab"
				aria-selected={tab === id}
				onclick={() => {
					tab = id as Tab;
					selected = [];
				}}>{label}</button
			>
		{/each}
		<button type="button" role="tab" class="tab" aria-selected="false" onclick={onduplicates}
			>{t('files.analysis.duplicates')}</button
		>
	</div>

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

	{#if tab === 'areas'}
		<p class="muted hint">{t('files.analysis.areasHint')}</p>
		<div class="map-wrap">
			<div class="measure" bind:clientWidth={mapWidth}></div>
			{#if data && areas.length}
				<div class="map" style:height="{mapHeight}px">
					{#each areas as area (area.item.path)}
						<div
							class="area"
							style:left="{area.x}px"
							style:top="{area.y}px"
							style:width="{area.w}px"
							style:height="{area.h}px"
						>
							{#if area.item.dir}
								<button
									type="button"
									class="area-head"
									title={t('files.analysis.open', { name: name(area.item.path) })}
									onclick={() => into(area.item.path)}
									><b>{name(area.item.path)}</b>
									<span>{formatBytes(area.item.bytes)}</span></button
								>
							{:else}
								<span class="area-head file"
									><b>{name(area.item.path)}</b> <span>{formatBytes(area.item.bytes)}</span></span
								>
							{/if}
							{#each subAreas(area) as sub (sub.item.path || 'rest')}
								{#if sub.item.path && sub.item.dir}
									<button
										type="button"
										class="sub"
										style:left="{sub.x + 4}px"
										style:top="{sub.y + 26}px"
										style:width="{sub.w}px"
										style:height="{sub.h}px"
										title="{name(sub.item.path)} · {formatBytes(sub.item.bytes)}"
										onclick={() => into(sub.item.path)}
										>{#if sub.w > 60 && sub.h > 34}<b>{name(sub.item.path)}</b><span
												>{formatBytes(sub.item.bytes)}</span
											>{/if}</button
									>
								{:else}
									<span
										class="sub plain"
										style:left="{sub.x + 4}px"
										style:top="{sub.y + 26}px"
										style:width="{sub.w}px"
										style:height="{sub.h}px"
										title="{sub.item.path
											? name(sub.item.path)
											: t('files.analysis.filesHere')} · {formatBytes(sub.item.bytes)}"
										>{#if sub.w > 60 && sub.h > 34}<b
												>{sub.item.path ? name(sub.item.path) : t('files.analysis.filesHere')}</b
											><span>{formatBytes(sub.item.bytes)}</span>{/if}</span
									>
								{/if}
							{/each}
						</div>
					{/each}
				</div>
				<table class="data-table legend">
					<thead
						><tr
							><th>{t('files.analysis.name')}</th><th class="num">{t('files.analysis.size')}</th><th
								class="num">{t('files.analysis.share')}</th
							><th></th></tr
						></thead
					>
					<tbody>
						{#each data.children.slice(0, 30) as child (child.path)}
							<tr>
								<td class="name">{name(child.path)}{child.dir ? '/' : ''}</td>
								<td class="num">{formatBytes(child.bytes)}</td>
								<td class="num"
									>{data.bytes ? Math.round((child.bytes / data.bytes) * 100) : 0} %</td
								>
								<td class="end"
									>{#if child.dir}<button
											type="button"
											class="btn small"
											onclick={() => onopen(child.path)}>{t('files.openFolder')}</button
										>{/if}</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			{:else if data}<p class="muted">{t('files.analysis.empty')}</p>{/if}
		</div>
	{:else}
		<div class="filters">
			{#if tab === 'large'}
				<label
					>{t('files.analysis.from')}
					<select class="field" bind:value={minLarge}>
						<option value={0}>{t('files.analysis.any')}</option>
						<option value={100e6}>100 MB</option>
						<option value={1e9}>1 GB</option>
						<option value={10e9}>10 GB</option>
					</select></label
				>
			{:else}
				<label
					>{t('files.analysis.notChanged')}
					<select class="field" bind:value={years}>
						{#each [1, 2, 3, 5, 10] as y (y)}<option value={y}
								>{y === 1
									? t('files.analysis.year')
									: t('files.analysis.years', { count: y })}</option
							>{/each}
					</select></label
				>
				<label
					>{t('files.analysis.atLeast')}
					<select class="field" bind:value={minOld}>
						<option value={0}>{t('files.analysis.any')}</option>
						<option value={100e6}>100 MB</option>
						<option value={1e9}>1 GB</option>
						<option value={10e9}>10 GB</option>
					</select></label
				>
			{/if}
		</div>
		{#if data && list.length}
			<div class="table-wrap">
				<table class="data-table">
					<thead
						><tr
							><th class="check"><span class="sr-only">{t('files.analysis.mark')}</span></th><th
								>{t('files.analysis.name')}</th
							><th class="num">{t('files.analysis.size')}</th><th class="when"
								>{t('files.analysis.modified')}</th
							><th></th></tr
						></thead
					>
					<tbody>
						{#each list as item (item.path)}
							<tr>
								<td class="check"
									><input
										type="checkbox"
										aria-label={name(item.path)}
										disabled={!root.writable || !inTrash(root, item.path)}
										title={inTrash(root, item.path) ? '' : t('files.analysis.noBin')}
										checked={selected.includes(item.path)}
										onchange={(e) => toggle(item.path, e.currentTarget.checked)}
									/></td
								>
								<td class="name"
									>{name(item.path)}<small class="muted">{parent(item.path) || root.name}</small
									></td
								>
								<td class="num">{formatBytes(item.bytes)}</td>
								<td class="when">{day(item.modified)}</td>
								<td class="end"
									><button type="button" class="btn small" onclick={() => onopen(parent(item.path))}
										>{t('files.openFolder')}</button
									></td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<div class="foot">
				<span
					>{t('files.analysis.marked', {
						count: selected.length,
						bytes: formatBytes(selectedBytes),
					})}</span
				>
				<span class="spacer"></span>
				<button
					type="button"
					class="btn"
					disabled={!selected.length}
					onclick={() => (selected = [])}>{t('files.analysis.clear')}</button
				>
				<button
					type="button"
					class="btn btn-danger"
					disabled={!binned.length}
					onclick={() => (confirm = true)}>{t('files.analysis.toTrash')}</button
				>
			</div>
			<p class="muted hint">{t('files.analysis.trashHint')}</p>
		{:else if data}<p class="muted hint">
				{t(tab === 'old' ? 'files.analysis.noOld' : 'files.analysis.empty')}
			</p>{/if}
	{/if}
</div>

{#if confirm}
	<Dialog
		title={t('files.analysis.confirmTitle')}
		locked={removing}
		onclose={() => (confirm = false)}
	>
		<p>
			{t('files.analysis.confirmText', { count: binned.length, bytes: formatBytes(selectedBytes) })}
		</p>
		<ul class="paths">
			{#each binned as path (path)}<li class="mono">{path}</li>{/each}
		</ul>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={removing} onclick={() => (confirm = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={removing} onclick={remove}
				>{t('files.analysis.toTrash')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.analysis {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2-5) var(--space-4);
		border-bottom: 1px solid var(--panel-border, var(--border));
	}
	.crumbs {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px;
		min-width: 0;
	}
	.crumb {
		background: none;
		border: 0;
		color: var(--text);
		font: inherit;
		padding: 2px var(--space-1);
		border-radius: 6px;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.crumb[aria-current='page'] {
		font-weight: 600;
	}
	.sep {
		color: var(--muted);
	}
	.spacer {
		flex: 1;
	}
	.summary {
		margin: 0;
		padding: var(--space-2) var(--space-4);
		font-size: var(--text-sm);
	}
	.tabs {
		display: flex;
		gap: var(--space-1);
		padding: 0 var(--space-4);
		border-bottom: 1px solid var(--panel-border, var(--border));
		overflow-x: auto;
	}
	.tab {
		background: none;
		border: 0;
		padding: var(--space-2-5) var(--space-3);
		font: inherit;
		font-size: var(--text-base);
		color: var(--muted);
		white-space: nowrap;
		cursor: pointer;
	}
	.tab[aria-selected='true'] {
		color: var(--text);
		font-weight: 600;
		box-shadow: inset 0 -2px 0 var(--accent);
	}
	.hint {
		margin: 0;
		padding: var(--space-2-5) var(--space-4) 0;
		font-size: var(--text-sm);
	}
	.map-wrap {
		padding: var(--space-2-5) var(--space-4) var(--space-4);
		min-width: 0;
	}
	.measure {
		width: 100%;
		height: 0;
	}
	.map {
		position: relative;
		width: 100%;
		overflow: hidden;
	}
	.area {
		position: absolute;
		box-sizing: border-box;
		padding: 2px;
	}
	.area-head {
		display: flex;
		gap: var(--space-1-5);
		align-items: baseline;
		width: 100%;
		padding: var(--space-1) var(--space-1) 0;
		background: none;
		border: 0;
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		text-align: left;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.area-head.file {
		cursor: default;
	}
	.area-head span,
	.sub span {
		color: var(--muted);
		font-weight: 400;
	}
	.area::before {
		content: '';
		position: absolute;
		inset: 2px;
		border-radius: 8px;
		border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
		background: color-mix(in srgb, var(--accent) 10%, var(--surface));
		z-index: -1;
	}
	.sub {
		position: absolute;
		box-sizing: border-box;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 2px;
		padding: 5px 7px;
		border-radius: 6px;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		background: color-mix(in srgb, var(--accent) 24%, var(--surface));
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		text-align: left;
		overflow: hidden;
		cursor: pointer;
	}
	.sub b,
	.sub span {
		max-width: 100%;
		overflow-wrap: anywhere;
	}
	.sub.plain {
		background: color-mix(in srgb, var(--accent) 12%, var(--surface));
		cursor: default;
	}
	button.sub:hover,
	button.sub:focus-visible {
		background: color-mix(in srgb, var(--accent) 36%, var(--surface));
	}
	.legend {
		margin-top: 14px;
	}
	.data-table {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-base);
	}
	.data-table :is(th, td) {
		padding: var(--space-2) var(--space-2-5);
		text-align: left;
		border-bottom: 1px solid var(--panel-border, var(--border));
	}
	.data-table th {
		color: var(--muted);
		font-weight: 500;
		font-size: var(--text-sm);
	}
	.num {
		text-align: right !important;
		white-space: nowrap;
	}
	.end {
		text-align: right !important;
	}
	.name {
		overflow-wrap: anywhere;
	}
	.name small {
		display: block;
		font-size: var(--text-sm);
	}
	.when {
		white-space: nowrap;
	}
	.check {
		width: 28px;
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2-5) var(--space-4);
		padding: var(--space-2-5) var(--space-4);
		border-bottom: 1px solid var(--panel-border, var(--border));
	}
	.filters label {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-base);
	}
	.filters .field {
		width: auto;
	}
	.table-wrap {
		overflow-x: auto;
	}
	.foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4);
		border-top: 1px solid var(--panel-border, var(--border));
	}
	.paths {
		max-height: 40vh;
		overflow: auto;
		padding-left: 1.2rem;
	}
	.mono {
		overflow-wrap: anywhere;
	}
	.small {
		min-height: 28px;
		padding: 0 var(--space-2-5);
		font-size: var(--text-sm);
	}
	@media (max-width: 640px) {
		.when {
			display: none;
		}
	}
</style>
