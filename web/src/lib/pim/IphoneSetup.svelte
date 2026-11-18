<script lang="ts">
	// 1.11: set up T-Kalender and Kontakte on the iPhone (TECHNIK 4.2, draft
	// 108 pictures 20/21/60/61): 1. device passwords, 2. what to type in –
	// the servers in the order of the owner (Let's Encrypt, Tailscale,
	// home network), the account name, the full addresses for emergencies –,
	// 3. the steps on the iPhone with the path of Stalwart's finding (TECHNIK
	// 1.5: through Settings › Apps › Kalender, not through Mail). One page for
	// both programs (1.11 step 11): /calendar/iphone in T-Kalender and
	// /contacts/iphone in Kontakte; `program` is the place above it.
	import { api } from '#lib/api.ts';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { DeviceSetup, SetupAddress } from '#lib/pim/devices.ts';
	import { session } from '#lib/session.svelte.ts';
	import Devices from './Devices.svelte';
	import ProfileSetup from './ProfileSetup.svelte';

	let { program }: { program: 'calendar' | 'contacts' } = $props();

	const phone = $derived(phoneView());
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	let setup = $state<DeviceSetup | null>(null);
	let error = $state('');
	let copied = $state('');
	// Raised when the iPhone fetched a profile: the list of devices reads again.
	let refresh = $state(0);

	$effect(() => {
		api
			.get<DeviceSetup>('/account/devices/setup')
			.then((r) => {
				setup = { ...r, addresses: r.addresses ?? [] };
				error = '';
			})
			.catch((err) => (error = apiErrorText(err)));
	});

	const ICONS: Record<SetupAddress['kind'], IconName> = {
		domain: 'globe',
		tailscale: 'tailnet',
		lan: 'home',
		ddns: 'globe',
		netbird: 'tailnet',
	};
	const KIND_TEXT: Record<SetupAddress['kind'], MessageKey> = {
		domain: 'iphone.server.domain',
		tailscale: 'iphone.server.tailscale',
		lan: 'iphone.server.lan',
		ddns: 'iphone.server.ddns',
		netbird: 'iphone.server.netbird',
	};
	const OR_TITLE: Record<SetupAddress['kind'], MessageKey> = {
		domain: 'iphone.serverOr.domain',
		tailscale: 'iphone.serverOr.tailscale',
		lan: 'iphone.serverOr.lan',
		ddns: 'iphone.serverOr.ddns',
		netbird: 'iphone.serverOr.netbird',
	};
	const HINTS: Record<string, MessageKey> = {
		tailscaleApp: 'iphone.hint.tailscaleApp',
		netbirdApp: 'iphone.hint.netbirdApp',
		homeOnly: 'iphone.hint.homeOnly',
		selfSignedOnce: 'iphone.hint.selfSignedOnce',
	};

	function describe(a: SetupAddress): string {
		const parts = [t(KIND_TEXT[a.kind])];
		if (a.kind === 'tailscale' && a.certificate === 'valid') parts.push(t('iphone.hint.validCert'));
		for (const h of a.hints) if (HINTS[h]) parts.push(t(HINTS[h]));
		return parts.join(' ');
	}

	async function copy(value: string) {
		try {
			await navigator.clipboard.writeText(value);
			copied = value;
		} catch {
			copied = '';
		}
	}

	// The steps on the iPhone, each a title and a text.
	const STEPS: [MessageKey, MessageKey][] = [
		['iphone.step1.title', 'iphone.step1.text'],
		['iphone.step2.title', 'iphone.step2.text'],
		['iphone.step3.title', 'iphone.step3.text'],
		['iphone.step4.title', 'iphone.step4.text'],
		['iphone.step5.title', 'iphone.step5.text'],
	];
	// Without any address known: the one this page came over.
	const here = typeof location === 'undefined' ? '' : location.host;
</script>

