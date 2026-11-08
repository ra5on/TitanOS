<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let started = $state(false);
	// PLAN 75-10: the browser itself downloads the bundle while titand packs
	// it, so the download bar shows it at once. Before, the page collected the
	// whole file first (minutes on a NAS with many disks) and then handed it
	// over - from a window of the desktop this ended with nothing to see.
	function pack() {
		const link = document.createElement('a');
		link.href = apiPath('/diagnostics/bundle');
		// titand's name (with the time) wins over this one when it sends it.
		link.download = 'titanos-diagnose.zip';
		document.body.append(link);
		link.click();
		link.remove();
		started = true;
	}

	// PLAN 67-1: Ablage › Diagnosepaket erstellen in the menu bar of the desktop.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({ 'logs.pack': { run: pack, enabled: () => true } });
</script>

<PageHead title={t('tools.bundle')} />

<section class="tile panel" id="tools-bundle" aria-label={t('tools.bundle')}>
	<p class="muted">{t('tools.bundleText')}</p>
	<dl class="facts">
		<dt>{t('tools.bundleIn')}</dt>
		<dd>{t('tools.bundleInText')}</dd>
		<dt>{t('tools.bundleOut')}</dt>
		<dd>{t('tools.bundleOutText')}</dd>
	</dl>
	<p class="notice-warn">{t('tools.bundleCheck')}</p>
	{#if started}<p class="notice-ok" role="status">{t('tools.bundleStarted')}</p>{/if}
	<div>
		<button type="button" class="btn btn-primary" onclick={pack}>{t('tools.bundleMake')}</button>
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
