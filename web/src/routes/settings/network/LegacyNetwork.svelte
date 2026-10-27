<script lang="ts">
	import { revealTarget } from '#lib/reveal-target.ts';
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Config = { hostname: string; ports: string[]; bondMode: string; vlan: number; bridge: boolean; dhcp: boolean; address: string; gateway: string; dns: string[] };
	type Status = {
		config: Config;
		pending: Config | null;
		pendingSeconds: number;
		interfaces: { name: string; mac: string; carrier: boolean; speedMbps: number }[];
		device: string;
		addresses: string[];
		gateway: string;
		dns: string[];
	};

	let status = $state<Status | null>(null);
	let bondModes = $state<string[]>([]);
	let draft = $state<Config | null>(null);
	let dns = $state('');
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let lost = $state(false);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: (code === 'invalid_network' || code === 'network_failed') && err instanceof ApiError ? err.message : '' };
	}

	function take(res: { network: Status; bondModes: string[] }) {
		status = res.network;
		bondModes = res.bondModes;
		// What is entered stays while it is being edited.
		if (!draft) {
			draft = { ...res.network.config, ports: [...res.network.config.ports], dns: [...res.network.config.dns] };
			dns = draft.dns.join(', ');
		}
	}
	async function load() {
		try {
			take(await api.get('/network'));
			if (!status?.pending) lost = false;
		} catch (err) {
			// While a change is on trial the NAS may be unreachable here.
			if (!status) problem(err);
		}
	}
	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && !busy && load(), 3000);
		return () => clearInterval(timer);
	});

	const VLAN_OFF = 0;
	const changed = $derived(!!status && !!draft && JSON.stringify({ ...draft, dns: parseDns(dns) }) !== JSON.stringify(status.config));
	const needsPort = $derived(!!draft && draft.ports.length === 0 && (draft.bridge || draft.vlan > VLAN_OFF || !draft.dhcp));

	function parseDns(text: string): string[] {
		return text.split(/[\s,;]+/).filter(Boolean);
	}

	function togglePort(name: string) {
		if (!draft) return;
		draft.ports = draft.ports.includes(name) ? draft.ports.filter((port) => port !== name) : [...draft.ports, name].sort();
		if (draft.ports.length > 1 && !draft.bondMode) draft.bondMode = bondModes[0] ?? 'active-backup';
		if (draft.ports.length < 2) draft.bondMode = '';
	}

	async function apply(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = true;
		error = null;
		lost = false;
		// With a new address the answer to this request never arrives.
		const patience = new Promise<'late'>((resolve) => setTimeout(() => resolve('late'), 8000));
		const request = api.put<{ network: Status; bondModes: string[] }>('/network', { ...draft, vlan: Number(draft.vlan) || 0, dns: parseDns(dns) });
		try {
			const res = await Promise.race([request, patience]);
			if (res === 'late') {
				lost = true;
				request.then(take, () => {});
			} else {
				take(res);
			}
		} catch (err) {
			if (err instanceof ApiError && err.code === 'unreachable') lost = true;
			else problem(err);
		}
		busy = false;
	}

	function reset() {
		draft = null;
		error = null;
		load();
	}
</script>

