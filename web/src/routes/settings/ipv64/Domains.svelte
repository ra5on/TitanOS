<script lang="ts">
	// Domains and DNS: create and delete domains, wildcard, ignored family,
	// fixed IPv6 prefix, new update token, reset of the zone and the records.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { validIpv64Wish } from '#lib/dns-providers.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { CAA_LETSENCRYPT, recordProblem } from '../ddns/ddns.ts';
	import type { DnsRecord, Domain, DomainDetail, Status } from './ipv64.ts';
	import { failure } from './ui.ts';

	let { status }: { status: Status } = $props();

	let domains = $state<Domain[]>([]);
	let ddnsHosts = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);
	let notice = $state('');
	let wish = $state('');
	let zone = $state('ipv64.de');
	let own = $state('');
	let confirmCreate = $state<{ name: string; own: boolean } | null>(null);
	let detail = $state<DomainDetail | null>(null);
	let prefix6 = $state('');
	let newRecord = $state({ type: 'A', prefix: '', content: '' });
	let editing = $state<DnsRecord | null>(null);
	let editContent = $state('');
	let confirm = $state<{ kind: 'delete' | 'reset' | 'rotate' | 'record'; record?: DnsRecord } | null>(null);

	async function load() {
		try {
			const answer = await api.get<{ domains: Domain[]; ddnsHosts: string[] }>('/ipv64/domains');
			domains = answer.domains;
			ddnsHosts = answer.ddnsHosts ?? [];
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
	async function open(name: string) {
		await run(async () => {
			detail = await api.get<DomainDetail>(`/ipv64/domains/${encodeURIComponent(name)}`);
			prefix6 = detail.ipv6Prefix ?? '';
		});
	}
	const path = $derived(detail ? `/ipv64/domains/${encodeURIComponent(detail.name)}` : '');
	async function change(body: Record<string, unknown>) {
		if (!detail) return;
		const name = detail.name;
		await run(async () => {
			await api.patch(path, body);
			await load();
			await open(name);
		}, t('ipv64x.saved'));
	}
	async function create() {
		if (!confirmCreate) return;
		const what = confirmCreate;
		if (
			await run(async () => {
				await api.post('/ipv64/domains', { name: what.name, own: what.own });
				await load();
			}, what.own ? t('ipv64x.ownCreated', { name: what.name }) : t('ipv64x.created'))
		) {
			confirmCreate = null;
			wish = own = '';
		}
	}
	async function confirmed() {
		if (!confirm || !detail) return;
		const name = detail.name;
		const what = confirm;
		const ok = await run(async () => {
			if (what.kind === 'delete') {
				await api.delete(path);
				detail = null;
			} else if (what.kind === 'reset') {
				await api.post(`${path}/reset`);
				await open(name);
			} else if (what.kind === 'rotate') {
				const answer = await api.post<{ ddnsUpdated: boolean }>(`${path}/rotate-token`);
				notice = answer.ddnsUpdated ? t('ipv64x.rotatedFollowed') : t('ipv64x.rotated');
			} else if (what.record) {
				await api.delete(`/ipv64/records/${what.record.token || what.record.id}`);
				await open(name);
			}
			await load();
		});
		if (ok) confirm = null;
	}
	const recordBad = $derived(recordProblem(newRecord.type, newRecord.content));
	async function addRecord(event: SubmitEvent) {
		event.preventDefault();
		if (!detail || recordBad) return;
		const name = detail.name;
		if (await run(async () => {
			await api.post(`${path}/records`, newRecord);
			await open(name);
		})) newRecord = { type: newRecord.type, prefix: '', content: '' };
	}
	async function saveRecord() {
		if (!editing || !detail) return;
		const name = detail.name;
		const record = editing;
		if (await run(async () => {
			await api.patch(`/ipv64/records/${record.token || record.id}`, { content: editContent.trim() });
			await open(name);
		})) editing = null;
	}
	const locked = (r: DnsRecord) => r.state.startsWith('cdn_') || r.state.startsWith('failover_');
</script>

<div class="ipv64-tab">
	<section class="tile panel" aria-labelledby="ipv64-domains">
		<h2 class="tile-title" id="ipv64-domains">{t('ipv64x.domains')}</h2>
		<p class="muted hint">{t('ipv64x.domainsHint')} <a href="/settings/ddns">{t('ipv64x.toDdns')}</a></p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.domain')}</th><th>{t('ipv64x.kind')}</th><th>{t('ipv64x.updates')}</th><th></th></tr></thead>
				<tbody>
					{#each domains as d (d.name)}
						<tr>
							<td class="mono"
								>{d.name}{#if d.wildcard}<span class="chip ok">*</span>{/if}{#if ddnsHosts.includes(d.name)}<span class="chip">{t('ipv64x.inDdns')}</span>{/if}{#if !d.active}<span class="chip error"
										>{t('ipv64x.inactive')}</span
									>{/if}</td
							>
							<td>{t(d.kind === 'own' ? 'ipv64x.kindOwn' : 'ipv64x.kindDyndns')}</td>
							<td>{d.updateCount}</td>
							<td class="actions"><button type="button" class="btn" onclick={() => open(d.name)}>{t('ipv64x.manage')}</button></td>
						</tr>
					{:else}
						<tr><td colspan="4" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="inline">
			<label class="stack"><span>{t('ipv64x.newName')}</span><input class="field mono" type="text" bind:value={wish} placeholder="meinnas" autocapitalize="off" spellcheck="false" /></label>
			<label class="stack"
				><span>{t('ipv64x.zone')}</span><select class="field mono" bind:value={zone}>{#each status.zones as z (z)}<option value={z}>{z}</option>{/each}</select></label
			>
			<button type="button" class="btn" disabled={busy || !validIpv64Wish(wish.trim().toLowerCase())} onclick={() => (confirmCreate = { name: `${wish.trim().toLowerCase()}.${zone}`, own: false })}
				>{t('ipv64x.create')}</button
			>
		</div>
		<div class="inline">
			<label class="stack grow"><span>{t('ipv64x.ownDomain')}</span><input class="field mono" type="text" bind:value={own} placeholder="example.org" autocapitalize="off" spellcheck="false" /></label>
			<button type="button" class="btn" disabled={busy || !/^[a-z0-9.-]+\.[a-z]{2,}$/i.test(own.trim())} onclick={() => (confirmCreate = { name: own.trim().toLowerCase(), own: true })}>{t('ipv64x.addOwn')}</button>
		</div>
	</section>

	{#if detail}
		<section class="tile panel" aria-labelledby="ipv64-domain">
			<h2 class="tile-title mono" id="ipv64-domain">{detail.name}</h2>
			<div class="inline">
				<label class="check"><input type="checkbox" checked={detail.wildcard} disabled={busy} onchange={(e) => change({ wildcard: e.currentTarget.checked })} /><span>{t('ipv64x.wildcard')}</span></label>
				<label class="stack"
					><span>{t('ipv64x.ignore')}</span><select class="field" value={detail.ignore} disabled={busy} onchange={(e) => change({ ignore: e.currentTarget.value })}>
						<option value="none">{t('ipv64x.ignoreNone')}</option>
						<option value="ipv4">{t('ipv64x.ignore4')}</option>
						<option value="ipv6">{t('ipv64x.ignore6')}</option>
					</select></label
				>
				<label class="stack grow"><span>{t('ipv64x.prefix6')}</span><input class="field mono" type="text" bind:value={prefix6} placeholder="2001:db8:1234:5600::/56" spellcheck="false" /></label>
				<button type="button" class="btn" disabled={busy || prefix6 === (detail.ipv6Prefix ?? '')} onclick={() => change({ ipv6_prefix: prefix6.trim() })}>{t('common.save')}</button>
			</div>
			<p class="muted hint">{t('ipv64x.wildcardHint')}</p>

			<h3>{t('ipv64x.records')}</h3>
			<div class="table-wrap">
				<table>
					<thead><tr><th>{t('ipv64x.type')}</th><th>{t('ipv64x.prefix')}</th><th>{t('ipv64x.content')}</th><th>{t('ipv64x.state')}</th><th></th></tr></thead>
					<tbody>
						{#each detail.records as r (r.id)}
							<tr>
								<td class="mono">{r.type}</td>
								<td class="mono">{r.prefix || '@'}</td>
								<td class="mono wrap">{r.content}{#if r.pending_dns}<span class="chip warn">{t('ipv64x.pendingDns')}</span>{/if}</td>
								<td>{t(`ipv64x.recordState.${r.state}` as 'ipv64x.recordState.active')}</td>
								<td class="actions">
									{#if locked(r)}<span class="muted">{t('ipv64x.recordLocked')}</span>{:else}
										<button type="button" class="btn" disabled={busy} onclick={() => ((editing = r), (editContent = r.content))}>{t('ipv64x.edit')}</button>
										<button type="button" class="btn" disabled={busy} onclick={() => (confirm = { kind: 'record', record: r })}>{t('ipv64x.delete')}</button>
									{/if}
								</td>
							</tr>
						{:else}
							<tr><td colspan="5" class="muted">{t('ipv64x.none')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<form class="inline" onsubmit={addRecord}>
				<label class="stack"
					><span>{t('ipv64x.type')}</span><select class="field mono" bind:value={newRecord.type}>{#each status.recordTypes as type (type)}<option value={type}>{type}</option>{/each}</select></label
				>
				<label class="stack"><span>{t('ipv64x.prefix')}</span><input class="field mono short" type="text" bind:value={newRecord.prefix} placeholder="@" spellcheck="false" autocapitalize="off" /></label>
				<label class="stack grow"><span>{t('ipv64x.content')}</span><input class="field mono" type="text" bind:value={newRecord.content} spellcheck="false" autocapitalize="off" /></label>
				<button class="btn" disabled={busy || recordBad}>{t('ipv64x.recordAdd')}</button>
				<button type="button" class="btn" onclick={() => (newRecord = { type: 'CAA', prefix: '', content: CAA_LETSENCRYPT })}>{t('ipv64x.caa')}</button>
			</form>
			<p class="muted hint">{t('ipv64x.recordHint')}</p>

			<h3>{t('ipv64x.dangerZone')}</h3>
			<div class="row-actions">
				<button type="button" class="btn btn-warn" disabled={busy} onclick={() => (confirm = { kind: 'rotate' })}>{t('ipv64x.rotate')}</button>
				<button type="button" class="btn btn-warn" disabled={busy} onclick={() => (confirm = { kind: 'reset' })}>{t('ipv64x.reset')}</button>
				<button type="button" class="btn btn-danger" disabled={busy} onclick={() => (confirm = { kind: 'delete' })}>{t('ipv64x.domainDelete')}</button>
			</div>
		</section>
	{/if}
</div>

{#if confirmCreate}
	<Dialog title={t('ipv64x.createTitle')} locked={busy} onclose={() => (confirmCreate = null)}>
		<p>{t(confirmCreate.own ? 'ipv64x.createOwnText' : 'ipv64x.createText', { name: confirmCreate.name })}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirmCreate = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={busy} onclick={create}>{t('ipv64x.create')}</button>
		</div>
	</Dialog>
{/if}

{#if editing}
	<Dialog title={t('ipv64x.recordEdit')} locked={busy} onclose={() => (editing = null)}>
		<p class="mono">{editing.type} {editing.fqdn}</p>
		<label class="stack"><span>{t('ipv64x.content')}</span><input class="field mono" type="text" bind:value={editContent} /></label>
		{#if editing.type !== 'A' && editing.type !== 'AAAA'}<p class="muted hint">{t('ipv64x.recordReplaced')}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (editing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={busy || recordProblem(editing.type, editContent)} onclick={saveRecord}>{t('common.save')}</button>
		</div>
	</Dialog>
{/if}

{#if confirm && detail}
	<Dialog title={t(`ipv64x.confirm.${confirm.kind}.title` as 'ipv64x.confirm.delete.title')} locked={busy} onclose={() => (confirm = null)}>
		<p>{t(`ipv64x.confirm.${confirm.kind}.text` as 'ipv64x.confirm.delete.text', { name: detail.name, record: confirm.record ? `${confirm.record.type} ${confirm.record.fqdn} ${confirm.record.content}` : '' })}</p>
		{#if confirm.kind === 'rotate' && ddnsHosts.includes(detail.name)}<p class="notice-warn">{t('ipv64x.rotateTitanOS')}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirm = null)}>{t('common.cancel')}</button>
			<button type="button" class={confirm.kind === 'rotate' ? 'btn btn-warn' : 'btn btn-danger'} disabled={busy} onclick={confirmed}
				>{t(`ipv64x.confirm.${confirm.kind}.button` as 'ipv64x.confirm.delete.button')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.chip {
		margin-left: 6px;
	}
</style>
