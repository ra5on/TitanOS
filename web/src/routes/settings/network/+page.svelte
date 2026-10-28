<script lang="ts">
	// Settings › Network: named networks (overview), ports and bonds,
	// diagnostics and remote access. Every change is a draft until it is
	// tested on trial; older single-network configurations arrive already
	// in the multi-network form (`migrated`) and keep working unchanged
	// until such a test is confirmed.
	import { beforeNavigate, goto, replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import TrialBanner from '#lib/components/TrialBanner.svelte';
	import { countdown } from '#lib/trial.ts';
	import {
		blankBond,
		blankNetwork,
		managementURL,
		networkPayload,
		type NASNetwork,
		type NetworkBond,
		type NetworkConfig,
		type NetworkStatus,
	} from '#lib/network.ts';
	import SystemCard from './SystemCard.svelte';
	import NetworkCard from './NetworkCard.svelte';
	import InterfacesTab from './InterfacesTab.svelte';
	import DiagnosticsTab from './DiagnosticsTab.svelte';
	import RemoteTab from './RemoteTab.svelte';
	import NetworkEditor from './NetworkEditor.svelte';
	import BondEditor from './BondEditor.svelte';

	const tabIds = ['overview', 'interfaces', 'diagnostics', 'remote'] as const;
	type Tab = (typeof tabIds)[number];
	const tabFrom = (value: string | null): Tab =>
		tabIds.includes(value as Tab) ? (value as Tab) : 'overview';

	let status = $state<NetworkStatus | null>(null),
		draft = $state<NetworkConfig | null>(null),
		modes = $state<string[]>([]),
		timezone = $state(''),
		error = $state(''),
		busy = $state(false),
		lost = $state(false);
	let tab = $state<Tab>(tabFrom(page.url.searchParams.get('tab'))),
		diagnose = $state(''),
		editor = $state<{ network: NASNetwork; isNew: boolean } | null>(null),
		bondEditor = $state<{ bond: NetworkBond; isNew: boolean } | null>(null),
		removeNetwork = $state<NASNetwork | null>(null),
		removeBond = $state<NetworkBond | null>(null),
		discard = $state(false),
		destination = '';

	const current = $derived(status?.pending ?? status?.config ?? null);
	const dirty = $derived(!!draft && !!current && JSON.stringify(draft) !== JSON.stringify(current));
	const locked = $derived(busy || !!status?.pending);
	const ports = $derived(status?.interfaces ?? []);

	beforeNavigate((n) => {
		if (dirty && !status?.pending) {
			n.cancel();
			if (!n.willUnload) {
				destination = n.to?.url.href ?? '';
				discard = true;
			}
		}
	});

	function problem(e: unknown) {
		error =
			errorText(e instanceof ApiError ? e.code : 'internal') +
			(e instanceof ApiError && e.message ? ' · ' + e.message : '');
	}
	function take(r: { network: NetworkStatus; bondModes: string[]; timezone?: string }) {
		const keep = dirty || !!editor || !!bondEditor;
		status = r.network;
		modes = r.bondModes;
		if (r.timezone !== undefined) timezone = r.timezone;
		if (!keep) draft = structuredClone(r.network.pending ?? r.network.config);
		if (!r.network.pending) lost = false;
	}
	async function load() {
		try {
			take(await api.get('/network'));
		} catch (e) {
			// While a change is on trial the NAS may be unreachable here.
			if (!status) problem(e);
		}
	}
	$effect(() => {
		void load();
		const timer = setInterval(() => {
			if (!document.hidden && !busy) void load();
		}, 3000);
		return () => clearInterval(timer);
	});

	function show(next: Tab) {
		tab = next;
		// Shallow: switching tabs is no navigation that would ask about the draft.
		replaceState(`/settings/network?tab=${next}`, {});
	}
	function reset() {
		draft = current ? structuredClone($state.snapshot(current)) : null;
		error = '';
	}

	async function apply() {
		if (!draft) return;
		busy = true;
		error = '';
		lost = false;
		const body = networkPayload($state.snapshot(draft));
		draft = body;
		// With a new address the answer to this request never arrives.
		const patience = new Promise<'late'>((resolve) => setTimeout(() => resolve('late'), 8000));
		const request = api.put<{ network: NetworkStatus; bondModes: string[] }>('/network', body);
		try {
			const res = await Promise.race([request, patience]);
			if (res === 'late') {
				lost = true;
				request.then(
					(r) => take(r),
					() => {},
				);
			} else {
				status = res.network;
				draft = structuredClone(res.network.pending ?? res.network.config);
			}
		} catch (e) {
			if (e instanceof ApiError && e.code === 'unreachable') lost = true;
			else problem(e);
		} finally {
			busy = false;
		}
	}
	async function settle(kind: 'confirm' | 'revert') {
		busy = true;
		error = '';
		try {
			const res = await api.post<{ network: NetworkStatus; bondModes: string[] }>(
				`/network/${kind}`,
				{},
			);
			status = res.network;
			draft = structuredClone(res.network.pending ?? res.network.config);
		} catch (e) {
			// While bonds or VLANs settle, the answer to "confirm" can be cut
			// off after the NAS stored it: what counts is the state now.
			if (kind === 'confirm' && !(e instanceof ApiError && e.status >= 400)) {
				await load();
				if (status && !status.pending) {
					draft = structuredClone($state.snapshot(status.config));
					return;
				}
			}
			problem(e);
		} finally {
			busy = false;
		}
	}

	function editNetwork(n?: NASNetwork) {
		if (!draft) return;
		editor = n
			? { network: structuredClone($state.snapshot(n)), isNew: false }
			: { network: blankNetwork($state.snapshot(draft), ports), isNew: true };
	}
	function saveNetwork(n: NASNetwork) {
		if (!draft || !editor) return;
		const networks = draft.networks ?? [];
		if (n.management) for (const other of networks) if (other.id !== n.id) other.management = false;
		draft.networks = editor.isNew
			? [...networks, n]
			: networks.map((old) => (old.id === n.id ? n : old));
		editor = null;
	}
	function editBond(b?: NetworkBond) {
		if (!draft) return;
		bondEditor = b
			? { bond: structuredClone($state.snapshot(b)), isNew: false }
			: { bond: blankBond($state.snapshot(draft)), isNew: true };
	}
	function saveBond(b: NetworkBond) {
		if (!draft || !bondEditor) return;
		const bonds = draft.bonds ?? [];
		draft.bonds = bondEditor.isNew
			? [...bonds, b]
			: bonds.map((old) => (old.id === b.id ? b : old));
		bondEditor = null;
	}
	function runDiagnosis(n: NASNetwork) {
		diagnose = n.id;
		show('diagnostics');
	}
	// The live state belongs to what the NAS runs: a network that is only in
	// the draft has none yet.
	const applied = (n: NASNetwork) => !!current?.networks?.some((c) => c.id === n.id);
</script>

<PageHead title={t('netcfg.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('netx.intro')}</p>
	{#snippet actions()}
		<button class="btn btn-primary" disabled={locked || !draft} onclick={() => editNetwork()}
			><Icon name="plus" />{t('netx.add')}</button
		>
	{/snippet}
</PageHead>

{#if status?.pending}
	<TrialBanner
		title={t('netx.trial', { time: countdown(status.pendingSeconds) })}
		hint={t('netx.confirmHint')}
		confirmLabel={t('netx.confirm')}
		revertLabel={t('netx.revert')}
		{busy}
		onconfirm={() => settle('confirm')}
		onrevert={() => settle('revert')}
	>
		{#each status.pending.networks ?? [] as n (n.id)}{#if n.management && managementURL(n, location.href)}<a
					href={managementURL(n, location.href)}>{t('netx.openManagement')}</a
				>{/if}{/each}
	</TrialBanner>
{:else if dirty}
	<section class="notice-warn unsaved" role="status">
		<strong>{t('netx.unsaved')}</strong>
		<p>{t('netcfg.trial')}</p>
		<div class="row-actions">
			<button class="btn" disabled={busy} onclick={reset}>{t('netcfg.discard')}</button><button
				class="btn btn-primary"
				disabled={busy}
				onclick={apply}>{t(busy ? 'netcfg.applying' : 'netx.test')}</button
			>
		</div>
	</section>
{/if}
{#if lost}<p class="notice-warn" role="status">{t('netcfg.lost')}</p>{/if}
{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

{#if status && draft}
	<Tabs
		label={t('netcfg.title')}
		value={tab}
		onchange={show}
		tabs={tabIds.map((id) => ({ id, label: t(`netx.tab.${id}`) }))}
	/>

	{#if tab === 'overview'}
		<SystemCard
			hostname={status.config.hostname}
			{timezone}
			disabled={locked}
			onsaved={(name) => {
				if (draft) draft.hostname = name;
				void load();
			}}
		/>
		<section class="tile panel" aria-labelledby="net-list">
			<h2 class="tile-title" id="net-list">{t('netx.networks')}</h2>
			{#if status.migrated && !dirty && !status.pending}<p class="muted">
					{t('netx.migrated')}
				</p>{/if}
			<div class="cards">
				{#each draft.networks ?? [] as n (n.id)}
					<NetworkCard
						network={n}
						{status}
						applied={applied(n)}
						disabled={locked}
						onedit={() => editNetwork(n)}
						ondiagnose={() => runDiagnosis(n)}
						onremove={() => (removeNetwork = n)}
					/>
				{/each}
			</div>
			<p class="muted">{t('netx.external')}</p>
		</section>
	{:else if tab === 'interfaces'}
		<InterfacesTab
			{draft}
			{ports}
			disabled={locked}
			onaddbond={() => editBond()}
			oneditbond={editBond}
			onremovebond={(b) => (removeBond = b)}
		/>
	{:else if tab === 'diagnostics'}
		<DiagnosticsTab
			networks={status.pending ? [] : (status.config.networks ?? [])}
			pending={!!status.pending}
			bind:selected={diagnose}
		/>
	{:else}
		<RemoteTab />
	{/if}
{/if}

{#if editor && draft}
	<NetworkEditor
		network={editor.network}
		isNew={editor.isNew}
		config={draft}
		{ports}
		onsave={saveNetwork}
		onclose={() => (editor = null)}
	/>
{/if}
{#if bondEditor && draft}
	<BondEditor
		bond={bondEditor.bond}
		isNew={bondEditor.isNew}
		config={draft}
		{ports}
		{modes}
		onsave={saveBond}
		onclose={() => (bondEditor = null)}
	/>
{/if}
{#if removeNetwork}
	<Dialog title={t('netx.remove')} onclose={() => (removeNetwork = null)}>
		<p>{t('netx.removeConfirm', { name: removeNetwork.name })}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (removeNetwork = null)}>{t('common.cancel')}</button
			><button
				class="btn btn-danger"
				onclick={() => {
					const id = removeNetwork?.id;
					if (draft) draft.networks = draft.networks?.filter((n) => n.id !== id);
					removeNetwork = null;
				}}>{t('netx.remove')}</button
			>
		</div>
	</Dialog>
{/if}
{#if removeBond}
	<Dialog title={t('netx.bondRemove')} onclose={() => (removeBond = null)}>
		<p>{t('netx.bondRemoveConfirm', { name: removeBond.id })}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (removeBond = null)}>{t('common.cancel')}</button><button
				class="btn btn-danger"
				onclick={() => {
					const id = removeBond?.id;
					if (draft) draft.bonds = draft.bonds?.filter((b) => b.id !== id);
					removeBond = null;
				}}>{t('netx.bondRemove')}</button
			>
		</div>
	</Dialog>
{/if}
{#if discard}
	<Dialog title={t('netx.unsaved')} onclose={() => (discard = false)}>
		<p>{t('netx.discardText')}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (discard = false)}>{t('common.cancel')}</button><button
				class="btn btn-warn"
				onclick={() => {
					reset();
					discard = false;
					void goto(destination);
				}}>{t('netcfg.discard')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.unsaved {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.unsaved p {
		margin: 0;
	}
	.unsaved .row-actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		justify-content: flex-end;
	}
	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 20rem), 1fr));
		gap: var(--gap);
	}
</style>
