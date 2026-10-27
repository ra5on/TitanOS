<script lang="ts">
	import {
		api,
		ApiError,
		type AppProxyRoute,
		type AppProxyStatus,
		type DockerContainer,
	} from '#lib/api.ts';
	import {
		normalDomain,
		proxyAddresses,
		suggestedProxyName,
		suggestedProxyPort,
		validProxyDomain,
		validProxyName,
	} from '#lib/app-proxy.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	// The HTTPS name of an app (e.g. https://immich.nas.local) is saved on
	// its own: it changes nothing in the compose file and recreates no
	// container, so it does not wait for "Apply".
	let { app, members }: { app: string; members: DockerContainer[] } = $props();

	let status = $state<AppProxyStatus | null>(null);
	let loadError = $state('');
	let form = $state({
		enabled: false,
		name: '',
		port: 0,
		scheme: 'http' as 'http' | 'https',
		localOnly: true,
	});
	let saved = $state<AppProxyRoute | null>(null);
	let domain = $state('');
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');
	let domainBusy = $state(false);
	let domainError = $state('');
	let domainNotice = $state('');

	function take(next: AppProxyStatus) {
		status = next;
		saved = next.apps.find((route) => route.app === app) ?? null;
		domain = next.domain ?? '';
		if (saved)
			form = {
				enabled: saved.enabled,
				name: saved.name,
				port: saved.port,
				scheme: saved.scheme,
				localOnly: !!saved.localOnly,
			};
	}

	async function load(current = app) {
		loadError = '';
		try {
			const next = await api.get<AppProxyStatus>('/docker/proxy');
			if (current !== app) return;
			take(next);
			if (!saved)
				form = {
					enabled: false,
					name: suggestedProxyName(app),
					port: 0,
					scheme: 'http',
					localOnly: true,
				};
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

	const taken = $derived(
		status?.apps.find((route) => route.app !== app && route.name === form.name.trim().toLowerCase())
			?.app ?? '',
	);
	const nameValid = $derived(validProxyName(form.name.trim().toLowerCase()));
	const portValid = $derived(Number.isInteger(form.port) && form.port > 0 && form.port < 65536);
	const changed = $derived(
		!saved ||
			saved.enabled !== form.enabled ||
			saved.name !== form.name.trim().toLowerCase() ||
			saved.port !== form.port ||
			saved.scheme !== form.scheme ||
			!!saved.localOnly !== form.localOnly,
	);
	const addresses = $derived(
		saved && !changed
			? saved.urls
			: proxyAddresses(form.name.trim().toLowerCase(), status?.suffixes ?? []),
	);

	async function save() {
		if (busy || !nameValid || !portValid || taken) return;
		busy = true;
		error = notice = '';
		try {
			take(
				await api.put<AppProxyStatus>(`/docker/proxy/apps/${encodeURIComponent(app)}`, {
					...$state.snapshot(form),
					name: form.name.trim().toLowerCase(),
				}),
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
			form = {
				enabled: false,
				name: suggestedProxyName(app),
				port: suggestedProxyPort(members),
				scheme: 'http',
				localOnly: true,
			};
			notice = t('proxy.removed');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	async function saveDomain() {
		if (domainBusy || !validProxyDomain(domain)) return;
		domainBusy = true;
		domainError = domainNotice = '';
		try {
			take(await api.put<AppProxyStatus>('/docker/proxy', { domain: normalDomain(domain) }));
			domainNotice = t('proxy.saved');
		} catch (err) {
			domainError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			domainBusy = false;
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
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else}
		<label class="check"
			><input type="checkbox" bind:checked={form.enabled} /><span>{t('proxy.enabled')}</span></label
		>
		<div class="grid">
			<label
				><span>{t('proxy.name')}</span><input
					class="field mono"
					type="text"
					bind:value={form.name}
					spellcheck="false"
					autocapitalize="off"
					maxlength="63"
				/>{#if form.name.trim() && !nameValid}<small class="problem" role="alert"
						>{t('proxy.nameInvalid')}</small
					>{:else if taken}<small class="problem" role="alert"
						>{t('proxy.nameTaken', { app: taken })}</small
					>{/if}</label
			>
			<label
				><span>{t('proxy.port')}</span><input
					class="field mono"
					type="number"
					min="1"
					max="65535"
					bind:value={form.port}
				/><small>{t('proxy.portHint')}</small>{#if !portValid}<small class="problem" role="alert"
						>{t('proxy.portInvalid')}</small
					>{/if}</label
			>
			<label
				><span>{t('proxy.scheme')}</span>
				<select class="field" bind:value={form.scheme}
					><option value="http">{t('proxy.scheme.http')}</option><option value="https"
						>{t('proxy.scheme.https')}</option
					></select
				>
			</label>
		</div>
		<label class="check"
			><input type="checkbox" bind:checked={form.localOnly} /><span>{t('proxy.localOnly')}</span
			></label
		>
		{#if !form.localOnly}<p class="notice-warn" role="alert">{t('proxy.localOnlyOff')}</p>{/if}
		{#if addresses.length}
			<div class="addresses">
				<span class="muted"
					>{t(saved?.enabled && !changed ? 'proxy.addresses' : 'proxy.addressesPreview')}</span
				>
				{#each addresses as address (address)}
					{#if saved?.enabled && !changed}<a
							class="mono"
							href={address}
							target="_blank"
							rel="noreferrer">{address}</a
						>{:else}<span class="mono">{address}</span>{/if}
				{/each}
			</div>
		{/if}
		<div class="actions">
			<button
				type="button"
				class="btn small btn-primary"
				disabled={busy || !changed || !nameValid || !portValid || !!taken}
				onclick={save}>{t('common.save')}</button
			>
			{#if saved}<button type="button" class="btn small" disabled={busy} onclick={remove}
					>{t('proxy.remove')}</button
				>{/if}
			{#if notice}<span class="muted" role="status">{notice}</span>{/if}
		</div>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

		<div class="domain">
			<label
				><span>{t('proxy.domain')}</span><input
					class="field mono"
					type="text"
					bind:value={domain}
					placeholder="home.example.org"
					spellcheck="false"
					autocapitalize="off"
				/><small>{t('proxy.domainHint')}</small>{#if !validProxyDomain(domain)}<small
						class="problem"
						role="alert">{t('proxy.domainInvalid')}</small
					>{/if}</label
			>
			<div class="actions">
				<button
					type="button"
					class="btn small"
					disabled={domainBusy ||
						!validProxyDomain(domain) ||
						normalDomain(domain) === (status.domain ?? '')}
					onclick={saveDomain}>{t('proxy.saveDomain')}</button
				>
				{#if domainNotice}<span class="muted" role="status">{domainNotice}</span>{/if}
			</div>
			{#if domainError}<p class="notice-error" role="alert">{domainError}</p>{/if}
		</div>
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
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.problem {
		color: var(--warn-text);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
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
		flex-direction: column;
		gap: 10px;
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
