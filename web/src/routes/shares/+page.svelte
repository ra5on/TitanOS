<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { beforeNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { api, type Account, type Share } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import {
		sanitizeShareView,
		shareNavigation,
		shareFilesURL,
		visibleShares,
		type ShareView,
	} from '#lib/share-view.ts';
	import ShareDialog from './ShareDialog.svelte';
	import ConnectDialog from './ConnectDialog.svelte';
	import DeleteShareDialog from './DeleteShareDialog.svelte';
	import ShareMenu, { type ShareChoice } from './ShareMenu.svelte';
	import UnlockDialog from './UnlockDialog.svelte';
	import { accessSummary } from './shares-page.ts';
	import { formatDiskBytes } from '#lib/format.ts';

	const canAdminister = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const volumes = $derived((storage.status?.volumes ?? []).map((v) => v.name));
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
	// The name over mDNS works without knowing the address and survives a new DHCP lease.
	const mdnsHost = $derived(
		system.info?.hostname && system.info.hostname !== host ? `${system.info.hostname}.local` : '',
	);
	const account = session.data?.user?.name ?? '';
	const navigation = shareNavigation(account);
	let view = $state(sanitizeShareView(session.shares));
	let query = $state(navigation.query);
	let selected = $state(page.url.searchParams.get('share') ?? navigation.selected);
	let tab = $state<'general' | 'access' | 'options'>('general');
	const detailTabs = ['general', 'access', 'options'] as const;
	$effect(() => {
		const name = page.url.searchParams.get('share'),
			requested = page.url.searchParams.get('tab');
		if (name !== null)
			untrack(() => {
				selected = name;
				query = '';
				view = { ...view, volume: '', protocol: 'all' };
				tab = detailTabs.includes(requested as typeof tab) ? (requested as typeof tab) : 'general';
			});
	});
	let shares = $state<Share[] | null>(null),
		accounts = $state<Account[]>([]);
	let loading = $state(false),
		accountReady = $state(false),
		savingView = $state(false);
	let error = $state(''),
		preferenceError = $state('');
	let purgeStarted = $state('');
	let dialog = $state<
		{ kind: 'create' } | { kind: 'edit' | 'delete' | 'connect'; share: Share } | null
	>(null);
	let menu = $state<{ share: Share; x: number; y: number } | null>(null);
	let disposed = false;
	const shown = $derived(visibleShares(shares ?? [], view, query, i18n.locale));
	const detail = $derived(shares?.find((s) => s.name === selected));
	let unlocking = $state<Share | null>(null);
	const volumeOptions = $derived(
		[
			...new Set([
				...volumes,
				...(shares ?? []).map((s) => s.volume),
				...(view.volume ? [view.volume] : []),
			]),
		].sort(),
	);
	beforeNavigate(() => {
		navigation.query = query;
		navigation.selected = selected;
		navigation.scroll = window.scrollY;
	});
	onMount(() => {
		void load().then(() => {
			if (!disposed)
				requestAnimationFrame(() => {
					if (!disposed) window.scrollTo({ top: navigation.scroll });
				});
		});
		return () => {
			disposed = true;
		};
	});
	// A load asked for while one runs (e.g. right after a save) runs again
	// once that one is done, so its answer is not older than the change.
	let reloadAfter = false;
	async function load(): Promise<void> {
		if (loading) {
			reloadAfter = true;
			return;
		}
		loading = true;
		reloadAfter = false;
		error = '';
		const results = await Promise.allSettled([
			api.get<Share[]>('/shares'),
			canAdminister ? api.get<Account[]>('/users') : Promise.resolve([]),
		]);
		if (disposed) return;
		if (results[0].status === 'fulfilled') shares = results[0].value;
		else error = apiErrorText(results[0].reason);
		if (results[1].status === 'fulfilled') {
			accounts = results[1].value;
			accountReady = true;
		} else {
			error = apiErrorText(results[1].reason);
			accountReady = false;
		}
		loading = false;
		if (reloadAfter) return load();
	}
	async function saveView() {
		if (savingView) return;
		savingView = true;
		preferenceError = '';
		const value = sanitizeShareView($state.snapshot(view));
		try {
			await api.put('/preferences/shares', value);
			if (session.data?.user?.name === account) session.shares = { ...value };
		} catch (err) {
			preferenceError = apiErrorText(err);
		} finally {
			savingView = false;
		}
	}
	function sortBy(column: ShareView['sort']) {
		view.descending = view.sort === column ? !view.descending : false;
		view.sort = column;
		void saveView();
	}
	function chooseShare(name: string) {
		selected = selected === name ? '' : name;
		tab = 'general';
		navigation.selected = selected;
	}
	async function choose(action: () => void) {
		menu = null;
		await tick();
		action();
	}
	function menuChoice(share: Share, choice: ShareChoice) {
		void choose(() => {
			if (choice === 'details') {
				selected = share.name;
				tab = 'general';
			} else if (choice === 'delete') dialog = { kind: 'delete', share };
			else dialog = { kind: choice, share };
		});
	}
	function showMenu(event: MouseEvent, share: Share) {
		const b = (event.currentTarget as HTMLElement).getBoundingClientRect();
		menu = { share, x: b.right, y: b.bottom };
	}
	async function done() {
		dialog = null;
		await load();
	}
	async function deleted(share: Share, purged: boolean) {
		if (purged) purgeStarted = share.name;
		dialog = null;
		if (selected === share.name) selected = '';
		await load();
	}
</script>

<PageHead title={t('shares.title')}>
	{#if shares}<p class="muted">{t('shares.summary', { count: shares.length })}</p>{/if}
	{#snippet actions()}{#if canAdminister}<button
				class="btn btn-primary"
				disabled={!accountReady || volumes.length === 0}
				onclick={() => (dialog = { kind: 'create' })}>{t('shares.new')}</button
			>{/if}{/snippet}
</PageHead>
{#if purgeStarted}<div class="notice-ok" role="status">
		{t('share.deleteDataStarted', { name: purgeStarted })}
		<a href="/tools/tasks">{t('share.deleteDataLink')}</a>
		<button class="btn" onclick={() => (purgeStarted = '')}>{t('common.done')}</button>
	</div>{/if}
{#if error}<div class="notice-error" role="alert">
		{error} <button class="btn" disabled={loading} onclick={load}>{t('common.retry')}</button>
	</div>{/if}
<div class="toolbar">
	<input
		class="field search"
		type="search"
		bind:value={query}
		aria-label={t('shares.search')}
		placeholder={t('shares.search')}
	/>
	<select
		class="field"
		aria-label={t('share.volume')}
		bind:value={view.volume}
		disabled={savingView}
		onchange={saveView}
		><option value="">{t('shares.allVolumes')}</option>{#each volumeOptions as v}<option value={v}
				>{v}</option
			>{/each}</select
	>
	<select
		class="field"
		aria-label={t('share.protocols')}
		bind:value={view.protocol}
		disabled={savingView}
		onchange={saveView}
		><option value="all">{t('shares.allProtocols')}</option><option value="smb">SMB</option><option
			value="nfs">NFS</option
		></select
	>
	<label class="density"
		><input
			type="checkbox"
			bind:checked={view.compact}
			disabled={savingView}
			onchange={saveView}
		/>{t('shares.compact')}</label
	>
	<button class="btn" disabled={loading} onclick={load} aria-label={t('shares.refresh')}
		><Icon name="restart" size={16} /></button
	>
</div>
{#if preferenceError}<div class="notice-error" role="alert">
		{preferenceError}
		<button class="btn" onclick={saveView} disabled={savingView}>{t('common.retry')}</button>
	</div>{/if}
{#if shares === null && loading}<p class="muted loading" role="status">{t('common.loading')}</p>
{:else if shares?.length === 0}<EmptyState
		title={t('shares.empty')}
		text={volumes.length === 0 ? t('shares.needVolume') : t('shares.emptyHint')}
	/>
{:else if shares}
	<div class="share-layout" class:with-detail={!!detail}>
		<section class="tile share-list" class:compact={view.compact} aria-label={t('shares.title')}>
			<div class="table-scroll">
				<table>
					<thead
						><tr>
							<th
								aria-sort={view.sort === 'name'
									? view.descending
										? 'descending'
										: 'ascending'
									: 'none'}
								><button onclick={() => sortBy('name')} disabled={savingView}
									>{t('share.name')}
									{view.sort === 'name' ? (view.descending ? '↓' : '↑') : ''}</button
								></th
							>
							<th
								class="secondary"
								aria-sort={view.sort === 'volume'
									? view.descending
										? 'descending'
										: 'ascending'
									: 'none'}
								><button onclick={() => sortBy('volume')} disabled={savingView}
									>{t('share.volume')}
									{view.sort === 'volume' ? (view.descending ? '↓' : '↑') : ''}</button
								></th
							>
							<th class="protocol">{t('share.protocols')}</th><th class="secondary"
								>{t('shares.access')}</th
							><th>{t('shares.status')}</th><th
								><span class="sr-only">{t('shares.actions')}</span></th
							>
						</tr></thead
					><tbody>
						{#each shown as share (share.name)}
							<tr class:selected={selected === share.name}>
								<td
									><button
										class="share-name"
										aria-expanded={selected === share.name}
										onclick={() => chooseShare(share.name)}>{share.name}</button
									>{#if share.timeMachine.enabled}<small>{t('shares.timeMachineChip')}</small
										>{/if}</td
								>
								<td class="mono secondary">{share.volume}</td><td class="protocol"
									><div class="chips">
										{#if share.smb}<span class="chip">SMB</span>{/if}{#if share.nfs.enabled}<span
												class="chip">NFS</span
											>{/if}
									</div></td
								>
								<td class="secondary"
									>{accessSummary(share)}{#if share.guest}<small class="warning"
											>{t('shares.guest')}</small
										>{/if}</td
								>
								<td
									><span
										class:problem={!share.available ||
											(!share.guest && share.smb && Object.keys(share.access).length === 0)}
										class="state"
										>{!share.available
											? t('shares.unavailable')
											: !share.guest && share.smb && Object.keys(share.access).length === 0
												? t('shares.nobodyChip')
												: t('shares.available')}</span
									></td
								>
								<td
									><button
										class="btn more"
										aria-label={t('shares.actionsFor', { name: share.name })}
										aria-haspopup="menu"
										onclick={(e) => showMenu(e, share)}>⋯</button
									></td
								>
							</tr>
						{:else}<tr><td colspan="6"><EmptyState inline title={t('shares.noMatch')} /></td></tr
							>{/each}
					</tbody>
				</table>
			</div>
			<p class="list-count" role="status">{t('ui.results', { count: shown.length })}</p>
		</section>
		{#if detail}<aside class="tile detail" aria-label={detail.name}>
				<div class="detail-head">
					<h2>{detail.name}</h2>
					<button
						class="btn more"
						aria-label={t('shares.closeDetails')}
						onclick={() => (selected = '')}>×</button
					>
				</div>
				{#if detail.guardLocked}<div class="notice-error locked" role="alert">
						<p>
							{t('shares.guardLocked', {
								time: new Date(detail.guardLocked.at).toLocaleString(),
								user: detail.guardLocked.user,
								files: detail.guardLocked.files,
							})}
						</p>
						<button class="btn small" onclick={() => (unlocking = detail)}
							>{t('shares.guardUnlock')}</button
						>
					</div>{/if}
				<div class="tabs" role="tablist" aria-label={t('shares.details')}>
					{#each detailTabs as id}<button
							role="tab"
							aria-selected={tab === id}
							onclick={() => (tab = id as typeof tab)}>{t(`shares.tab.${id}`)}</button
						>{/each}
				</div>
				<dl>
					{#if tab === 'general'}<dt>{t('share.volume')}</dt>
						<dd>{detail.volume}</dd>
						<dt>{t('shares.path')}</dt>
						<dd class="mono">{detail.path}</dd>
						<dt>{t('shares.status')}</dt>
						<dd>{t(detail.available ? 'shares.available' : 'shares.unavailable')}</dd>
						<dt>{t('shares.quota')}</dt>
						<dd>
							{#if !detail.quota}{t(
									'shares.noQuota',
								)}{:else if detail.quotaStatus?.state === 'active' || detail.quotaStatus?.state === 'applying'}{t(
									'shares.quotaUsed',
									{
										used: formatDiskBytes(detail.quotaStatus.used),
										limit: formatDiskBytes(detail.quota),
									},
								)}<span class="quota-bar" class:full={detail.quotaStatus.used >= detail.quota * 0.9}
									><span
										style:width="{Math.min(100, (100 * detail.quotaStatus.used) / detail.quota)}%"
									></span></span
								>{#if detail.quotaStatus.state === 'applying'}<small class="muted"
										>{t('shares.quotaApplying')}</small
									>{/if}{:else}{formatDiskBytes(detail.quota)} ·
								<span class:problem={detail.quotaStatus?.state === 'error'}
									>{detail.quotaStatus?.state === 'error'
										? t('shares.quotaError', { error: detail.quotaStatus.error ?? '' })
										: t('shares.quotaPending')}</span
								>{/if}
						</dd>
					{:else if tab === 'access'}{#each Object.entries(detail.access).sort( ([a], [b]) => a.localeCompare(b) ) as [name, level]}<dt
								class="mono"
							>
								{name}
							</dt>
							<dd>{t(`share.${level}`)}</dd>{/each}
						<dt>{t('shares.guest')}</dt>
						<dd>{detail.guest ? t('share.ro') : t('share.none')}</dd>
						{#if detail.nfs.enabled}<dt>NFS</dt>
							<dd>{(detail.nfs.clients ?? []).join(', ')}</dd>{/if}
					{:else}<dt>{t('share.recycle')}</dt>
						<dd>
							{detail.recycle.enabled
								? t('shares.recycleChip', { days: detail.recycle.days })
								: t('shares.off')}
						</dd>
						<dt>{t('share.timeMachine')}</dt>
						<dd>
							{detail.timeMachine.enabled
								? detail.timeMachine.sizeGB
									? `${detail.timeMachine.sizeGB} GB`
									: t('shares.on')
								: t('shares.off')}
						</dd>
						<dt>{t('share.previousVersions')}</dt>
						<dd>{t(detail.previousVersions ? 'shares.on' : 'shares.off')}</dd>
						<dt>{t('share.guard')}</dt>
						<dd>{t(detail.guard ? 'shares.on' : 'shares.off')}</dd>
						<dt>{t('share.browseable')}</dt>
						<dd>{t(detail.browseable ? 'shares.on' : 'shares.off')}</dd>{/if}
				</dl>
				{#if tab === 'access' && detail.nfs.enabled}<p class="muted hint">
						{t('share.nfsWarning')}
					</p>{/if}
				<div class="detail-actions">
					<button
						class="btn btn-primary"
						onclick={() => (dialog = { kind: 'connect', share: detail })}
						>{t('shares.connect')}</button
					><a class="btn" href={shareFilesURL(detail.name)}>{t('shares.openFiles')}</a
					>{#if canAdminister}<button
							class="btn"
							disabled={!accountReady}
							onclick={() => (dialog = { kind: 'edit', share: detail })}>{t('shares.edit')}</button
						>{/if}
				</div>
			</aside>{/if}
	</div>
{/if}
{#if menu}
	{@const share = menu.share}
	<ShareMenu
		{share}
		x={menu.x}
		y={menu.y}
		{canAdminister}
		{accountReady}
		onclose={() => (menu = null)}
		onchoose={(choice) => menuChoice(share, choice)}
	/>
{/if}
{#if dialog?.kind === 'create'}<ShareDialog
		{volumes}
		{accounts}
		onclose={() => (dialog = null)}
		ondone={done}
	/>
{:else if dialog?.kind === 'edit'}{#key dialog}<ShareDialog
			share={dialog.share}
			{volumes}
			{accounts}
			onclose={() => (dialog = null)}
			ondone={done}
		/>{/key}
{:else if dialog?.kind === 'connect'}
	<ConnectDialog share={dialog.share} {host} {mdnsHost} onclose={() => (dialog = null)} />
{:else if dialog?.kind === 'delete'}
	{@const share = dialog.share}
	<DeleteShareDialog
		{share}
		onclose={() => (dialog = null)}
		ondeleted={(purged) => deleted(share, purged)}
	/>
{/if}

{#if unlocking}
	<UnlockDialog
		share={unlocking}
		onclose={() => (unlocking = null)}
		ondone={async () => {
			unlocking = null;
			await load();
		}}
	/>
{/if}

<style>
	.locked {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
		margin-bottom: 10px;
	}
	.locked p {
		margin: 0;
	}
	.quota-bar {
		display: block;
		height: 6px;
		margin: 6px 0 2px;
		max-width: 260px;
		border-radius: 3px;
		background: var(--track);
		overflow: hidden;
	}
	.quota-bar > span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.quota-bar.full > span {
		background: var(--warn-text);
	}
	dd .problem {
		color: var(--error-text);
	}
	.toolbar select {
		width: auto;
		max-width: 100%;
	}
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.search {
		width: 16rem;
		max-width: 100%;
	}
	.density {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 0.8125rem;
		margin-left: auto;
	}
	.share-layout {
		display: grid;
		gap: var(--gap);
		align-items: start;
	}
	.with-detail {
		grid-template-columns: minmax(0, 1fr) 300px;
	}
	.share-list {
		min-width: 0;
	}
	.table-scroll {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.875rem;
	}
	th {
		text-align: left;
		font-weight: 500;
		color: var(--muted);
		font-size: 0.75rem;
	}
	th,
	td {
		padding: 18px 14px;
		border-bottom: 1px solid var(--panel-border);
		vertical-align: middle;
	}
	.compact td {
		padding: 10px 14px;
	}
	th button,
	.share-name {
		font: inherit;
		background: none;
		color: inherit;
		border: 0;
		padding: 0;
		text-align: left;
		cursor: pointer;
	}
	.share-name {
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.share-name:hover {
		color: var(--accent);
	}
	tr.selected {
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	tbody tr:hover {
		background: color-mix(in srgb, var(--chip) 60%, transparent);
	}
	small {
		display: block;
		color: var(--muted);
		font-size: 0.75rem;
		margin-top: 3px;
	}
	.warning {
		color: var(--warn-text);
	}
	.chips {
		display: flex;
		gap: 4px;
		flex-wrap: wrap;
	}
	.chip {
		font-size: 0.6875rem;
	}
	.state {
		font-size: 0.75rem;
	}
	.state::before {
		content: '';
		display: inline-block;
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--ok-text);
		margin-right: 6px;
	}
	.state.problem {
		color: var(--warn-text);
	}
	.state.problem::before {
		background: var(--warn-text);
	}
	.more {
		padding: 0;
		width: 2.25rem;
		min-width: 2.25rem;
		height: 2.25rem;
		justify-content: center;
	}
	.list-count {
		padding: 10px 14px;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.detail {
		padding: 16px;
		min-width: 0;
	}
	.detail-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.detail h2 {
		font-size: 1.125rem;
		overflow-wrap: anywhere;
	}
	.tabs {
		display: flex;
		gap: 16px;
		border-bottom: 1px solid var(--panel-border);
		margin: 12px 0;
	}
	.tabs button {
		background: none;
		color: var(--muted);
		border: 0;
		border-bottom: 2px solid transparent;
		padding: 10px 0;
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.tabs button[aria-selected='true'] {
		color: var(--accent);
		border-color: var(--accent);
	}
	dl {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr);
		gap: 12px;
		font-size: 0.8125rem;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		text-align: right;
		overflow-wrap: anywhere;
	}
	.detail-actions {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
		margin-top: 20px;
	}
	.hint {
		font-size: 0.75rem;
		margin-top: 12px;
	}
	.notice-error {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
	}
	.sr-only {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
	}
	@media (max-width: 1100px) {
		.with-detail {
			grid-template-columns: 1fr;
		}
		.detail {
			max-width: none;
		}
	}
	@media (max-width: 760px) {
		.secondary {
			display: none;
		}
		.toolbar .field {
			flex: 1;
		}
		.search {
			width: 100%;
		}
		.density {
			margin-left: 0;
		}
		td,
		th,
		.compact td {
			padding: 10px;
		}
		.state {
			white-space: normal;
		}
		.more {
			min-height: 44px;
		}
		.detail-actions .btn {
			flex: 1;
		}
		.share-name {
			font-size: 0.8125rem;
		}
	}
</style>
