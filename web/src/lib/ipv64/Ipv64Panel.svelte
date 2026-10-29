<script lang="ts">
	// What IPv64.net offers for the updater: picking and creating domains
	// and the healthcheck that watches whether the NAS is reachable. Account,
	// records and everything else are under Settings › IPv64. Every call
	// counts against the limits of the account, so nothing is fetched by
	// itself: each part loads on a click.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { IPV64_ZONES, validIpv64Wish } from '#lib/dns-providers.ts';
	import Heartbeat from '#lib/ddns/Heartbeat.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { keyBody, type DdnsStatus, type Ipv64Domain } from '#lib/ddns/ddns.ts';

	let {
		status,
		apiKey,
		hostnames,
		onhost,
		onstatus,
	}: {
		status: DdnsStatus;
		apiKey: string;
		hostnames: string[];
		onhost: (name: string) => void;
		onstatus: (next: DdnsStatus) => void;
	} = $props();

	const keyKnown = $derived(!!apiKey.trim() || !!status.secrets.token);
	const body = (extra: Record<string, unknown> = {}) => keyBody(apiKey, extra);
	function failure(err: unknown): string {
		if (err instanceof ApiError && err.code.startsWith('ipv64_') && err.code !== 'ipv64_no_key')
			return `${errorText(err.code)} ${err.message}`;
		return errorText(err instanceof ApiError ? err.code : 'internal');
	}

	// --- domains ---
	let domains = $state<Ipv64Domain[] | null>(null);
	let zones = $state<string[]>(IPV64_ZONES);
	let domainsError = $state('');
	let domainsBusy = $state(false);
	let wish = $state('');
	let zone = $state('ipv64.de');
	let creating = $state(false);
	let proxyDomain = $state<string | null>(null);
	let notice = $state('');
	async function loadDomains() {
		domainsBusy = true;
		domainsError = '';
		try {
			const answer = await api.post<{ domains: Ipv64Domain[]; zones: string[] }>('/network/ipv64/domains', body());
			domains = answer.domains;
			if (answer.zones?.length) zones = answer.zones;
		} catch (err) {
			domainsError = failure(err);
		} finally {
			domainsBusy = false;
		}
	}
	const newName = $derived(`${wish.trim().toLowerCase()}.${zone}`);
	async function createDomain() {
		domainsBusy = true;
		domainsError = '';
		try {
			const created = await api.post<Ipv64Domain>('/network/ipv64/domains/create', body({ name: newName }));
			creating = false;
			wish = '';
			onhost(created.name);
			await loadDomains();
		} catch (err) {
			domainsError = failure(err);
		} finally {
			domainsBusy = false;
		}
	}
	async function setWildcard(domain: Ipv64Domain, wildcard: boolean) {
		domainsBusy = true;
		domainsError = '';
		try {
			const changed = await api.post<Ipv64Domain>('/network/ipv64/domains/change', body({ domain: domain.name, wildcard }));
			domains = (domains ?? []).map((d) => (d.name === changed.name ? { ...d, wildcard: changed.wildcard } : d));
		} catch (err) {
			domainsError = failure(err);
		} finally {
			domainsBusy = false;
		}
	}
	async function useForApps(name: string) {
		domainsBusy = true;
		domainsError = notice = '';
		try {
			await api.put('/docker/proxy', { domain: name });
			notice = t('ipv64.proxyDone', { domain: name });
			proxyDomain = null;
		} catch (err) {
			domainsError = failure(err);
		} finally {
			domainsBusy = false;
		}
	}

</script>

