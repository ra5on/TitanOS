<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';
	import StandardApps from '#lib/webapps/StandardApps.svelte';
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

	// The status is RAM in titand: reading it touches neither disks nor the stick.
	async function refresh() {
		try {
			const next = await api.get<Answer>('/files/index', AbortSignal.timeout(10000));
			answer = next;
			if (!dirty) draft = indexSettings(next.status);
			loadError = '';
		} catch (err) {
			loadError = apiErrorText(err);
		}
	}
	onMount(() => poll(refresh, { interval: 5000, skip: () => busy }));
	function touch() {
		dirty = true;
		saved = false;
	}
	function toggle(list: string[], value: string, on: boolean) {
		touch();
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
			error = apiErrorText(err);
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
			error = apiErrorText(err);
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
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('fileIndex.title')}>
	<p class="muted">{t('fileIndex.intro')}</p>
</PageHead>
<!-- PLAN 66: per account, also for accounts that do not manage the index. -->
<StandardApps />
{#if loadError}<p class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={refresh}>{t('common.retry')}</button>
	</p>{/if}
{#if answer}
	{@const off = !draft.enabled || busy}
	<!-- PLAN 78-1: the settings of the index are rows of one form with one
	     Save, as before; what is indexed opens inside its row. -->
	<form onsubmit={save}>
		<SettingGroup id="index-settings" title={t('fileIndex.settings')}>
			<SettingRow
				icon="search"
				title={t('fileIndex.enabled')}
				desc={t('fileIndex.enabledHint')}
				for="index-enabled"
			>
				{#snippet control()}
					<input
						id="index-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={draft.enabled}
						onchange={touch}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				id="index-volumes"
				icon="disk"
				title={t('fileIndex.volumes')}
				summary={t('fileIndex.chosen', {
					count: draft.volumes.filter((v) => answer?.volumes.includes(v)).length,
					total: answer.volumes.length,
				})}
			>
				{#each answer.volumes as volume, i (volume)}
					<SettingSub title={volume} for="index-volume-{i}">
						{#snippet control()}
							<input
								id="index-volume-{i}"
								class="switch"
								type="checkbox"
								role="switch"
								disabled={off}
								checked={draft.volumes.includes(volume)}
								onchange={(event) =>
									(draft.volumes = toggle(draft.volumes, volume, event.currentTarget.checked))}
							/>
						{/snippet}
					</SettingSub>
				{:else}<div class="srow-block"><p class="muted">{t('fileIndex.noVolumes')}</p></div>{/each}
			</SettingRow>
			<SettingRow
				id="index-shares"
				icon="folder"
				title={t('fileIndex.shares')}
				summary={t('fileIndex.chosen', {
					count: answer.shares.filter(
						(s) => draft.shares.includes(s.name) || draft.volumes.includes(s.volume),
					).length,
					total: answer.shares.length,
				})}
			>
				{#each answer.shares as share, i (share.name)}
					<SettingSub title={share.name} desc={share.volume} for="index-share-{i}">
						{#snippet control()}
							<input
								id="index-share-{i}"
								class="switch"
								type="checkbox"
								role="switch"
								checked={draft.shares.includes(share.name) || draft.volumes.includes(share.volume)}
								disabled={off || draft.volumes.includes(share.volume)}
								onchange={(event) =>
									(draft.shares = toggle(draft.shares, share.name, event.currentTarget.checked))}
							/>
						{/snippet}
					</SettingSub>
				{:else}<div class="srow-block"><p class="muted">{t('fileIndex.noShares')}</p></div>{/each}
			</SettingRow>
			<SettingRow
				id="index-when"
				icon="clock"
				title={t('fileIndex.when')}
				desc={t('fileIndex.whenHint')}
				summary={draft.time || t('fileIndex.onUse')}
			>
				<SettingSub title={t('fileIndex.time')} for="index-time">
					{#snippet control()}
						<input
							id="index-time"
							class="field"
							type="time"
							disabled={off}
							bind:value={draft.time}
							oninput={touch}
						/>
					{/snippet}
				</SettingSub>
				{#if draft.time}<div class="srow-block">
						<p class="notice-warn">{t('fileIndex.timeWarning')}</p>
					</div>{/if}
			</SettingRow>
			<SettingRow
				icon="fileText"
				title={t('fileIndex.ocr')}
				desc={answer.status.ocrAvailable ? t('fileIndex.ocrHint') : t('fileIndex.ocrMissing')}
				for="index-ocr"
			>
				{#snippet control()}
					<input
						id="index-ocr"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={draft.ocr}
						disabled={off || !answer?.status.ocrAvailable}
						onchange={touch}
					/>
				{/snippet}
			</SettingRow>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy || !dirty}>{t('common.save')}</button>
				{#if saved}<span role="status" class="muted">{t('fileIndex.saved')}</span>{/if}
			</div>
		</SettingGroup>
	</form>

	<SettingGroup id="index-status" title={t('fileIndex.status')} desc={t('fileIndex.runHint')}>
		{#each answer.status.indexes as index (index.volume)}
			<SettingRow icon="disk" title={index.volume}>
				{#snippet badge()}<span class="chip">{t(indexStateLabel(index.state))}</span>{/snippet}
				{#snippet description()}
					<p>
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
				{/snippet}
				{#snippet control()}
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
				{/snippet}
			</SettingRow>
		{:else}<p class="muted">{t('fileIndex.none')}</p>{/each}
	</SettingGroup>

	<SettingGroup id="index-thumbs" title={t('fileIndex.thumbs')}>
		<SettingRow icon="image" title={t('fileIndex.thumbs')} desc={t('fileIndex.thumbsHint')}>
			{#snippet control()}
				{#each answer?.mounted ?? [] as volume (volume)}
					<button class="btn" disabled={busy} onclick={() => (confirm = { kind: 'thumbs', volume })}
						>{t('fileIndex.clearThumbs', { volume })}</button
					>
				{/each}
			{/snippet}
		</SettingRow>
	</SettingGroup>
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
	form .row-actions {
		margin-top: var(--space-2);
	}
	/* The notes of a volume's state stand below its line of facts. */
	:global(.srow-desc) p {
		margin: 0;
	}
	:global(.srow-desc) .notice-warn {
		margin-top: var(--space-1);
	}
</style>
