<script lang="ts">
	// The fields of one HTTPS name, shared by the app editor and the dialog
	// on Settings › HTTPS names for apps. The form is the caller's; problems
	// come from proxyProblems, so both places check alike.
	import { proxyAddresses, splitList, type ProxyForm, type ProxyProblem } from '#lib/app-proxy.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		form = $bindable(),
		target = false,
		problems = [],
		suffixes = [],
		mdns = true,
		tailnet,
		disabled = false,
	}: {
		form: ProxyForm;
		target?: boolean;
		problems?: ProxyProblem[];
		suffixes?: string[];
		mdns?: boolean;
		/** What the NAS offers for names in the tailnet; undefined = an older NAS. */
		tailnet?: { available: boolean; keySet: boolean };
		disabled?: boolean;
	} = $props();

	const name = $derived(form.name.trim().toLowerCase());
	const preview = $derived([
		...proxyAddresses(name, suffixes),
		...(form.short && mdns ? proxyAddresses(name, ['local']) : []),
	]);
	const aliasCount = $derived(splitList(form.aliases).length);
	const domainCount = $derived(splitList(form.domains).length);
</script>

<div class="fields">
	<label class="check"
		><input type="checkbox" bind:checked={form.enabled} {disabled} /><span
			>{t('proxy.enabled')}</span
		></label
	>
	<div class="grid">
		<label
			><span>{t('proxy.name')}</span><input
				class="field mono"
				type="text"
				bind:value={form.name}
				spellcheck="false"
				autocapitalize="off"
				maxlength="63"
				{disabled}
			/></label
		>
		<label
			><span>{t('proxy.aliases')}</span><input
				class="field mono"
				type="text"
				bind:value={form.aliases}
				placeholder={t('proxy.aliasesPlaceholder')}
				spellcheck="false"
				autocapitalize="off"
				{disabled}
			/><small>{t('proxy.aliasesHint', { count: aliasCount })}</small></label
		>
		<label
			><span>{t('proxy.domains')}</span><input
				class="field mono"
				type="text"
				bind:value={form.domains}
				placeholder="fotos.example.org"
				spellcheck="false"
				autocapitalize="off"
				{disabled}
			/><small>{t('proxy.domainsHint', { count: domainCount })}</small></label
		>
		{#if target}
			<label
				><span>{t('proxy.host')}</span><input
					class="field mono"
					type="text"
					bind:value={form.host}
					placeholder="192.168.1.30"
					spellcheck="false"
					autocapitalize="off"
					{disabled}
				/><small>{t('proxy.hostHint')}</small></label
			>
		{/if}
		<label
			><span>{t(target ? 'proxy.targetPort' : 'proxy.port')}</span><input
				class="field mono"
				type="number"
				min="1"
				max="65535"
				bind:value={form.port}
				{disabled}
			/>{#if !target}<small>{t('proxy.portHint')}</small>{/if}</label
		>
		<label
			><span>{t(target ? 'proxy.targetScheme' : 'proxy.scheme')}</span>
			<select class="field" bind:value={form.scheme} {disabled}
				><option value="http">{t('proxy.scheme.http')}</option><option value="https"
					>{t('proxy.scheme.https')}</option
				></select
			>
		</label>
	</div>
	{#if form.scheme === 'https'}
		<div class="option">
			<label class="check"
				><input type="checkbox" bind:checked={form.verifyTLS} {disabled} /><span
					>{t('proxy.verifyTLS')}</span
				></label
			>
			<small>{t('proxy.verifyTLSHint')}</small>
		</div>
	{/if}
	<div class="option">
		<label class="check"
			><input type="checkbox" bind:checked={form.short} disabled={disabled || !mdns} /><span
				>{t('proxy.short', { name: name || 'app' })}</span
			></label
		>
		<small>{mdns ? t('proxy.shortHint') : t('proxy.shortNoMdns')}</small>
	</div>
	{#if tailnet}
		<div class="option">
			<label class="check"
				><input type="checkbox" bind:checked={form.tailscale} {disabled} /><span
					>{t('proxy.tailscale')}</span
				></label
			>
			<small>{t('proxy.tailscaleHint')}</small>
		</div>
		{#if form.tailscale}
			<label class="tailnet-name"
				><span>{t('proxy.tailscaleName')}</span><input
					class="field mono"
					type="text"
					bind:value={form.tailscaleName}
					placeholder={name || 'app'}
					spellcheck="false"
					autocapitalize="off"
					maxlength="63"
					{disabled}
				/><small
					>{t('proxy.tailscaleNameHint', {
						name: form.tailscaleName.trim().toLowerCase() || name || 'app',
					})}</small
				></label
			>
			{#if !tailnet.available}<p class="notice-warn">{t('proxy.tailscaleNoPlugin')}</p>
			{:else if !tailnet.keySet}<p class="notice-warn">{t('proxy.tailscaleNoKey')}</p>{/if}
		{/if}
	{/if}
	<label class="access"
		><span>{t('proxy.access')}</span>
		<select class="field" bind:value={form.access} {disabled}
			><option value="local">{t('proxy.access.local')}</option><option value="networks"
				>{t('proxy.access.networks')}</option
			><option value="all">{t('proxy.access.all')}</option></select
		>
	</label>
	{#if form.access === 'networks'}
		<label
			><span>{t('proxy.networks')}</span><textarea
				class="field mono"
				rows="3"
				bind:value={form.networks}
				placeholder={'192.168.1.0/24\n100.64.0.0/10'}
				spellcheck="false"
				{disabled}></textarea><small>{t('proxy.networksHint')}</small></label
		>
	{:else if form.access === 'all'}
		<p class="notice-warn" role="alert">{t('proxy.localOnlyOff')}</p>
	{/if}
	{#if preview.length}
		<div class="addresses">
			<span class="muted">{t('proxy.addressesPreview')}</span>
			{#each preview as address (address)}<span class="mono">{address}</span>{/each}
		</div>
	{/if}
	{#if problems.length}
		<ul class="problems" role="alert">
			{#each problems as problem, i (i)}
				<li>
					{problem.key === 'proxy.nameTaken'
						? t(problem.key, { app: problem.app })
						: problem.key === 'proxy.aliasInvalid'
							? t(problem.key, { alias: problem.alias })
							: problem.key === 'proxy.networksInvalid'
								? t(problem.key, { network: problem.network })
								: problem.key === 'proxy.shortIsNas'
									? t(problem.key, { name: problem.name })
									: problem.key === 'proxy.tailnetNameTaken'
										? t(problem.key, { app: problem.app })
										: problem.key === 'proxy.domainInvalid'
											? t(problem.key, { domain: problem.domain })
											: t(problem.key)}
				</li>
			{/each}
		</ul>
	{/if}
</div>

<style>
	.fields {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
		min-width: 0;
	}
	label > span {
		color: var(--muted);
	}
	.access,
	.tailnet-name {
		max-width: 32rem;
	}
	small {
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.option {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.option small {
		padding-left: 28px;
	}
	textarea {
		resize: vertical;
		padding-block: 8px;
	}
	.addresses {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 14px;
		font-size: 0.8125rem;
		overflow-wrap: anywhere;
	}
	.problems {
		margin: 0;
		padding: 10px 14px 10px 28px;
		background: var(--warn-bg);
		border: 1px solid var(--warn-border);
		border-radius: var(--radius-ctl);
		color: var(--warn-text);
		font-size: 0.875rem;
	}
</style>
