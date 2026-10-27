<script lang="ts">
	// All HTTPS names of Docker apps in one place (backend package appproxy).
	// A name is set up per app in its editor; here it can be switched,
	// removed and the domain for all apps chosen.
	import { onMount } from 'svelte';
	import { api, ApiError, type AppProxyRoute, type AppProxyStatus } from '#lib/api.ts';
	import { normalDomain, validProxyDomain } from '#lib/app-proxy.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	let status = $state<AppProxyStatus | null>(null);
	let loadError = $state('');
	let busyApp = $state('');
	let error = $state('');
	let domain = $state('');
	let domainBusy = $state(false);
	let domainError = $state('');
	let domainNotice = $state('');
	let removing = $state<AppProxyRoute | null>(null);

	const routes = $derived(
		(status?.apps ?? []).toSorted((a, b) => a.app.localeCompare(b.app)),
	);

	function failure(err: unknown) {
		return err instanceof ApiError && err.code === 'proxy_invalid'
			? err.message
			: errorText(err instanceof ApiError ? err.code : 'internal');
	}
	function take(next: AppProxyStatus) {
		status = next;
		domain = next.domain ?? '';
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

	async function toggle(route: AppProxyRoute, enabled: boolean) {
		if (busyApp) return;
		busyApp = route.app;
		error = '';
		try {
			take(
				await api.put<AppProxyStatus>(`/docker/proxy/apps/${encodeURIComponent(route.app)}`, {
					enabled,
					name: route.name,
					port: route.port,
					scheme: route.scheme,
					localOnly: !!route.localOnly,
				}),
			);
		} catch (err) {
			error = failure(err);
		} finally {
			busyApp = '';
		}
	}
	async function remove() {
		if (!removing || busyApp) return;
		busyApp = removing.app;
		error = '';
		try {
			take(
				await api.delete<AppProxyStatus>(
					`/docker/proxy/apps/${encodeURIComponent(removing.app)}`,
				),
			);
			removing = null;
		} catch (err) {
			error = failure(err);
		} finally {
			busyApp = '';
		}
	}
	async function saveDomain(event: SubmitEvent) {
		event.preventDefault();
		if (domainBusy || !validProxyDomain(domain)) return;
		domainBusy = true;
		domainError = domainNotice = '';
		try {
			take(await api.put<AppProxyStatus>('/docker/proxy', { domain: normalDomain(domain) }));
			domainNotice = t('proxy.saved');
		} catch (err) {
			domainError = failure(err);
		} finally {
			domainBusy = false;
		}
	}
</script>

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
	<section class="tile panel" aria-labelledby="proxy-apps">
		<h2 class="tile-title" id="proxy-apps">{t('proxy.appsTitle')}</h2>
		{#each routes as route (route.app)}
			<div class="route">
				<label class="check switch"
					><input
						type="checkbox"
						checked={route.enabled}
						disabled={!!busyApp}
						aria-label={t('proxy.toggle', { app: route.app })}
						onchange={(event) => toggle(route, event.currentTarget.checked)}
					/></label
				>
				<div class="route-text">
					<div class="route-name">
						<strong>{route.app}</strong>
						<span class="chip" class:ok={route.enabled}
							>{t(route.enabled ? 'proxy.on' : 'proxy.off')}</span
						>
						<span class="chip" class:warn={!route.localOnly}
							>{t(route.localOnly ? 'proxy.localChip' : 'proxy.publicChip')}</span
						>
					</div>
					<div class="urls">
						{#each route.urls as url (url)}
							{#if route.enabled}<a class="mono" href={url} target="_blank" rel="noreferrer"
									>{url}</a
								>{:else}<span class="mono muted">{url}</span>{/if}
						{/each}
					</div>
					<span class="muted small"
						>{t('proxy.target')}:
						<span class="mono">{route.scheme}://127.0.0.1:{route.port}</span></span
					>
				</div>
				<div class="row-actions">
					<a class="btn" href="/docker/apps/{encodeURIComponent(route.app)}#h-proxy"
						>{t('proxy.edit')}</a
					>
					<button
						class="btn"
						type="button"
						disabled={!!busyApp}
						onclick={() => {
							error = '';
							removing = route;
						}}>{t('proxy.remove')}</button
					>
				</div>
			</div>
		{:else}
			<p class="muted">{t('proxy.appsEmpty')}</p>
			<div class="row-actions"><a class="btn" href="/docker">{t('proxy.openDocker')}</a></div>
		{/each}
		{#if error && !removing}<p class="notice-error" role="alert">{error}</p>{/if}
	</section>

	<form class="tile panel" aria-labelledby="proxy-domain" onsubmit={saveDomain}>
		<h2 class="tile-title" id="proxy-domain">{t('proxy.domainTitle')}</h2>
		<label class="stack"
			><span>{t('proxy.domain')}</span><input
				class="field mono"
				type="text"
				bind:value={domain}
				oninput={() => (domainNotice = '')}
				placeholder="home.example.org"
				spellcheck="false"
				autocapitalize="off"
			/></label
		>
		<p class="muted hint">{t('proxy.domainHint')}</p>
		{#if !validProxyDomain(domain)}<p class="notice-warn" role="alert">
				{t('proxy.domainInvalid')}
			</p>{/if}
		{#if status.suffixes.length}<p class="muted hint">
				{t('proxy.domainCurrent', { suffixes: status.suffixes.join(', ') })}
			</p>{/if}
		{#if domainError}<p class="notice-error" role="alert">{domainError}</p>{/if}
		<div class="row-actions">
			<button
				class="btn btn-primary"
				disabled={domainBusy ||
					!validProxyDomain(domain) ||
					normalDomain(domain) === (status.domain ?? '')}>{t('proxy.saveDomain')}</button
			>
			{#if domainNotice}<span class="muted" role="status">{domainNotice}</span>{/if}
		</div>
	</form>

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

{#if removing}
	<Dialog title={t('proxy.removeTitle')} locked={!!busyApp} onclose={() => (removing = null)}>
		<p>
			{t('proxy.removeText', {
				app: removing.app,
				name: removing.urls[0] ?? removing.name,
			})}
		</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={!!busyApp} onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={!!busyApp} onclick={remove}
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
	.small {
		font-size: 0.8125rem;
	}
	.route {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto;
		align-items: center;
		gap: 14px;
		padding-top: 14px;
		border-top: 1px solid var(--panel-border);
	}
	.route:first-of-type {
		padding-top: 0;
		border-top: 0;
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
	@media (max-width: 760px) {
		.route {
			grid-template-columns: auto minmax(0, 1fr);
		}
		.route .row-actions {
			grid-column: 1 / -1;
		}
	}
</style>
