<script lang="ts">
	// What IPv64.net offers for the updater: picking and creating domains
	// and the healthcheck that watches whether the NAS is reachable. Account,
	// records and everything else are under Settings › IPv64. Every call
	// counts against the limits of the account, so nothing is fetched by
	// itself: each part loads on a click.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { IPV64_ZONES, validIpv64Wish } from '#lib/dns-providers.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { type DdnsStatus, type Ipv64Domain, type Ipv64Healthcheck, type Ipv64Integration } from './ddns.ts';

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
	function body(extra: Record<string, unknown> = {}) {
		return apiKey.trim() ? { apiKey: apiKey.trim(), ...extra } : extra;
	}
	function failure(err: unknown): string {
		if (err instanceof ApiError && err.code.startsWith('ipv64_') && err.code !== 'ipv64_no_key')
			return `${errorText(err.code)} ${err.message}`;
		return errorText(err instanceof ApiError ? err.code : 'internal');
	}
	const when = (value?: string) => (value ? formatDateTime(new Date(value)) : '–');

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

	// --- healthcheck ---
	const beat = $derived(status.settings.heartbeat);
	let beatError = $state('');
	let beatBusy = $state(false);
	let switchingOn = $state(false);
	let switchingOff = $state(false);
	let checks = $state<Ipv64Healthcheck[]>([]);
	let integrations = $state<Ipv64Integration[]>([]);
	let choice = $state('new');
	let checkName = $state('');
	let period = $state(30);
	let grace = $state(10);
	let chosen = $state<number[]>([0]);
	let autoPause = $state(true);
	let remote = $state<Ipv64Healthcheck | null>(null);
	let testWait = $state(false);
	let testNotice = $state('');

	async function startOn() {
		beatError = '';
		beatBusy = true;
		try {
			const answer = await api.post<{ healthchecks: Ipv64Healthcheck[]; integrations: Ipv64Integration[]; suggestedName: string }>(
				'/network/ipv64/healthchecks',
				body(),
			);
			checks = answer.healthchecks;
			integrations = answer.integrations;
			checkName = answer.suggestedName;
			choice = 'new';
			switchingOn = true;
		} catch (err) {
			beatError = failure(err);
		} finally {
			beatBusy = false;
		}
	}
	async function switchOn() {
		beatBusy = true;
		beatError = '';
		try {
			const next = await api.post<DdnsStatus>(
				'/network/ipv64/heartbeat/on',
				body(
					choice === 'new'
						? { name: checkName.trim(), periodMinutes: period, graceMinutes: grace, integrations: chosen, autoPause }
						: { id: choice, autoPause },
				),
			);
			onstatus(next);
			switchingOn = false;
		} catch (err) {
			beatError = failure(err);
		} finally {
			beatBusy = false;
		}
	}
	async function switchOff(action: '' | 'pause' | 'delete') {
		beatBusy = true;
		beatError = '';
		try {
			onstatus(await api.post<DdnsStatus>('/network/ipv64/heartbeat/off', body({ action })));
			switchingOff = false;
			remote = null;
		} catch (err) {
			beatError = failure(err);
		} finally {
			beatBusy = false;
		}
	}
	async function loadRemote() {
		beatBusy = true;
		beatError = '';
		try {
			remote = await api.post<Ipv64Healthcheck>('/network/ipv64/heartbeat/status', body());
		} catch (err) {
			beatError = failure(err);
		} finally {
			beatBusy = false;
		}
	}
	async function sendTest() {
		testWait = true;
		testNotice = beatError = '';
		setTimeout(() => (testWait = false), 30_000);
		try {
			await api.post('/network/ipv64/heartbeat/test', body());
			testNotice = t('ipv64.testSent');
		} catch (err) {
			beatError = failure(err);
		}
	}
	function toggleIntegration(id: number, on: boolean) {
		chosen = on ? [...new Set([...chosen, id])] : chosen.filter((x) => x !== id);
	}
	const choiceValid = $derived(
		choice !== 'new' || (checkName.trim().length > 0 && period >= 20 && period <= 1440 && grace >= 1 && grace <= 1440),
	);
</script>

