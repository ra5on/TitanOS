<script lang="ts">
	// DynDNS within Settings › IPv64: everything Settings › Dynamic DNS can
	// do, with IPv64 as provider and the key of this area. Both pages work
	// on the same settings; Settings › Dynamic DNS stays for other providers.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import DdnsStatusPanel from '#lib/ddns/DdnsStatus.svelte';
	import Heartbeat from '#lib/ddns/Heartbeat.svelte';
	import UpdaterFields from '#lib/ddns/UpdaterFields.svelte';
	import {
		ipv64UpdateURL,
		MAX_HOSTS,
		updaterForm,
		updaterPayload,
		withHost,
		type DdnsStatus,
		type UpdaterForm,
	} from '#lib/ddns/ddns.ts';
	import { validHostName, validIpv64Wish } from '#lib/dns-providers.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Domain, Status } from './ipv64.ts';
	import { failure } from './ui.ts';

	let { status: area }: { status: Status } = $props();

	let ddns = $state<DdnsStatus | null>(null);
	let domains = $state<Domain[]>([]);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');
	let domainError = $state('');
	let domainNotice = $state('');

	// The form.
	let enabled = $state(false);
	let hosts = $state<string[]>([]);
	let updater = $state<UpdaterForm>({
		ipv4: true,
		ipv6: true,
		interval: 0,
		lookupURL: '',
		iface: '',
	});
	let prefixMode = $state<'none' | 'auto' | 'fixed'>('none');
	let prefixValue = $state('');
	let pick = $state('');
	let typed = $state('');
	let wish = $state('');
	let zone = $state('ipv64.de');

	// Dialogs.
	let creating = $state<string | null>(null);
	let forApps = $state<string | null>(null);
	let rotating = $state<string | null>(null);
	let updaterFor = $state<string | null>(null);
	let prefixes = $state<Record<string, string>>({});

	function fill(next: DdnsStatus) {
		const s = next.settings;
		const ipv64 = s.provider.type === 'ipv64';
		enabled = ipv64 && s.enabled;
		hosts = ipv64 ? [...s.hostnames] : [];
		updater = updaterForm(s);
		const prefix = ipv64 ? (s.provider.ipv6Prefix ?? '') : '';
		prefixMode = prefix === '' ? 'none' : prefix === 'auto' ? 'auto' : 'fixed';
		prefixValue = prefixMode === 'fixed' ? prefix : '';
	}
	async function loadDomains() {
		const answer = await api.get<{ domains: Domain[] }>('/ipv64/domains');
		domains = answer.domains;
		prefixes = Object.fromEntries(domains.map((d) => [d.name, d.ipv6Prefix ?? '']));
	}
	async function load(refill = true) {
		try {
			const [next] = await Promise.all([
				api.get<DdnsStatus>('/network/ddns'),
				refill ? loadDomains() : Promise.resolve(),
			]);
			if (refill || !ddns) fill(next);
			ddns = next;
			loadError = '';
		} catch (err) {
			loadError = failure(err);
		}
	}
	onMount(() => {
		load();
	});
	$effect(() => {
		if (!ddns?.running) return;
		return poll(() => load(false), { interval: 3000, immediate: false });
	});

	const provider = $derived(ddns?.settings.provider.type ?? '');
	const otherProvider = $derived(provider !== '' && provider !== 'ipv64');
	const keyKnown = $derived(provider === 'ipv64' && !!ddns?.secrets.token);
	const badHosts = $derived(hosts.filter((h) => !validHostName(h)));
	const ipv6Prefix = $derived(
		prefixMode === 'none' ? '' : prefixMode === 'auto' ? 'auto' : prefixValue.trim(),
	);
	const prefixOK = $derived(prefixMode !== 'fixed' || /^[0-9a-f:]+\/\d{1,3}$/i.test(ipv6Prefix));
	const valid = $derived(
		badHosts.length === 0 &&
			hosts.length <= MAX_HOSTS &&
			prefixOK &&
			(!enabled || (hosts.length > 0 && (updater.ipv4 || updater.ipv6))),
	);
	const choices = $derived(domains.map((d) => d.name).filter((name) => !hosts.includes(name)));
	const typedName = $derived(typed.trim().toLowerCase().replace(/\.$/, ''));

	function add(name: string) {
		hosts = withHost(hosts, name);
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (busy || !valid) return;
		busy = true;
		error = notice = '';
		try {
			const next = await api.put<DdnsStatus>('/ipv64/ddns', {
				enabled,
				hostnames: hosts,
				...updaterPayload(updater),
				ipv6Prefix,
			});
			ddns = next;
			fill(next);
			notice = t('ipv64x.saved');
		} catch (err) {
			error =
				err instanceof ApiError && err.code === 'invalid_ddns'
					? `${errorText(err.code)} ${err.message.replace(/^ddns: invalid settings: /, '')}`
					: failure(err);
		} finally {
			busy = false;
		}
	}

	async function domainRun(action: () => Promise<unknown>, done = '') {
		busy = true;
		domainError = domainNotice = '';
		try {
			await action();
			domainNotice = done;
			return true;
		} catch (err) {
			domainError = failure(err);
			return false;
		} finally {
			busy = false;
		}
	}
	const domainPath = (name: string) => `/ipv64/domains/${encodeURIComponent(name)}`;
	async function change(name: string, body: Record<string, unknown>) {
		await domainRun(async () => {
			await api.patch(domainPath(name), body);
			await loadDomains();
		}, t('ipv64x.saved'));
	}
	async function create() {
		const name = creating;
		if (!name) return;
		if (
			await domainRun(
				async () => {
					await api.post('/ipv64/domains', { name, own: false });
					await loadDomains();
					add(name);
				},
				t('ipv64x.dyn.createdAdded', { name }),
			)
		) {
			creating = null;
			wish = '';
		}
	}
	async function useForApps() {
		const name = forApps;
		if (
			name &&
			(await domainRun(
				() => api.put('/docker/proxy', { domain: name }),
				t('ipv64.proxyDone', { domain: name }),
			))
		)
			forApps = null;
	}
	async function rotate() {
		const name = rotating;
		if (!name) return;
		let followed = false;
		if (
			await domainRun(async () => {
				followed = (await api.post<{ ddnsUpdated: boolean }>(`${domainPath(name)}/rotate-token`))
					.ddnsUpdated;
			})
		) {
			domainNotice = t(followed ? 'ipv64x.rotatedFollowed' : 'ipv64x.rotated');
			rotating = null;
		}
	}
