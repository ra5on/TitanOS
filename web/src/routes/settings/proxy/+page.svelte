<script lang="ts">
	import { page } from '$app/state';
	// All HTTPS names in one place (backend package appproxy): the names of
	// Docker apps and of targets outside Docker (VMs, containers, devices in
	// the home network) first, one line each, then what holds for all names.
	import { onMount } from 'svelte';
	import { api, ApiError, type AppProxyRoute, type AppProxyStatus } from '#lib/api.ts';
	import {
		normalDomain,
		normalLocalName,
		proxyBody,
		proxyForm,
		proxyProblems,
		suggestedProxyName,
		validLocalName,
		validProxyDomain,
		type ProxyForm,
	} from '#lib/app-proxy.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import ProxyRouteFields from '#lib/components/ProxyRouteFields.svelte';
	import { gatedAppURL } from '#lib/app-links.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';

	type Kind = 'app' | 'target';
	// The route being edited; key is null for a new target.
	type Editing = { kind: Kind; key: string | null; form: ProxyForm };

	let status = $state<AppProxyStatus | null>(null);
	let loadError = $state('');
	let busyKey = $state('');
	let error = $state('');
	let general = $state({ domain: '', localName: '', mdns: true });
	let generalBusy = $state(false);
	let generalError = $state('');
	let generalNotice = $state('');
	let offBusy = $state(false);
	let offError = $state('');
	// Routes whose further addresses and target are shown, by kind and key.
	let expanded = $state<Record<string, boolean>>({});
	let removing = $state<{ kind: Kind; route: AppProxyRoute } | null>(null);
	let editing = $state<Editing | null>(null);
	let editError = $state('');
	let tailnetKey = $state('');
	let tailnetBusy = $state(false);
	let tailnetError = $state('');
	let tailnetNotice = $state('');
	let autoBusy = $state(false);
	let autoError = $state('');
	type CAInfo = { available: boolean; name?: string; fingerprint?: string; notAfter?: string };
	let ca = $state<CAInfo | null>(null);
	// A link to a group of this page (#proxy-ca): its content arrives after
	// the browser's own anchor scroll, so bring it into view then.
	const caLoaded = $derived(!!status && !!ca);
	$effect(() => {
		const id = page.url.hash.slice(1);
		if (!caLoaded || !id) return;
		requestAnimationFrame(() => document.getElementById(id)?.scrollIntoView({ block: 'start' }));
	});

	const apps = $derived(status?.apps ?? []);
	const targets = $derived(status?.targets ?? []);
	// An older titand (before 0.30.0) knows none of the new options.
	const extended = $derived(status?.mdns !== undefined);
	const localLabel = $derived(normalLocalName(general.localName) || status?.defaultLocalName || '');
	const savedGeneral = $derived({
		domain: status?.domain ?? '',
		localName: status?.localName ?? '',
		mdns: status?.mdns ?? true,
	});
	const generalChanged = $derived(
		normalDomain(general.domain) !== savedGeneral.domain ||
			normalLocalName(general.localName) !== savedGeneral.localName ||
			general.mdns !== savedGeneral.mdns,
	);
	const generalValid = $derived(
		validProxyDomain(general.domain) && validLocalName(general.localName),
	);
	const unreachable = $derived(!general.mdns && !normalDomain(general.domain));

	function failure(err: unknown) {
		return err instanceof ApiError &&
			(err.code === 'proxy_invalid' || err.code === 'proxy_name_taken')
			? err.code === 'proxy_name_taken'
				? errorText(err.code)
				: `${errorText(err.code)} ${err.message}`
			: apiErrorText(err);
	}
	function take(next: AppProxyStatus) {
		status = next;
		general = {
			domain: next.domain ?? '',
			localName: next.localName ?? '',
			mdns: next.mdns ?? true,
		};
	}
	async function load() {
		loadError = '';
		try {
			take(await api.get<AppProxyStatus>('/docker/proxy'));
		} catch (err) {
			loadError = failure(err);
		}
	}
	onMount(load);
	onMount(() => {
		api.get<CAInfo>('/certs/ca').then(
			(info) => (ca = info),
			() => (ca = { available: false }),
		);
	});

	function path(kind: Kind, key: string) {
		return `/docker/proxy/${kind === 'app' ? 'apps' : 'targets'}/${encodeURIComponent(key)}`;
	}

	async function saveGeneral(event: SubmitEvent) {
		event.preventDefault();
		if (generalBusy || !generalValid) return;
		generalBusy = true;
		generalError = generalNotice = '';
		try {
			take(
				await api.put<AppProxyStatus>('/docker/proxy', {
					domain: normalDomain(general.domain),
					...(extended
						? { localName: normalLocalName(general.localName), mdns: general.mdns }
						: {}),
				}),
			);
			generalNotice = t('proxy.saved');
		} catch (err) {
			generalError = failure(err);
		} finally {
			generalBusy = false;
		}
	}
	async function setOff(off: boolean) {
		if (offBusy) return;
		offBusy = true;
		offError = '';
		try {
			take(await api.put<AppProxyStatus>('/docker/proxy', { off }));
		} catch (err) {
			offError = failure(err);
		} finally {
			offBusy = false;
		}
	}

	async function setAuto(auto: boolean) {
		if (autoBusy) return;
		autoBusy = true;
		autoError = '';
		try {
			take(await api.put<AppProxyStatus>('/docker/proxy', { auto }));
		} catch (err) {
			autoError = failure(err);
		} finally {
			autoBusy = false;
		}
	}

	async function toggle(kind: Kind, route: AppProxyRoute, enabled: boolean) {
		if (busyKey) return;
		busyKey = route.app;
		error = '';
		try {
			take(
				await api.put<AppProxyStatus>(path(kind, route.app), {
					...proxyBody(proxyForm(route), kind === 'target'),
					enabled,
				}),
			);
		} catch (err) {
			error = failure(err);
		} finally {
			busyKey = '';
		}
	}
	async function remove() {
		if (!removing || busyKey) return;
		busyKey = removing.route.app;
		error = '';
		try {
			take(await api.delete<AppProxyStatus>(path(removing.kind, removing.route.app)));
			removing = null;
		} catch (err) {
			error = failure(err);
		} finally {
			busyKey = '';
		}
	}

	function edit(kind: Kind, route: AppProxyRoute | null) {
		editError = '';
		const form = proxyForm(route);
		if (!route) {
			form.enabled = true;
			form.scheme = 'http';
		}
		editing = { kind, key: route?.app ?? null, form };
	}
	// A new target is stored under an id made from its name, unique among targets.
	function newTargetId(name: string) {
		const base = suggestedProxyName(name).slice(0, 32).replace(/-+$/, '') || 'target';
		let id = base;
		for (let i = 2; targets.some((target) => target.app === id); i++)
			id = `${base.slice(0, 29)}-${i}`;
		return id;
	}
	const editProblems = $derived(
		editing && status
			? proxyProblems(
					editing.form,
					editing.kind === 'target',
					[
						...apps.filter((r) => !(editing?.kind === 'app' && r.app === editing.key)),
						...targets.filter((r) => !(editing?.kind === 'target' && r.app === editing.key)),
					],
					localLabel,
				)
			: [],
	);
	async function saveEdit() {
		if (!editing || busyKey || editProblems.length) return;
		const target = editing.kind === 'target';
		const key = editing.key ?? newTargetId(editing.form.name);
		busyKey = key;
		editError = '';
		try {
			take(await api.put<AppProxyStatus>(path(editing.kind, key), proxyBody(editing.form, target)));
			editing = null;
		} catch (err) {
			editError = failure(err);
		} finally {
			busyKey = '';
		}
	}

	// The auth key is sent once and never shown again; "" removes it.
	async function saveTailnetKey(key: string) {
		if (tailnetBusy) return;
		tailnetBusy = true;
		tailnetError = tailnetNotice = '';
		try {
			take(await api.put<AppProxyStatus>('/docker/proxy', { tailscaleKey: key }));
			tailnetKey = '';
			tailnetNotice = t(key ? 'proxy.tailscaleKeySaved' : 'proxy.tailscaleKeyRemoved');
		} catch (err) {
			tailnetError = failure(err);
		} finally {
			tailnetBusy = false;
		}
	}

	function access(route: AppProxyRoute) {
		if (route.networks?.length) return t('proxy.networksChip', { count: route.networks.length });
		return t(route.localOnly ? 'proxy.localChip' : 'proxy.publicChip');
	}
