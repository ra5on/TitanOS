<script lang="ts">
	// Public access: domains in the CDN of IPv64 with reverse proxy and
	// portmapper, and the Docker apps TitanOS published through it.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { FRONTENDS, TRANSPORTS, size, tone, validCIDR, validListenPort, validPort, type CDN, type DnsRecord, type Domain, type Published, type Status } from './ipv64.ts';
	import { failure, when } from './ui.ts';

	let { status }: { status: Status } = $props();

	let list = $state<CDN[]>([]);
	let published = $state<Published[]>([]);
	let domains = $state<Domain[]>([]);
	let error = $state('');
	let notice = $state('');
	let busy = $state(false);
	let newDomain = $state('');

	async function load() {
		try {
			const answer = await api.get<{ cdn: CDN[]; published: Published[] }>('/ipv64/cdn');
			list = answer.cdn;
			published = answer.published ?? [];
			domains = (await api.get<{ domains: Domain[] }>('/ipv64/domains')).domains;
			error = '';
		} catch (err) {
			error = failure(err);
		}
	}
	onMount(load);

	async function run(action: () => Promise<unknown>, done = '') {
		busy = true;
		error = notice = '';
		try {
			await action();
			if (done) notice = done;
			return true;
		} catch (err) {
			error = failure(err);
			return false;
		} finally {
			busy = false;
		}
	}

	let detail = $state<CDN | null>(null);
	let records = $state<DnsRecord[]>([]);
	let access = $state<{ id: number; address: string }[]>([]);
	let proxy = $state({ frontend: 'https_redirect', scheme: 'http', port: 80, websocket: false, cache: false, cacheMinutes: 60, cacheMB: 100, accessOn: false, accessMode: 'allowlist' });
	async function open(id: string) {
		await run(async () => {
			const answer = await api.get<{ cdn: CDN; records: DnsRecord[]; accessList: { id: number; address: string }[] }>(`/ipv64/cdn/${id}`);
			detail = answer.cdn;
			records = answer.records;
			access = answer.accessList;
			const p = answer.cdn.proxy;
			proxy = {
				frontend: p.frontend ?? 'https_redirect',
				scheme: p.backend.scheme,
				port: p.backend.port,
				websocket: p.websocket,
				cache: p.caching.enabled,
				cacheMinutes: p.caching.time_minutes || 60,
				cacheMB: p.caching.size_mb || 100,
				accessOn: p.access_list.enabled,
				accessMode: p.access_list.mode || 'allowlist',
			};
		});
	}
	const base = $derived(detail ? `/ipv64/cdn/${detail.id}` : '');
	async function act(action: string) {
		if (!detail) return;
		const id = detail.id;
		await run(async () => {
			await api.post(`${base}/action`, { action });
			await open(id);
			await load();
		}, t('ipv64x.accepted'));
	}
	async function saveProxy() {
		if (!detail) return;
		const id = detail.id;
		const body: Record<string, unknown> = {
			frontend: proxy.frontend,
			backend_scheme: proxy.scheme,
			backend_port: proxy.port,
			caching: { enabled: proxy.cache, time_minutes: proxy.cacheMinutes, size_mb: proxy.cacheMB },
			access_list: { enabled: proxy.accessOn, mode: proxy.accessMode },
		};
		if (detail.proxy.state === 'on') body.websocket = proxy.websocket;
		await run(async () => {
			await api.patch(base, body);
			await open(id);
		}, t('ipv64x.saved'));
	}
	async function routeRecord(r: DnsRecord, mode: string) {
		if (!detail) return;
		const id = detail.id;
		await run(async () => {
			await api.post(`${base}/records/${r.token}`, { mode });
			await open(id);
		}, t('ipv64x.accepted'));
	}
	let address = $state('');
	async function addAccess() {
		if (!detail || !validCIDR(address)) return;
		const id = detail.id;
		if (await run(async () => {
			await api.post(`${base}/access-list`, { address: address.trim() });
			await open(id);
		})) address = '';
	}
	let mapping = $state<{ name: string; record: string; listen: number; target: number; transport: string } | null>(null);
	const mappingOK = $derived(!!mapping && !!mapping.record && validListenPort(mapping.listen) && validPort(mapping.target));
	async function addMapping() {
		if (!detail || !mapping || !mappingOK) return;
		const m = mapping;
		const id = detail.id;
		if (await run(async () => {
			await api.post(`${base}/mappings`, { name: m.name.trim(), record: m.record, target_port: m.target, transport: m.transport, ...(m.listen ? { listen_port: m.listen } : {}) });
			await open(id);
		}, t('ipv64x.saved'))) mapping = null;
	}
	let confirm = $state<{ title: MessageKey; text: string; button: MessageKey; run: () => Promise<unknown> } | null>(null);
	async function confirmed() {
		if (!confirm) return;
		const what = confirm;
		if (await run(async () => {
			await what.run();
			await load();
		})) {
			confirm = null;
			if (detail) await open(detail.id);
		}
	}
	const routedRecords = $derived(records.filter((r) => r.state === 'cdn_proxied' || r.state === 'cdn_partial'));
