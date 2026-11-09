<script lang="ts">
	import { onMount } from 'svelte';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import { api, type Account, type StorageStatus } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
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
			await api.download(`/activity?${q}`, `titanos-activity-${days}d.${format}`);
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	function describe(entry: ActivityEntry): string {
		const label = activityLabel(entry.action);
		if (label.kind === 'named')
			return label.key.startsWith('lock.')
				? t(label.key as 'lock.activity.locked')
				: t(`activity.do.${label.key}` as 'activity.do.login');
		return t('activity.generic', {
			area: t(`activity.area.${label.area}` as 'activity.area.other'),
			verb: t(`activity.verb.${label.verb}` as 'activity.verb.run'),
		});
	}

	// PLAN 67-1: Ablage › Exportieren in the menu bar of the desktop.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'logs.export': { run: () => void download('csv'), enabled: () => !busy },
		'logs.exportJson': { run: () => void download('json'), enabled: () => !busy },
	});
</script>

<NavigationGuard {dirty} />
<PageHead title={t('ops.activity')}
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
	<!-- Style W4 (0.74): the entries as a dense table, the figures in the status bar. -->
	{#if entries.length}
		<div class="table-wrap">
			<table class="data-table entries">
				<thead
					><tr
						><th>{t('activity.col.time')}</th><th>{t('activity.user')}</th><th
							>{t('activity.col.action')}</th
						><th>{t('activity.col.target')}</th><th>{t('activity.col.result')}</th></tr
					></thead
				>
				<tbody>
					{#each entries as entry, index (index)}
						<tr class:failed={!entry.ok}>
							<td class="nowrap"
								><time datetime={entry.at}>{formatDateTime(new Date(entry.at))}</time></td
							>
							<td class="who"
								>{entry.user || '—'}{#if entry.via !== 'web'}<span class="via"
										>{t(`activity.via.${entry.via}` as 'activity.via.token')}</span
									>{/if}</td
							>
							<td class="what"
								><strong>{describe(entry)}</strong><span class="raw mono"
									>{entry.action}{#if entry.ip}&ensp;·&ensp;{entry.ip}{/if}</span
								></td
							>
							<td class="target" class:none={!entry.target}>{entry.target || '—'}</td>
							<td class="nowrap"
								>{#if !entry.ok && !entry.action.startsWith('login_') && !entry.action.startsWith('ssh_login')}<span
										class="tag"
										>{t('activity.failed')}{#if entry.status}
											({entry.status}){/if}</span
									>{:else if !entry.ok}<span class="tag">{t('activity.failed')}</span>{:else}<span
										class="muted">{t('activity.ok')}</span
									>{/if}</td
							>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<EmptyState inline title={busy ? t('tools.loading') : t('activity.none')} />
	{/if}
	{#if more}<div>
			<button class="btn" disabled={busy} onclick={() => load(true)}>{t('activity.older')}</button>
		</div>{/if}
</section>

{#if status}<StatusBar label={t('activity.statusBar')}>
		<span>{t('activity.statEntries', { count: entries.length })}</span>
		<span class:warn={entries.some((e) => !e.ok)}
			>{t('activity.statFailed')} <b>{entries.filter((e) => !e.ok).length}</b></span
		>
		<span class="end"
			>{status.settings.enabled ? t('activity.statOn') : t('activity.statOff')}{#if status.settings.volume}
				· {status.settings.volume}{/if}</span
		>
	</StatusBar>{/if}

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
		font-size: var(--text-md);
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
		gap: var(--space-3);
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-3) var(--space-4);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		min-width: 0;
		font-size: var(--text-base);
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
		gap: var(--space-2);
		height: 2.75rem;
	}
	.check span {
		color: var(--text);
	}
	time,
	.who {
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.via {
		display: block;
		font-size: var(--text-sm);
	}
	.what {
		min-width: 0;
	}
	.what strong {
		display: block;
	}
	.target {
		overflow-wrap: anywhere;
	}
	.raw {
		display: block;
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.failed strong,
	.tag {
		color: var(--error-text);
	}
	.tag {
		font-weight: 600;
		font-size: var(--text-base);
	}
	.entries .nowrap {
		white-space: nowrap;
	}
	.entries tr.failed td:first-child {
		box-shadow: inset 3px 0 0 var(--error-text);
	}
	.entries .target {
		overflow-wrap: anywhere;
	}
	/* Phone: one entry per block – time and account above, the action below. */
	@media (max-width: 640px) {
		.entries thead {
			display: none;
		}
		.entries tr {
			display: grid;
			grid-template-columns: auto minmax(0, 1fr);
			gap: 2px var(--space-2);
			padding: var(--space-2) 0;
			border-bottom: 1px solid var(--border);
		}
		.entries td {
			padding: 0;
			border: 0;
		}
		.entries td.what,
		.entries td.target {
			grid-column: 1 / -1;
		}
		.entries td.target.none {
			display: none;
		}
	}
</style>
