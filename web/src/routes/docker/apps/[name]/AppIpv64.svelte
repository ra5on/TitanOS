<script lang="ts">
	// "Public through IPv64": the app's port reachable from the internet
	// through the CDN/reverse proxy of IPv64.net (Settings › IPv64). titand
	// creates the record and the CDN entry; never for the ports of TitanOS.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';

	let { app }: { app: string } = $props();

	type Published = {
		domain: string;
		prefix?: string;
		port: number;
		firewall?: boolean;
		at: string;
	};
	type Info = {
		ports: number[];
		firewall: boolean;
		zones: { internet: boolean };
		published?: Published;
		keySource: string;
	};

	let info = $state<Info | null>(null);
	let domains = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);
	let domain = $state('');
	let prefix = $state('');
	let port = $state(0);
	let scheme = $state('http');
	let firewall = $state(true);
	let warning = $state(false);
	let ownLogin = $state(false);
	let removing = $state(false);

	function failure(err: unknown) {
		if (err instanceof ApiError && err.code.startsWith('ipv64_'))
			return `${errorText(err.code)} ${err.message.replace(/^IPv64: (HTTP \d+: )?/, '')}`;
		return apiErrorText(err);
	}
	async function load(current = app) {
		try {
			const next = await api.get<Info>(`/ipv64/apps/${encodeURIComponent(current)}`);
			if (current !== app) return;
			info = next;
			if (!port && next.ports.length) port = next.ports[0];
			if (next.keySource && !domains.length) {
				domains = (await api.get<{ domains: { name: string }[] }>('/ipv64/domains')).domains.map(
					(d) => d.name,
				);
				if (!domain) domain = domains[0] ?? '';
			}
			error = '';
		} catch (err) {
			error = failure(err);
		}
	}
	$effect(() => {
		void load(app);
	});
	const prefixOK = $derived(prefix === '' || /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(prefix));
	const address = $derived(`https://${prefix ? `${prefix}.` : ''}${domain}`);

	async function publish() {
		busy = true;
		error = '';
		try {
			await api.put(`/ipv64/apps/${encodeURIComponent(app)}`, {
				domain,
				prefix,
				port,
				scheme,
				firewall: info?.firewall && firewall,
				confirm: true,
			});
			warning = false;
			await load();
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
	async function unpublish() {
		busy = true;
		error = '';
		try {
			await api.delete(`/ipv64/apps/${encodeURIComponent(app)}`);
			removing = false;
			await load();
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile panel" aria-labelledby="app-ipv64">
	<h2 class="tile-title" id="app-ipv64">{t('ipv64x.appTitle')}</h2>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if info}
		{#if info.published}
			<p>
				{t('ipv64x.appPublished')}
				<a
					href={`https://${info.published.prefix ? `${info.published.prefix}.` : ''}${info.published.domain}`}
					target="_blank"
					rel="noopener noreferrer"
					class="mono"
					>https://{info.published.prefix ? `${info.published.prefix}.` : ''}{info.published
						.domain}</a
				>
				→ {t('ipv64x.port')}
				{info.published.port}
			</p>
			{#if info.firewall}<p class="muted">
					{t(info.zones.internet ? 'ipv64x.appFirewallOpen' : 'ipv64x.appFirewallClosed')}
				</p>{/if}
			<div class="row-actions">
				<button
					type="button"
					class="btn btn-danger"
					disabled={busy}
					onclick={() => (removing = true)}>{t('ipv64x.appUnpublish')}</button
				>
			</div>
		{:else if !info.keySource}
			<p class="muted">
				{t('ipv64x.appNoKey')} <a href="/settings/ddns?tab=ipv64">{t('ipv64x.title')}</a>
			</p>
		{:else if !info.ports.length}
			<p class="muted">{t('ipv64x.appNoPort')}</p>
		{:else}
			<p class="muted">{t('ipv64x.appHint')}</p>
			<div class="fields">
				<label class="stack"
					><span>{t('ipv64x.domain')}</span><select class="field mono" bind:value={domain}
						>{#each domains as d (d)}<option value={d}>{d}</option>{/each}</select
					></label
				>
				<label class="stack"
					><span>{t('ipv64x.prefix')} <span class="muted">({t('ipv64x.optional')})</span></span
					><input
						class="field mono"
						type="text"
						bind:value={prefix}
						placeholder={app.toLowerCase()}
						autocapitalize="off"
						spellcheck="false"
					/></label
				>
				<label class="stack"
					><span>{t('ipv64x.port')}</span><select class="field" bind:value={port}
						>{#each info.ports as p (p)}<option value={p}>{p}</option>{/each}</select
					></label
				>
				<label class="stack"
					><span>{t('ipv64x.backendScheme')}</span><select class="field" bind:value={scheme}
						><option value="http">http</option><option value="https">https</option></select
					></label
				>
			</div>
			{#if info.firewall}<label class="check"
					><input type="checkbox" bind:checked={firewall} /><span>{t('ipv64x.appFirewall')}</span
					></label
				>{:else}<p class="muted hint">{t('ipv64x.appFirewallOff')}</p>{/if}
			<div class="row-actions">
				<button
					type="button"
					class="btn btn-warn"
					disabled={busy || !domain || !prefixOK || !port}
					onclick={() => ((warning = true), (ownLogin = false))}>{t('ipv64x.appPublish')}</button
				>
			</div>
		{/if}
	{/if}
</section>

{#if warning}
	<Dialog title={t('ipv64x.appWarnTitle')} locked={busy} onclose={() => (warning = false)}>
		<p class="notice-warn"><strong>{t('ipv64x.appWarnInternet', { address })}</strong></p>
		<ul>
			<li>{t('ipv64x.appWarnLogin')}</li>
			<li>{t('ipv64x.appWarnCreates')}</li>
			<li>{t('ipv64x.appWarnRouter', { port })}</li>
			<li>
				{info?.firewall && firewall ? t('ipv64x.appWarnFirewall') : t('ipv64x.appWarnNoFirewall')}
			</li>
		</ul>
		<label class="check"
			><input type="checkbox" bind:checked={ownLogin} /><span>{t('ipv64x.appOwnLogin')}</span
			></label
		>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (warning = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={busy || !ownLogin} onclick={publish}
				>{t(busy ? 'ipv64x.appPublishing' : 'ipv64x.appPublish')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('ipv64x.appUnpublish')} locked={busy} onclose={() => (removing = false)}>
		<p>{t('ipv64x.appUnpublishText')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={unpublish}
				>{t('ipv64x.appUnpublish')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	section {
		font-size: 0.875rem;
	}
	.fields {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
		align-items: flex-end;
	}
	.hint {
		margin: 0;
	}
</style>