</script>

<div class="ipv64-tab">
	<section class="tile panel" aria-labelledby="ipv64-cdn">
		<h2 class="tile-title" id="ipv64-cdn">{t('ipv64x.cdn')}</h2>
		<p class="muted hint">{t('ipv64x.cdnHint')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.domain')}</th><th>{t('ipv64x.proxy')}</th><th>{t('ipv64x.portmapper')}</th><th>{t('ipv64x.traffic')}</th><th></th></tr></thead>
				<tbody>
					{#each list as c (c.id)}
						<tr>
							<td class="mono">{c.domain ?? c.id}{#if c.pending.length}<span class="chip warn">{t('ipv64x.pending')}</span>{/if}</td>
							<td><span class="chip {tone(c.proxy.state)}">{t(`ipv64x.onOff.${c.proxy.state}` as MessageKey)}</span></td>
							<td><span class="chip {tone(c.portmapper.state)}">{t(`ipv64x.onOff.${c.portmapper.state}` as MessageKey)}</span></td>
							<td>↓ {size(c.traffic_mb.in * 1e6)} · ↑ {size(c.traffic_mb.out * 1e6)}</td>
							<td class="actions"><button type="button" class="btn" onclick={() => open(c.id)}>{t('ipv64x.manage')}</button></td>
						</tr>
					{:else}
						<tr><td colspan="5" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="inline">
			<label class="stack"
				><span>{t('ipv64x.domain')}</span><select class="field mono" bind:value={newDomain}><option value=""></option>{#each domains.filter((d) => !list.some((c) => c.domain === d.name)) as d (d.name)}<option value={d.name}>{d.name}</option>{/each}</select></label
			>
			<button type="button" class="btn" disabled={busy || !newDomain} onclick={() => (confirm = { title: 'ipv64x.cdnCreate', text: t('ipv64x.cdnCreateText', { domain: newDomain }), button: 'ipv64x.cdnCreate', run: () => api.post('/ipv64/cdn', { domain: newDomain }) })}>{t('ipv64x.cdnCreate')}</button>
		</div>
	</section>

	{#if detail}
		<section class="tile panel" aria-labelledby="ipv64-cdn-detail">
			<h2 class="tile-title mono" id="ipv64-cdn-detail">{detail.domain}</h2>
			{#if detail.pending.length}<p class="notice-warn">{t('ipv64x.cdnPending', { what: detail.pending.map((p) => t(`ipv64x.pendingKind.${p}` as MessageKey)).join(', ') })}</p>{/if}
			{#if detail.portmapper.state === 'suspended_traffic_limit'}<p class="notice-error">{t('ipv64x.trafficLimit')}</p>{/if}
			<dl>
				<div><dt>{t('ipv64x.server')}</dt><dd class="mono">{detail.server ? `${detail.server.hostname} (${detail.server.ipv4}${detail.server.ipv6 ? ', ' + detail.server.ipv6 : ''})` : '–'}</dd></div>
				<div><dt>{t('ipv64x.certificate')}</dt><dd>{t(`ipv64x.cert.${detail.proxy.certificate}` as MessageKey)}</dd></div>
				<div><dt>{t('ipv64x.traffic')}</dt><dd>↓ {size(detail.traffic_mb.in * 1e6)} · ↑ {size(detail.traffic_mb.out * 1e6)}</dd></div>
			</dl>
			<p class="muted hint">{t('ipv64x.cdnSourceHint')}</p>

			<h3>{t('ipv64x.cdnRecords')}</h3>
			<div class="table-wrap">
				<table>
					<tbody>
						{#each records as r (r.token)}
							<tr>
								<td class="mono">{r.type} {r.fqdn} {r.content}</td>
								<td>{t(`ipv64x.recordState.${r.state}` as MessageKey)}</td>
								<td class="actions">
									{#if r.state === 'cdn_proxied' || r.state === 'cdn_partial'}
										<button type="button" class="btn" disabled={busy} onclick={() => routeRecord(r, '')}>{t('ipv64x.direct')}</button>
									{:else}
										<button type="button" class="btn" disabled={busy} onclick={() => routeRecord(r, 'full')}>{t('ipv64x.viaCdn')}</button>
										<button type="button" class="btn" disabled={busy} onclick={() => routeRecord(r, 'other_family_only')}>{t('ipv64x.otherFamily')}</button>
									{/if}
								</td>
							</tr>
						{:else}
							<tr><td class="muted">{t('ipv64x.none')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="muted hint">{t('ipv64x.otherFamilyHint')}</p>

			<h3>{t('ipv64x.proxy')}</h3>
			<div class="row-actions">
				<span class="chip {tone(detail.proxy.state)}">{t(`ipv64x.onOff.${detail.proxy.state}` as MessageKey)}</span>
				{#if detail.proxy.state === 'on'}
					<button type="button" class="btn" disabled={busy} onclick={() => act('proxy/disable')}>{t('ipv64x.switchOff')}</button>
				{:else}
					<button type="button" class="btn" disabled={busy || !routedRecords.length} onclick={() => (confirm = { title: 'ipv64x.proxyOnTitle', text: t('ipv64x.proxyOnText', { domain: detail?.domain ?? '' }), button: 'ipv64x.switchOn', run: () => api.post(`${base}/action`, { action: 'proxy/enable' }) })}>{t('ipv64x.switchOn')}</button>
				{/if}
			</div>
			<div class="inline">
				<label class="stack"><span>{t('ipv64x.frontend')}</span><select class="field" bind:value={proxy.frontend}>{#each FRONTENDS as f (f)}<option value={f}>{t(`ipv64x.frontendKind.${f}` as MessageKey)}</option>{/each}</select></label>
				<label class="stack"><span>{t('ipv64x.backendScheme')}</span><select class="field short" bind:value={proxy.scheme}><option value="http">http</option><option value="https">https</option></select></label>
				<label class="stack"><span>{t('ipv64x.backendPort')}</span><input class="field short" type="number" min="1" max="65535" bind:value={proxy.port} /></label>
				<label class="check"><input type="checkbox" bind:checked={proxy.websocket} disabled={detail.proxy.state !== 'on'} /><span>WebSocket</span></label>
			</div>
			<div class="inline">
				<label class="check"><input type="checkbox" bind:checked={proxy.cache} /><span>{t('ipv64x.caching')}</span></label>
				<label class="stack"><span>{t('ipv64x.cacheMinutes')}</span><input class="field short" type="number" min="1" max="600" bind:value={proxy.cacheMinutes} disabled={!proxy.cache} /></label>
				<label class="stack"><span>{t('ipv64x.cacheMB')}</span><input class="field short" type="number" min="1" bind:value={proxy.cacheMB} disabled={!proxy.cache} /></label>
			</div>
			<div class="inline">
				<label class="check"><input type="checkbox" bind:checked={proxy.accessOn} /><span>{t('ipv64x.accessList')}</span></label>
				<label class="stack"><span>{t('ipv64x.accessMode')}</span><select class="field" bind:value={proxy.accessMode} disabled={!proxy.accessOn}><option value="allowlist">{t('ipv64x.allowlist')}</option><option value="denylist">{t('ipv64x.denylist')}</option></select></label>
			</div>
			<div class="row-actions"><button type="button" class="btn btn-primary" disabled={busy || !validPort(proxy.port)} onclick={saveProxy}>{t('common.save')}</button></div>
			<ul class="plain">
				{#each access as e (e.id)}<li class="mono">{e.address} <button type="button" class="btn" disabled={busy} onclick={() => run(async () => { await api.delete(`${base}/access-list/${e.id}`); if (detail) await open(detail.id); })}>{t('ipv64x.remove')}</button></li>{/each}
			</ul>
			<div class="inline">
				<label class="stack"><span>{t('ipv64x.accessAddress')}</span><input class="field mono" type="text" bind:value={address} placeholder="198.51.100.0/24" /></label>
				<button type="button" class="btn" disabled={busy || !validCIDR(address)} onclick={addAccess}>{t('ipv64x.add')}</button>
			</div>

			<h3>{t('ipv64x.portmapper')}</h3>
			<div class="row-actions">
				<span class="chip {tone(detail.portmapper.state)}">{t(`ipv64x.onOff.${detail.portmapper.state}` as MessageKey)}</span>
				{#if detail.portmapper.state === 'on'}
					<button type="button" class="btn" disabled={busy} onclick={() => act('portmapper/disable')}>{t('ipv64x.switchOff')}</button>
				{:else}
					<button type="button" class="btn" disabled={busy || !routedRecords.length} onclick={() => (confirm = { title: 'ipv64x.portmapperOnTitle', text: t('ipv64x.portmapperOnText'), button: 'ipv64x.switchOn', run: () => api.post(`${base}/action`, { action: 'portmapper/enable' }) })}>{t('ipv64x.switchOn')}</button>
				{/if}
			</div>
			<div class="table-wrap">
				<table>
					<thead><tr><th>{t('ipv64x.name')}</th><th>{t('ipv64x.listen')}</th><th>{t('ipv64x.targetPort')}</th><th>{t('ipv64x.transport')}</th><th></th></tr></thead>
					<tbody>
						{#each detail.mappings ?? [] as m (m.id)}
							<tr>
								<td>{m.name} <span class="muted mono">{m.fqdn ?? ''}</span></td>
								<td class="mono">{m.listen_port}</td>
								<td class="mono">{m.target_port}</td>
								<td>{m.transport.toUpperCase()}</td>
								<td class="actions"><button type="button" class="btn" disabled={busy} onclick={() => run(async () => { await api.delete(`${base}/mappings/${m.id}`); if (detail) await open(detail.id); })}>{t('ipv64x.delete')}</button></td>
							</tr>
						{:else}
							<tr><td colspan="5" class="muted">{t('ipv64x.none')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<div class="row-actions"><button type="button" class="btn" disabled={!routedRecords.length} onclick={() => (mapping = { name: '', record: routedRecords[0]?.token ?? '', listen: 0, target: 0, transport: 'tcp' })}>{t('ipv64x.mappingAdd')}</button></div>

			<h3>{t('ipv64x.more')}</h3>
			<div class="row-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => act('redeploy')}>{t('ipv64x.redeploy')}</button>
				<button type="button" class="btn" disabled={busy || detail.proxy.state !== 'on'} onclick={() => act('renew-certificate')}>{t('ipv64x.renew')}</button>
				<button type="button" class="btn" disabled={busy} onclick={() => (confirm = { title: 'ipv64x.resetTraffic', text: t('ipv64x.resetTrafficText'), button: 'ipv64x.resetTraffic', run: () => api.post(`${base}/action`, { action: 'reset-traffic' }) })}>{t('ipv64x.resetTraffic')}</button>
				<button type="button" class="btn" disabled={busy} onclick={() => (confirm = { title: 'ipv64x.move', text: t('ipv64x.moveText'), button: 'ipv64x.move', run: () => api.post(`${base}/action`, { action: 'move' }) })}>{t('ipv64x.move')}</button>
				<button type="button" class="btn btn-danger" disabled={busy} onclick={() => (confirm = { title: 'ipv64x.cdnDelete', text: t('ipv64x.cdnDeleteText', { domain: detail?.domain ?? '' }), button: 'ipv64x.cdnDelete', run: async () => { await api.delete(base); detail = null; } })}>{t('ipv64x.cdnDelete')}</button>
			</div>
		</section>
	{/if}

	<section class="tile panel" aria-labelledby="ipv64-published">
		<h2 class="tile-title" id="ipv64-published">{t('ipv64x.publishedApps')}</h2>
		<p class="muted hint">{t('ipv64x.publishedHint')}</p>
		<ul class="plain">
			{#each published as p (p.app)}
				<li><a href={`/docker/apps/${encodeURIComponent(p.app)}`}>{p.app}</a> → <span class="mono">https://{p.prefix ? `${p.prefix}.` : ''}{p.domain}</span> ({t('ipv64x.port')} {p.port}, {when(p.at)}){#if p.firewall}<span class="chip">{t('ipv64x.firewallOpened')}</span>{/if}</li>
			{:else}
				<li class="muted">{t('ipv64x.none')}</li>
			{/each}
		</ul>
	</section>
</div>

{#if mapping}
	<Dialog title={t('ipv64x.mappingAdd')} locked={busy} onclose={() => (mapping = null)}>
		<label class="stack"><span>{t('ipv64x.name')}</span><input class="field" type="text" maxlength="100" bind:value={mapping.name} /></label>
		<label class="stack"><span>{t('ipv64x.record')}</span><select class="field mono" bind:value={mapping.record}>{#each routedRecords as r (r.token)}<option value={r.token}>{r.type} {r.fqdn}</option>{/each}</select></label>
		<div class="inline">
			<label class="stack"><span>{t('ipv64x.listen')}</span><input class="field short" type="number" min="0" max="62000" bind:value={mapping.listen} /></label>
			<label class="stack"><span>{t('ipv64x.targetPort')}</span><input class="field short" type="number" min="1" max="65535" bind:value={mapping.target} /></label>
			<label class="stack"><span>{t('ipv64x.transport')}</span><select class="field short" bind:value={mapping.transport}>{#each TRANSPORTS as tr (tr)}<option value={tr}>{tr.toUpperCase()}</option>{/each}</select></label>
		</div>
		<p class="muted hint">{t('ipv64x.listenHint')}</p>
		<p class="notice-warn">{t('ipv64x.mappingWarn')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (mapping = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={busy || !mappingOK} onclick={addMapping}>{t('ipv64x.add')}</button>
		</div>
	</Dialog>
{/if}

{#if confirm}
	<Dialog title={t(confirm.title)} locked={busy} onclose={() => (confirm = null)}>
		<p>{confirm.text}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirm = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={confirmed}>{t(confirm.button)}</button>
		</div>
	</Dialog>
{/if}

<style>
	.chip {
		margin-left: 6px;
	}
	.plain {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
</style>