{#snippet copyable(value: string, what: string)}
	<span class="val">
		<code>{value}</code><button
			type="button"
			class="btn cp"
			aria-label={t('iphone.copyWhat', { what })}
			onclick={() => copy(value)}
			><Icon name={copied === value ? 'check' : 'copy'} size={15} />{phone
				? ''
				: copied === value
					? t('iphone.copied')
					: t('iphone.copy')}</button
		>
	</span>
{/snippet}

<PageHead
	title={t('iphone.title')}
	path={[
		program === 'calendar'
			? { label: t('desktop.app.calendar'), href: '/calendar' }
			: { label: t('desktop.app.contacts'), href: '/contacts' },
	]}
/>

<div class="page" class:phone>
	<p class="lead">{t('iphone.lead')}</p>

	<ProfileSetup {setup} {describe} onfetched={() => refresh++} />

	<div class="manual">
		<h2>{t('iphone.manual.title')}</h2>
		<p class="lead">{t('iphone.manual.desc')}</p>
	</div>

	<Devices {refresh} />

	<SettingGroup title={t('iphone.details.title')} id="details">
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if setup}
			{#each setup.addresses as a, i (a.server)}
				<SettingRow icon={ICONS[a.kind]} title={i === 0 ? t('iphone.server') : t(OR_TITLE[a.kind])}>
					{#snippet badge()}{#if a.recommended}<span class="chip ok">{t('iphone.recommended')}</span
							>{/if}{/snippet}
					{#snippet description()}
						{describe(a)}
						{#if a.hints.includes('tailscaleCertificate')}
							<span class="line"
								>{t('iphone.hint.tailscaleCertificate')}
								{#if admin}<a href="/settings/network?tab=remote#remote-tailscale"
										>{t('iphone.hint.tailscaleCertificateLink')}</a
									>{:else}{t('iphone.hint.tailscaleCertificateAdmin')}{/if}</span
							>
						{/if}
					{/snippet}
					{#snippet control()}{@render copyable(a.server, t('iphone.server'))}{/snippet}
				</SettingRow>
			{:else}
				<SettingRow icon="globe" title={t('iphone.server')} desc={t('iphone.server.none')}>
					{#snippet control()}{@render copyable(here, t('iphone.server'))}{/snippet}
				</SettingRow>
			{/each}
			<SettingRow icon="users" title={t('iphone.user')} desc={t('iphone.userDesc')}>
				{#snippet control()}{@render copyable(setup!.user, t('iphone.user'))}{/snippet}
			</SettingRow>
			<SettingRow icon="key" title={t('iphone.password')} desc={t('iphone.passwordDesc')}>
				{#snippet control()}<span class="muted">{t('iphone.passwordValue')}</span>{/snippet}
			</SettingRow>
			{#if setup.addresses.length}
				<SettingRow icon="info" title={t('iphone.more.title')}>
					{#snippet description()}
						{t('iphone.more.text', { port: setup!.port })}
						{#each setup!.addresses as a (a.server)}
							<span class="line url"
								><span class="host">{a.server}:</span>
								{t('iphone.more.calendar')} <code>{a.caldav}</code>, {t('iphone.more.contacts')}
								<code>{a.carddav}</code></span
							>
						{/each}
					{/snippet}
				</SettingRow>
			{/if}
		{/if}
	</SettingGroup>

	<SettingGroup title={t('iphone.steps.title')} id="steps">
		{#each STEPS as [title, text], i (title)}
			<div class="srow plain step">
				<span class="num" aria-hidden="true">{i + 1}</span>
				<span class="st"
					><b><span class="sr-only">{i + 1}. </span>{t(title)}</b><span class="muted"
						>{t(text)}</span
					>
					{#if i === 0}<span class="muted note">{t('iphone.step1.notMail')}</span>{/if}</span
				>
			</div>
		{/each}
	</SettingGroup>
</div>

<style>
	.page {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		max-width: 980px;
	}
	.lead {
		margin: 0;
		color: var(--muted);
	}
	.manual {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		margin-top: var(--space-2);
	}
	.manual h2 {
		margin: 0;
		font-size: var(--text-lg);
	}
	code {
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: 0.92em;
		padding: 2px 6px;
		border-radius: var(--radius-small);
		background: var(--chip);
		overflow-wrap: anywhere;
	}
	.val {
		display: inline-flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
		max-width: 100%;
	}
	.cp {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.phone :global(.btn),
	.phone :global(.field) {
		min-height: 44px;
		min-width: 44px;
		justify-content: center;
	}
	.line {
		display: block;
		margin-top: var(--space-1);
	}
	.url {
		overflow-wrap: anywhere;
	}
	.host {
		font-weight: 600;
	}
	.step {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3);
	}
	.num {
		flex: none;
		display: inline-grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 700;
		font-size: var(--text-sm);
	}
	.st {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.note {
		margin-top: var(--space-1);
	}
</style>
