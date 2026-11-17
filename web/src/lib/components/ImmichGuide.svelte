<script lang="ts">
	// 1.07, owner's decision of 16/17 Nov 2026: "Fotos vom iPhone sichern"
	// with Immich from the catalog and Immich's own app (no shortcuts). Four
	// short steps with the real addresses of this NAS (GET /immich/phone:
	// the home network and, if set up, Tailscale, each with the port of the
	// Immich app), and what the app can and cannot do in the background on
	// an iPhone. Checked against Immich's documentation of v3.3.3
	// (docs/features/mobile-backup.md, i18n of the app).
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		installHref = '/apps/install/titanos/immich',
		open = $bindable(false),
	}: {
		/** Where "Immich installieren" leads; '' when this page is that place. */
		installHref?: string;
		open?: boolean;
	} = $props();

	type Phone = { immich: boolean; port: number; addresses?: { kind: string; url: string }[] };
	let phone = $state<Phone | null>(null);
	$effect(() => {
		api.get<Phone>('/immich/phone').then(
			(p) => (phone = p),
			() => (phone = { immich: false, port: 0 }),
		);
	});
	const lan = $derived(phone?.addresses?.find((a) => a.kind === 'lan')?.url ?? '');
	const tailnet = $derived(phone?.addresses?.find((a) => a.kind === 'tailscale')?.url ?? '');
</script>

<details class="guide" bind:open>
	<summary>
		<strong>{t('immichGuide.title')}</strong>
		<span>{t('immichGuide.lead')}</span>
	</summary>
	<ol>
		<li>
			<strong>{t('immichGuide.install')}</strong>
			{#if phone?.immich}
				<span class="ok">{t('immichGuide.installed', { port: phone.port })}</span>
			{:else}
				<span>{t('immichGuide.installText')}</span>
				{#if installHref}<a class="btn" href={installHref}>{t('immichGuide.installButton')}</a>{/if}
			{/if}
			<span>{t('immichGuide.account')}</span>
		</li>
		<li>
			<strong>{t('immichGuide.app')}</strong>
			<span>{t('immichGuide.appText')}</span>
			{#if lan}
				<span>{t('immichGuide.home')} <code>{lan}</code></span>
			{:else}
				<span>{t('immichGuide.homeUnknown')}</span>
			{/if}
			{#if tailnet}
				<span>{t('immichGuide.away')} <code>{tailnet}</code></span>
				<span>{t('immichGuide.switching')}</span>
			{:else}
				<span>{t('immichGuide.noTailscale')}</span>
			{/if}
			<span class="muted">{t('immichGuide.https')}</span>
		</li>
		<li>
			<strong>{t('immichGuide.backup')}</strong>
			<span>{t('immichGuide.backupText')}</span>
		</li>
		<li>
			<strong>{t('immichGuide.background')}</strong>
			<span>{t('immichGuide.backgroundText')}</span>
		</li>
	</ol>
	<div class="limits">
		<strong>{t('immichGuide.can')}</strong>
		<ul>
			<li>{t('immichGuide.can1')}</li>
			<li>{t('immichGuide.can2')}</li>
		</ul>
		<strong>{t('immichGuide.cannot')}</strong>
		<ul>
			<li>{t('immichGuide.cannot1')}</li>
			<li>{t('immichGuide.cannot2')}</li>
			<li>{t('immichGuide.cannot3')}</li>
		</ul>
	</div>
</details>

<style>
	.guide {
		border: 1px solid var(--srow-line, var(--border));
		border-radius: var(--srow-radius, 6px);
		background: var(--srow-bg, var(--surface));
		padding: 0 var(--space-4);
		line-height: 1.45;
	}
	summary {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-height: 44px;
		justify-content: center;
		padding: var(--space-2) 0;
		cursor: pointer;
	}
	summary span,
	.muted {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	ol {
		margin: 0;
		padding: 0 0 0 1.4em;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	ol > li > * {
		display: block;
		margin-top: 2px;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	ol > li > .btn {
		display: inline-flex;
		align-items: center;
		min-height: 44px;
		margin-top: var(--space-1);
	}
	code {
		overflow-wrap: anywhere;
		user-select: all;
	}
	.ok {
		color: var(--status-good-text, var(--text));
	}
	.limits {
		margin: var(--space-3) 0 var(--space-3);
	}
	.limits ul {
		margin: var(--space-1) 0 var(--space-2);
		padding-left: 1.2em;
	}
</style>