</script>

<div class="ipv64-tab">
	{#if loadError}
		<div class="notice-error" role="alert">
			{loadError}<button class="btn" onclick={() => load()}>{t('ui.retry')}</button>
		</div>
	{:else if !ddns}
		<p class="muted loading" role="status">{t('common.loading')}</p>
	{:else}
		<DdnsStatusPanel
			status={ddns}
			id="ipv64-dyn-status"
			intro={t('ipv64x.dyn.intro')}
			onstatus={(next) => (ddns = next)}
			onrefresh={() => load(false)}
		/>

		<form class="tile panel" aria-labelledby="ipv64-dyn-settings" onsubmit={save}>
			<h2 class="tile-title" id="ipv64-dyn-settings">{t('ipv64x.dyn.settings')}</h2>
			{#if otherProvider}<p class="notice-warn">
					{t('ipv64x.dyn.otherProvider', { provider })}
					<a href="/settings/ddns">{t('ipv64x.toDdns')}</a>
				</p>{/if}
			<p class="muted hint">
				{t(`ipv64x.dyn.key.${area.status.keySource || 'none'}` as MessageKey)}
				{t('ipv64x.dyn.samePage')}
			</p>
			<label class="check"
				><input type="checkbox" bind:checked={enabled} disabled={busy} /><span
					>{t('ddns.enable')}</span
				></label
			>

			<h3>{t('ipv64x.dyn.hostnames', { count: hosts.length, max: MAX_HOSTS })}</h3>
			<div class="table-wrap">
				<table>
					<tbody>
						{#each hosts as host (host)}
							<tr>
								<td class="mono"
									>{host}{#if !validHostName(host)}<span class="chip error"
											>{t('ipv64x.dyn.invalid')}</span
										>{/if}</td
								>
								<td class="actions"
									><button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() => (hosts = hosts.filter((h) => h !== host))}
										>{t('ipv64x.dyn.remove')}</button
									></td
								>
							</tr>
						{:else}
							<tr><td colspan="2" class="muted">{t('ipv64x.dyn.noHosts')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<div class="inline">
				<label class="stack grow"
					><span>{t('ipv64x.dyn.pick')}</span><select
						class="field mono"
						bind:value={pick}
						disabled={busy || !choices.length}
					>
						<option value=""
							>{t(choices.length ? 'ipv64x.dyn.pickNone' : 'ipv64x.dyn.pickEmpty')}</option
						>
						{#each choices as name (name)}<option value={name}>{name}</option>{/each}
					</select></label
				>
				<button
					type="button"
					class="btn"
					disabled={busy || !pick || hosts.length >= MAX_HOSTS}
					onclick={() => ((hosts = withHost(hosts, pick)), (pick = ''))}
					>{t('ipv64x.dyn.add')}</button
				>
			</div>
			<div class="inline">
				<label class="stack grow"
					><span>{t('ipv64x.dyn.typed')}</span><input
						class="field mono"
						type="text"
						bind:value={typed}
						placeholder="photos.nas.ipv64.de"
						autocapitalize="off"
						spellcheck="false"
						disabled={busy}
					/></label
				>
				<button
					type="button"
					class="btn"
					disabled={busy || !validHostName(typedName) || hosts.length >= MAX_HOSTS}
					onclick={() => ((hosts = withHost(hosts, typedName)), (typed = ''))}
					>{t('ipv64x.dyn.add')}</button
				>
			</div>
			<div class="inline">
				<label class="stack"
					><span>{t('ipv64x.newName')}</span><input
						class="field mono"
						type="text"
						bind:value={wish}
						placeholder="meinnas"
						autocapitalize="off"
						spellcheck="false"
						disabled={busy}
					/></label
				>
				<label class="stack"
					><span>{t('ipv64x.zone')}</span><select
						class="field mono"
						bind:value={zone}
						disabled={busy}
						>{#each area.zones as z (z)}<option value={z}>{z}</option>{/each}</select
					></label
				>
				<button
					type="button"
					class="btn"
					disabled={busy || !validIpv64Wish(wish.trim().toLowerCase()) || hosts.length >= MAX_HOSTS}
					onclick={() => (creating = `${wish.trim().toLowerCase()}.${zone}`)}
					>{t('ipv64x.dyn.createAdd')}</button
				>
			</div>
			{#if hosts.length > MAX_HOSTS}<p class="notice-warn" role="alert">
					{t('ddns.hostnamesTooMany')}
				</p>{/if}

			<div class="divided">
				<h3>{t('ipv64x.dyn.prefixTitle')}</h3>
				<label class="stack"
					><span>{t('ipv64x.dyn.prefixMode')}</span><select
						class="field short"
						bind:value={prefixMode}
						disabled={busy}
					>
						<option value="none">{t('ipv64x.dyn.prefixNone')}</option>
						<option value="auto">{t('ipv64x.dyn.prefixAuto')}</option>
						<option value="fixed">{t('ipv64x.dyn.prefixFixed')}</option>
					</select></label
				>
				{#if prefixMode === 'fixed'}
					<label class="stack"
						><span>{t('ipv64x.prefix6')}</span><input
							class="field mono"
							type="text"
							bind:value={prefixValue}
							placeholder="2001:db8:1234:5600::/56"
							spellcheck="false"
							disabled={busy}
						/></label
					>
				{/if}
				<p class="muted hint">{t('ipv64x.dyn.prefixHint')}</p>
			</div>

			<UpdaterFields bind:form={updater} disabled={busy} />

			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy || !valid}>{t('common.save')}</button>
				{#if notice}<span class="muted" role="status">{notice}</span>{/if}
			</div>
		</form>

		<section class="tile panel" aria-labelledby="ipv64-dyn-domains">
			<h2 class="tile-title" id="ipv64-dyn-domains">{t('ipv64x.dyn.domains')}</h2>
			<p class="muted hint">{t('ipv64x.dyn.domainsHint')}</p>
			{#if domainError}<p class="notice-error" role="alert">{domainError}</p>{/if}
			{#if domainNotice}<p class="notice-ok" role="status">{domainNotice}</p>{/if}
			<div class="table-wrap">
				<table>
					<thead
						><tr
							><th>{t('ipv64x.domain')}</th><th>{t('ipv64x.wildcard')}</th><th
								>{t('ipv64x.ignore')}</th
							><th>{t('ipv64x.prefix6')}</th><th></th></tr
						></thead
					>
					<tbody>
						{#each domains as d (d.name)}
							<tr>
								<td class="mono"
									>{d.name}{#if hosts.includes(d.name)}<span class="chip">{t('ipv64x.inDdns')}</span
										>{/if}</td
								>
								<td
									><input
										type="checkbox"
										checked={d.wildcard}
										disabled={busy}
										aria-label={t('ipv64x.wildcard')}
										onchange={(e) => change(d.name, { wildcard: e.currentTarget.checked })}
									/></td
								>
								<td
									><select
										class="field"
										value={d.ignore}
										disabled={busy}
										aria-label={t('ipv64x.ignore')}
										onchange={(e) => change(d.name, { ignore: e.currentTarget.value })}
									>
										<option value="none">{t('ipv64x.ignoreNone')}</option>
										<option value="ipv4">{t('ipv64x.ignore4')}</option>
										<option value="ipv6">{t('ipv64x.ignore6')}</option>
									</select></td
								>
								<td class="prefix"
									><input
										class="field mono"
										type="text"
										bind:value={prefixes[d.name]}
										placeholder="2001:db8::/56"
										spellcheck="false"
										aria-label={t('ipv64x.prefix6')}
										disabled={busy}
									/><button
										type="button"
										class="btn"
										disabled={busy || (prefixes[d.name] ?? '') === (d.ipv6Prefix ?? '')}
										onclick={() => change(d.name, { ipv6_prefix: (prefixes[d.name] ?? '').trim() })}
										>{t('common.save')}</button
									></td
								>
								<td class="actions">
									<button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() => (forApps = d.name)}>{t('ipv64.forApps')}</button
									>
									<button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() => (updaterFor = d.name)}>{t('ipv64x.dyn.router')}</button
									>
								</td>
							</tr>
						{:else}
							<tr><td colspan="5" class="muted">{t('ipv64x.none')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="muted hint">{t('ipv64x.wildcardHint')}</p>
		</section>

		<section class="tile panel" aria-labelledby="ipv64-dyn-heartbeat">
			<h2 class="tile-title" id="ipv64-dyn-heartbeat">{t('ipv64x.dyn.heartbeatTitle')}</h2>
			{#if !keyKnown}<p class="notice-warn">{t('ipv64x.dyn.heartbeatNeedsSave')}</p>{/if}
			<Heartbeat status={ddns} {keyKnown} onstatus={(next) => (ddns = next)} />
		</section>
	{/if}
</div>

{#if creating}
	<Dialog title={t('ipv64x.createTitle')} locked={busy} onclose={() => (creating = null)}>
		<p>{t('ipv64x.createText', { name: creating })}</p>
		<p class="muted">{t('ipv64x.dyn.createAddText')}</p>
		{#if domainError}<p class="notice-error" role="alert">{domainError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (creating = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" disabled={busy} onclick={create}
				>{t('ipv64x.dyn.createAdd')}</button
			>
		</div>
	</Dialog>
{/if}

{#if forApps}
	<Dialog title={t('ipv64.forAppsTitle')} locked={busy} onclose={() => (forApps = null)}>
		<p>{t('ipv64.forAppsText', { domain: forApps })}</p>
		{#if domainError}<p class="notice-error" role="alert">{domainError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (forApps = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" disabled={busy} onclick={useForApps}
				>{t('ipv64.forApps')}</button
			>
		</div>
	</Dialog>
{/if}

{#if updaterFor}
	<Dialog
		title={t('ipv64x.dyn.routerTitle', { name: updaterFor })}
		onclose={() => (updaterFor = null)}
	>
		<p>{t('ipv64x.dyn.routerText')}</p>
		<pre class="mono url">{ipv64UpdateURL(updaterFor)}</pre>
		<p class="muted hint">{t('ipv64x.dyn.routerToken')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (updaterFor = null)}
				>{t('ipv64x.dyn.close')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				onclick={() => ((rotating = updaterFor), (updaterFor = null))}>{t('ipv64x.rotate')}</button
			>
		</div>
	</Dialog>
{/if}

{#if rotating}
	<Dialog title={t('ipv64x.confirm.rotate.title')} locked={busy} onclose={() => (rotating = null)}>
		<p>{t('ipv64x.confirm.rotate.text', { name: rotating })}</p>
		{#if hosts.includes(rotating)}<p class="notice-warn">{t('ipv64x.rotateTitanOS')}</p>{/if}
		{#if domainError}<p class="notice-error" role="alert">{domainError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (rotating = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={rotate}
				>{t('ipv64x.confirm.rotate.button')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.chip {
		margin-left: 6px;
	}
	td.prefix {
		display: flex;
		gap: 6px;
		align-items: center;
		min-width: 16rem;
	}
	.url {
		margin: 0;
		padding: 8px 10px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: 0.8125rem;
		user-select: all;
	}
</style>
