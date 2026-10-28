<script lang="ts">
	// Monitoring: healthchecks of every type with events, incidents and
	// statistics, integrations (secrets write-only) and status pages.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		CHECK_TYPES,
		INTEGRATION_FIELDS,
		STATS_RANGES,
		chartPoints,
		checkBody,
		checkForm,
		checkProblems,
		integrationBody,
		integrationProblems,
		statusPageURL,
		tone,
		validSlug,
		type Check,
		type CheckEvent,
		type CheckForm,
		type Incident,
		type Integration,
		type Stats,
		type Status,
		type StatusPage,
	} from './ipv64.ts';
	import { failure, when } from './ui.ts';

	let { status }: { status: Status } = $props();

	let checks = $state<Check[]>([]);
	let integrations = $state<Integration[]>([]);
	let nasCheck = $state('');
	let pages = $state<StatusPage[]>([]);
	let error = $state('');
	let notice = $state('');
	let busy = $state(false);

	async function load() {
		try {
			const answer = await api.get<{ healthchecks: Check[]; integrations: Integration[]; nasCheck: string }>('/ipv64/healthchecks');
			checks = answer.healthchecks;
			integrations = answer.integrations;
			nasCheck = answer.nasCheck;
			pages = (await api.get<{ pages: StatusPage[] }>('/ipv64/status-pages')).pages;
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

	// --- one check in detail ---
	let selected = $state<Check | null>(null);
	let range = $state<string>('24h');
	let stats = $state<Stats | null>(null);
	let events = $state<CheckEvent[]>([]);
	let incidents = $state<Incident[]>([]);
	async function show(c: Check) {
		selected = c;
		await run(async () => {
			const [s, e, i] = await Promise.all([
				api.get<Stats>(`/ipv64/healthchecks/${c.id}/stats?range=${range}`),
				api.get<{ events: CheckEvent[] }>(`/ipv64/healthchecks/${c.id}/events`),
				api.get<{ incidents: Incident[] }>(`/ipv64/healthchecks/${c.id}/incidents`),
			]);
			stats = s;
			events = e.events;
			incidents = i.incidents;
		});
	}
	async function setRange(next: string) {
		range = next;
		if (selected) {
			const id = selected.id;
			await run(async () => (stats = await api.get<Stats>(`/ipv64/healthchecks/${id}/stats?range=${next}`)));
		}
	}

	// --- create and edit ---
	let form = $state<CheckForm | null>(null);
	let editID = $state('');
	const problems = $derived(form ? checkProblems(form) : []);
	function edit(c?: Check) {
		form = checkForm(c);
		editID = c?.id ?? '';
	}
	async function saveCheck() {
		if (!form || problems.length) return;
		const body = checkBody(form);
		if (await run(async () => {
			if (editID) await api.patch(`/ipv64/healthchecks/${editID}`, body);
			else await api.post('/ipv64/healthchecks', body);
			await load();
		}, t('ipv64x.saved'))) form = null;
	}
	let testWait = $state<Record<string, boolean>>({});
	async function action(c: Check, what: string) {
		if (what === 'test-notification') {
			testWait = { ...testWait, [c.id]: true };
			setTimeout(() => (testWait = { ...testWait, [c.id]: false }), 30_000);
		}
		await run(async () => {
			const answer = await api.post<{ nasUpdated?: boolean }>(`/ipv64/healthchecks/${c.id}/${what}`);
			if (what === 'rotate-token') notice = answer?.nasUpdated ? t('ipv64x.checkRotatedNas') : t('ipv64x.checkRotated');
			await load();
		}, what === 'test-notification' ? t('ipv64x.testSent') : '');
	}
	let confirm = $state<{ kind: 'deleteCheck' | 'rotateCheck' | 'deleteIntegration' | 'deletePage'; name: string; run: () => Promise<unknown> } | null>(null);
	async function confirmed() {
		if (!confirm) return;
		const what = confirm;
		let message = '';
		if (await run(async () => {
			const answer = (await what.run()) as { nasUpdated?: boolean } | undefined;
			if (what.kind === 'rotateCheck') message = answer?.nasUpdated ? t('ipv64x.checkRotatedNas') : t('ipv64x.checkRotated');
			await load();
		})) {
			confirm = null;
			notice = message;
		}
	}

	// --- integrations ---
	let newIntegration = $state<{ type: string; name: string; preselect: boolean; values: Record<string, string> } | null>(null);
	const integrationBad = $derived(newIntegration ? integrationProblems(newIntegration.type, newIntegration.values).length > 0 || !newIntegration.name.trim() : true);
	async function createIntegration() {
		if (!newIntegration || integrationBad) return;
		const n = newIntegration;
		if (await run(async () => {
			await api.post('/ipv64/integrations', integrationBody(n.type, n.name, n.preselect, n.values));
			await load();
		}, n.type === 'email' || n.type === 'sms' ? t('ipv64x.integrationVerify') : t('ipv64x.saved'))) newIntegration = null;
	}
	let integrationWait = $state<Record<number, boolean>>({});
	async function testIntegration(i: Integration) {
		integrationWait = { ...integrationWait, [i.id]: true };
		setTimeout(() => (integrationWait = { ...integrationWait, [i.id]: false }), 30_000);
		await run(() => api.post(`/ipv64/integrations/${i.id}/test`), t('ipv64x.testSent'));
	}

	// --- status pages ---
	let pageForm = $state<{ id: number; name: string; description: string; slug: string; password: string; clearPassword: boolean; design: number; autorefresh: boolean; checks: string[] } | null>(null);
	function editPage(p?: StatusPage) {
		pageForm = { id: p?.id ?? 0, name: p?.name ?? '', description: p?.description ?? '', slug: p?.slug ?? '', password: '', clearPassword: false, design: p?.design || 1, autorefresh: p?.autorefresh ?? true, checks: [...(p?.healthchecks ?? [])] };
	}
	function move(i: number, by: number) {
		if (!pageForm) return;
		const list = [...pageForm.checks];
		const [item] = list.splice(i, 1);
		list.splice(Math.max(0, Math.min(list.length, i + by)), 0, item);
		pageForm.checks = list;
	}
	async function savePage() {
		if (!pageForm) return;
		const f = pageForm;
		const body: Record<string, unknown> = { name: f.name.trim(), description: f.description, design: f.design, autorefresh: f.autorefresh, healthchecks: f.checks };
		if (f.slug) body.slug = f.slug;
		if (f.password) body.password = f.password;
		else if (f.clearPassword) body.password = '';
		if (await run(async () => {
			if (f.id) await api.patch(`/ipv64/status-pages/${f.id}`, body);
			else await api.post('/ipv64/status-pages', body);
			await load();
		}, t('ipv64x.saved'))) pageForm = null;
	}
	const checkName = (id: string) => checks.find((c) => c.id === id)?.name ?? id;
</script>

<div class="ipv64-tab">
	<section class="tile panel" aria-labelledby="ipv64-checks">
		<h2 class="tile-title" id="ipv64-checks">{t('ipv64x.checks')}</h2>
		<p class="muted hint">{t('ipv64x.checksHint')} <a href="/settings/ddns#ipv64-heartbeat">{t('ipv64x.toHeartbeat')}</a></p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.name')}</th><th>{t('ipv64x.type')}</th><th>{t('ipv64x.state')}</th><th>{t('ipv64x.lastSeen')}</th><th></th></tr></thead>
				<tbody>
					{#each checks as c (c.id)}
						<tr>
							<td>{c.name}{#if c.id === nasCheck}<span class="chip ok">{t('ipv64x.thisNas')}</span>{/if}</td>
							<td>{t(`ipv64x.checkType.${c.type}` as MessageKey)}</td>
							<td><span class="chip {tone(c.status)}">{t(`ipv64x.checkState.${c.status}` as MessageKey)}</span></td>
							<td>{when(c.last_seen_at)}</td>
							<td class="actions">
								<button type="button" class="btn" onclick={() => show(c)}>{t('ipv64x.details')}</button>
								<button type="button" class="btn" disabled={busy} onclick={() => edit(c)}>{t('ipv64x.edit')}</button>
								<button type="button" class="btn" disabled={busy} onclick={() => action(c, c.status === 'paused' ? 'resume' : 'pause')}>{t(c.status === 'paused' ? 'ipv64x.resume' : 'ipv64x.pause')}</button>
								<button type="button" class="btn" disabled={busy || testWait[c.id]} onclick={() => action(c, 'test-notification')}>{t('ipv64x.test')}</button>
								<button type="button" class="btn" disabled={busy} onclick={() => (confirm = { kind: 'rotateCheck', name: c.name, run: () => api.post(`/ipv64/healthchecks/${c.id}/rotate-token`) })}>{t('ipv64x.newToken')}</button>
								<button type="button" class="btn" disabled={busy} onclick={() => (confirm = { kind: 'deleteCheck', name: c.name + (c.id === nasCheck ? ` (${t('ipv64x.thisNas')})` : ''), run: () => api.delete(`/ipv64/healthchecks/${c.id}`) })}
									>{t('ipv64x.delete')}</button
								>
							</td>
						</tr>
					{:else}
						<tr><td colspan="5" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="row-actions"><button type="button" class="btn" onclick={() => edit()}>{t('ipv64x.checkCreate')}</button></div>
	</section>

	{#if selected}
		<section class="tile panel" aria-labelledby="ipv64-check">
			<h2 class="tile-title" id="ipv64-check">{selected.name}</h2>
			<div class="row-actions" role="group" aria-label={t('ipv64x.range')}>
				{#each STATS_RANGES as r (r)}<button type="button" class="btn" aria-pressed={range === r} onclick={() => setRange(r)}>{r}</button>{/each}
			</div>
			{#if stats}
				<dl>
					<div><dt>{t('ipv64x.availability')}</dt><dd>{stats.availability_percent === null ? '–' : `${stats.availability_percent.toFixed(2)} %`}</dd></div>
					<div><dt>{t('ipv64x.latency')}</dt><dd>{stats.latency_ms.avg === null ? '–' : `${Math.round(stats.latency_ms.avg)} ms (${stats.latency_ms.min ?? '–'}–${stats.latency_ms.max ?? '–'})`}</dd></div>
					<div><dt>{t('ipv64x.measurements')}</dt><dd>{stats.measurements}</dd></div>
				</dl>
				{#if stats.series.length}
					<svg class="chart" viewBox="0 0 300 60" preserveAspectRatio="none" role="img" aria-label={t('ipv64x.chart')}>
						<polyline points={chartPoints(stats.series, 300, 60)} fill="none" stroke="var(--accent)" stroke-width="2" vector-effect="non-scaling-stroke" />
					</svg>
				{/if}
			{/if}
			<h3>{t('ipv64x.events')}</h3>
			<ul class="plain">
				{#each events as e (e.id)}<li><span class="chip {tone(e.status)}">{t(`ipv64x.checkState.${e.status}` as MessageKey)}</span> {when(e.time)} {e.message}{#if e.latency_ms !== null} · {e.latency_ms} ms{/if}</li>{:else}<li class="muted">{t('ipv64x.none')}</li>{/each}
			</ul>
			<h3>{t('ipv64x.incidents')}</h3>
			<ul class="plain">
				{#each incidents as i (i.id)}<li>{when(i.created_at)} – {i.closed_at ? when(i.closed_at) : t('ipv64x.open')} <span class="chip {i.status === 'open' ? 'error' : ''}">{t(i.status === 'open' ? 'ipv64x.open' : 'ipv64x.closed')}</span></li>{:else}<li class="muted">{t('ipv64x.none')}</li>{/each}
			</ul>
		</section>
	{/if}

	<section class="tile panel" aria-labelledby="ipv64-integrations">
		<h2 class="tile-title" id="ipv64-integrations">{t('ipv64x.integrations')}</h2>
		<p class="muted hint">{t('ipv64x.integrationsHint')}</p>
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.name')}</th><th>{t('ipv64x.type')}</th><th>{t('ipv64x.state')}</th><th></th></tr></thead>
				<tbody>
					{#each integrations as i (i.id)}
						<tr>
							<td>{i.name}{#if i.default}<span class="chip">{t('ipv64x.preselected')}</span>{/if}</td>
							<td>{t(`ipv64x.integrationType.${i.type}` as MessageKey)}</td>
							<td><span class="chip {tone(i.status)}">{t(i.status === 'active' ? 'ipv64x.active' : 'ipv64x.unconfirmed')}</span></td>
							<td class="actions">
								<button type="button" class="btn" disabled={busy || integrationWait[i.id]} onclick={() => testIntegration(i)}>{t('ipv64x.test')}</button>
								{#if i.id !== 0}
									<button type="button" class="btn" disabled={busy} onclick={() => run(async () => { await api.patch(`/ipv64/integrations/${i.id}`, { default: !i.default }); await load(); })}
										>{t(i.default ? 'ipv64x.unpreselect' : 'ipv64x.preselect')}</button
									>
									<button type="button" class="btn" disabled={busy} onclick={() => (confirm = { kind: 'deleteIntegration', name: i.name, run: () => api.delete(`/ipv64/integrations/${i.id}`) })}>{t('ipv64x.delete')}</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="row-actions"><button type="button" class="btn" onclick={() => (newIntegration = { type: 'ntfy', name: '', preselect: false, values: {} })}>{t('ipv64x.integrationCreate')}</button></div>
	</section>

	<section class="tile panel" aria-labelledby="ipv64-pages">
		<h2 class="tile-title" id="ipv64-pages">{t('ipv64x.statusPages')}</h2>
		<p class="muted hint">{t('ipv64x.statusPagesHint')}</p>
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.name')}</th><th>{t('ipv64x.address')}</th><th></th></tr></thead>
				<tbody>
					{#each pages as p (p.id)}
						<tr>
							<td>{p.name}{#if p.password_protected}<span class="chip">{t('ipv64x.withPassword')}</span>{/if}</td>
							<td><a href={statusPageURL(p.slug)} target="_blank" rel="noopener noreferrer" class="mono">ipv64.net/status/{p.slug}</a></td>
							<td class="actions">
								<button type="button" class="btn" disabled={busy} onclick={() => editPage(p)}>{t('ipv64x.edit')}</button>
								<button type="button" class="btn" disabled={busy} onclick={() => (confirm = { kind: 'deletePage', name: p.name || p.slug, run: () => api.delete(`/ipv64/status-pages/${p.id}`) })}>{t('ipv64x.delete')}</button>
							</td>
						</tr>
					{:else}
						<tr><td colspan="3" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="row-actions"><button type="button" class="btn" onclick={() => editPage()}>{t('ipv64x.pageCreate')}</button></div>
	</section>
</div>

{#if form}
	<Dialog title={t(editID ? 'ipv64x.checkEdit' : 'ipv64x.checkCreate')} locked={busy} onclose={() => (form = null)}>
		<label class="stack"><span>{t('ipv64x.name')}</span><input class="field" type="text" maxlength="100" bind:value={form.name} /></label>
		<label class="stack"
			><span>{t('ipv64x.type')}</span><select class="field" bind:value={form.type}>
				{#each CHECK_TYPES as type (type)}<option value={type}>{t(`ipv64x.checkType.${type}` as MessageKey)}</option>{/each}
			</select></label
		>
		{#if editID}<p class="muted hint">{t('ipv64x.typeChangeHint')}</p>{/if}
		{#if form.type === 'push'}
			<div class="inline">
				<label class="stack"><span>{t('ipv64x.period')}</span><input class="field short" type="number" min="1" bind:value={form.period} /></label>
				<label class="stack"><span>{t('ipv64x.grace')}</span><input class="field short" type="number" min="1" bind:value={form.grace} /></label>
			</div>
			<p class="muted hint">{t('ipv64x.pushHint')}</p>
		{:else}
			{#if form.type === 'icmp' || form.type === 'tcp'}
				<label class="stack"><span>{t('ipv64x.target')}</span><input class="field mono" type="text" bind:value={form.target} placeholder="nas.example.org" /></label>
				<label class="check"><input type="checkbox" bind:checked={form.preferIPv4} /><span>{t('ipv64x.preferIPv4')}</span></label>
			{/if}
			{#if form.type === 'tcp'}<label class="stack"><span>{t('ipv64x.port')}</span><input class="field short" type="number" min="1" max="65535" bind:value={form.port} /></label>{/if}
			{#if form.type === 'http' || form.type === 'keyword'}
				<label class="stack"><span>URL</span><input class="field mono" type="url" bind:value={form.url} placeholder="https://" /></label>
				<label class="check"><input type="checkbox" bind:checked={form.verifyTLS} /><span>{t('ipv64x.verifyTLS')}</span></label>
			{/if}
			{#if form.type === 'keyword'}<label class="stack"><span>{t('ipv64x.keyword')}</span><input class="field" type="text" bind:value={form.keyword} /></label>{/if}
			<div class="inline">
				<label class="stack"><span>{t('ipv64x.interval')}</span><input class="field short" type="number" min="1" max="86400" bind:value={form.interval} /></label>
				<label class="stack"><span>{t('ipv64x.retry')}</span><input class="field short" type="number" min="10" max="86400" bind:value={form.retry} /></label>
				<label class="stack"><span>{t('ipv64x.failures')}</span><input class="field short" type="number" min="1" max="60" bind:value={form.failures} /></label>
			</div>
			<p class="muted hint">{t('ipv64x.intervalHint')}</p>
		{/if}
		<fieldset>
			<legend>{t('ipv64x.notify')}</legend>
			<label class="check"><input type="checkbox" bind:checked={form.onDown} /><span>{t('ipv64x.onDown')}</span></label>
			<label class="check"><input type="checkbox" bind:checked={form.onUp} /><span>{t('ipv64x.onUp')}</span></label>
			{#each integrations as i (i.id)}
				<label class="check"
					><input
						type="checkbox"
						checked={form.integrations.includes(i.id)}
						onchange={(e) => form && (form.integrations = e.currentTarget.checked ? [...new Set([...form.integrations, i.id])] : form.integrations.filter((x) => x !== i.id))}
					/><span>{i.name}</span></label
				>
			{/each}
		</fieldset>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (form = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={busy || problems.length > 0} onclick={saveCheck}>{t('common.save')}</button>
		</div>
	</Dialog>
{/if}

{#if newIntegration}
	<Dialog title={t('ipv64x.integrationCreate')} locked={busy} onclose={() => (newIntegration = null)}>
		<label class="stack"
			><span>{t('ipv64x.type')}</span><select class="field" bind:value={newIntegration.type} onchange={() => newIntegration && (newIntegration.values = {})}>
				{#each Object.keys(INTEGRATION_FIELDS) as type (type)}<option value={type}>{t(`ipv64x.integrationType.${type}` as MessageKey)}</option>{/each}
			</select></label
		>
		<label class="stack"><span>{t('ipv64x.name')}</span><input class="field" type="text" maxlength="100" bind:value={newIntegration.name} /></label>
		{#each INTEGRATION_FIELDS[newIntegration.type] as field (field.name)}
			<label class="stack"
				><span>{t(`ipv64x.field.${field.name}` as MessageKey)}{#if !field.required} <span class="muted">({t('ipv64x.optional')})</span>{/if}</span><input
					class="field mono"
					type={field.secret ? 'password' : field.kind === 'number' ? 'number' : 'text'}
					autocomplete="off"
					min={field.min}
					max={field.max}
					bind:value={newIntegration.values[field.name]}
				/></label
			>
		{/each}
		{#if newIntegration.type === 'sms'}<p class="muted hint">{t('ipv64x.smsHint')}</p>{/if}
		<label class="check"><input type="checkbox" bind:checked={newIntegration.preselect} /><span>{t('ipv64x.preselect')}</span></label>
		<p class="muted hint">{t('ipv64x.writeOnly')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (newIntegration = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={busy || integrationBad} onclick={createIntegration}>{t('ipv64x.create')}</button>
		</div>
	</Dialog>
{/if}

{#if pageForm}
	<Dialog title={t(pageForm.id ? 'ipv64x.pageEdit' : 'ipv64x.pageCreate')} locked={busy} onclose={() => (pageForm = null)}>
		<label class="stack"><span>{t('ipv64x.pageTitle')}</span><input class="field" type="text" maxlength="200" bind:value={pageForm.name} /></label>
		<label class="stack"><span>{t('ipv64x.description')}</span><input class="field" type="text" maxlength="2000" bind:value={pageForm.description} /></label>
		<label class="stack"
			><span>{t('ipv64x.slug')} <span class="muted">({t('ipv64x.optional')})</span></span><input class="field mono" type="text" bind:value={pageForm.slug} placeholder={t('ipv64x.slugRandom')} /></label
		>
		{#if pageForm.slug && !validSlug(pageForm.slug)}<p class="notice-warn">{t('ipv64x.slugInvalid')}</p>{/if}
		{#if pageForm.slug}<p class="muted hint mono">{statusPageURL(pageForm.slug)}</p>{/if}
		<label class="stack"
			><span>{t('ipv64x.pagePassword')} <span class="muted">({t('ipv64x.optional')})</span></span><input class="field" type="password" autocomplete="new-password" bind:value={pageForm.password} /></label
		>
		{#if pageForm.id}<label class="check"><input type="checkbox" bind:checked={pageForm.clearPassword} /><span>{t('ipv64x.pagePasswordClear')}</span></label>{/if}
		<p class="muted hint">{t('ipv64x.writeOnly')}</p>
		<div class="inline">
			<label class="stack"
				><span>{t('ipv64x.design')}</span><select class="field short" bind:value={pageForm.design}>{#each [1, 2, 3, 4] as d (d)}<option value={d}>{d}</option>{/each}</select></label
			>
			<label class="check"><input type="checkbox" bind:checked={pageForm.autorefresh} /><span>{t('ipv64x.autorefresh')}</span></label>
		</div>
		<fieldset>
			<legend>{t('ipv64x.pageChecks')}</legend>
			{#each pageForm.checks as id, i (id)}
				<div class="inline">
					<span class="grow">{i + 1}. {checkName(id)}</span>
					<button type="button" class="btn" disabled={i === 0} onclick={() => move(i, -1)} aria-label={t('ipv64x.up')}>↑</button>
					<button type="button" class="btn" disabled={i === pageForm.checks.length - 1} onclick={() => move(i, 1)} aria-label={t('ipv64x.down')}>↓</button>
					<button type="button" class="btn" onclick={() => pageForm && (pageForm.checks = pageForm.checks.filter((x) => x !== id))}>{t('ipv64x.remove')}</button>
				</div>
			{/each}
			{#each checks.filter((c) => !pageForm?.checks.includes(c.id)) as c (c.id)}
				<button type="button" class="btn" onclick={() => pageForm && (pageForm.checks = [...pageForm.checks, c.id])}>+ {c.name}</button>
			{/each}
		</fieldset>
		<p class="notice-warn">{t('ipv64x.pagePublic')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (pageForm = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={busy || (!!pageForm.slug && !validSlug(pageForm.slug))} onclick={savePage}>{t('common.save')}</button>
		</div>
	</Dialog>
{/if}

{#if confirm}
	<Dialog title={t(`ipv64x.confirm.${confirm.kind}.title` as MessageKey)} locked={busy} onclose={() => (confirm = null)}>
		<p>{t(`ipv64x.confirm.${confirm.kind}.text` as MessageKey, { name: confirm.name })}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirm = null)}>{t('common.cancel')}</button>
			<button type="button" class={confirm.kind === 'rotateCheck' ? 'btn btn-warn' : 'btn btn-danger'} disabled={busy} onclick={confirmed}>{t(`ipv64x.confirm.${confirm.kind}.button` as MessageKey)}</button>
		</div>
	</Dialog>
{/if}

<style>
	.chip {
		margin-left: 6px;
	}
	.chart {
		width: 100%;
		max-width: 40rem;
		height: 80px;
		background: var(--track);
		border-radius: var(--radius-small);
	}
	.plain {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	[aria-pressed='true'] {
		color: var(--accent);
		border-color: var(--accent);
	}
</style>
