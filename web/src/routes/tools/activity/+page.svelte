<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	import { onMount } from 'svelte';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import { api, type Account, type StorageStatus } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import { formatDateTime, mbToMiB, mibToMB } from '#lib/format.ts';
	import { operationError } from '#lib/operations.ts';
	import {
		activityLabel,
		type ActivityEntry,
		type ActivityPage,
		type ActivitySettings,
		type ActivityStatus,
	} from '#lib/activity.ts';

	let entries = $state<ActivityEntry[]>([]),
		more = $state(false),
		stored = $state(true),
		status = $state<ActivityStatus | null>(null),
		settings = $state<ActivitySettings | null>(null),
		initialSettings = $state(''),
		storage = $state<StorageStatus | null>(null),
		accounts = $state<Account[]>([]),
		days = $state(7),
		user = $state(''),
		search = $state(''),
		failed = $state(false),
		busy = $state(false),
		error = $state(''),
		saved = $state(false);
	const dirty = $derived(!!settings && JSON.stringify(settings) !== initialSettings);

	function query(before?: string) {
		const q = new URLSearchParams({ days: String(days), limit: '200' });
		if (user) q.set('user', user);
		if (search.trim()) q.set('q', search.trim());
		if (failed) q.set('failed', '1');
		if (before) q.set('before', before);
		return q;
	}
	async function load(older = false) {
		busy = true;
		error = '';
		try {
			const last = older ? entries.at(-1)?.at : undefined;
			const page = await api.get<ActivityPage>(`/activity?${query(last)}`);
			entries = older ? [...entries, ...page.entries] : page.entries;
			more = page.more;
			stored = page.stored;
			status = page.status;
			if (!settings) {
				settings = structuredClone($state.snapshot(page.status.settings));
				initialSettings = JSON.stringify(settings);
			}
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	let searchTimer: ReturnType<typeof setTimeout> | undefined;
	function searchLater() {
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => load(), 300);
	}
	onMount(() => {
		load();
		api
			.get<StorageStatus>('/storage')
			.then((s) => (storage = s))
			.catch(() => {});
		api
			.get<Account[]>('/users')
			.then((a) => (accounts = a))
			.catch(() => {});
		return () => clearTimeout(searchTimer);
	});
	async function save() {
		if (!settings) return;
		busy = true;
		saved = false;
		error = '';
		try {
			status = await api.put<ActivityStatus>('/activity', settings);
			settings = structuredClone($state.snapshot(status.settings));
			initialSettings = JSON.stringify(settings);
			saved = true;
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
		if (!error) load();
	}
	async function download(format: 'csv' | 'json') {
		busy = true;
		error = '';
		try {
			const q = query();
			q.set('format', format);
			const response = await fetch(apiPath(`/activity?${q}`), { credentials: 'same-origin' });
			if (!response.ok)
				throw new Error((await response.json()).error?.message ?? response.statusText);
			const url = URL.createObjectURL(await response.blob());
			const link = document.createElement('a');
			link.href = url;
			link.download = `titanos-activity-${days}d.${format}`;
			link.click();
			setTimeout(() => URL.revokeObjectURL(url), 1000);
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	function describe(entry: ActivityEntry): string {
		const label = activityLabel(entry.action);
		if (label.kind === 'named') return t(`activity.do.${label.key}` as 'activity.do.login');
		return t('activity.generic', {
			area: t(`activity.area.${label.area}` as 'activity.area.other'),
			verb: t(`activity.verb.${label.verb}` as 'activity.verb.run'),
		});
	}
</script>

<NavigationGuard {dirty} />
<PageHead title={t('ops.activity')} parent={{ href: '/tools', label: t('nav.tools') }}
	>{#snippet actions()}<button class="btn" disabled={busy} onclick={() => load()}
			>{t('ops.refresh')}</button
		>{/snippet}</PageHead
>
<p class="muted">{t('activity.hint')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status?.error}<p class="notice-warn">{status.error}</p>{/if}
{#if status?.settings.enabled && !stored}<p class="notice-warn">{t('activity.memoryOnly')}</p>{/if}
{#if status && !status.settings.enabled}<p class="notice-warn">{t('activity.off')}</p>{/if}
{#if status?.dropped}<p class="notice-warn">
		{t('activity.dropped', { count: status.dropped })}
	</p>{/if}

<section class="tile list" aria-labelledby="activity-list">
	<h2 class="tile-title" id="activity-list">{t('activity.entries')}</h2>
	<div class="filters">
		<label
			><span>{t('ops.range')}</span><select class="field" bind:value={days} onchange={() => load()}
				><option value={1}>24 h</option><option value={7}>7 d</option><option value={30}
					>30 d</option
				><option value={90}>90 d</option><option value={365}>365 d</option></select
			></label
		>
		<label
			><span>{t('activity.user')}</span><select
				class="field"
				bind:value={user}
				onchange={() => load()}
				><option value="">{t('activity.everyone')}</option>{#each accounts as a (a.name)}<option
						value={a.name}>{a.name}</option
					>{/each}</select
			></label
		>
		<label class="grow"
			><span>{t('tools.search')}</span><input
				class="field"
				type="search"
				bind:value={search}
				oninput={searchLater}
				maxlength="200"
				spellcheck="false"
			/></label
		>
		<label class="check"
			><input type="checkbox" bind:checked={failed} onchange={() => load()} /><span
				>{t('activity.failedOnly')}</span
			></label
		>
		<button class="btn" disabled={busy} onclick={() => download('csv')}
			>{t('tasks.exportCSV')}</button
		><button class="btn" disabled={busy} onclick={() => download('json')}
			>{t('tasks.exportJSON')}</button
		>
	</div>
	<ul class="entries">
		{#each entries as entry, index (index)}
			<li class:failed={!entry.ok}>
				<time datetime={entry.at}>{formatDateTime(new Date(entry.at))}</time>
				<span class="who"
					>{entry.user || '—'}{#if entry.via !== 'web'}<span class="via"
							>{t(`activity.via.${entry.via}` as 'activity.via.token')}</span
						>{/if}</span
				>
				<span class="what"
					><strong>{describe(entry)}</strong
					>{#if !entry.ok && !entry.action.startsWith('login_') && !entry.action.startsWith('ssh_login')}<span
							class="tag"
							>{t('activity.failed')}{#if entry.status}
								({entry.status}){/if}</span
						>{/if}{#if entry.target}<span class="target">{entry.target}</span>{/if}<span
						class="raw mono"
						>{entry.action}{#if entry.ip}&ensp;·&ensp;{entry.ip}{/if}</span
					></span
				>
			</li>
		{:else}
			<EmptyState inline title={busy ? t('tools.loading') : t('activity.none')} />
		{/each}
	</ul>
	{#if more}<div>
			<button class="btn" disabled={busy} onclick={() => load(true)}>{t('activity.older')}</button>
		</div>{/if}
</section>

{#if settings}<form
		class="tile settings"
		onsubmit={(e) => {
			e.preventDefault();
			save();
		}}
	>
		<h2 class="tile-title">{t('activity.settings')}</h2>
		<label class="check"
			><input type="checkbox" bind:checked={settings.enabled} /><span>{t('ops.enabled')}</span
			></label
		><label
			><span>{t('ops.volume')}</span><select class="field" bind:value={settings.volume}
				><option value="">{t('activity.autoVolume')}</option
				>{#each storage?.volumes ?? [] as volume (volume.name)}<option value={volume.name}
						>{volume.name}</option
					>{/each}</select
			></label
		><label
			><span>{t('ops.days')}</span><select class="field" bind:value={settings.days}
				><option value={30}>30</option><option value={90}>90</option><option value={365}>365</option
				></select
			></label
		><label
			><span>{t('ops.budget')}</span><input
				class="field"
				type="number"
				min="8"
				max="268"
				bind:value={
					() => mibToMB(settings?.maxMiB ?? 0), (mb) => settings && (settings.maxMiB = mbToMiB(mb))
				}
				required
			/></label
		>
		<p class="muted note">
			{status?.volume ? t('activity.storedOn', { volume: status.volume }) : t('activity.noVolume')}
		</p>
		<div class="row">
			<button class="btn btn-primary" disabled={busy || !dirty}>{t('ops.save')}</button
			>{#if saved && !dirty}<span role="status">{t('notify.saved')}</span>{/if}
		</div>
	</form>{/if}

<style>
	section,
	.settings {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: var(--tile-pad);
		margin: var(--gap) 0;
		font-size: 0.875rem;
	}
	.settings {
		flex-flow: row wrap;
		align-items: end;
	}
	.settings .tile-title,
	.settings .note,
	.settings .row {
		flex: 1 1 100%;
		margin: 0;
	}
	.settings label {
		flex: 1 1 150px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 12px 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		min-width: 0;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.grow {
		flex: 1 1 220px;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 8px;
		height: 2.75rem;
	}
	.check span {
		color: var(--text);
	}
	.entries {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.entries li {
		display: grid;
		grid-template-columns: 10.5rem 9rem minmax(0, 1fr);
		gap: 4px 14px;
		padding: 10px 0;
		border-bottom: 1px solid var(--border);
	}
	.entries li:last-child {
		border-bottom: 0;
	}
	time,
	.who {
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.via {
		display: block;
		font-size: 0.75rem;
	}
	.what {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.target {
		overflow-wrap: anywhere;
	}
	.raw {
		color: var(--muted);
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.failed strong,
	.tag {
		color: var(--error-text);
	}
	.tag {
		font-weight: 600;
		font-size: 0.8125rem;
	}
	@media (max-width: 640px) {
		.entries li {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