</script>

{#snippet routeRow(kind: Kind, route: AppProxyRoute)}
	{@const name = kind === 'app' ? route.app : route.name}
	{@const live = route.enabled && !status?.off}
	{@const tailnetUrl = route.tailnet?.state === 'running' ? route.tailnet.url : undefined}
	{@const more = route.urls.length - 1 + (tailnetUrl ? 1 : 0)}
	{@const key = `${kind}:${route.app}`}
	<div class="route" class:off={!route.enabled}>
		<label class="check"
			><input
				type="checkbox"
				checked={route.enabled}
				disabled={!!busyKey}
				aria-label={t('proxy.toggle', { app: name })}
				onchange={(event) => toggle(kind, route, event.currentTarget.checked)}
			/></label
		>
		<div class="route-text">
			<div class="route-name">
				<strong>{name}</strong>
				{#if !route.enabled}<span class="chip">{t('proxy.off')}</span>{/if}
				{#if route.auto}<span class="chip">{t('appaccess.autoChip')}</span>{/if}
				{#if !route.localOnly || route.networks?.length}<span
						class="chip"
						class:warn={!route.localOnly && !route.networks?.length}>{access(route)}</span
					>{/if}
				{#if route.tailnet}<span
						class="chip"
						class:ok={route.tailnet.state === 'running'}
						class:warn={['login', 'https-off', 'error'].includes(route.tailnet.state)}
						>Tailscale · {t(
							`proxy.tailnetState.${route.tailnet.state}` as 'proxy.tailnetState.running',
						)}</span
					>{/if}
				{#if route.scheme === 'https' && !route.verifyTLS}<span class="chip"
						>{t('proxy.noVerifyChip')}</span
					>{/if}
			</div>
			<div class="urls">
				{#if route.urls[0]}{@render link(route.urls[0], live, kind === 'app' ? route.app : '')}{/if}
				{#if more > 0}<button
						type="button"
						class="more"
						aria-expanded={expanded[key] ?? false}
						aria-label={t('proxy.allAddresses', { name })}
						onclick={() => (expanded[key] = !expanded[key])}
						>{expanded[key] ? t('proxy.fewerUrls') : t('proxy.moreUrls', { count: more })}</button
					>{/if}
			</div>
			{#if expanded[key]}
				<div class="details">
					{#each route.urls.slice(1) as url (url)}{@render link(
							url,
							live,
							kind === 'app' ? route.app : '',
						)}{/each}
					{#if tailnetUrl}{@render link(tailnetUrl, live, kind === 'app' ? route.app : '')}{/if}
					{#if route.tailnet?.error && route.tailnet.error !== 'plugin'}<span class="muted small"
							>{route.tailnet.error}</span
						>{/if}
					{#if route.short}<span class="muted small">{t('proxy.shortChip')}</span>{/if}
					<span class="muted small"
						>{t('proxy.target')}:
						<span class="mono">{route.scheme}://{route.host || '127.0.0.1'}:{route.port}</span
						></span
					>
				</div>
			{/if}
		</div>
		<Dropdown
			label={t('proxy.actionsFor', { name })}
			icon="more"
			align="right"
			disabled={!!busyKey}
		>
			{#snippet children(close)}
				{#if extended}<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							edit(kind, route);
						}}><Icon name="edit" size={16} /><span>{t('proxy.editHere')}</span></button
					>{/if}
				{#if kind === 'app'}<a
						role="menuitem"
						href="/docker/apps/{encodeURIComponent(route.app)}#h-proxy"
						onclick={close}><Icon name="external" size={16} /><span>{t('proxy.edit')}</span></a
					>{/if}
				{#if !route.auto}
					<hr />
					<button
						type="button"
						role="menuitem"
						class="danger"
						onclick={() => {
							close();
							error = '';
							removing = { kind, route };
						}}><Icon name="trash" size={16} /><span>{t('proxy.remove')}</span></button
					>
				{/if}
			{/snippet}
		</Dropdown>
	</div>
{/snippet}

{#snippet link(url: string, live: boolean, app = '')}
	{#if live}<a
			class="mono"
			href={app ? gatedAppURL(app, url, apiPath) : url}
			target="_blank"
			rel="noreferrer">{url}</a
		>{:else}<span class="mono muted">{url}</span>{/if}
{/snippet}

<PageHead title={t('proxy.settingsTitle')}>
	<p class="muted">{t('proxy.settingsIntro')}</p>
	{#snippet actions()}
		{#if status && extended}<label class="check head-switch" title={t('proxy.allOnHint')}
				><input
					type="checkbox"
					checked={!status.off}
					disabled={offBusy}
					onchange={(event) => setOff(!event.currentTarget.checked)}
				/><span>{t('proxy.allOn')}</span></label
			>{/if}
	{/snippet}
</PageHead>

{#if loadError}
	<div class="notice-error retry" role="alert">
		<span>{loadError}</span><button class="btn" type="button" onclick={load}
			>{t('common.retry')}</button
		>
	</div>
{:else if !status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{:else}
	{#if status.off}<p class="notice-warn" role="status">{t('proxy.allOffHint')}</p>{/if}
	{#if offError}<p class="notice-error" role="alert">{offError}</p>{/if}

	<!-- PLAN 78-1: groups are plain headings. The names stay a list (name,
	     addresses, switch and menu per line); real settings are rows. -->
	<SettingGroup title={t('proxy.appsTitle')} id="proxy-apps">
		{#if status.auto !== undefined}
			<SettingRow
				icon="sparkle"
				title={t('appaccess.autoNames')}
				desc={t('appaccess.autoNamesHint')}
				for="proxy-auto"
			>
				{#snippet control()}
					<input
						id="proxy-auto"
						class="switch"
						type="checkbox"
						role="switch"
						checked={status!.auto}
						disabled={autoBusy || status!.off}
						onchange={(event) => setAuto(event.currentTarget.checked)}
					/>
				{/snippet}
			</SettingRow>
			{#if autoError}<p class="notice-error" role="alert">{autoError}</p>{/if}
		{/if}
		<div class="routes">
			{#each apps as route (route.app)}
				{@render routeRow('app', route)}
			{:else}
				<p class="muted">{t('proxy.appsEmpty')}</p>
				<div class="row-actions"><a class="btn" href="/docker">{t('proxy.openDocker')}</a></div>
			{/each}
		</div>
		{#if error && !removing}<p class="notice-error" role="alert">{error}</p>{/if}
	</SettingGroup>

	{#if extended}
		<SettingGroup title={t('proxy.targetsTitle')} id="proxy-targets" desc={t('proxy.targetsIntro')}>
			{#snippet tools()}
				<button class="btn" type="button" disabled={!!busyKey} onclick={() => edit('target', null)}
					>{t('proxy.addTarget')}</button
				>
			{/snippet}
			{#if targets.length}
				<div class="routes">
					{#each targets as route (route.app)}
						{@render routeRow('target', route)}
					{/each}
				</div>
			{/if}
		</SettingGroup>
	{/if}

	<!-- One Save for the names of all apps (one request, typed text). -->
	<form class="rows" onsubmit={saveGeneral}>
		<SettingGroup title={t('proxy.generalTitle')} id="proxy-general">
			{#if extended}
				<SettingRow icon="home" title={t('proxy.mdns')} desc={t('proxy.mdnsHint')} for="proxy-mdns">
					{#snippet control()}
						<input
							id="proxy-mdns"
							class="switch"
							type="checkbox"
							role="switch"
							bind:checked={general.mdns}
						/>
					{/snippet}
				</SettingRow>
				<SettingRow
					icon="edit"
					title={t('proxy.localName')}
					desc={t('proxy.localNameHint', { example: `immich.${localLabel || 'nas'}.local` })}
					for="proxy-local-name"
				>
					{#snippet control()}
						<input
							id="proxy-local-name"
							class="field mono"
							type="text"
							bind:value={general.localName}
							oninput={() => (generalNotice = '')}
							placeholder={status!.defaultLocalName}
							spellcheck="false"
							autocapitalize="off"
							disabled={!general.mdns}
						/>
					{/snippet}
				</SettingRow>
				{#if !validLocalName(general.localName)}<p class="notice-warn" role="alert">
						{t('proxy.localNameInvalid')}
					</p>{/if}
			{/if}
			<SettingRow
				icon="globe"
				title={t('proxy.domain')}
				desc={t('proxy.domainHint')}
				for="proxy-domain"
			>
				{#snippet control()}
					<input
						id="proxy-domain"
						class="field mono"
						type="text"
						bind:value={general.domain}
						oninput={() => (generalNotice = '')}
						placeholder="home.example.org"
						spellcheck="false"
						autocapitalize="off"
					/>
				{/snippet}
			</SettingRow>
			{#if !validProxyDomain(general.domain)}<p class="notice-warn" role="alert">
					{t('proxy.domainInvalid')}
				</p>{/if}
			{#if extended && unreachable}<p class="notice-warn" role="alert">
					{t('proxy.unreachable')}
				</p>{/if}
			{#if status.suffixes.length}<p class="muted hint">
					{t('proxy.domainCurrent', { suffixes: status.suffixes.join(', ') })}
				</p>{/if}
			{#if generalError}<p class="notice-error" role="alert">{generalError}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={generalBusy || !generalValid || !generalChanged}
					>{t('common.save')}</button
				>
				{#if generalNotice}<span class="muted" role="status">{generalNotice}</span>{/if}
			</div>
		</SettingGroup>
	</form>

	{#if status.tailnet}
		<form
			class="rows"
			onsubmit={(event) => {
				event.preventDefault();
				void saveTailnetKey(tailnetKey.trim());
			}}
		>
			<SettingGroup title={t('proxy.tailnetTitle')} id="proxy-tailnet">
				{#if !status.tailnet.available}<p class="notice-warn">
						{t('proxy.tailscaleNoPlugin')}
					</p>{/if}
				<SettingRow icon="key" title={t('proxy.tailscaleKey')} for="proxy-tailnet-key">
					{#snippet control()}
						<input
							id="proxy-tailnet-key"
							class="field mono"
							type="password"
							autocomplete="off"
							bind:value={tailnetKey}
							placeholder={status!.tailnet!.keySet ? t('proxy.tailscaleKeySet') : 'tskey-auth-…'}
							disabled={tailnetBusy}
						/>
						<button
							class="btn btn-primary"
							disabled={tailnetBusy || !tailnetKey.trim().startsWith('tskey-')}
							>{t('proxy.tailscaleKeySave')}</button
						>
						{#if status!.tailnet!.keySet}<button
								type="button"
								class="btn"
								disabled={tailnetBusy}
								onclick={() => saveTailnetKey('')}>{t('proxy.tailscaleKeyRemove')}</button
							>{/if}
					{/snippet}
				</SettingRow>
				<SettingRow id="proxy-tailnet-help" icon="info" title={t('proxy.tailnetHelp')}>
					<div class="srow-block">
						<p class="hint">{t('proxy.tailnetIntro')}</p>
						<p class="hint">{t('proxy.tailscaleKeyHint')}</p>
					</div>
				</SettingRow>
				{#if tailnetError}<p class="notice-error" role="alert">{tailnetError}</p>{/if}
				{#if tailnetNotice}<p class="muted" role="status">{tailnetNotice}</p>{/if}
			</SettingGroup>
		</form>
	{/if}

	{#if ca}
		<SettingGroup title={t('appaccess.caTitle')} id="proxy-ca" desc={t('appaccess.caIntro')}>
			<p class="muted hint">{t('appaccess.caNotNeeded')}</p>
			{#if ca.available}
				<SettingRow id="proxy-ca-cert" icon="lock" title={ca.name || t('appaccess.caName')} open>
					<div class="srow-block">
						<dl class="facts">
							{#if ca.notAfter}<div>
									<dt>{t('appaccess.caUntil')}</dt>
									<dd>{formatDateTime(new Date(ca.notAfter))}</dd>
								</div>{/if}
							<div>
								<dt>{t('appaccess.caFingerprint')}</dt>
								<dd class="mono fingerprint">{ca.fingerprint}</dd>
							</div>
						</dl>
					</div>
					<div class="srow-block actions">
						<a class="btn btn-primary" href={apiPath('/certs/ca.crt')} download
							>{t('appaccess.caDownloadCrt')}</a
						>
						<a class="btn" href={apiPath('/certs/ca.cer')} download
							>{t('appaccess.caDownloadCer')}</a
						>
					</div>
				</SettingRow>
				<SettingRow id="proxy-ca-how" icon="info" title={t('appaccess.caHowTitle')}>
					<div class="srow-block">
						<dl class="howto">
							{#each ['Windows', 'Mac', 'Ios', 'Android', 'Linux'] as const as os (os)}
								<div>
									<dt>{t(`appaccess.ca${os}Title`)}</dt>
									<dd class="hint">{t(`appaccess.ca${os}`)}</dd>
								</div>
							{/each}
						</dl>
					</div>
				</SettingRow>
				<p class="muted hint">{t('appaccess.caKey')}</p>
			{:else}
				<p class="notice-warn">{t('appaccess.caUnavailable')}</p>
			{/if}
		</SettingGroup>
	{/if}

	<SettingGroup>
		<SettingRow id="proxy-how" icon="info" title={t('proxy.howTitle')}>
			<div class="srow-block">
				<p class="hint">{t('proxy.howLocal')}</p>
				<p class="hint">{t('proxy.howDomain')}</p>
				<p class="hint">{t('proxy.howCert')}</p>
			</div>
			<div class="srow-block actions">
				<a class="btn" href="/settings/access#access-cert">{t('proxy.openCert')}</a>
			</div>
		</SettingRow>
	</SettingGroup>
{/if}

{#if editing && status}
	<Dialog
		title={editing.key === null
			? t('proxy.addTarget')
			: t('proxy.editTitle', { name: editing.kind === 'app' ? editing.key : editing.form.name })}
		locked={!!busyKey}
		onclose={() => (editing = null)}
	>
		<form
			class="edit"
			onsubmit={(event) => {
				event.preventDefault();
				void saveEdit();
			}}
		>
			<ProxyRouteFields
				bind:form={editing.form}
				target={editing.kind === 'target'}
				problems={editProblems}
				suffixes={status.suffixes}
				mdns={status.mdns ?? true}
				tailnet={status.tailnet}
				disabled={!!busyKey}
			/>
			{#if editError}<p class="notice-error" role="alert">{editError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={!!busyKey} onclick={() => (editing = null)}
					>{t('common.cancel')}</button
				>
				<button class="btn btn-primary" disabled={!!busyKey || editProblems.length > 0}
					>{t('common.save')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('proxy.removeTitle')} locked={!!busyKey} onclose={() => (removing = null)}>
		<p>
			{t(removing.kind === 'app' ? 'proxy.removeText' : 'proxy.removeTargetText', {
				app: removing.kind === 'app' ? removing.route.app : removing.route.name,
				name: removing.route.urls[0] ?? removing.route.name,
			})}
		</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={!!busyKey} onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={!!busyKey} onclick={remove}
				>{t('proxy.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.retry {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5);
	}
	.hint {
		font-size: var(--text-md);
	}
	form.rows {
		display: contents;
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	/* The list of names: lines with a hair line between, no box. */
	.routes {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		margin-top: var(--space-2);
	}
	.routes p {
		margin: 0;
	}
	.route > .check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.small {
		font-size: var(--text-base);
	}
	/* Outside a tile, so without the tile's look of checkboxes. */
	.head-switch {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		font-size: var(--text-md);
	}
	.head-switch input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.route {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: start;
		gap: var(--space-1) var(--space-3);
		padding-top: var(--space-3);
		border-top: 1px solid var(--srow-line);
	}
	.route > .check,
	.route > :global(.dropdown) {
		margin-top: 2px;
	}
	.route.off strong {
		color: var(--muted);
	}
	.route-text {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	.route-name {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5) var(--space-2);
	}
	.urls,
	.details {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--space-1) var(--space-3);
		font-size: var(--text-md);
		overflow-wrap: anywhere;
	}
	.details {
		flex-direction: column;
		gap: var(--space-1);
	}
	.more {
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.more:hover {
		text-decoration: underline;
	}
	.facts,
	.howto {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 0;
		font-size: var(--text-md);
	}
	.facts > div {
		display: grid;
		grid-template-columns: minmax(8rem, 12rem) minmax(0, 1fr);
		gap: var(--space-3);
	}
	.facts dt,
	.howto dt {
		color: var(--muted);
	}
	.howto dt {
		font-weight: 600;
		color: var(--text);
	}
	.facts dd,
	.howto dd {
		margin: 0;
		min-width: 0;
	}
	.fingerprint {
		overflow-wrap: anywhere;
	}
	@media (max-width: 640px) {
		.facts > div {
			grid-template-columns: minmax(0, 1fr);
			gap: 2px;
		}
	}
	.edit {
		display: flex;
		flex-direction: column;
		gap: 14px;
		min-width: min(36rem, 100%);
	}
</style>
