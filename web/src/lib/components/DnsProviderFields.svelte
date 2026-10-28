<script lang="ts">
	// The DNS provider and its sign-in, shared by Let's Encrypt and dynamic
	// DNS. Secrets are never shown: a stored one is kept when its field stays
	// empty (for the same account), an optional one can be dropped.
	import {
		fieldsFor,
		providerTypes,
		sameAccount,
		type DnsProvider,
		type DnsProviderType,
		type FieldName,
		type SecretName,
	} from '#lib/dns-providers.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let {
		form = $bindable(),
		cleared = $bindable([]),
		stored,
		secrets = {},
		purpose,
		missing = [],
		disabled = false,
		id,
	}: {
		form: DnsProvider;
		cleared?: SecretName[];
		stored: DnsProvider;
		secrets?: Partial<Record<SecretName, boolean>>;
		purpose: 'challenge' | 'address';
		missing?: FieldName[];
		disabled?: boolean;
		id: string;
	} = $props();

	const fields = $derived(form.type ? fieldsFor(form.type as DnsProviderType, purpose) : []);
	const keeps = $derived(sameAccount(form, stored));
	function kept(name: FieldName): boolean {
		return keeps && !!secrets[name as SecretName] && !cleared.includes(name as SecretName);
	}
	function drop(name: FieldName) {
		cleared = [...cleared, name as SecretName];
	}
</script>

<div class="fields">
	<label class="stack"
		><span>{t('dnsProvider.type')}</span><select class="field" bind:value={form.type} {disabled}>
			<option value="" disabled>{t('dnsProvider.choose')}</option>
			{#each providerTypes(purpose) as type (type)}
				<option value={type}>{t(`dnsProvider.type.${type}` as MessageKey)}</option>
			{/each}
		</select></label
	>
	{#if form.type}
		<p class="muted hint">{t(`dnsProvider.help.${form.type}` as MessageKey)}</p>
		{#each fields as field (field.name)}
			<label class="stack"
				><span
					>{t(`dnsProvider.field.${field.label}` as MessageKey)}{#if !field.required}
						<span class="muted">({t('dnsProvider.optional')})</span>{/if}</span
				>
				{#if field.choices}
					<select class="field mono" bind:value={form[field.name]} {disabled}>
						{#each field.choices as choice (choice)}<option value={choice}>{choice}</option>{/each}
					</select>
				{:else if field.secret}
					<input
						class="field mono"
						type="password"
						id="{id}-{field.name}"
						bind:value={form[field.name]}
						autocomplete="new-password"
						spellcheck="false"
						placeholder={kept(field.name) ? t('dnsProvider.secretKept') : ''}
						aria-invalid={missing.includes(field.name)}
						{disabled}
					/>
				{:else}
					<input
						class="field mono"
						type="text"
						bind:value={form[field.name]}
						placeholder={field.placeholder ?? ''}
						spellcheck="false"
						autocapitalize="off"
						aria-invalid={missing.includes(field.name)}
						{disabled}
					/>
				{/if}</label
			>
			{#if field.secret && kept(field.name) && !field.required}
				<div class="row-actions">
					<button type="button" class="btn" {disabled} onclick={() => drop(field.name)}
						>{t('dnsProvider.secretDrop')}</button
					>
				</div>
			{/if}
		{/each}
		{#if form.type === 'dyndns2'}<p class="muted hint">{t('dnsProvider.dyndns2Placeholders')}</p>{/if}
		{#if form.type === 'ipv64'}<p class="muted hint">{t(purpose === 'address' ? 'dnsProvider.ipv64KeysAddress' : 'dnsProvider.ipv64KeysChallenge')}</p>{/if}
		{#if secrets && !keeps && Object.values(secrets).some(Boolean)}
			<p class="muted hint">{t('dnsProvider.secretsNotMoved')}</p>
		{/if}
		{#if missing.length}<p class="notice-warn" role="alert">{t('dnsProvider.missing')}</p>{/if}
	{/if}
</div>

<style>
	.fields {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.hint {
		margin: 0;
	}
	input,
	select {
		max-width: 32rem;
	}
</style>
