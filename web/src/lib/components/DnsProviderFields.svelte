<script lang="ts">
	// The DNS provider and its sign-in, shared by Let's Encrypt and dynamic
	// DNS (both pages of the control panel: a row with parts, PLAN 78-1). Secrets are never shown: a stored one is kept when its field stays
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
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
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
	// The fields are what is set here: the row stands open.
	let open = $state(true);
	function drop(name: FieldName) {
		cleared = [...cleared, name as SecretName];
	}
</script>

<!-- PLAN 78-1: the provider is a row with its choice; the fields of the
     chosen provider are its parts. -->
{#snippet parts()}
	<div class="srow-block">
		<p class="muted">{t(`dnsProvider.help.${form.type}` as MessageKey)}</p>
	</div>
	{#each fields as field (field.name)}
		<SettingSub
			title={t(`dnsProvider.field.${field.label}` as MessageKey) +
				(field.required ? '' : ` (${t('dnsProvider.optional')})`)}
			for="{id}-{field.name}"
			desc={field.secret && kept(field.name) ? t('dnsProvider.secretKept') : undefined}
		>
			{#snippet control()}
				{#if field.choices}
					<select
						class="field mono"
						id="{id}-{field.name}"
						bind:value={form[field.name]}
						{disabled}
					>
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
						aria-invalid={missing.includes(field.name)}
						{disabled}
					/>
				{:else}
					<input
						class="field mono"
						type="text"
						id="{id}-{field.name}"
						bind:value={form[field.name]}
						placeholder={field.placeholder ?? ''}
						spellcheck="false"
						autocapitalize="off"
						aria-invalid={missing.includes(field.name)}
						{disabled}
					/>
				{/if}
				{#if field.secret && kept(field.name) && !field.required}
					<button type="button" class="btn" {disabled} onclick={() => drop(field.name)}
						>{t('dnsProvider.secretDrop')}</button
					>
				{/if}
			{/snippet}
		</SettingSub>
	{/each}
	{#if form.type === 'dyndns2' || form.type === 'ipv64' || (secrets && !keeps && Object.values(secrets).some(Boolean)) || missing.length}
		<div class="srow-block">
			{#if form.type === 'dyndns2'}<p class="muted">
					{t('dnsProvider.dyndns2Placeholders')}
				</p>{/if}
			{#if form.type === 'ipv64'}<p class="muted">
					{t(
						purpose === 'address'
							? 'dnsProvider.ipv64KeysAddress'
							: 'dnsProvider.ipv64KeysChallenge',
					)}
				</p>{/if}
			{#if secrets && !keeps && Object.values(secrets).some(Boolean)}
				<p class="muted">{t('dnsProvider.secretsNotMoved')}</p>
			{/if}
			{#if missing.length}<p class="notice-warn" role="alert">{t('dnsProvider.missing')}</p>{/if}
		</div>
	{/if}
{/snippet}
<SettingRow
	id="{id}-row"
	icon="globe"
	title={t('dnsProvider.title')}
	for="{id}-type"
	bind:open
	children={form.type ? parts : undefined}
>
	{#snippet control()}
		<select class="field" id="{id}-type" bind:value={form.type} {disabled}>
			<option value="" disabled>{t('dnsProvider.choose')}</option>
			{#each providerTypes(purpose) as type (type)}
				<option value={type}>{t(`dnsProvider.type.${type}` as MessageKey)}</option>
			{/each}
		</select>
	{/snippet}
</SettingRow>