<PageHead title={t('netcfg.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if lost}<p class="notice-warn" role="status">{t('netcfg.lost')}</p>{/if}

{#if status && draft}
	<section class="tile panel" aria-labelledby="net-now">
		<h2 class="tile-title" id="net-now">{t('netcfg.now')}</h2>
		<dl>
			<div><dt>{t('netcfg.device')}</dt><dd class="mono">{status.device || '–'}</dd></div>
			<div class="wide"><dt>{t('netcfg.address')}</dt><dd class="mono">{#each status.addresses as address (address)}<span>{address}</span>{:else}–{/each}</dd></div>
			<div><dt>{t('netcfg.gateway')}</dt><dd class="mono">{status.gateway || '–'}</dd></div>
			<div><dt>{t('netcfg.dns')}</dt><dd class="mono">{#each status.dns as server (server)}<span>{server}</span>{:else}–{/each}</dd></div>
		</dl>
	</section>

	{#if status.pending}
		<p class="notice-warn" role="status">{t('netcfg.pendingHere')}</p>
	{:else}
		<form class="tile panel" onsubmit={apply}>
			<label class="narrow"><span>{t('netcfg.hostname')}</span>
				<input class="field mono" type="text" bind:value={draft.hostname} maxlength="63" autocapitalize="none" spellcheck="false" required />
				<small>{t('netcfg.hostnameHint')}</small>
			</label>

			<fieldset>
				<legend>{t('netcfg.ports')}</legend>
				<div class="ports">
					{#each status.interfaces as port (port.name)}
						<label class="port">
							<input type="checkbox" checked={draft.ports.includes(port.name)} onchange={() => togglePort(port.name)} />
							<span class="mono">{port.name}</span>
							<span class="muted"><span class="dot" class:ok={port.carrier} aria-hidden="true"></span>{port.carrier ? (port.speedMbps ? t('netcfg.link', { speed: port.speedMbps }) : t('netcfg.linkUp')) : t('netcfg.noLink')}</span>
							<span class="muted mono mac">{port.mac}</span>
						</label>
					{/each}
				</div>
				<small>{t(draft.ports.length === 0 ? 'netcfg.portsAll' : draft.ports.length > 1 ? 'netcfg.portsBond' : 'netcfg.portsOne')}</small>
				{#if draft.ports.length > 1}
					<label class="narrow"><span>{t('netcfg.bondMode')}</span>
						<select class="field" bind:value={draft.bondMode}>{#each bondModes as mode (mode)}<option value={mode}>{t(`network.bond.${mode}` as 'netcfg.bond.active-backup')}</option>{/each}</select>
					</label>
				{/if}
			</fieldset>

			<div class="row">
				<label class="narrow"><span>{t('netcfg.vlan')}</span>
					<input class="field mono" type="number" min="0" max="4094" bind:value={draft.vlan} />
					<small>{t('netcfg.vlanHint')}</small>
				</label>
				<label class="check wide"><input type="checkbox" bind:checked={draft.bridge} /><span>{t('netcfg.bridge')}</span><small>{t('netcfg.bridgeHint')}</small></label>
			</div>

			<fieldset>
				<legend>{t('netcfg.addressing')}</legend>
				<div class="segments" role="radiogroup" aria-label={t('netcfg.addressing')}>
					<button type="button" role="radio" aria-checked={draft.dhcp} onclick={() => (draft!.dhcp = true)}>{t('netcfg.dhcp')}</button>
					<button type="button" role="radio" aria-checked={!draft.dhcp} onclick={() => (draft!.dhcp = false)}>{t('netcfg.static')}</button>
				</div>
				{#if !draft.dhcp}
					<div class="row">
						<label class="narrow"><span>{t('netcfg.addressField')}</span><input class="field mono" type="text" bind:value={draft.address} placeholder="192.168.1.20/24" spellcheck="false" required /></label>
						<label class="narrow"><span>{t('netcfg.gateway')}</span><input class="field mono" type="text" bind:value={draft.gateway} placeholder="192.168.1.1" spellcheck="false" /></label>
					</div>
				{/if}
				<label class="narrow" id="network-dns" use:revealTarget><span>{t('netcfg.dnsField')}</span>
					<input class="field mono" type="text" bind:value={dns} placeholder={draft.dhcp ? t('netcfg.dnsAuto') : '192.168.1.1, 1.1.1.1'} spellcheck="false" />
				</label>
			</fieldset>

			{#if needsPort}<p class="notice-warn">{t('netcfg.needsPort')}</p>{/if}
			<p class="notice-warn">{t('netcfg.trial')}</p>
			<div class="actions">
				<button type="button" class="btn" onclick={reset} disabled={!changed || busy}>{t('netcfg.discard')}</button>
				<button type="submit" class="btn btn-primary" disabled={!changed || busy || needsPort}>{t(busy ? 'netcfg.applying' : 'netcfg.apply')}</button>
			</div>
		</form>
	{/if}
{/if}

<style>
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 12px 24px;
		margin: 0;
		font-size: 0.875rem;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	dd > span {
		display: block;
	}
	dl > .wide {
		grid-column: span 2;
	}
	@media (max-width: 760px) {
		dl > .wide {
			grid-column: auto;
		}
	}
	form {
		gap: 18px;
	}
	fieldset {
		gap: 12px;
	}
	legend {
		padding: 0;
		margin-bottom: 4px;
		font-size: 0.8125rem;
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--muted);
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
	.narrow {
		flex: 0 1 320px;
		max-width: 320px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 16px 24px;
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.ports {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.port {
		flex-direction: row;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 16px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		font-size: 0.875rem;
	}
	.port > .mono:first-of-type {
		min-width: 7rem;
		color: var(--text);
	}
	.port .muted {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 0.8125rem;
	}
	.mac {
		margin-left: auto;
	}
	input[type='checkbox'] {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.check {
		flex-direction: row;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 10px;
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.check small {
		flex: 1 1 100%;
	}
	.wide {
		flex: 1 1 360px;
	}
	.segments {
		display: inline-flex;
		align-self: flex-start;
		gap: 4px;
		padding: 3px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		height: 2.25rem;
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
	}
</style>
