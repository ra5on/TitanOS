<script lang="ts">
	// All HTTPS names in one place (backend package appproxy): what holds for
	// all names, the names of Docker apps and names for targets outside
	// Docker (VMs, containers, devices in the home network).
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
	import PageHead from '#lib/components/PageHead.svelte';
	import ProxyRouteFields from '#lib/components/ProxyRouteFields.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

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
	let removing = $state<{ kind: Kind; route: AppProxyRoute } | null>(null);
	let editing = $state<Editing | null>(null);
	let editError = $state('');

	const apps = $derived(status?.apps ?? []);
	const targets = $derived(status?.targets ?? []);
	// An older titand (before 0.30.0) knows none of the new options.
	const extended = $derived(status?.mdns !== undefined);
	const localLabel = $derived(
		normalLocalName(general.localName) || status?.defaultLocalName || '',
	);
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
		return err instanceof ApiError && (err.code === 'proxy_invalid' || err.code === 'proxy_name_taken')
			? err.code === 'proxy_name_taken'
				? errorText(err.code)
				: err.message
			: errorText(err instanceof ApiError ? err.code : 'internal');
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
		generalError = '';
		try {
			take(await api.put<AppProxyStatus>('/docker/proxy', { off }));
		} catch (err) {
			generalError = failure(err);
		} finally {
			offBusy = false;
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

	function access(route: AppProxyRoute) {
		if (route.networks?.length) return t('proxy.networksChip', { count: route.networks.length });
		return t(route.localOnly ? 'proxy.localChip' : 'proxy.publicChip');
	}
</script>

{#snippet routeRow(kind: Kind, route: AppProxyRoute)}
	<div class="route">
		<label class="check switch"
			><input
				type="checkbox"
				checked={route.enabled}
				disabled={!!busyKey}
				aria-label={t('proxy.toggle', { app: route.app })}
				onchange={(event) => toggle(kind, route, event.currentTarget.checked)}
			/></label
		>
		<div class="route-text">
			<div class="route-name">
				<strong>{kind === 'app' ? route.app : route.name}</strong>
				<span class="chip" class:ok={route.enabled && !status?.off}
					>{t(route.enabled ? 'proxy.on' : 'proxy.off')}</span
				>
				<span class="chip" class:warn={!route.localOnly && !route.networks?.length}
					>{access(route)}</span
				>
				{#if route.short}<span class="chip">{t('proxy.shortChip')}</span>{/if}
				{#if route.scheme === 'https'}<span class="chip"
						>{t(route.verifyTLS ? 'proxy.verifyChip' : 'proxy.noVerifyChip')}</span
					>{/if}
			</div>
			<div class="urls">
				{#each route.urls as url (url)}
					{#if route.enabled && !status?.off}<a
							class="mono"
							href={url}
							target="_blank"
							rel="noreferrer">{url}</a
						>{:else}<span class="mono muted">{url}</span>{/if}
				{/each}
			</div>
			{#if route.aliases?.length}<span class="muted small"
					>{t('proxy.aliasesList')}: <span class="mono">{route.aliases.join(', ')}</span></span
				>{/if}
			<span class="muted small"
				>{t('proxy.target')}:
				<span class="mono">{route.scheme}://{route.host || '127.0.0.1'}:{route.port}</span></span
			>
		</div>
		<div class="row-actions">
			{#if extended}<button
					class="btn"
					type="button"
					disabled={!!busyKey}
					onclick={() => edit(kind, route)}>{t('proxy.editHere')}</button
				>{/if}
			{#if kind === 'app'}<a class="btn" href="/docker/apps/{encodeURIComponent(route.app)}#h-proxy"
					>{t('proxy.edit')}</a
				>{/if}
			<button
				class="btn"
				type="button"
				disabled={!!busyKey}
				onclick={() => {
					error = '';
					removing = { kind, route };
				}}>{t('proxy.remove')}</button
			>
		</div>
	</div>
{/snippet}

<PageHead title={t('proxy.settingsTitle')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('proxy.settingsIntro')}</p>
</PageHead>

{#if loadError}
	<div class="notice-error retry" role="alert">
		<span>{loadError}</span><button class="btn" type="button" onclick={load}
			>{t('common.retry')}</button
		>
	</div>
{:else if !status}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<form class="tile panel" aria-labelledby="proxy-general" onsubmit={saveGeneral}>
		<h2 class="tile-title" id="proxy-general">{t('proxy.generalTitle')}</h2>
		{#if extended}
			<div class="option">
				<label class="check"
					><input
						type="checkbox"
						checked={!status.off}
						disabled={offBusy}
						onchange={(event) => setOff(!event.currentTarget.checked)}
					/><span>{t('proxy.allOn')}</span></label
				>
				<p class="muted hint">{t(status.off ? 'proxy.allOffHint' : 'proxy.allOnHint')}</p>
			</div>
			<div class="divided option">
				<label class="check"
					><input type="checkbox" bind:checked={general.mdns} /><span>{t('proxy.mdns')}</span
					></label
				>
				<p class="muted hint">{t('proxy.mdnsHint')}</p>
			</div>
			<label class="stack"
				><span>{t('proxy.localName')}</span><input
					class="field mono"
					type="text"
					bind:value={general.localName}
					oninput={() => (generalNotice = '')}
					placeholder={status.defaultLocalName}
					spellcheck="false"
					autocapitalize="off"
					disabled={!general.mdns}
				/></label
			>
			<p class="muted hint">
				{t('proxy.localNameHint', { example: `immich.${localLabel || 'nas'}.local` })}
			</p>
			{#if !validLocalName(general.localName)}<p class="notice-warn" role="alert">
					{t('proxy.localNameInvalid')}
				</p>{/if}
		{/if}
		{#if extended}<div class="rule"></div>{/if}
		<label class="stack"
			><span>{t('proxy.domain')}</span><input
				class="field mono"
				type="text"
				bind:value={general.domain}
				oninput={() => (generalNotice = '')}
				placeholder="home.example.org"
				spellcheck="false"
				autocapitalize="off"
			/></label
		>
		<p class="muted hint">{t('proxy.domainHint')}</p>
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
	</form>

	<section class="tile panel" aria-labelledby="proxy-apps">
		<h2 class="tile-title" id="proxy-apps">{t('proxy.appsTitle')}</h2>
		{#each apps as route (route.app)}
			{@render routeRow('app', route)}
		{:else}
			<p class="muted">{t('proxy.appsEmpty')}</p>
			<div class="row-actions"><a class="btn" href="/docker">{t('proxy.openDocker')}</a></div>
		{/each}
		{#if error && !removing}<p class="notice-error" role="alert">{error}</p>{/if}
	</section>

	{#if extended}
		<section class="tile panel" aria-labelledby="proxy-targets">
			<h2 class="tile-title" id="proxy-targets">{t('proxy.targetsTitle')}</h2>
			<p class="muted hint">{t('proxy.targetsIntro')}</p>
			{#each targets as route (route.app)}
				{@render routeRow('target', route)}
			{/each}
			<div class="row-actions">
				<button class="btn" type="button" disabled={!!busyKey} onclick={() => edit('target', null)}
					>{t('proxy.addTarget')}</button
				>
			</div>
		</section>
	{/if}

	<section class="tile panel" aria-labelledby="proxy-how">
		<h2 class="tile-title" id="proxy-how">{t('proxy.howTitle')}</h2>
		<p class="hint">{t('proxy.howLocal')}</p>
		<p class="hint">{t('proxy.howDomain')}</p>
		<p class="hint">{t('proxy.howCert')}</p>
		<div class="row-actions">
			<a class="btn" href="/settings/access#access-cert">{t('proxy.openCert')}</a>
		</div>
	</section>
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
		gap: 10px;
	}
	.hint {
		font-size: 0.875rem;
	}
	.option {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.option .hint {
		padding-left: 28px;
	}
	.small {
		font-size: 0.8125rem;
	}
	.rule {
		border-top: 1px solid var(--panel-border);
	}
	.route {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: center;
		gap: 14px;
		padding-top: 14px;
		border-top: 1px solid var(--panel-border);
	}
	.route-text {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.route-name {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.urls {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 14px;
		font-size: 0.875rem;
		overflow-wrap: anywhere;
	}
	.edit {
		display: flex;
		flex-direction: column;
		gap: 14px;
		min-width: min(36rem, 100%);
	}
	@media (max-width: 760px) {
		.route {
			grid-template-columns: auto minmax(0, 1fr);
		}
		.route .row-actions {
			grid-column: 1 / -1;
		}
	}
</style>
