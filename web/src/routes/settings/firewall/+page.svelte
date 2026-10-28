<script lang="ts">
	// Settings › Firewall: master switch and the tabs Zones (matrix source ×
	// destination with the policies of the selected cell), Networks (which
	// network is in which zone), Policies (all of them as one list),
	// automatic blocking and the log of dropped packets. Zones, networks and
	// policies go on trial together when the firewall is on; the automatic
	// blocking is saved on its own.
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, type FirewallConfig, type FirewallStatus, type FirewallZoneDef } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import TrialBanner from '#lib/components/TrialBanner.svelte';
	import {
		assignNetwork,
		cloneConfig,
		countdown,
		editCell,
		ownZones,
		policyProblem,
		removeZone,
		newZoneId,
		samePolicies,
		TABS,
		tabFrom,
		zoneProblem,
		type Tab,
	} from '#lib/firewall.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import AutoBlockTab from './AutoBlockTab.svelte';
	import CellPanel from './CellPanel.svelte';
	import NetworksTab from './NetworksTab.svelte';
	import PoliciesTab from './PoliciesTab.svelte';
	import RecentTab from './RecentTab.svelte';
	import ZoneDialog from './ZoneDialog.svelte';
	import ZoneMatrix from './ZoneMatrix.svelte';

	let status = $state<FirewallStatus | null>(null);
	let loadError = $state('');
	let draft = $state<FirewallConfig | null>(null);
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');
	let left = $state(0);
	let tab = $state<Tab>(tabFrom(page.url.searchParams.get('tab'), page.url.hash));
	let selected = $state<{ from: string; to: string } | null>(null);
	let editing = $state<{ zone: FirewallZoneDef; isNew: boolean } | null>(null);

	const services = $derived(status?.services ?? []);
	const base = $derived(status ? (status.pending ?? status.config) : null);
	const dirty = $derived(!!draft && !!base && !samePolicies(draft, base, services));
	const valid = $derived(
		!!draft &&
			draft.cells.every((c) => c.policies.every((p) => policyProblem(p) === '')) &&
			ownZones(draft).every((z) => zoneProblem(z) === ''),
	);
	const locked = $derived(busy || !!status?.pending);
	const trialTab = $derived(tab === 'zones' || tab === 'networks' || tab === 'policies');
	// Apps for app policies: running ones and those policies name.
	const apps = $derived.by(() => {
		const list = (status?.apps ?? []).map((a) => ({ key: a.key, name: a.name }));
		for (const cell of draft?.cells ?? [])
			for (const p of cell.policies)
				if (p.app && !list.some((a) => a.key === p.app)) list.push({ key: p.app, name: p.app.replace(/^container:/, '') });
		return list.toSorted((a, b) => a.name.localeCompare(b.name));
	});

	async function load() {
		try {
			take(await api.get<FirewallStatus>('/firewall'));
			loadError = '';
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	// Takes over the answer; a draft being edited stays as it is.
	function take(next: FirewallStatus) {
		const keep = dirty;
		status = next;
		left = next.pendingSeconds;
		if (!keep || !draft) draft = cloneConfig(next.pending ?? next.config);
		if (selected && draft && !draft.zones.some((z) => z.id === selected?.from)) selected = null;
	}
	$effect(() => {
		void load();
	});
	// Links of the settings search change ?tab= on this page.
	$effect(() => {
		tab = tabFrom(page.url.searchParams.get('tab'), page.url.hash);
	});

	// While a change is on trial: the countdown every second, the state
	// every five (pausing while the page is hidden).
	$effect(() => {
		if (!status?.pending) return;
		const tick = setInterval(() => (left = Math.max(0, left - 1)), 1000);
		const poll = setInterval(() => {
			if (!document.hidden) void load();
		}, 5000);
		return () => {
			clearInterval(tick);
			clearInterval(poll);
		};
	});

	function choose(next: Tab) {
		tab = next;
		goto(`/settings/firewall?tab=${next}`, { replaceState: true });
	}

	async function apply() {
		if (!draft || busy) return;
		busy = true;
		error = notice = '';
		try {
			const next = await api.put<FirewallStatus>('/firewall', $state.snapshot(draft) as Record<string, unknown>);
			draft = null;
			take(next);
			notice = next.pending ? '' : t('ui.saved');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	async function trial(action: 'confirm' | 'revert') {
		if (busy) return;
		busy = true;
		error = notice = '';
		try {
			const next = await api.post<FirewallStatus>(`/firewall/${action}`);
			draft = null;
			take(next);
			notice = t(action === 'confirm' ? 'firewall.confirmed' : 'firewall.reverted');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			void load();
		} finally {
			busy = false;
		}
	}

	function discard() {
		if (base) draft = cloneConfig(base);
		error = '';
	}

	function select(from: string, to: string) {
		if (!draft) return;
		if (selected?.from === from && selected?.to === to) {
			selected = null;
			return;
		}
		editCell(draft, services, from, to);
		selected = { from, to };
	}

	function newZone() {
		if (!draft) return;
		editing = { zone: { id: newZoneId(draft), name: '', networks: [], sources: [] }, isNew: true };
	}
	function saveZone(zone: FirewallZoneDef) {
		if (!draft || !editing) return;
		let target = draft.zones.find((z) => z.id === zone.id);
		if (!target) {
			draft.zones.push({ id: zone.id, name: zone.name, networks: [], sources: [] });
			target = draft.zones[draft.zones.length - 1];
		}
		target.name = zone.name;
		target.sources = [...zone.sources];
		target.networks = [];
		for (const id of zone.networks) assignNetwork(draft, id, zone.id);
		editing = null;
	}
	function deleteZone(id: string) {
		if (!draft) return;
		removeZone(draft, id);
		if (selected && (selected.from === id || selected.to === id)) selected = null;
		editing = null;
	}
	function moveZone(id: string, by: number) {
		if (!draft) return;
		const i = draft.zones.findIndex((z) => z.id === id);
		const j = i + by;
		if (j < 0 || j >= draft.zones.length || draft.zones[j].builtin) return;
		[draft.zones[i], draft.zones[j]] = [draft.zones[j], draft.zones[i]];
	}

	const stateChip = $derived.by((): { text: MessageKey; tone: string } => {
		if (!status) return { text: 'common.loading', tone: '' };
		if (status.error) return { text: 'firewall.stateError', tone: 'error' };
		if (status.pending) return { text: 'firewall.stateTrial', tone: 'warn' };
		if (status.config.enabled) return { text: 'firewall.stateOn', tone: 'ok' };
		if (status.active) return { text: 'firewall.stateBlocksOnly', tone: 'info' };
		return { text: 'firewall.stateOff', tone: '' };
	});
	function zoneLabel(id: string): string {
		if (['home', 'vpn', 'internet', 'docker'].includes(id)) return t(`firewall.zone.${id}` as MessageKey);
		const own = (draft ?? status?.config)?.zones.find((z) => z.id === id) ?? status?.config.zones.find((z) => z.id === id);
		return own?.name || id;
	}
	function zoneText(id: string): string {
		if (['home', 'vpn', 'internet', 'docker'].includes(id)) return t(`firewall.zoneText.${id}` as MessageKey);
		const own = draft?.zones.find((z) => z.id === id);
		return own ? t('firewall.zoneMembers', { networks: own.networks.length, sources: own.sources.length }) : '';
	}
	const selectedTo = $derived(
		selected
			? selected.to === 'nas'
				? t('firewall.toNas')
				: selected.to === 'apps'
					? t('firewall.toApps')
					: zoneLabel(selected.to)
			: '',
	);
</script>

<PageHead title={t('firewall.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if loadError}<div class="notice-error" role="alert">
		{loadError}<button class="btn" onclick={load}>{t('ui.retry')}</button>
	</div>{/if}

{#if status && draft}
	{#if status.pending && trialTab}<TrialBanner
			title={t('firewall.trial', { time: countdown(left) })}
			hint={t('firewall.trialHint')}
			confirmLabel={t('firewall.confirm')}
			revertLabel={t('firewall.revert')}
			{busy}
			onconfirm={() => trial('confirm')}
			onrevert={() => trial('revert')}
		/>{/if}

	<section class="tile panel" aria-labelledby="firewall-main">
		<h2 class="tile-title" id="firewall-main" use:revealTarget>{t('firewall.title')}</h2>
		<div><span class="chip {stateChip.tone}">{t(stateChip.text)}</span></div>
		<label class="check"
			><input type="checkbox" bind:checked={draft.enabled} disabled={locked} /><span>{t('firewall.enable')}</span></label
		>
		<p class="muted">{t('firewall.intro')}</p>
		{#if status.clientAddress}<p class="muted">
				{t('firewall.you', {
					address: status.clientAddress,
					zone: status.clientZone ? zoneLabel(status.clientZone) : '–',
				})}
			</p>{/if}
		{#if status.error}<p class="notice-error" role="alert">{t('firewall.loadError', { detail: status.error })}</p>{/if}
		{#if status.dockerError && status.config.enabled}<p class="notice-warn" role="alert">
				{t('firewall.dockerError', { detail: status.dockerError })}
			</p>{/if}
	</section>

	<Tabs
		tabs={TABS.map((id) => ({
			id,
			label: t(`firewall.tab.${id}` as MessageKey),
			badge: id === 'autoblock' ? status?.blocked.length || undefined : undefined,
		}))}
		value={tab}
		label={t('firewall.title')}
		onchange={choose}
	/>

	{#if tab === 'zones'}
		<section class="tile panel" aria-labelledby="firewall-zones">
			<h2 class="tile-title" id="firewall-zones" use:revealTarget>{t('firewall.zones')}</h2>
			<p class="muted">{t('firewall.matrixText')}</p>
			<ZoneMatrix cfg={draft} {services} {selected} {zoneLabel} {zoneText} onselect={select} />
			<p class="muted small">{t('firewall.notThroughNasText')}</p>
			<p class="muted small">{t('firewall.always')}</p>
		</section>
		{#if selected}<CellPanel
				bind:draft
				from={selected.from}
				to={selected.to}
				fromLabel={zoneLabel(selected.from)}
				toLabel={selectedTo}
				{services}
				{apps}
				{locked}
				hostNetworkApps={(status.apps ?? []).filter((a) => a.hostNetwork).map((a) => a.name)}
				onclose={() => (selected = null)}
			/>{/if}
		<section class="tile panel" aria-labelledby="firewall-zone-list">
			<h2 class="tile-title" id="firewall-zone-list">{t('firewall.zoneList')}</h2>
			<dl class="zones-list">
				<div>
					<dt>{zoneLabel('home')}</dt>
					<dd class="mono">{(status.homeNetworks ?? []).join(', ') || '–'}</dd>
				</div>
				<div>
					<dt>{zoneLabel('vpn')}</dt>
					<dd><span class="mono">{(status.vpnNetworks ?? []).join(', ')}</span> · tailscale0, wt0, ipv64wg</dd>
				</div>
				<div><dt>{zoneLabel('internet')}</dt><dd>{t('firewall.internetText')}</dd></div>
				<div><dt>{zoneLabel('docker')}</dt><dd>{t('firewall.dockerText')}</dd></div>
				{#each ownZones(draft) as zone, i (zone.id)}
					<div class="own">
						<dt>{zone.name || zone.id}</dt>
						<dd>
							<span class="mono"
								>{[...zone.networks.map((n) => n.replace(/^[a-z]+:/, '')), ...zone.sources].join(', ') || '–'}</span
							>
							<span class="buttons"
								><button
									class="btn small"
									type="button"
									disabled={locked || i === 0}
									aria-label={t('firewall.ruleUp')}
									onclick={() => moveZone(zone.id, -1)}>↑</button
								><button
									class="btn small"
									type="button"
									disabled={locked || i === ownZones(draft).length - 1}
									aria-label={t('firewall.ruleDown')}
									onclick={() => moveZone(zone.id, 1)}>↓</button
								><button
									class="btn small"
									type="button"
									disabled={locked}
									onclick={() => (editing = { zone: $state.snapshot(zone), isNew: false })}>{t('firewall.zoneEdit')}</button
								></span
							>
						</dd>
					</div>
				{/each}
			</dl>
			<p class="muted small">{t('firewall.zonesHint')}</p>
			<p class="muted small">{t('firewall.zoneOrder')}</p>
			<p class="muted small">{t('firewall.bypassText')}</p>
			<div class="row-actions start">
				<button class="btn" type="button" disabled={locked || ownZones(draft).length >= 16} onclick={newZone}
					>{t('firewall.zoneAdd')}</button
				>
			</div>
		</section>
	{:else if tab === 'networks'}
		<NetworksTab bind:draft networks={status.networks ?? []} {zoneLabel} {locked} />
	{:else if tab === 'policies'}
		<PoliciesTab bind:draft {services} {apps} {zoneLabel} {locked} />
	{:else if tab === 'autoblock'}
		<AutoBlockTab {status} ontake={take} />
	{:else}
		<RecentTab {status} {zoneLabel} onreload={load} ontake={take} />
	{/if}

	{#if trialTab && (dirty || error || notice)}<div class="save-bar {dirty ? 'notice-warn pinned' : ''}">
			{#if dirty}<p>{t(draft.enabled ? 'firewall.unsavedTrial' : 'firewall.unsaved')}</p>{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if notice}<p role="status">{notice}</p>{/if}
			{#if dirty}<div class="row-actions">
					<button class="btn" disabled={busy} onclick={discard}>{t('common.cancel')}</button
					><button class="btn btn-primary" disabled={busy || !valid} onclick={apply}
						>{t(draft.enabled ? 'firewall.applyTrial' : 'common.save')}</button
					>
				</div>{/if}
		</div>{/if}
{:else if !loadError}<p role="status">{t('common.loading')}</p>{/if}

{#if editing && draft && status}
	<ZoneDialog
		zone={editing.zone}
		isNew={editing.isNew}
		cfg={draft}
		networks={status.networks ?? []}
		{zoneLabel}
		onsave={saveZone}
		onremove={editing.isNew ? undefined : () => deleteZone(editing!.zone.id)}
		onclose={() => (editing = null)}
	/>
{/if}

<style>
	.small {
		font-size: 0.8125rem;
	}
	.zones-list {
		display: grid;
		gap: 6px;
		margin: 0;
	}
	.zones-list div {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 12px;
	}
	.zones-list dt {
		font-weight: 500;
		min-width: 7rem;
	}
	.zones-list dd {
		margin: 0;
		overflow-wrap: anywhere;
		flex: 1 1 12rem;
		display: flex;
		flex-wrap: wrap;
		gap: 4px 12px;
		align-items: center;
	}
	.buttons {
		margin-left: auto;
		white-space: nowrap;
	}
	.buttons .btn + .btn {
		margin-left: 4px;
	}
	.start {
		justify-content: flex-start;
	}
	/* Only unsaved changes stick to the bottom: a plain message has no
	   background of its own and would float over the tables. */
	.save-bar.pinned {
		position: sticky;
		bottom: 12px;
		z-index: 2;
	}
</style>
