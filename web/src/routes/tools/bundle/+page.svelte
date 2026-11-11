<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
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

<PageHead title={t('tools.bundle')}>
	<p class="muted">{t('tools.bundleText')}</p>
</PageHead>

<!-- PLAN 78-5: what the bundle holds and leaves out as two flat rows, the
     button as the third. -->
<SettingGroup id="tools-bundle">
	<SettingRow icon="archive" title={t('tools.bundleIn')} desc={t('tools.bundleInText')} />
	<SettingRow icon="shield" title={t('tools.bundleOut')} desc={t('tools.bundleOutText')} />
	<p class="notice-warn">{t('tools.bundleCheck')}</p>
	{#if started}<p class="notice-ok" role="status">{t('tools.bundleStarted')}</p>{/if}
	<div class="row-actions">
		<button type="button" class="btn btn-primary" onclick={pack}>{t('tools.bundleMake')}</button>
	</div>
</SettingGroup>

<style>
	.row-actions {
		margin-top: var(--space-2);
	}
</style>
