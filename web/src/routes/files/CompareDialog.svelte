<script lang="ts">
	import { api, type FileRoot, type FileEntry } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatBytes } from '#lib/format.ts';
	let {
		left,
		leftDir,
		right,
		rightDir,
		showHidden,
		onclose,
		onopen,
		ondone,
	}: {
		left: FileRoot;
		leftDir: string;
		right: FileRoot;
		rightDir: string;
		showHidden: boolean;
		onclose: () => void;
		ondone: () => void;
		onopen: (root: string, path: string) => void;
	} = $props();
	type Difference = {
		leftVersion?: string;
		rightVersion?: string;
		path: string;
		state: 'left' | 'right' | 'different';
		Left?: FileEntry;
		Right?: FileEntry;
	};
	let checksum = $state(false),
		busy = $state(false),
		error = $state(''),
		result = $state<CompareResult | null>(null);
	type CompareResult = { entries: Difference[]; same: number; checksum: boolean };
	let direction = $state('right'),
		selected = $state<string[]>([]),
		review = $state(false),
		confirmed = $state(false);
	const source = $derived(direction === 'right' ? left : right),
		target = $derived(direction === 'right' ? right : left);
	const sourceDir = $derived(direction === 'right' ? leftDir : rightDir),
		targetDir = $derived(direction === 'right' ? rightDir : leftDir);
	const chosen = $derived(result?.entries.filter((e) => selected.includes(e.path)) ?? []);
	const replacing = $derived(chosen.some((e) => (direction === 'right' ? !!e.Right : !!e.Left)));
	const eligible = (e: Difference) => {
		const s = direction === 'right' ? e.Left : e.Right,
			d = direction === 'right' ? e.Right : e.Left;
		return !!s && !s.dir && !d?.dir;
	};
	async function copySelected() {
		if (!chosen.length || !target.writable || (replacing && !confirmed)) return;
		busy = true;
		error = '';
		const join = (a: string, b: string) => [a, b].filter(Boolean).join('/');
		try {
			await api.post('/files/copy', {
				root: source.id,
				toRoot: target.id,
				toDir: targetDir,
				paths: chosen.map((e) => join(sourceDir, e.path)),
				move: false,
				conflict: 'skip',
				choices: Object.fromEntries(
					chosen.map((e) => [
						join(sourceDir, e.path),
						{
							action: (direction === 'right' ? e.Right : e.Left) ? 'replace' : 'copy',
							target: join(targetDir, e.path),
							sourceVersion: direction === 'right' ? e.leftVersion : e.rightVersion,
							targetVersion: (direction === 'right' ? e.rightVersion : e.leftVersion) || 'absent',
						},
					]),
				),
			});
			ondone();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	let controller: AbortController | undefined;
	$effect(() => () => controller?.abort());
	async function compare() {
		controller?.abort();
		controller = new AbortController();
		busy = true;
		error = '';
		result = null;
		selected = [];
		review = false;
		confirmed = false;
		const current = controller;
		try {
			result = await api.post<CompareResult>(
				'/files/compare',
				{ root: left.id, path: leftDir, toRoot: right.id, toDir: rightDir, checksum, showHidden },
				current.signal,
			);
		} catch (err) {
			if (!current.signal.aborted) error = apiErrorText(err);
		} finally {
			if (current === controller) busy = false;
		}
	}
	const folder = (dir: string, p: string) =>
		[dir, p.split('/').slice(0, -1).join('/')].filter(Boolean).join('/');
</script>

<Dialog locked={busy} title={t('files.compareFolders')} {onclose} wide>
	<p class="mono">
		{t('files.compareLeft')}: {left.name}/{leftDir}<br />{t('files.compareRight')}: {right.name}/{rightDir}
	</p>
	<label
		><input
			type="checkbox"
			bind:checked={checksum}
			disabled={busy}
			onchange={() => (result = null)}
		/>{t('files.compareChecksum')}</label
	>
	<p class="muted">{t('files.compareHint')}</p>
	<button class="btn btn-primary" disabled={busy} onclick={compare}
		>{t('files.compareStart')}</button
	>
	{#if busy}<p role="status">{t('files.compareRunning')}</p>
		<button
			class="btn"
			onclick={() => {
				controller?.abort();
				busy = false;
			}}>{t('common.cancel')}</button
		>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if result}<p role="status">
			{t('files.compareCount', { count: result.entries.length, same: result.same })}
		</p>
		<label
			>{t('files.copyDirection')}<select
				class="field"
				aria-label={t('files.copyDirection')}
				bind:value={direction}
				disabled={busy}
				onchange={() => {
					selected = [];
					review = false;
					confirmed = false;
				}}
				><option value="right">{t('files.copyToRight')}</option><option value="left"
					>{t('files.copyToLeft')}</option
				></select
			></label
		>
		<p class="muted">{t('files.compareCopyHint')}</p>
		<div class="results">
			{#each result.entries as entry (entry.path)}<div class="result">
					<label class="mono selection"
						><input
							type="checkbox"
							disabled={busy || review || !eligible(entry) || !target.writable}
							checked={selected.includes(entry.path)}
							onchange={(e) => {
								selected = e.currentTarget.checked
									? [...selected, entry.path]
									: selected.filter((p) => p !== entry.path);
								confirmed = false;
							}}
						/>{entry.path}</label
					><span class="chip">{t(`files.compareState.${entry.state}`)}</span>
					<span class="muted"
						>{t('files.compareLeft')}: {entry.Left
							? entry.Left.dir
								? t('files.folder')
								: formatBytes(entry.Left.size)
							: '–'} · {t('files.compareRight')}: {entry.Right
							? entry.Right.dir
								? t('files.folder')
								: formatBytes(entry.Right.size)
							: '–'}</span
					>
					{#if entry.Left}<button
							class="btn"
							onclick={() => onopen(left.id, folder(leftDir, entry.path))}
							>{t('files.openLeft')}</button
						>{/if}
					{#if entry.Right}<button
							class="btn"
							onclick={() => onopen(right.id, folder(rightDir, entry.path))}
							>{t('files.openRight')}</button
						>{/if}
				</div>{/each}
		</div>
		{#if review}<p class="mono">{source.name}/{sourceDir} → {target.name}/{targetDir}</p>
			<ul>
				{#each chosen as entry}<li class="mono">{entry.path}</li>{/each}
			</ul>
			{#if replacing}<label
					><input type="checkbox" bind:checked={confirmed} disabled={busy} />{t(
						'files.replaceConfirm',
					)}</label
				>{/if}
			<div class="dialog-actions">
				<button class="btn" disabled={busy} onclick={() => (review = false)}
					>{t('common.cancel')}</button
				><button
					class:btn-danger={replacing}
					class="btn"
					disabled={busy || !target.writable || (replacing && !confirmed)}
					onclick={copySelected}>{t('files.compareCopyStart')}</button
				>
			</div>
		{:else}<button
				class="btn"
				disabled={busy || !chosen.length || !target.writable}
				onclick={() => (review = true)}>{t('files.compareCopyReview')}</button
			>{/if}{/if}
	<div class="dialog-actions">
		<button class="btn" onclick={onclose}>{t('files.editClose')}</button>
	</div>
</Dialog>

<style>
	.selection {
		width: 100%;
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.results {
		max-height: 50vh;
		overflow: auto;
	}
	.result {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		padding: 12px 0;
		border-top: 1px solid var(--border);
	}
	p {
		overflow-wrap: anywhere;
	}
</style>
