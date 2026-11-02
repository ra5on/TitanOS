<script lang="ts">
	// NVR views: live pictures in a grid of six columns. A cell no camera
	// covers stays empty. Several views can be saved; the live pictures run
	// only while this page is open (at most six at a time).
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';
	import { freeSpot, tileFits, VIEW_COLUMNS, type NvrState, type Tile, type View } from '../nvr.ts';

	let nvr = $state<NvrState | null>(null);
	let views = $state<View[]>([]);
	let current = $state('');
	let editing = $state(false);
	let error = $state('');
	let busy = $state(false);

	async function load() {
		try {
			nvr = await api.get<NvrState>('/nvr');
			views = structuredClone($state.snapshot(nvr.views));
			if (!views.some((v) => v.id === current)) current = views[0]?.id ?? '';
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	onMount(() => void load());

	const view = $derived(views.find((v) => v.id === current));
	const names = $derived(new Map((nvr?.cameras ?? []).map((c) => [c.id, c.name])));
	const rows = $derived(view ? Math.max(1, ...view.tiles.map((tl) => tl.y + tl.h)) : 1);
	const live = $derived(view ? view.tiles.slice(0, 6) : []);

	async function save() {
		busy = true;
		error = '';
		try {
			nvr = await api.put<NvrState>('/nvr/views', { views: $state.snapshot(views) });
			views = structuredClone($state.snapshot(nvr.views));
			editing = false;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}

	function addView() {
		const id = `new-${Date.now()}`;
		views.push({ id, name: t('nvr.viewNew'), tiles: [] });
		current = id;
		editing = true;
	}
	function removeView() {
		views = views.filter((v) => v.id !== current);
		current = views[0]?.id ?? '';
		void save();
	}
	function addTile(camera: string) {
		if (!view) return;
		const spot = freeSpot(view.tiles, 2, 2) ?? freeSpot(view.tiles, 1, 1);
		if (!spot) {
			error = t('nvr.viewFull');
			return;
		}
		const w = freeSpot(view.tiles, 2, 2) ? 2 : 1;
		view.tiles.push({ camera, x: spot.x, y: spot.y, w, h: w });
	}
	function change(tile: Tile, key: 'x' | 'y' | 'w' | 'h', value: number) {
		if (!view) return;
		const next = { ...tile, [key]: value };
		if (
			tileFits(
				next,
				view.tiles.filter((o) => o !== tile),
			)
		)
			Object.assign(tile, next);
		else error = t('nvr.tileNoFit');
	}
	const range = (n: number) => Array.from({ length: n }, (_, i) => i);
</script>

<PageHead title={t('nvr.viewsTitle')} parent={{ href: '/nvr', label: t('nvr.title') }}>
	<p class="muted">{t('nvr.viewsIntro')}</p>
	{#snippet actions()}
		<button type="button" class="btn" onclick={addView}>{t('nvr.viewAdd')}</button>
	{/snippet}
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

{#if nvr && views.length === 0}
	<EmptyState title={t('nvr.viewsEmptyTitle')} text={t('nvr.viewsEmptyText')} />
{/if}

{#if view}
	<div class="tabs" role="tablist">
		{#each views as v (v.id)}
			<button
				type="button"
				role="tab"
				class="btn"
				aria-selected={v.id === current}
				class:active={v.id === current}
				onclick={() => {
					current = v.id;
					editing = false;
				}}>{v.name}</button
			>
		{/each}
	</div>

	<section class="tile panel">
		<div class="viewbar">
			<strong>{view.name}</strong>
			<div class="row-actions">
				<button type="button" class="btn" onclick={() => (editing = !editing)}
					>{editing ? t('nvr.viewDone') : t('nvr.viewEdit')}</button
				>
			</div>
		</div>

		<div class="grid" style:grid-template-rows={`repeat(${rows}, 120px)`}>
			{#each view.tiles as tile, i (i)}
				<div
					class="cell"
					style:grid-column={`${tile.x + 1} / span ${tile.w}`}
					style:grid-row={`${tile.y + 1} / span ${tile.h}`}
				>
					{#if !editing && i < 6}
						<img
							src={`/api/v1/nvr/cameras/${tile.camera}/live`}
							alt={names.get(tile.camera) ?? ''}
						/>
					{/if}
					<span class="label">{names.get(tile.camera) ?? '?'}</span>
				</div>
			{/each}
		</div>
		{#if view.tiles.length > live.length}<p class="muted small">{t('nvr.viewLiveLimit')}</p>{/if}

		{#if editing}
			<div class="editor">
				<label
					><span>{t('nvr.viewName')}</span>
					<input class="field" bind:value={view.name} maxlength="60" /></label
				>
				{#each view.tiles as tile, i (i)}
					<div class="row">
						<strong>{names.get(tile.camera) ?? '?'}</strong>
						{#each [['x', t('nvr.tileX'), VIEW_COLUMNS - 1], ['y', t('nvr.tileY'), 11], ['w', t('nvr.tileW'), VIEW_COLUMNS], ['h', t('nvr.tileH'), 12]] as const as [key, label, max] (key)}
							<label
								><span class="small">{label}</span>
								<select
									class="field"
									value={tile[key]}
									onchange={(e) => change(tile, key, Number(e.currentTarget.value))}
								>
									{#each range(key === 'w' || key === 'h' ? max : max + 1) as n (n)}
										{@const v = key === 'w' || key === 'h' ? n + 1 : n}
										<option value={v}>{key === 'x' || key === 'y' ? v + 1 : v}</option>
									{/each}
								</select>
							</label>
						{/each}
						<button
							type="button"
							class="btn btn-danger-outline"
							onclick={() => view.tiles.splice(i, 1)}>{t('nvr.tileRemove')}</button
						>
					</div>
				{/each}
				<label
					><span>{t('nvr.tileAdd')}</span>
					<select
						class="field"
						value=""
						onchange={(e) => {
							if (e.currentTarget.value) addTile(e.currentTarget.value);
							e.currentTarget.value = '';
						}}
					>
						<option value="">{t('nvr.tileChoose')}</option>
						{#each nvr?.cameras ?? [] as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
					</select>
				</label>
				<div class="row-actions">
					<button type="button" class="btn btn-primary" onclick={save} disabled={busy}
						>{t('common.save')}</button
					>
					<button type="button" class="btn btn-danger-outline" onclick={removeView} disabled={busy}
						>{t('nvr.viewRemove')}</button
					>
				</div>
			</div>
		{/if}
	</section>
{/if}

<style>
	.tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.tabs .active {
		outline: 2px solid var(--accent, currentColor);
	}
	.viewbar {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-bottom: 12px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(6, minmax(0, 1fr));
		gap: 6px;
	}
	.cell {
		position: relative;
		background: #000;
		border-radius: 8px;
		overflow: hidden;
		min-height: 0;
	}
	.cell img {
		width: 100%;
		height: 100%;
		object-fit: contain;
		display: block;
	}
	.label {
		position: absolute;
		left: 6px;
		bottom: 6px;
		padding: 2px 6px;
		border-radius: 4px;
		background: rgb(0 0 0 / 0.6);
		color: #fff;
		font-size: var(--text-sm, 12px);
	}
	.editor {
		display: grid;
		gap: 12px;
		margin-top: 16px;
	}
	.editor .row {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: end;
	}
	.editor .row > strong {
		min-width: 120px;
	}
	label {
		display: grid;
		gap: 4px;
	}
</style>
