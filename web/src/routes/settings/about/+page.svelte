<script lang="ts">
	import { session } from '#lib/session.svelte.ts';
	// What is running, and under which licenses.
	import { api } from '#lib/api.ts';
	import Logo from '#lib/components/Logo.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import DisclaimerText from '#lib/components/DisclaimerText.svelte';
	import { formatDateTime, osVersion } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type About = { version: string; kernel: string; license: string; thirdParty: string };

	let about = $state<About | null>(null);
	let error = $state('');

	$effect(() => {
		api.get<About>('/about').then(
			(res) => (about = res),
			(err) => (error = apiErrorText(err)),
		);
	});

	// The owner's confirmation of the disclaimer (23 Nov 2026).
	type Disclaimer = { accepted: boolean; by?: string; at?: string };
	let disclaimer = $state<Disclaimer | null>(null);
	$effect(() => {
		api
			.get<Disclaimer>('/system/disclaimer')
			.then((res) => (disclaimer = res))
			.catch(() => (disclaimer = null));
	});

	const slot = $derived(system.info?.image?.slot.toUpperCase() ?? '');
</script>

<PageHead title={t('about.title')} />

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#snippet disclaimerText()}
	<div class="srow-block"><DisclaimerText /></div>
{/snippet}

{#snippet licenseText()}
	<div class="srow-block"><pre>{about?.license}</pre></div>
{/snippet}
{#snippet thirdPartyText()}
	<div class="srow-block"><pre class="scroll">{about?.thirdParty}</pre></div>
{/snippet}

{#if about}
	<!-- PLAN 78-1: what runs here as rows with the value at the right; the
	     licence texts open inside their rows. -->
	<SettingGroup>
		<div class="logo"><Logo size={40} /></div>
		<SettingRow icon="info" title={t('about.version')}>
			{#snippet control()}<span class="mono value">{osVersion(about?.version ?? '')}</span
				>{/snippet}
		</SettingRow>
		{#if slot}
			<SettingRow icon="disc" title={t('about.slot')}>
				{#snippet control()}<span class="mono value">{slot}</span>{/snippet}
			</SettingRow>
		{/if}
		<SettingRow icon="chip" title={t('about.kernel')}>
			{#snippet control()}<span class="mono value">{about?.kernel || '–'}</span>{/snippet}
		</SettingRow>
		<SettingRow icon="linux" title={t('about.base')}>
			{#snippet control()}<span class="value">{t('about.baseText')}</span>{/snippet}
		</SettingRow>
		<SettingRow icon="external" title={t('about.source')}>
			{#snippet control()}<a
					href="https://github.com/ra5on/TitanOS"
					target="_blank"
					rel="noreferrer">github.com/ra5on/TitanOS</a
				>{/snippet}
		</SettingRow>
		{#if session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin'}
			<SettingRow href="/welcome?step=4" icon="list" title={t('about.setupList')} />
		{/if}
	</SettingGroup>

	<SettingGroup title={t('about.licenses')}>
		<SettingRow
			id="about-disclaimer"
			icon="shield"
			title={t('disclaimer.title')}
			desc={disclaimer?.accepted && disclaimer.by && disclaimer.at
				? t('disclaimer.confirmed', {
						by: disclaimer.by,
						at: formatDateTime(new Date(disclaimer.at)),
					})
				: disclaimer
					? t('disclaimer.notConfirmed')
					: undefined}
			children={disclaimerText}
		/>
		<SettingRow
			id="about-license"
			icon="fileText"
			title={t('about.license')}
			summary="FSL-1.1-ALv2"
			children={about.license ? licenseText : undefined}
		>
			{#snippet description()}
				<p>{t('about.licenseText')}</p>
				<p>{t('about.licenseLater')}</p>
			{/snippet}
		</SettingRow>
		<SettingRow
			id="about-third"
			icon="box"
			title={t('about.thirdParty')}
			desc={t('about.thirdPartyText')}
			children={about.thirdParty ? thirdPartyText : undefined}
		/>
	</SettingGroup>
{/if}

<style>
	p {
		margin: 0;
	}
	.logo {
		margin-bottom: var(--space-2);
	}
	.value {
		color: var(--text);
		overflow-wrap: anywhere;
	}
	pre {
		margin: 0;
		padding: var(--space-3);
		background: var(--track);
		border-radius: var(--radius-ctl);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		line-height: 1.5;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.scroll {
		max-height: 28rem;
		overflow: auto;
		white-space: pre;
		overflow-wrap: normal;
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
	}
</style>