<section class="tile panel" aria-labelledby="ipv64-title">
	<h2 class="tile-title" id="ipv64-title">{t('ipv64.title')}</h2>
	<p class="muted">{t('ipv64.intro')}</p>
	{#if !keyKnown}<p class="notice-warn">{t('ipv64.needsKey')}</p>{/if}

	<p class="muted hint">{t('ipv64.moved')} <a href="/settings/ipv64?tab=domains">{t('ipv64.movedLink')}</a></p>

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

	<div class="part divided">
		<h3 id="ipv64-heartbeat">{t('ipv64.heartbeat')}</h3>
		<p class="muted hint">{t('ipv64.heartbeatHint')}</p>
		<label class="check"
			><input
				type="checkbox"
				checked={beat.enabled}
				disabled={beatBusy || (!beat.enabled && !keyKnown)}
				onchange={(event) => {
					const field = event.currentTarget;
					field.checked = beat.enabled;
					if (beat.enabled) switchingOff = true;
					else startOn();
				}}
			/><span>{t('ipv64.heartbeatEnable')}</span></label
		>
		{#if beat.enabled}
			<dl>
				<div><dt>{t('ipv64.check')}</dt><dd>{beat.check || '–'}</dd></div>
				<div><dt>{t('ipv64.lastPing')}</dt><dd>{when(status.heartbeat.lastPing)}</dd></div>
				<div><dt>{t('ipv64.nextPing')}</dt><dd>{when(status.heartbeat.nextPing)}</dd></div>
				<div><dt>{t('ipv64.autoPause')}</dt><dd>{t(beat.autoPause ? 'ipv64.yes' : 'ipv64.no')}</dd></div>
				{#if remote}
					<div><dt>{t('ipv64.remoteStatus')}</dt><dd>{remote.status}</dd></div>
					<div><dt>{t('ipv64.lastSeen')}</dt><dd>{when(remote.lastSeenAt)}</dd></div>
				{/if}
			</dl>
			{#if status.heartbeat.paused}<p class="notice-warn">{t('ipv64.pausedForShutdown')}</p>{/if}
			{#if status.heartbeat.lastError}<p class="notice-error" role="alert">{status.heartbeat.lastError}</p>{/if}
			<div class="row-actions">
				<button type="button" class="btn" disabled={!keyKnown || beatBusy} onclick={loadRemote}>{t('ipv64.remoteLoad')}</button>
				<button type="button" class="btn" disabled={!keyKnown || testWait} onclick={sendTest}>{t('ipv64.test')}</button>
			</div>
			{#if testNotice}<p class="muted" role="status">{testNotice}</p>{/if}
		{/if}
		{#if beatError}<p class="notice-error" role="alert">{beatError}</p>{/if}
	</div>
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

{#if switchingOn}
	<Dialog title={t('ipv64.onTitle')} locked={beatBusy} onclose={() => (switchingOn = false)}>
		<p>{t('ipv64.onText')}</p>
		<label class="stack"
			><span>{t('ipv64.check')}</span><select class="field" bind:value={choice}>
				<option value="new">{t('ipv64.newCheck')}</option>
				{#each checks as check (check.id)}<option value={check.id}>{check.name} ({check.status})</option>{/each}
			</select></label
		>
		{#if choice === 'new'}
			<label class="stack"><span>{t('ipv64.checkName')}</span><input class="field" type="text" bind:value={checkName} maxlength="100" /></label>
			<label class="stack"><span>{t('ipv64.period')}</span><input class="field short" type="number" min="20" max="1440" bind:value={period} /></label>
			<label class="stack"><span>{t('ipv64.grace')}</span><input class="field short" type="number" min="1" max="1440" bind:value={grace} /></label>
			<p class="muted hint">{t('ipv64.periodHint')}</p>
			<fieldset>
				<legend>{t('ipv64.integrations')}</legend>
				{#each integrations as integration (integration.id)}
					<label class="check"
						><input type="checkbox" checked={chosen.includes(integration.id)} onchange={(event) => toggleIntegration(integration.id, event.currentTarget.checked)} /><span
							>{integration.name} <span class="muted">({integration.type})</span></span
						></label
					>
				{/each}
			</fieldset>
		{/if}
		<label class="check"><input type="checkbox" bind:checked={autoPause} /><span>{t('ipv64.autoPauseLabel')}</span></label>
		<p class="muted hint">{t('ipv64.autoPauseHint')}</p>
		{#if beatError}<p class="notice-error" role="alert">{beatError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={beatBusy} onclick={() => (switchingOn = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={beatBusy || !choiceValid} onclick={switchOn}>{t('ipv64.onSubmit')}</button>
		</div>
	</Dialog>
{/if}

{#if switchingOff}
	<Dialog title={t('ipv64.offTitle')} locked={beatBusy} onclose={() => (switchingOff = false)}>
		<p>{t('ipv64.offText', { check: beat.check || '' })}</p>
		{#if beatError}<p class="notice-error" role="alert">{beatError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={beatBusy} onclick={() => (switchingOff = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={beatBusy || !keyKnown} onclick={() => switchOff('delete')}>{t('ipv64.offDelete')}</button>
			<button type="button" class="btn btn-warn" disabled={beatBusy || !keyKnown} onclick={() => switchOff('pause')}>{t('ipv64.offPause')}</button>
			<button type="button" class="btn" disabled={beatBusy} onclick={() => switchOff('')}>{t('ipv64.offOnly')}</button>
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
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
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
	.short {
		max-width: 10rem;
	}
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
</style>