<section class="tile panel" aria-labelledby="ipv64-title">
	<h2 class="tile-title" id="ipv64-title">{t('ipv64.title')}</h2>
	<p class="muted">{t('ipv64.intro')}</p>
	{#if !keyKnown}<p class="notice-warn">{t('ipv64.needsKey')}</p>{/if}

	<p class="muted hint">{t('ipv64.moved')} <a href="/settings/ddns?tab=ipv64&sub=domains">{t('ipv64.movedLink')}</a></p>

	<div class="part">
		<h3>{t('ipv64.domains')}</h3>
		<div class="row-actions">
			<button type="button" class="btn" disabled={!keyKnown || domainsBusy} onclick={loadDomains}>{t('ipv64.domainsLoad')}</button>
		</div>
		{#if domainsError}<p class="notice-error" role="alert">{domainsError}</p>{/if}
		{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}
		{#if domains}
			{#if domains.length === 0}<p class="muted">{t('ipv64.noDomains')}</p>{/if}
			<div class="table-wrap">
				<table>
					<tbody>
						{#each domains as domain (domain.name)}
							<tr>
								<td class="mono">{domain.name}</td>
								<td>
									{#if domain.wildcard}<span class="chip ok">{t('ipv64.wildcardOn')}</span>{/if}
									{#if hostnames.includes(domain.name)}<span class="chip">{t('ipv64.inUse')}</span>{/if}
								</td>
								<td class="actions">
									{#if !hostnames.includes(domain.name)}<button type="button" class="btn" onclick={() => onhost(domain.name)}>{t('ipv64.use')}</button>{/if}
									<button type="button" class="btn" disabled={domainsBusy} onclick={() => setWildcard(domain, !domain.wildcard)}
										>{t(domain.wildcard ? 'ipv64.wildcardOff' : 'ipv64.wildcardTurnOn')}</button
									>
									<button type="button" class="btn" disabled={domainsBusy} onclick={() => (proxyDomain = domain.name)}>{t('ipv64.forApps')}</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="muted hint">{t('ipv64.wildcardHint')}</p>
			<div class="create">
				<label class="stack"
					><span>{t('ipv64.newName')}</span><input class="field mono" type="text" bind:value={wish} placeholder="meinnas" spellcheck="false" autocapitalize="off" /></label
				>
				<label class="stack"
					><span>{t('ipv64.zone')}</span><select class="field mono" bind:value={zone}>
						{#each zones as z (z)}<option value={z}>{z}</option>{/each}
					</select></label
				>
				<button type="button" class="btn" disabled={!validIpv64Wish(wish.trim().toLowerCase()) || domainsBusy} onclick={() => (creating = true)}
					>{t('ipv64.create')}</button
				>
			</div>
		{/if}
	</div>

	<Heartbeat {status} {apiKey} {keyKnown} {onstatus} />
</section>

{#if creating}
	<Dialog title={t('ipv64.createTitle')} locked={domainsBusy} onclose={() => (creating = false)}>
		<p>{t('ipv64.createText', { name: newName })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={domainsBusy} onclick={() => (creating = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={domainsBusy} onclick={createDomain}>{t('ipv64.create')}</button>
		</div>
	</Dialog>
{/if}

{#if proxyDomain}
	<Dialog title={t('ipv64.forAppsTitle')} locked={domainsBusy} onclose={() => (proxyDomain = null)}>
		<p>{t('ipv64.forAppsText', { domain: proxyDomain })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={domainsBusy} onclick={() => (proxyDomain = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={domainsBusy} onclick={() => proxyDomain && useForApps(proxyDomain)}>{t('ipv64.forApps')}</button>
		</div>
	</Dialog>
{/if}

<style>
	section {
		font-size: 0.875rem;
	}
	.part {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	h3 {
		margin: 0;
		font-size: 0.9375rem;
	}
	.hint {
		margin: 0;
	}
	.table-wrap {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
	}
	td {
		text-align: left;
		padding: 6px 8px;
		border-bottom: 1px solid var(--border);
		vertical-align: middle;
	}
	.actions {
		text-align: right;
		white-space: nowrap;
	}
	.actions .btn + .btn {
		margin-left: 6px;
	}
	.create {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 10px;
	}
</style>
