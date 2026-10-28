<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { t, errorText } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';
	import {
		indexSettings,
		indexStateLabel,
		type IndexSettings,
		type IndexStatus,
	} from '#lib/file-index.ts';

	type Answer = {
		status: IndexStatus;
		volumes: string[];
		shares: { name: string; volume: string }[];
		mounted: string[] | null;
	};
	let answer = $state<Answer | null>(null);
	let draft = $state<IndexSettings>(indexSettings(null));
	let busy = $state(false),
		error = $state(''),
		loadError = $state(''),
		saved = $state(false),
		dirty = $state(false);
	let confirm = $state<{ kind: 'index' | 'thumbs'; volume: string } | null>(null);

	function failure(err: unknown) {
		return errorText(err instanceof ApiError ? err.code : 'internal');
	}
	// The status is RAM in titand: reading it touches neither disks nor the stick.
	async function refresh() {
		try {
			const next = await api.get<Answer>('/files/index', AbortSignal.timeout(10000));
			answer = next;
			if (!dirty) draft = indexSettings(next.status);
			loadError = '';
		} catch (err) {
			loadError = failure(err);
		}
	}
	onMount(() => {
		void refresh();
		const timer = setInterval(() => {
			if (!document.hidden && !busy) void refresh();
		}, 5000);
		return () => clearInterval(timer);
	});
	function toggle(list: string[], value: string, on: boolean) {
		dirty = true;
		saved = false;
		return on ? [...list, value] : list.filter((item) => item !== value);
	}
	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		saved = false;
		try {
			const status = await api.put<IndexStatus>('/files/index', draft);
			if (answer) answer.status = status;
			draft = indexSettings(status);
			dirty = false;
			saved = true;
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
	async function run(volume: string) {
		error = '';
		try {
			await api.post('/files/index/run', { volume });
			await refresh();
		} catch (err) {
			error = failure(err);
		}
	}
	async function remove() {
		if (!confirm) return;
		const { kind, volume } = confirm;
		busy = true;
		error = '';
		try {
			await api.delete(
				kind === 'index'
					? `/files/index/${encodeURIComponent(volume)}`
					: `/files/thumbs/${encodeURIComponent(volume)}`,
			);
			confirm = null;
			await refresh();
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('fileIndex.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('fileIndex.intro')}</p>
</PageHead>
{#if loadError}<p class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={refresh}>{t('common.retry')}</button>
	</p>{/if}
{#if answer}
	<form class="tile panel" onsubmit={save} aria-labelledby="index-settings">
		<h2 class="tile-title" id="index-settings">{t('fileIndex.settings')}</h2>
		<div class="option">
			<label class="check"
				><input
					type="checkbox"
					bind:checked={draft.enabled}
					onchange={() => {
						dirty = true;
						saved = false;
					}}
				/>
				{t('fileIndex.enabled')}</label
			>
			<p class="muted hint">{t('fileIndex.enabledHint')}</p>
		</div>
		<fieldset class="divided" disabled={!draft.enabled || busy}>
			<legend>{t('fileIndex.volumes')}</legend>
			{#each answer.volumes as volume (volume)}
				<label class="check"
					><input
						type="checkbox"
						checked={draft.volumes.includes(volume)}
						onchange={(event) =>
							(draft.volumes = toggle(draft.volumes, volume, event.currentTarget.checked))}
					/> <span class="mono">{volume}</span></label
				>
			{:else}<p class="muted">{t('fileIndex.noVolumes')}</p>{/each}
		</fieldset>
		<fieldset disabled={!draft.enabled || busy}>
			<legend>{t('fileIndex.shares')}</legend>
			{#each answer.shares as share (share.name)}
				<label class="check"
					><input
						type="checkbox"
						checked={draft.shares.includes(share.name) || draft.volumes.includes(share.volume)}
						disabled={draft.volumes.includes(share.volume)}
						onchange={(event) =>
							(draft.shares = toggle(draft.shares, share.name, event.currentTarget.checked))}
					/> <span class="mono">{share.name}</span>
					<span class="muted">({share.volume})</span></label
				>
			{:else}<p class="muted">{t('fileIndex.noShares')}</p>{/each}
		</fieldset>
		<fieldset class="divided" disabled={!draft.enabled || busy}>
			<legend>{t('fileIndex.when')}</legend>
			<p class="muted hint">{t('fileIndex.whenHint')}</p>
			<label class="stack time"
				><span>{t('fileIndex.time')}</span>
				<input
					class="field"
					type="time"
					bind:value={draft.time}
					oninput={() => {
						dirty = true;
						saved = false;
					}}
				/></label
			>
			{#if draft.time}<p class="notice-warn">{t('fileIndex.timeWarning')}</p>{/if}
			<div class="option">
				<label class="check"
					><input
						type="checkbox"
						bind:checked={draft.ocr}
						disabled={!answer.status.ocrAvailable}
						onchange={() => {
							dirty = true;
							saved = false;
						}}
					/>
					{t('fileIndex.ocr')}</label
				>
				<p class="muted hint">
					{answer.status.ocrAvailable ? t('fileIndex.ocrHint') : t('fileIndex.ocrMissing')}
				</p>
			</div>
		</fieldset>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={busy || !dirty}>{t('common.save')}</button>
			{#if saved}<span role="status" class="muted">{t('fileIndex.saved')}</span>{/if}
		</div>
	</form>

	<section class="tile panel" aria-labelledby="index-status">
		<h2 class="tile-title" id="index-status">{t('fileIndex.status')}</h2>
		{#each answer.status.indexes as index (index.volume)}
			<div class="row">
				<div class="row-text">
					<div class="row-name">
						<strong class="mono">{index.volume}</strong>
						<span class="chip">{t(indexStateLabel(index.state))}</span>
					</div>
					<p class="muted">
						{#if index.known}{t('fileIndex.files', { count: index.files })} · {formatBytes(
								index.bytes,
							)}{:else}{t('fileIndex.unknown')}{/if}
						{#if index.state === 'running'}
							· {t('fileIndex.progress', {
								scanned: index.scanned,
								updated: index.updated,
							})}{/if}
						· {index.lastRun
							? t('fileIndex.lastRun', { date: formatDateTime(new Date(index.lastRun)) })
							: t('fileIndex.never')}
					</p>
					{#if index.lastError}<p class="notice-warn">{index.lastError}</p>{/if}
					{#if index.truncated}<p class="notice-warn">{t('fileIndex.truncated')}</p>{/if}
				</div>
				<div class="row-actions">
					<button
						class="btn"
						disabled={index.state !== 'idle' || dirty}
						onclick={() => run(index.volume)}>{t('fileIndex.runNow')}</button
					>
					<button
						class="btn btn-danger"
						disabled={index.state === 'running' || busy}
						onclick={() => (confirm = { kind: 'index', volume: index.volume })}
						>{t('fileIndex.remove')}</button
					>
				</div>
			</div>
		{:else}<p class="muted">{t('fileIndex.none')}</p>{/each}
		<p class="muted hint">{t('fileIndex.runHint')}</p>
	</section>

	<section class="tile panel" aria-labelledby="index-thumbs">
		<h2 class="tile-title" id="index-thumbs">{t('fileIndex.thumbs')}</h2>
		<p class="muted hint">{t('fileIndex.thumbsHint')}</p>
		{#if answer.mounted?.length}
			<div class="row-actions">
				{#each answer.mounted as volume (volume)}
					<button class="btn" disabled={busy} onclick={() => (confirm = { kind: 'thumbs', volume })}
						>{t('fileIndex.clearThumbs', { volume })}</button
					>
				{/each}
			</div>
		{/if}
	</section>
{/if}

{#if confirm}
	<Dialog
		title={confirm.kind === 'index' ? t('fileIndex.removeTitle') : t('fileIndex.clearThumbsTitle')}
		locked={busy}
		onclose={() => (confirm = null)}
	>
		<p>
			{confirm.kind === 'index'
				? t('fileIndex.removeText', { volume: confirm.volume })
				: t('fileIndex.clearThumbsText', { volume: confirm.volume })}
		</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirm = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				>{confirm.kind === 'index'
					? t('fileIndex.remove')
					: t('fileIndex.clearThumbsShort')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.option {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.option .hint {
		padding-left: 28px;
	}
	.hint {
		font-size: 0.8125rem;
	}
	.time input {
		max-width: 10rem;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		padding-top: 14px;
		border-top: 1px solid var(--panel-border);
	}
	.row-text {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.row-name {
		display: flex;
		align-items: center;
		gap: 8px;
	}
</style>
