<script lang="ts">
	// Assignments: every domain of the account and the names below it with
	// where they point — the NAS (dynamic DNS), an app (its HTTPS name on
	// the NAS, or over the internet through the CDN of IPv64), or a VM or
	// device (a target of the HTTPS names). Changing one takes it from
	// where it pointed before; taking it back leaves the domain at IPv64.
	import { onMount } from 'svelte';
	import { api, type AppProxyStatus, type DockerStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import ProxyRouteFields from '#lib/components/ProxyRouteFields.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import {
		normalLocalName,
		proxyBody,
		proxyForm,
		proxyProblems,
		suggestedProxyName,
		type ProxyForm,
	} from '#lib/app-proxy.ts';
	import { validIpv64Wish } from '#lib/dns-providers.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		assignBody,
		assignForm,
		assignName,
		assignProblems,
		assignRows,
		validAssignPrefix,
		type AssignForm,
		type Assignment,
		type Assignments,
		type Status,
	} from './ipv64.ts';
	import { failure } from './ui.ts';

	let { status }: { status: Status } = $props();

	let data = $state<Assignments | null>(null);
	let proxy = $state<AppProxyStatus | null>(null);
	let apps = $state<string[]>([]);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let notice = $state('');

	// A new name: a prefix under a domain of the account, or a new domain.
	let prefix = $state('');
	let parent = $state('');
	let wish = $state('');
	let zone = $state('ipv64.de');

	// The dialog: name, whether the domain is still to be created, the form.
	type Editing = { name: string; create: boolean; current: Assignment | null; form: AssignForm };
	let editing = $state<Editing | null>(null);
	let newTarget = $state<ProxyForm | null>(null);
	let ports = $state<number[]>([]);
	let removing = $state<Assignment | null>(null);

	async function load() {
		try {
			const [next, routes, docker] = await Promise.all([
				api.get<Assignments>('/ipv64/assignments'),
				api.get<AppProxyStatus>('/docker/proxy'),
				api.get<DockerStatus>('/docker').catch(() => null),
			]);
			data = next;
			proxy = routes;
			const names = new Set<string>();
			for (const c of docker?.containers ?? []) if (c.managed && c.app) names.add(c.app);
			for (const r of routes.apps) names.add(r.app);
			apps = [...names].sort();
			if (!parent && next.domains.length) parent = next.domains[0].name;
			loadError = '';
		} catch (err) {
			loadError = failure(err);
		}
	}
	onMount(load);

	const rows = $derived(data ? assignRows(data) : []);
	const targets = $derived(proxy?.targets ?? []);
	const routeOf = (app: string) => proxy?.apps.find((r) => r.app === app);

	function open(name: string, current: Assignment | null, create = false) {
		error = notice = '';
		newTarget = null;
		ports = [];
		editing = { name, create, current, form: assignForm(current) };
		if (editing.form.app) void loadPorts(editing.form.app);
	}
	// The TCP ports an app publishes (never those of TitanOS).
	async function loadPorts(app: string) {
		ports = [];
		if (!app) return;
		try {
			const info = await api.get<{ ports: number[] }>(`/ipv64/apps/${encodeURIComponent(app)}`);
			if (editing?.form.app !== app) return;
			ports = info.ports;
			if (!ports.includes(editing.form.port))
				editing.form.port = routeOf(app)?.port ?? ports[0] ?? 0;
		} catch {
			ports = [];
		}
	}

	const hasRoute = $derived(!!(editing?.form.app && routeOf(editing.form.app)));
	const localLabel = $derived(
		normalLocalName(proxy?.localName ?? '') || proxy?.defaultLocalName || '',
	);
	const targetProblems = $derived(
		newTarget && proxy
			? proxyProblems(newTarget, true, [...proxy.apps, ...targets], localLabel)
			: [],
	);
	const problems = $derived(
		editing
			? [
					...assignProblems(editing.form, hasRoute),
					...(editing.form.kind === 'target' && newTarget
						? targetProblems.length
							? ['newTarget']
							: []
						: []),
				].filter((p) => !(p === 'target' && newTarget))
			: [],
	);
	const nameOK = $derived(
		!editing ||
			editing.form.kind !== 'app' ||
			editing.form.via !== 'public' ||
			isDirectChild(editing.name),
	);

	// Through the CDN only one label may stand before a domain of the account.
	function isDirectChild(name: string) {
		const zone = data?.domains
			.map((d) => d.name)
			.filter((d) => name === d || name.endsWith(`.${d}`))
			.sort((a, b) => b.length - a.length)[0];
		if (!zone) return true;
		const before = name === zone ? '' : name.slice(0, -zone.length - 1);
		return !before.includes('.');
	}

	function newTargetId(name: string) {
		const base = suggestedProxyName(name).slice(0, 32).replace(/-+$/, '') || 'target';
		let id = base;
		for (let i = 2; targets.some((target) => target.app === id); i++)
			id = `${base.slice(0, 29)}-${i}`;
		return id;
	}

	async function submit() {
		if (!editing || busy || problems.length || !nameOK) return;
		const what = editing;
		busy = true;
		error = '';
		try {
			if (what.create) {
				await api.post('/ipv64/domains', { name: what.name, own: false });
				what.create = false;
			}
			const form = { ...what.form };
			if (form.kind === 'target' && newTarget) {
				const id = newTargetId(newTarget.name);
				proxy = await api.put<AppProxyStatus>(
					`/docker/proxy/targets/${encodeURIComponent(id)}`,
					proxyBody(newTarget, true),
				);
				form.targetId = id;
				newTarget = null;
			}
			data = await api.put<Assignments>(
				`/ipv64/assignments/${encodeURIComponent(what.name)}`,
				assignBody(form, !!data?.firewall),
			);
			proxy = await api.get<AppProxyStatus>('/docker/proxy');
			notice = t('ipv64x.assign.done', { name: what.name });
			editing = null;
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}

	async function remove() {
		if (!removing) return;
		const name = removing.domain;
		busy = true;
		error = '';
		try {
			data = await api.delete<Assignments>(`/ipv64/assignments/${encodeURIComponent(name)}`);
			proxy = await api.get<AppProxyStatus>('/docker/proxy');
			notice = t('ipv64x.assign.removed', { name });
			removing = null;
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}

	function targetText(a: Assignment): string {
		switch (a.target) {
			case 'nas':
				return t('ipv64x.assign.toNas');
			case 'app':
				return t('ipv64x.assign.toApp', { app: a.app ?? '', port: a.port ?? '' });
			case 'public':
				return t('ipv64x.assign.toPublic', { app: a.app ?? '', port: a.port ?? '' });
			default: {
				const target = targets.find((r) => r.app === a.app);
				return t('ipv64x.assign.toTarget', {
					name: target?.name ?? a.app ?? '',
					host: target?.host ?? '',
				});
			}
		}
	}
	const dnsText = (a: Assignment) =>
		t(`ipv64x.assign.dns.${a.target === 'public' ? 'cdn' : a.dns || 'none'}` as MessageKey);
	const full = $derived(!!data && data.ddns.hostnames.length >= data.ddns.max);
</script>

<div class="ipv64-tab">
	{#if loadError}
		<div class="notice-error" role="alert">
			{loadError}<button class="btn" onclick={load}>{t('ui.retry')}</button>
		</div>
	{:else if !data}
		<p class="muted loading" role="status">{t('common.loading')}</p>
	{:else}
		<section class="tile panel" aria-labelledby="ipv64-assign">
			<h2 class="tile-title" id="ipv64-assign">{t('ipv64x.assign.title')}</h2>
			<p class="muted hint">{t('ipv64x.assign.intro')}</p>
			{#if data.ddns.provider && data.ddns.provider !== 'ipv64'}<p class="notice-warn">
					{t('ipv64x.dyn.otherProvider', { provider: data.ddns.provider })}
				</p>{/if}
			{#if !editing && error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}
			<div class="table-wrap">
				<table>
					<thead
						><tr
							><th>{t('ipv64x.domain')}</th><th>{t('ipv64x.assign.target')}</th><th
								>{t('ipv64x.assign.dns')}</th
							><th></th></tr
						></thead
					>
					<tbody>
						{#each rows as row (row.name)}
							<tr>
								<td class="mono" class:sub={row.name !== row.zone}
									>{#if row.assignment?.url}<a
											href={row.assignment.url}
											target="_blank"
											rel="noopener noreferrer">{row.name}</a
										>{:else}{row.name}{/if}</td
								>
								<td class="wrap"
									>{#if row.assignment}<span
											class="chip {row.assignment.target === 'public' ? 'warn' : 'ok'}"
											>{t(`ipv64x.assign.kind.${row.assignment.target}` as MessageKey)}</span
										>
										{targetText(row.assignment)}{:else}<span class="muted"
											>{t('ipv64x.assign.none')}</span
										>{/if}</td
								>
								<td>{row.assignment ? dnsText(row.assignment) : '–'}</td>
								<td class="actions">
									<button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() => open(row.name, row.assignment)}
										>{t(row.assignment ? 'ipv64x.assign.change' : 'ipv64x.assign.assign')}</button
									>
									{#if row.assignment}<button
											type="button"
											class="btn"
											disabled={busy}
											onclick={() => ((removing = row.assignment), (error = ''))}
											>{t('ipv64x.assign.remove')}</button
										>{/if}
								</td>
							</tr>
						{:else}
							<tr><td colspan="4" class="muted">{t('ipv64x.none')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			{#if full}<p class="muted hint">{t('ipv64x.assign.full', { max: data.ddns.max })}</p>{/if}

			<h3>{t('ipv64x.assign.newTitle')}</h3>
			<div class="inline">
				<label class="stack"
					><span>{t('ipv64x.assign.prefix')}</span><input
						class="field mono short"
						type="text"
						bind:value={prefix}
						placeholder="photos"
						autocapitalize="off"
						spellcheck="false"
					/></label
				>
				<label class="stack grow"
					><span>{t('ipv64x.domain')}</span><select class="field mono" bind:value={parent}
						>{#each data.domains as d (d.name)}<option value={d.name}>{d.name}</option
							>{/each}</select
					></label
				>
				<button
					type="button"
					class="btn"
					disabled={busy || !parent || !prefix.trim() || !validAssignPrefix(prefix)}
					onclick={() =>
						open(
							assignName(prefix, parent),
							data?.assignments.find((a) => a.domain === assignName(prefix, parent)) ?? null,
						)}>{t('ipv64x.assign.assign')}</button
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
					/></label
				>
				<label class="stack"
					><span>{t('ipv64x.zone')}</span><select class="field mono" bind:value={zone}
						>{#each status.zones as z (z)}<option value={z}>{z}</option>{/each}</select
					></label
				>
				<button
					type="button"
					class="btn"
					disabled={busy || !validIpv64Wish(wish.trim().toLowerCase())}
					onclick={() => open(`${wish.trim().toLowerCase()}.${zone}`, null, true)}
					>{t('ipv64x.assign.createAssign')}</button
				>
			</div>
			<p class="muted hint">{t('ipv64x.assign.newHint')}</p>
		</section>
	{/if}
</div>

{#if editing}
	<Dialog
		title={t('ipv64x.assign.dialogTitle', { name: editing.name })}
		locked={busy}
		wide
		onclose={() => (editing = null)}
	>
		<form
			class="assign"
			onsubmit={(event) => {
				event.preventDefault();
			}}
		>
			{#if editing.create}<p class="notice-warn">
					{t('ipv64x.createText', { name: editing.name })}
				</p>{/if}
			{#if editing.current}<p class="muted">
					{t('ipv64x.assign.currently', { target: targetText(editing.current) })}
				</p>{/if}
			<Segments
				label={t('ipv64x.assign.target')}
				options={[
					{ value: 'nas', label: t('ipv64x.assign.kind.nas') },
					{ value: 'app', label: t('ipv64x.assign.kind.app') },
					{ value: 'target', label: t('ipv64x.assign.kind.target') },
				]}
				bind:value={editing.form.kind}
				disabled={busy}
			/>

			{#if editing.form.kind === 'nas'}
				<p class="muted">{t('ipv64x.assign.nasText')}</p>
			{:else if editing.form.kind === 'app'}
				<label class="stack"
					><span>{t('ipv64x.assign.app')}</span><select
						class="field"
						bind:value={editing.form.app}
						disabled={busy}
						onchange={() => editing && loadPorts(editing.form.app)}
					>
						<option value="">{t('ipv64x.assign.appPick')}</option>
						{#each apps as app (app)}<option value={app}>{app}</option>{/each}
					</select></label
				>
				<Segments
					label={t('ipv64x.assign.via')}
					options={[
						{ value: 'local', label: t('ipv64x.assign.viaLocal') },
						{ value: 'public', label: t('ipv64x.assign.viaPublic') },
					]}
					bind:value={editing.form.via}
					disabled={busy}
				/>
				{#if editing.form.app}
					{#if editing.form.via === 'local' && hasRoute}
						<p class="muted">
							{t('ipv64x.assign.routeExists', {
								name: routeOf(editing.form.app)?.name ?? '',
								port: routeOf(editing.form.app)?.port ?? '',
							})}
						</p>
					{:else}
						{#if ports.length}
							<div class="inline">
								<label class="stack"
									><span>{t('ipv64x.port')}</span><select
										class="field"
										bind:value={editing.form.port}
										disabled={busy}
										>{#each ports as p (p)}<option value={p}>{p}</option>{/each}</select
									></label
								>
								<label class="stack"
									><span>{t('ipv64x.backendScheme')}</span><select
										class="field"
										bind:value={editing.form.scheme}
										disabled={busy}
										><option value="http">http</option><option value="https">https</option></select
									></label
								>
							</div>
						{:else}
							<p class="notice-warn">{t('ipv64x.appNoPort')}</p>
						{/if}
						{#if editing.form.via === 'local'}
							<label class="check"
								><input
									type="checkbox"
									bind:checked={editing.form.localOnly}
									disabled={busy}
								/><span>{t('proxy.localOnly')}</span></label
							>
							<p class="muted hint">
								{t(editing.form.localOnly ? 'ipv64x.assign.localOnlyHint' : 'proxy.localOnlyOff')}
							</p>
						{/if}
					{/if}
					{#if editing.form.via === 'local'}
						<p class="muted hint">{t('ipv64x.assign.localText')}</p>
					{:else}
						{#if !nameOK}<p class="notice-warn">{t('ipv64x.assign.publicOneLabel')}</p>{/if}
						<p class="notice-warn">
							<strong>{t('ipv64x.appWarnInternet', { address: `https://${editing.name}` })}</strong>
						</p>
						<ul>
							<li>{t('ipv64x.appWarnLogin')}</li>
							<li>{t('ipv64x.appWarnCreates')}</li>
							<li>{t('ipv64x.appWarnRouter', { port: editing.form.port })}</li>
							<li>
								{data?.firewall && editing.form.firewall
									? t('ipv64x.appWarnFirewall')
									: t('ipv64x.appWarnNoFirewall')}
							</li>
						</ul>
						{#if data?.firewall}<label class="check"
								><input type="checkbox" bind:checked={editing.form.firewall} disabled={busy} /><span
									>{t('ipv64x.appFirewall')}</span
								></label
							>{/if}
						<label class="check"
							><input type="checkbox" bind:checked={editing.form.ownLogin} disabled={busy} /><span
								>{t('ipv64x.appOwnLogin')}</span
							></label
						>
					{/if}
				{/if}
			{:else}
				<label class="stack"
					><span>{t('ipv64x.assign.device')}</span><select
						class="field"
						value={newTarget ? '__new' : editing.form.targetId}
						disabled={busy}
						onchange={(e) => {
							if (!editing) return;
							const value = e.currentTarget.value;
							if (value === '__new') {
								newTarget = proxyForm(null);
								newTarget.enabled = true;
								editing.form.targetId = '';
							} else {
								newTarget = null;
								editing.form.targetId = value;
							}
						}}
					>
						<option value="">{t('ipv64x.assign.devicePick')}</option>
						{#each targets as target (target.app)}<option value={target.app}
								>{target.name} ({target.host}:{target.port})</option
							>{/each}
						<option value="__new">{t('ipv64x.assign.deviceNew')}</option>
					</select></label
				>
				{#if newTarget && proxy}
					<ProxyRouteFields
						bind:form={newTarget}
						target
						problems={targetProblems}
						suffixes={proxy.suffixes}
						mdns={proxy.mdns ?? true}
						tailnet={proxy.tailnet}
						disabled={busy}
					/>
				{/if}
				<p class="muted hint">{t('ipv64x.assign.deviceText')}</p>
			{/if}

			{#if editing.form.kind !== 'app' || editing.form.via === 'local'}<p class="muted hint">
					{t('ipv64x.assign.dnsText', { max: data?.ddns.max ?? 8 })}
				</p>{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (editing = null)}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class={editing.form.kind === 'app' && editing.form.via === 'public'
						? 'btn btn-warn'
						: 'btn btn-primary'}
					disabled={busy || problems.length > 0 || !nameOK}
					onclick={submit}
					>{t(
						busy
							? 'ipv64x.assign.working'
							: editing.create
								? 'ipv64x.assign.createAssign'
								: 'ipv64x.assign.assign',
					)}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if removing}
	<Dialog
		title={t('ipv64x.assign.removeTitle', { name: removing.domain })}
		locked={busy}
		onclose={() => (removing = null)}
	>
		<p>
			{t(removing.target === 'public' ? 'ipv64x.assign.removePublic' : 'ipv64x.assign.removeText', {
				name: removing.domain,
			})}
		</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				>{t('ipv64x.assign.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.assign {
		display: flex;
		flex-direction: column;
		gap: 12px;
		font-size: 0.875rem;
	}
	.sub {
		padding-left: 22px;
	}
	.chip {
		margin-right: 6px;
	}
	ul {
		margin: 0;
		padding-left: 1.25rem;
	}
</style>
