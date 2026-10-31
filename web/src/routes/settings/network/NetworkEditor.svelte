<script lang="ts">
	// Adds or edits one network of the draft. Nothing is applied here; the
	// page tests the whole draft on trial afterwards.
	import { t } from '#lib/i18n/index.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import {
		looksLikeIP,
		purposes,
		type NASNetwork,
		type NetworkConfig,
		type NetworkPort,
	} from '#lib/network.ts';
	import DnsChips from './DnsChips.svelte';

	let {
		network,
		isNew,
		config,
		ports,
		vlans = true,
		onsave,
		onclose,
	}: {
		network: NASNetwork;
		isNew: boolean;
		config: NetworkConfig;
		ports: NetworkPort[];
		/** Offer the VLAN ID (Interfaces › VLANs); a network that has one keeps it visible. */
		vlans?: boolean;
		onsave: (n: NASNetwork) => void;
		onclose: () => void;
	} = $props();

	// The dialog edits its own copy; the caller passed a fresh one.
	// Props are read once on purpose: the draft starts from them and later
	// changes of the props must not overwrite what is being edited; the page mounts this editor anew for each network ({#key editor}).
	// svelte-ignore state_referenced_locally
	let n = $state($state.snapshot(network));
	n.routes ??= [];
	// svelte-ignore state_referenced_locally
	const wasManagement = network.management && !isNew;

	const bonded = $derived(new Set((config.bonds ?? []).flatMap((b) => b.ports)));
	const clash = $derived(
		(config.networks ?? []).find(
			(o) => o.id !== n.id && o.parent === n.parent && Number(o.vlan || 0) === Number(n.vlan || 0),
		),
	);
	const noAddress = $derived(n.management && n.ipv4 === 'off' && n.ipv6 === 'off');
	const dnsOk = $derived(n.dns.length <= 3 && n.dns.every(looksLikeIP));
	const ok = $derived(!!n.name.trim() && !!n.parent && !clash && !noAddress && dnsOk);

	function save(event: SubmitEvent) {
		event.preventDefault();
		if (!ok) return;
		onsave($state.snapshot(n));
	}
</script>

