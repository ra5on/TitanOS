<script lang="ts">
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let packing = $state(false);
	let packError = $state('');
	// The bundle is made while it is being downloaded; the browser saves it.
	async function pack() {
		packing = true;
		packError = '';
		try {
			await api.download('/diagnostics/bundle', 'titanos-diagnose.zip', { serverName: true });
		} catch {
			packError = t('tools.bundleFailed');
		}
		packing = false;
	}
</script>

<PageHead title={t('tools.bundle')} parent={{ href: '/tools', label: t('nav.tools') }} />

<section class="tile panel" id="tools-bundle" aria-label={t('tools.bundle')}>
	<p class="muted">{t('tools.bundleText')}</p>
	<dl class="facts">
		<dt>{t('tools.bundleIn')}</dt>
		<dd>{t('tools.bundleInText')}</dd>
		<dt>{t('tools.bundleOut')}</dt>
		<dd>{t('tools.bundleOutText')}</dd>
	</dl>
	<p class="notice-warn">{t('tools.bundleCheck')}</p>
	{#if packError}<p class="notice-error" role="alert">{packError}</p>{/if}
	<div>
		<button type="button" class="btn btn-primary" disabled={packing} onclick={pack}
			>{t(packing ? 'tools.bundlePacking' : 'tools.bundleMake')}</button
		>
	</div>
</section>

<style>
	.facts {
		display: grid;
		grid-template-columns: 12rem minmax(0, 1fr);
		gap: var(--space-2) var(--space-4);
		margin: 0;
		font-size: var(--text-md);
	}
	.facts dt {
		color: var(--muted);
	}
	.facts dd {
		margin: 0;
		min-width: 0;
	}
	@media (max-width: 760px) {
		.facts {
			grid-template-columns: minmax(0, 1fr);
			gap: 2px;
		}
		.facts dd + dt {
			margin-top: var(--space-2);
		}
	}
</style>
