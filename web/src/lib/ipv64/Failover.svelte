<script lang="ts">
	// Failover: offline pages, DNS failover policies with monitors and rules,
	// and the one-click maintenance page for when this NAS fails.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		POLICY_MODES,
		iconProblem,
		tone,
		type Check,
		type DnsRecord,
		type Domain,
		type DomainDetail,
		type Maintenance,
		type OfflinePage,
		type Policy,
		type Status,
	} from './ipv64.ts';
	import { failure, when } from './ui.ts';

	let { status, onchange }: { status: Status; onchange: () => void } = $props();

	let policies = $state<Policy[]>([]);
	let maintenance = $state<Maintenance[]>([]);
	let pages = $state<OfflinePage[]>([]);
	let checks = $state<Check[]>([]);
	let domains = $state<Domain[]>([]);
	let error = $state('');
	let notice = $state('');
	let busy = $state(false);

	async function load() {
		try {
			const answer = await api.get<{ policies: Policy[]; maintenance: Maintenance[] }>(
				'/ipv64/failover',
			);
			policies = answer.policies;
			maintenance = answer.maintenance ?? [];
			pages = (await api.get<{ pages: OfflinePage[] }>('/ipv64/offline-pages')).pages;
			checks = (await api.get<{ healthchecks: Check[] }>('/ipv64/healthchecks')).healthchecks;
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

	// --- one click ---
	let wizard = $state<{ domain: string; text: string; email: string } | null>(null);
	async function setUp() {
		if (!wizard) return;
		const w = wizard;
		if (
			await run(async () => {
				await api.post('/ipv64/maintenance', { domain: w.domain, text: w.text, email: w.email });
				await load();
				onchange();
			}, t('ipv64x.wizardDone'))
		)
			wizard = null;
	}
	let removingSetup = $state<Maintenance | null>(null);
	let deleteCheck = $state(false);
	async function removeSetup() {
		if (!removingSetup) return;
		const m = removingSetup;
		if (
			await run(async () => {
				await api.delete(`/ipv64/maintenance/${m.id}`, { deleteCheck });
				await load();
				onchange();
			})
		)
			removingSetup = null;
	}

	// --- offline pages ---
	let pageForm = $state<{
		id: string;
		name: string;
		domain: string;
		email: string;
		phone: string;
		alternative_url: string;
		text: string;
	} | null>(null);
	function editPage(p?: OfflinePage) {
		pageForm = {
			id: p?.id ?? '',
			name: p?.name ?? '',
			domain: p?.domain ?? '',
			email: p?.email ?? '',
			phone: p?.phone ?? '',
			alternative_url: p?.alternative_url ?? '',
			text: p?.text ?? '',
		};
	}
	async function savePage() {
		if (!pageForm) return;
		const { id, ...body } = pageForm;
		if (
			await run(async () => {
				if (id) await api.patch(`/ipv64/offline-pages/${id}`, body);
				else await api.post('/ipv64/offline-pages', body);
				await load();
			}, t('ipv64x.saved'))
		)
			pageForm = null;
	}
	async function upload(p: OfflinePage, file: File | undefined) {
		if (!file) return;
		const problem = iconProblem(file);
		if (problem) {
			error = t(problem === 'type' ? 'ipv64x.iconType' : 'ipv64x.iconSize');
			return;
		}
		await run(async () => {
			await api.put(`/ipv64/offline-pages/${p.id}/icon`, file);
			await load();
		}, t('ipv64x.iconDone'));
	}

	// --- policies ---
	let policyForm = $state<{
		id: string;
		name: string;
		mode: string;
		linking: string;
		fallbackAll: boolean;
		notify: boolean;
	} | null>(null);
	function editPolicy(p?: Policy) {
		policyForm = {
			id: p?.id ?? '',
			name: p?.name ?? '',
			mode: p?.mode ?? 'auto_failover_and_fallback',
			linking: p?.linking ?? 'or',
			fallbackAll: p?.fallback_requires_all ?? false,
			notify: p?.notifications_enabled ?? true,
		};
	}
	async function savePolicy() {
		if (!policyForm) return;
		const f = policyForm;
		const body = {
			name: f.name.trim(),
			mode: f.mode,
			linking: f.linking,
			fallback_requires_all: f.fallbackAll,
			notifications_enabled: f.notify,
		};
		if (
			await run(async () => {
				if (f.id) await api.patch(`/ipv64/failover/${f.id}`, body);
				else await api.post('/ipv64/failover', { ...body, integrations: [0] });
				await load();
			}, t('ipv64x.saved'))
		)
			policyForm = null;
	}
	let monitor = $state<{ policy: Policy; check: string; negate: boolean } | null>(null);
	async function addMonitor() {
		if (!monitor) return;
		const m = monitor;
		if (
			await run(async () => {
				await api.post(`/ipv64/failover/${m.policy.id}/monitors`, {
					healthcheck: m.check,
					negate: m.negate,
				});
				await load();
			})
		)
			monitor = null;
	}
	let rule = $state<{
		policy: Policy;
		domain: string;
		records: DnsRecord[];
		record: string;
		kind: 'offline_page' | 'target_ip' | 'target_record';
		value: string;
	} | null>(null);
	async function ruleDomain(domain: string) {
		if (!rule) return;
		rule.domain = domain;
		const detail = await api.get<DomainDetail>(`/ipv64/domains/${encodeURIComponent(domain)}`);
		if (rule) rule.records = detail.records.filter((r) => r.type === 'A' || r.type === 'AAAA');
	}
	async function addRule() {
		if (!rule) return;
		const r = rule;
		if (
			await run(async () => {
				await api.post(`/ipv64/failover/${r.policy.id}/rules`, {
					record: r.record,
					[r.kind]: r.value || (r.kind === 'offline_page' ? 'default' : ''),
				});
				await load();
			})
		)
			rule = null;
	}
	let confirm = $state<{
		title: MessageKey;
		text: string;
		button: MessageKey;
		danger: boolean;
		run: () => Promise<unknown>;
	} | null>(null);
	async function confirmed() {
		if (!confirm) return;
		const what = confirm;
		if (
			await run(async () => {
				await what.run();
				await load();
			})
		)
			confirm = null;
	}
	const pageName = (id?: string) => pages.find((p) => p.id === id)?.name || id || '';
	const nasCheck = $derived(status.nasCheck);
</script>

<div class="ipv64-tab">
	<section class="tile panel" aria-labelledby="ipv64-wizard">
		<h2 class="tile-title" id="ipv64-wizard">{t('ipv64x.wizard')}</h2>
		<p>{t('ipv64x.wizardText')}</p>
		<ol class="steps">
			<li>{nasCheck ? t('ipv64x.wizardStepCheckKept') : t('ipv64x.wizardStepCheck')}</li>
			<li>{t('ipv64x.wizardStepPage')}</li>
			<li>{t('ipv64x.wizardStepPolicy')}</li>
		</ol>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}
		{#each maintenance as m (m.id)}
			<div class="setup">
				<strong class="mono">{m.domain}</strong> <span class="muted">{when(m.at)}</span>
				<ul>
					<li>
						{t('ipv64x.resCheck', { name: m.checkName })}{#if m.createdCheck}
							({t('ipv64x.madeForIt')}){/if}
					</li>
					<li>{t('ipv64x.resPage', { name: pageName(m.offlinePage) })}</li>
					<li>
						{t('ipv64x.resPolicy', {
							name: policies.find((p) => p.id === m.policy)?.name ?? m.policy,
						})}
					</li>
					<li>{t('ipv64x.resRules', { count: m.records.length })}</li>
				</ul>
				<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() => ((removingSetup = m), (deleteCheck = false))}>{t('ipv64x.remove')}</button
				>
			</div>
		{/each}
		<div class="row-actions">
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !domains.length}
				onclick={() => (wizard = { domain: domains[0]?.name ?? '', text: '', email: '' })}
				>{t('ipv64x.wizardStart')}</button
			>
		</div>
	</section>

	<section class="tile panel" aria-labelledby="ipv64-policies">
		<h2 class="tile-title" id="ipv64-policies">{t('ipv64x.policies')}</h2>
		<p class="muted hint">{t('ipv64x.policiesHint')}</p>
		{#each policies as p (p.id)}
			<div class="policy divided">
				<div class="inline">
					<strong class="grow">{p.name}</strong>
					<span class="chip {tone(p.status)}"
						>{t(`ipv64x.policyState.${p.status}` as MessageKey)}</span
					>
					<span class="muted"
						>{t(`ipv64x.mode.${p.mode}` as MessageKey)} · {t(
							p.linking === 'and' ? 'ipv64x.linkAnd' : 'ipv64x.linkOr',
						)}</span
					>
				</div>
				<div class="table-wrap">
					<table>
						<thead
							><tr><th>{t('ipv64x.monitors')}</th><th>{t('ipv64x.state')}</th><th></th></tr></thead
						>
						<tbody>
							{#each p.monitors as m (m.healthcheck)}
								<tr>
									<td
										>{m.name}{#if m.healthcheck === nasCheck}<span class="chip ok"
												>{t('ipv64x.thisNas')}</span
											>{/if}{#if m.negate}<span class="chip">{t('ipv64x.negated')}</span>{/if}</td
									>
									<td>{m.healthcheck_status ?? '–'}</td>
									<td class="actions"
										><button
											type="button"
											class="btn"
											disabled={busy}
											onclick={() =>
												run(async () => {
													await api.delete(`/ipv64/failover/${p.id}/monitors/${m.healthcheck}`);
													await load();
												})}>{t('ipv64x.remove')}</button
										></td
									>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<div class="table-wrap">
					<table>
						<thead
							><tr
								><th>{t('ipv64x.rules')}</th><th>{t('ipv64x.target')}</th><th
									>{t('ipv64x.state')}</th
								><th></th></tr
							></thead
						>
						<tbody>
							{#each p.rules as r (r.record)}
								<tr>
									<td class="mono">{r.record_type} {r.fqdn ?? r.record}</td>
									<td
										>{r.target.kind === 'offline_page'
											? t('ipv64x.offlinePage') + ' ' + pageName(r.target.offline_page)
											: r.target.ip || r.target.record}</td
									>
									<td
										><span class="chip {tone(r.state)}"
											>{t(
												r.state === 'failed_over' ? 'ipv64x.ruleFailed' : 'ipv64x.ruleStandby',
											)}</span
										></td
									>
									<td class="actions"
										><button
											type="button"
											class="btn"
											disabled={busy}
											onclick={() =>
												run(async () => {
													await api.delete(`/ipv64/failover/${p.id}/rules/${r.record}`);
													await load();
												})}>{t('ipv64x.remove')}</button
										></td
									>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
				<div class="row-actions">
					<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() => (monitor = { policy: p, check: checks[0]?.id ?? '', negate: false })}
						>{t('ipv64x.monitorAdd')}</button
					>
					<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() =>
							(rule = {
								policy: p,
								domain: '',
								records: [],
								record: '',
								kind: 'offline_page',
								value: pages[0]?.id ?? 'default',
							})}>{t('ipv64x.ruleAdd')}</button
					>
					<button type="button" class="btn" disabled={busy} onclick={() => editPolicy(p)}
						>{t('ipv64x.edit')}</button
					>
					<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() =>
							run(async () => {
								await api.post(
									`/ipv64/failover/${p.id}/do/${p.status === 'paused' ? 'resume' : 'pause'}`,
								);
								await load();
							})}>{t(p.status === 'paused' ? 'ipv64x.resume' : 'ipv64x.pause')}</button
					>
					{#if p.status === 'active'}<button
							type="button"
							class="btn btn-warn"
							disabled={busy}
							onclick={() =>
								(confirm = {
									title: 'ipv64x.failoverTitle',
									text: t('ipv64x.failoverText', { name: p.name }),
									button: 'ipv64x.failoverNow',
									danger: false,
									run: () => api.post(`/ipv64/failover/${p.id}/do/failover`),
								})}>{t('ipv64x.failoverNow')}</button
						>{/if}
					{#if p.status === 'failed_over'}<button
							type="button"
							class="btn btn-warn"
							disabled={busy}
							onclick={() =>
								(confirm = {
									title: 'ipv64x.fallbackTitle',
									text: t('ipv64x.fallbackText', { name: p.name }),
									button: 'ipv64x.fallbackNow',
									danger: false,
									run: () => api.post(`/ipv64/failover/${p.id}/do/fallback`),
								})}>{t('ipv64x.fallbackNow')}</button
						>{/if}
					<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() =>
							(confirm = {
								title: 'ipv64x.policyDeleteTitle',
								text: t('ipv64x.policyDeleteText', { name: p.name }),
								button: 'ipv64x.delete',
								danger: true,
								run: () => api.delete(`/ipv64/failover/${p.id}`),
							})}>{t('ipv64x.delete')}</button
					>
				</div>
			</div>
		{:else}
			<p class="muted">{t('ipv64x.none')}</p>
		{/each}
		<div class="row-actions">
			<button type="button" class="btn" onclick={() => editPolicy()}
				>{t('ipv64x.policyCreate')}</button
			>
		</div>
	</section>

	<section class="tile panel" aria-labelledby="ipv64-offline">
		<h2 class="tile-title" id="ipv64-offline">{t('ipv64x.offlinePages')}</h2>
		<p class="muted hint">{t('ipv64x.offlineHint')}</p>
		<div class="table-wrap">
			<table>
				<thead
					><tr
						><th>{t('ipv64x.name')}</th><th>{t('ipv64x.domain')}</th><th>{t('ipv64x.state')}</th><th
						></th></tr
					></thead
				>
				<tbody>
					{#each pages as p (p.id)}
						<tr>
							<td
								>{#if p.icon_url}<img
										src={p.icon_url}
										alt=""
										class="icon"
										referrerpolicy="no-referrer"
									/>{/if}{p.name}</td
							>
							<td class="mono">{p.domain}</td>
							<td
								>{#if p.active}<span class="chip error">{t('ipv64x.shownNow')}</span
									>{:else if p.in_use_by_policy}<span class="chip">{t('ipv64x.inUse')}</span
									>{/if}</td
							>
							<td class="actions">
								<a class="btn" href={p.preview_url} target="_blank" rel="noopener noreferrer"
									>{t('ipv64x.preview')}</a
								>
								<label class="btn upload"
									>{t('ipv64x.icon')}<input
										type="file"
										accept="image/jpeg,image/png,image/webp"
										onchange={(e) => upload(p, e.currentTarget.files?.[0])}
									/></label
								>
								{#if p.icon_url}<button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() =>
											run(async () => {
												await api.delete(`/ipv64/offline-pages/${p.id}/icon`);
												await load();
											})}>{t('ipv64x.iconRemove')}</button
									>{/if}
								<button type="button" class="btn" disabled={busy} onclick={() => editPage(p)}
									>{t('ipv64x.edit')}</button
								>
								<button
									type="button"
									class="btn"
									disabled={busy || !!p.in_use_by_policy}
									onclick={() =>
										(confirm = {
											title: 'ipv64x.pageDeleteTitle',
											text: t('ipv64x.pageDeleteText', { name: p.name }),
											button: 'ipv64x.delete',
											danger: true,
											run: () => api.delete(`/ipv64/offline-pages/${p.id}`),
										})}>{t('ipv64x.delete')}</button
								>
							</td>
						</tr>
					{:else}
						<tr><td colspan="4" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<p class="muted hint">{t('ipv64x.iconHint')}</p>
		<div class="row-actions">
			<button type="button" class="btn" onclick={() => editPage()}
				>{t('ipv64x.offlineCreate')}</button
			>
		</div>
	</section>
</div>

{#if wizard}
	<Dialog title={t('ipv64x.wizard')} locked={busy} onclose={() => (wizard = null)}>
		<p>{t('ipv64x.wizardConfirm')}</p>
		<label class="stack"
			><span>{t('ipv64x.domain')}</span><select class="field mono" bind:value={wizard.domain}
				>{#each domains as d (d.name)}<option value={d.name}>{d.name}</option>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('ipv64x.pageText')} <span class="muted">({t('ipv64x.optional')})</span></span><input
				class="field"
				type="text"
				bind:value={wizard.text}
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.field.email')} <span class="muted">({t('ipv64x.optional')})</span></span
			><input class="field" type="email" bind:value={wizard.email} /></label
		>
		<p class="muted hint">{t('ipv64x.wizardNeedsRecord')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (wizard = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !wizard.domain}
				onclick={setUp}>{t('ipv64x.wizardStart')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removingSetup}
	<Dialog title={t('ipv64x.wizardRemove')} locked={busy} onclose={() => (removingSetup = null)}>
		<p>{t('ipv64x.wizardRemoveText', { domain: removingSetup.domain })}</p>
		{#if removingSetup.createdCheck}<label class="check"
				><input type="checkbox" bind:checked={deleteCheck} /><span
					>{t('ipv64x.wizardRemoveCheck')}</span
				></label
			>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removingSetup = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={removeSetup}
				>{t('ipv64x.remove')}</button
			>
		</div>
	</Dialog>
{/if}

{#if pageForm}
	<Dialog
		title={t(pageForm.id ? 'ipv64x.offlineEdit' : 'ipv64x.offlineCreate')}
		locked={busy}
		onclose={() => (pageForm = null)}
	>
		<label class="stack"
			><span>{t('ipv64x.headline')}</span><input
				class="field"
				type="text"
				maxlength="200"
				bind:value={pageForm.name}
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.domain')}</span><select class="field mono" bind:value={pageForm.domain}
				><option value=""></option>{#each domains as d (d.name)}<option value={d.name}
						>{d.name}</option
					>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('ipv64x.field.email')}</span><input
				class="field"
				type="email"
				bind:value={pageForm.email}
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.phone')}</span><input
				class="field"
				type="tel"
				bind:value={pageForm.phone}
				placeholder="+49…"
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.alternative')}</span><input
				class="field mono"
				type="url"
				bind:value={pageForm.alternative_url}
				placeholder="https://"
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.pageText')}</span><textarea
				class="field"
				rows="4"
				bind:value={pageForm.text}></textarea></label
		>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (pageForm = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" disabled={busy} onclick={savePage}
				>{t('common.save')}</button
			>
		</div>
	</Dialog>
{/if}

{#if policyForm}
	<Dialog
		title={t(policyForm.id ? 'ipv64x.policyEdit' : 'ipv64x.policyCreate')}
		locked={busy}
		onclose={() => (policyForm = null)}
	>
		<label class="stack"
			><span>{t('ipv64x.name')}</span><input
				class="field"
				type="text"
				maxlength="100"
				bind:value={policyForm.name}
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.modeLabel')}</span><select class="field" bind:value={policyForm.mode}
				>{#each POLICY_MODES as m (m)}<option value={m}
						>{t(`ipv64x.mode.${m}` as MessageKey)}</option
					>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('ipv64x.linking')}</span><select class="field" bind:value={policyForm.linking}
				><option value="or">{t('ipv64x.linkOr')}</option><option value="and"
					>{t('ipv64x.linkAnd')}</option
				></select
			></label
		>
		<label class="check"
			><input type="checkbox" bind:checked={policyForm.fallbackAll} /><span
				>{t('ipv64x.fallbackAll')}</span
			></label
		>
		<label class="check"
			><input type="checkbox" bind:checked={policyForm.notify} /><span
				>{t('ipv64x.policyNotify')}</span
			></label
		>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (policyForm = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !policyForm.name.trim()}
				onclick={savePolicy}>{t('common.save')}</button
			>
		</div>
	</Dialog>
{/if}

{#if monitor}
	<Dialog title={t('ipv64x.monitorAdd')} locked={busy} onclose={() => (monitor = null)}>
		<label class="stack"
			><span>{t('ipv64x.checks')}</span><select class="field" bind:value={monitor.check}
				>{#each checks as c (c.id)}<option value={c.id}
						>{c.name}{c.id === nasCheck ? ` (${t('ipv64x.thisNas')})` : ''}</option
					>{/each}</select
			></label
		>
		<label class="check"
			><input type="checkbox" bind:checked={monitor.negate} /><span>{t('ipv64x.negate')}</span
			></label
		>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (monitor = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !monitor.check}
				onclick={addMonitor}>{t('ipv64x.monitorAdd')}</button
			>
		</div>
	</Dialog>
{/if}

{#if rule}
	<Dialog title={t('ipv64x.ruleAdd')} locked={busy} onclose={() => (rule = null)}>
		<label class="stack"
			><span>{t('ipv64x.domain')}</span><select
				class="field mono"
				value={rule.domain}
				onchange={(e) => ruleDomain(e.currentTarget.value)}
				><option value=""></option>{#each domains as d (d.name)}<option value={d.name}
						>{d.name}</option
					>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('ipv64x.protected')}</span><select class="field mono" bind:value={rule.record}
				><option value=""></option>{#each rule.records as r (r.token)}<option value={r.token}
						>{r.type} {r.fqdn} {r.content}</option
					>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('ipv64x.target')}</span><select class="field" bind:value={rule.kind}>
				<option value="offline_page">{t('ipv64x.offlinePage')}</option>
				<option value="target_ip">{t('ipv64x.targetIP')}</option>
				<option value="target_record">{t('ipv64x.targetRecord')}</option>
			</select></label
		>
		{#if rule.kind === 'offline_page'}
			<select class="field" bind:value={rule.value} aria-label={t('ipv64x.offlinePage')}
				><option value="default">{t('ipv64x.defaultPage')}</option>{#each pages as p (p.id)}<option
						value={p.id}>{p.name}</option
					>{/each}</select
			>
		{:else}
			<input
				class="field mono"
				type="text"
				bind:value={rule.value}
				aria-label={t('ipv64x.target')}
				placeholder={rule.kind === 'target_ip' ? '203.0.113.5' : t('ipv64x.recordToken')}
			/>
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (rule = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !rule.record || (rule.kind !== 'offline_page' && !rule.value.trim())}
				onclick={addRule}>{t('ipv64x.ruleAdd')}</button
			>
		</div>
	</Dialog>
{/if}

{#if confirm}
	<Dialog title={t(confirm.title)} locked={busy} onclose={() => (confirm = null)}>
		<p>{confirm.text}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirm = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class={confirm.danger ? 'btn btn-danger' : 'btn btn-warn'}
				disabled={busy}
				onclick={confirmed}>{t(confirm.button)}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.chip {
		margin-left: 6px;
	}
	.steps {
		margin: 0;
		padding-left: 1.25rem;
	}
	.setup {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
	}
	.setup ul {
		margin: 6px 0;
		padding-left: 1.25rem;
	}
	.policy {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.icon {
		width: 20px;
		height: 20px;
		object-fit: contain;
		vertical-align: middle;
		margin-right: 6px;
	}
	.upload input {
		display: none;
	}
	textarea {
		height: auto;
		padding: 8px 12px;
	}
</style>