<Dialog title={isNew ? t('netx.add') : t('netx.edit')} wide {onclose}>
	<form class="panel editor" onsubmit={save}>
		<fieldset>
			<legend>{t('netx.general')}</legend>
			<div class="grid">
				<label class="stack"
					><span>{t('netx.name')}</span><input
						class="field"
						bind:value={n.name}
						required
						maxlength="80"
					/></label
				>
				<label class="stack"
					><span>{t('netx.purpose')}</span><select class="field" bind:value={n.purpose}
						>{#each purposes as purpose (purpose)}<option value={purpose}
								>{t(`netx.purpose.${purpose}`)}</option
							>{/each}</select
					></label
				>
				<label class="stack"
					><span>{t('netx.connection')}</span><select
						class="field mono"
						bind:value={n.parent}
						required
						>{#each ports as p (p.name)}<option value={p.name} disabled={bonded.has(p.name)}
								>{p.name}{bonded.has(p.name) ? ` · ${t('netx.inBond')}` : ''}</option
							>{/each}{#each config.bonds ?? [] as b (b.id)}<option value={'bond:' + b.id}
								>{t('netx.bondNamed', { name: b.id })}</option
							>{/each}</select
					></label
				>
				{#if vlans || n.vlan}<label class="stack"
						><span>{t('netx.vlan')}</span><input
							class="field mono"
							type="number"
							min="0"
							max="4094"
							bind:value={n.vlan}
						/><small class="muted">{t('netx.optional')}</small></label
					>{/if}
			</div>
			{#if clash}<p class="notice-warn" role="alert">
					{t('netx.clash', { name: clash.name })}
				</p>{/if}
			<label class="check"
				><input type="checkbox" bind:checked={n.management} disabled={wasManagement} /><span
					>{t('netx.preferred')}</span
				></label
			>
			{#if wasManagement}<p class="muted hint">{t('netx.preferredHint')}</p>{/if}
			<label class="check"
				><input type="checkbox" bind:checked={n.bridge} /><span>{t('netx.bridge')}</span></label
			>
		</fieldset>

		<fieldset class="divided">
			<legend>{t('network.ipv4')}</legend>
			<Segments
				label={t('netx.method') + ' ' + t('network.ipv4')}
				bind:value={n.ipv4}
				options={[
					{ value: 'dhcp', label: t('netx.mode.dhcp') },
					{ value: 'static', label: t('netx.static') },
					{ value: 'off', label: t('netx.off') },
				]}
			/>
			{#if n.ipv4 === 'static'}
				<div class="grid">
					<label class="stack"
						><span>{t('netx.address')}</span><input
							class="field mono"
							bind:value={n.address}
							required
							spellcheck="false"
							placeholder="192.168.1.20/24"
						/></label
					>
					<label class="stack"
						><span>{t('netx.gateway')}</span><input
							class="field mono"
							bind:value={n.gateway}
							spellcheck="false"
							placeholder="192.168.1.1"
						/></label
					>
				</div>
			{/if}
		</fieldset>

		<fieldset class="divided">
			<legend>{t('network.ipv6')}</legend>
			<Segments
				label={t('netx.method') + ' ' + t('network.ipv6')}
				bind:value={n.ipv6}
				options={[
					{ value: 'auto', label: t('netx.mode.auto') },
					{ value: 'dhcp', label: t('netx.mode.dhcp6') },
					{ value: 'static', label: t('netx.static') },
					{ value: 'off', label: t('netx.off') },
				]}
			/>
			{#if n.ipv6 === 'static'}
				<div class="grid">
					<label class="stack"
						><span>{t('netx.address')}</span><input
							class="field mono"
							bind:value={n.address6}
							required
							spellcheck="false"
							placeholder="fd00::20/64"
						/></label
					>
					<label class="stack"
						><span>{t('netx.gateway')}</span><input
							class="field mono"
							bind:value={n.gateway6}
							spellcheck="false"
							placeholder="fe80::1"
						/></label
					>
				</div>
			{/if}
			{#if noAddress}<p class="notice-warn" role="alert">{t('netx.needsAddress')}</p>{/if}
		</fieldset>

		<fieldset class="divided">
			<legend><label for="net-dns-{n.id}">{t('netx.dns')}</label></legend>
			<DnsChips id="net-dns-{n.id}" bind:value={n.dns} />
			<p class="muted hint">
				{n.management ? t('netx.dnsNote.management') : t('netx.dnsNote.other')}
			</p>
		</fieldset>

		<fieldset class="divided">
			<legend>{t('netx.routes')}</legend>
			{#each n.routes ?? [] as route, i (i)}
				<div class="route">
					<label class="stack"
						><span>{t('netx.destination')}</span><input
							class="field mono"
							bind:value={route.destination}
							required
							spellcheck="false"
							placeholder="10.20.0.0/16"
						/></label
					>
					<label class="stack"
						><span>{t('netx.gateway')}</span><input
							class="field mono"
							bind:value={route.gateway}
							required
							spellcheck="false"
						/></label
					>
					<label class="stack"
						><span>{t('netx.metric')}</span><input
							class="field"
							type="number"
							min="0"
							max="65535"
							bind:value={route.metric}
						/></label
					>
					<button
						class="btn"
						type="button"
						onclick={() => (n.routes = n.routes?.filter((_, j) => j !== i))}
						>{t('netx.remove')}</button
					>
				</div>
			{:else}
				<p class="muted">{t('netx.noRoutes')}</p>
			{/each}
			<div class="row-actions">
				<button
					class="btn"
					type="button"
					onclick={() =>
						(n.routes = [...(n.routes ?? []), { destination: '', gateway: '', metric: 100 }])}
					>{t('netx.addRoute')}</button
				>
			</div>
			<p class="muted hint">{t('netx.routesHint')}</p>
		</fieldset>

		<p class="muted">{t('netx.addressHint')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button><button
				class="btn btn-primary"
				disabled={!ok}>{t('netx.takeOver')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	.editor {
		padding: 0;
	}
	.grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px 16px;
	}
	.grid .stack {
		max-width: none;
	}
	.route {
		display: grid;
		grid-template-columns: 1fr 1fr 6rem auto;
		gap: 8px;
		align-items: end;
	}
	.route .stack {
		max-width: none;
	}
	small,
	.hint {
		font-size: var(--text-sm);
	}
	legend label {
		font: inherit;
	}
	@media (max-width: 760px) {
		.grid,
		.route {
			grid-template-columns: 1fr;
		}
	}
</style>
