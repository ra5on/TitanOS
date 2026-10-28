<script lang="ts">
	import {
		api,
		ApiError,
		type AppProxyRoute,
		type AppProxyStatus,
		type DockerContainer,
	} from '#lib/api.ts';
	import {
		proxyBody,
		proxyForm,
		proxyProblems,
		suggestedProxyName,
		suggestedProxyPort,
		type ProxyForm,
	} from '#lib/app-proxy.ts';
	import ProxyRouteFields from '#lib/components/ProxyRouteFields.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	// The HTTPS name of an app (e.g. https://immich.nas.local) is saved on
	// its own: it changes nothing in the compose file and recreates no
	// container, so it does not wait for "Apply". What holds for all names
	// (domain, local name, mDNS, the switch for all) is on
	// Settings › HTTPS names for apps.
	let { app, members }: { app: string; members: DockerContainer[] } = $props();

	let status = $state<AppProxyStatus | null>(null);
	let loadError = $state('');
	let form = $state<ProxyForm>(proxyForm(null));
	let saved = $state<AppProxyRoute | null>(null);
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');

	function take(next: AppProxyStatus) {
		status = next;
		saved = next.apps.find((route) => route.app === app) ?? null;
		if (saved) form = proxyForm(saved);
	}

	async function load(current = app) {
		loadError = '';
		try {
			const next = await api.get<AppProxyStatus>('/docker/proxy');
			if (current !== app) return;
			take(next);
			if (!saved) form = proxyForm(null, suggestedProxyName(app));
		} catch (err) {
			if (current === app) loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	$effect(() => {
		void load(app);
	});
	// The port is only suggested while nothing is saved and nothing typed.
	$effect(() => {
		if (status && !saved && !form.port && members.length) form.port = suggestedProxyPort(members);
	});

	const localLabel = $derived(status?.localName || status?.defaultLocalName || '');
	const mdns = $derived(status?.mdns ?? true);
	const problems = $derived(
		status
			? proxyProblems(
					form,
					false,
					[...status.apps.filter((route) => route.app !== app), ...(status.targets ?? [])],
					localLabel,
				)
			: [],
	);
	const changed = $derived(
		!saved ||
			JSON.stringify(proxyBody(form, false)) !== JSON.stringify(proxyBody(proxyForm(saved), false)),
	);

	async function save() {
		if (busy || problems.length) return;
		busy = true;
		error = notice = '';
		try {
			take(
				await api.put<AppProxyStatus>(
					`/docker/proxy/apps/${encodeURIComponent(app)}`,
					proxyBody(form, false),
				),
			);
			notice = t('proxy.saved');
		} catch (err) {
			error =
				err instanceof ApiError && err.code === 'proxy_invalid'
					? err.message
					: errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (busy) return;
		busy = true;
		error = notice = '';
		try {
			take(await api.delete<AppProxyStatus>(`/docker/proxy/apps/${encodeURIComponent(app)}`));
			form = proxyForm(null, suggestedProxyName(app));
			form.port = suggestedProxyPort(members);
			notice = t('proxy.removed');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile" aria-labelledby="h-proxy">
	<h2 class="tile-title" id="h-proxy">{t('proxy.title')}</h2>
	<p class="muted hint">{t('proxy.intro')}</p>
	{#if loadError}
		<div class="notice-error row-note" role="alert">
			<span>{loadError}</span><button type="button" class="btn small" onclick={() => load()}
				>{t('common.retry')}</button
			>
		</div>
	{:else if !status}
		<p class="muted loading" role="status">{t('common.loading')}</p>
	{:else}
		{#if status.off}<p class="notice-warn">{t('proxy.allOffHint')}</p>{/if}
		<ProxyRouteFields
			bind:form
			problems={changed ? problems : []}
			suffixes={status.suffixes}
			{mdns}
			tailnet={status.tailnet}
			disabled={busy}
		/>
		{#if saved?.enabled && !changed && saved.urls.length && !status.off}
			<div class="addresses">
				<span class="muted">{t('proxy.addresses')}</span>
				{#each saved.urls as address (address)}<a
						class="mono"
						href={address}
						target="_blank"
						rel="noreferrer">{address}</a
					>{/each}
			</div>
		{/if}
		<div class="actions">
			<button
				type="button"
				class="btn small btn-primary"
				disabled={busy || !changed || problems.length > 0}
				onclick={save}>{t('common.save')}</button
			>
			{#if saved}<button type="button" class="btn small" disabled={busy} onclick={remove}
					>{t('proxy.remove')}</button
				>{/if}
			{#if notice}<span class="muted" role="status">{notice}</span>{/if}
		</div>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

		<p class="muted hint domain">
			{#if status.domain}{t('proxy.domainCurrent', { suffixes: status.suffixes.join(', ') })}{/if}
			<a href="/settings/proxy">{t('proxy.settingsTitle')}</a>
		</p>
	{/if}
</section>

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
	}
	.hint {
		margin: 0;
		font-size: 0.8125rem;
	}
	.addresses {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 14px;
		font-size: 0.8125rem;
		overflow-wrap: anywhere;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		font-size: 0.8125rem;
	}
	.small {
		height: 2.25rem;
		padding: 0 0.75rem;
		font-size: 0.8125rem;
	}
	.domain {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 10px;
		padding-top: 12px;
		border-top: 1px solid var(--panel-border);
	}
	.row-note {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
</style>
