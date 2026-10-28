<script lang="ts">
	// One policy of a cell as a small form card: what it matches (a service,
	// an app or ports), from where (and, between zones, to where), allow or
	// deny, a note; with ↑/↓ for the order of its cell (first match wins).
	import type { FirewallApp, FirewallPolicy, FirewallService } from '#lib/api.ts';
	import { APPS, NAS, policyProblem, servicePorts } from '#lib/firewall.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let {
		policy = $bindable(),
		to,
		services,
		apps,
		index,
		count,
		locked,
		heading = '',
		onmove,
		onremove,
	}: {
		policy: FirewallPolicy;
		/** nas, apps or a zone. */
		to: string;
		services: FirewallService[];
		apps: { key: string; name: string }[];
		index: number;
		count: number;
		locked: boolean;
		heading?: string;
		onmove: (by: number) => void;
		onremove: () => void;
	} = $props();

	type Kind = 'service' | 'app' | 'ports';
	const kind = $derived<Kind>(policy.service !== undefined ? 'service' : policy.app !== undefined ? 'app' : 'ports');
	const problem = $derived(policyProblem(policy));
	const id = $props.id();

	function setKind(next: Kind) {
		delete policy.service;
		delete policy.app;
		if (next === 'service') policy.service = services[0]?.id ?? 'webui';
		if (next === 'app') policy.app = apps[0]?.key ?? '';
		if (next !== 'ports') {
			policy.protocol = 'any';
			policy.ports = '';
		} else if (policy.protocol === 'any') {
			policy.protocol = 'tcp';
		}
	}
	const zoneCell = $derived(to !== NAS && to !== APPS);
</script>

<li class="policy" class:off={policy.disabled} data-policy={index}>
	<div class="head">
		<span class="order mono">{index + 1}</span>
		{#if heading}<strong class="where">{heading}</strong>{/if}
		<label class="check on"
			><input
				type="checkbox"
				checked={!policy.disabled}
				disabled={locked}
				onchange={(event) => (policy.disabled = !event.currentTarget.checked || undefined)}
			/><span>{t('firewall.ruleOn')}</span></label
		>
		<span class="buttons">
			<button
				class="btn small"
				type="button"
				disabled={locked || index === 0}
				aria-label={t('firewall.ruleUp')}
				title={t('firewall.ruleUp')}
				onclick={() => onmove(-1)}>↑</button
			><button
				class="btn small"
				type="button"
				disabled={locked || index === count - 1}
				aria-label={t('firewall.ruleDown')}
				title={t('firewall.ruleDown')}
				onclick={() => onmove(1)}>↓</button
			><button class="btn small" type="button" disabled={locked} onclick={onremove}>{t('firewall.ruleRemove')}</button>
		</span>
	</div>
	<div class="fields">
		<label class="stack"
			><span>{t('firewall.ruleAction')}</span><select class="field" bind:value={policy.action} disabled={locked}
				><option value="allow">{t('firewall.allow')}</option><option value="deny">{t('firewall.deny')}</option></select
			></label
		>
		{#if !zoneCell}<label class="stack"
				><span>{t('firewall.policyKind')}</span><select
					class="field"
					value={kind}
					disabled={locked}
					onchange={(event) => setKind(event.currentTarget.value as Kind)}
				>
					{#if to === NAS}<option value="service">{t('firewall.kindService')}</option>{/if}
					{#if to === APPS}<option value="app">{t('firewall.kindApp')}</option>{/if}
					<option value="ports">{t('firewall.kindPorts')}</option>
				</select></label
			>{/if}
		{#if kind === 'service'}<label class="stack"
				><span>{t('firewall.kindService')}</span><select class="field" bind:value={policy.service} disabled={locked}
					>{#each services as s (s.id)}<option value={s.id}>{t(`firewall.svc.${s.id}` as MessageKey)}</option>{/each}</select
				></label
			>{:else if kind === 'app'}<label class="stack"
				><span>{t('firewall.kindApp')}</span><select class="field" bind:value={policy.app} disabled={locked}
					>{#each apps as app (app.key)}<option value={app.key}>{app.name}</option>{/each}{#if policy.app && !apps.some((a) => a.key === policy.app)}<option
							value={policy.app}>{policy.app.replace(/^container:/, '')}</option
						>{/if}</select
				></label
			>{:else}<label class="stack"
				><span>{t('firewall.ruleProtocol')}</span><select class="field" bind:value={policy.protocol} disabled={locked}
					><option value="tcp">TCP</option><option value="udp">UDP</option><option value="any"
						>{t('firewall.anyProtocol')}</option
					></select
				></label
			><label class="stack"
				><span>{t('firewall.rulePortsLabel')}</span><input
					class="field mono"
					bind:value={policy.ports}
					placeholder={t('firewall.allPorts')}
					disabled={locked}
				/></label
			>{/if}
		<label class="stack"
			><span>{t('firewall.ruleSourceLabel')}</span><input
				class="field mono"
				bind:value={policy.source}
				placeholder={t('firewall.anySource')}
				spellcheck="false"
				disabled={locked}
			/></label
		>
		{#if zoneCell}<label class="stack"
				><span>{t('firewall.policyDestinationLabel')}</span><input
					class="field mono"
					bind:value={policy.destination}
					placeholder={t('firewall.anyDestination')}
					spellcheck="false"
					disabled={locked}
				/></label
			>{/if}
		<label class="stack note"
			><span>{t('firewall.ruleCommentLabel')}</span><input
				class="field"
				bind:value={policy.comment}
				maxlength="80"
				disabled={locked}
			/></label
		>
	</div>
	{#if kind === 'service' && policy.service}
		{@const service = services.find((s) => s.id === policy.service)}
		<p class="muted small">
			{t(`firewall.svcText.${policy.service}` as MessageKey)}{#if service}
				· <span class="mono">{servicePorts(service)}</span>{/if}
		</p>
	{/if}
	{#if problem}<p class="notice-warn" role="alert" id="{id}-problem">{t(problem as MessageKey)}</p>{/if}
</li>

<style>
	.policy {
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.policy.off {
		opacity: 0.7;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
	}
	.small {
		font-size: 0.8125rem;
		margin: 0;
	}
	.order {
		color: var(--muted);
		min-width: 1.5rem;
	}
	.where {
		font-weight: 500;
	}
	.on {
		margin: 0;
	}
	.buttons {
		margin-left: auto;
		white-space: nowrap;
	}
	.buttons .btn + .btn {
		margin-left: 4px;
	}
	.fields {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(9.5rem, 1fr));
		gap: 8px 12px;
	}
	.fields .note {
		grid-column: span 2;
	}
	.fields .field {
		width: 100%;
		min-width: 0;
	}
	@media (max-width: 760px) {
		.fields .note {
			grid-column: auto;
		}
	}
</style>
