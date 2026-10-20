<script lang="ts">
	import PageHead from '#lib/components/PageHead.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import DataBackups from '../DataBackups.svelte';
	const version = $derived(system.info?.version ?? '');
</script>
<PageHead title={t('safeguard.restore')} parent={{ href: '/settings/backups', label: t('safeguard.title') }} />
<DataBackups restoreOnly />
<section class="tile" id="restore-config" use:revealTarget>
	<h2>{t('safeguard.configRestore')}</h2>
	<p class="muted">{t('safeguard.restoreHint')}</p>
	<a class="btn" href="/settings/backups/configuration#config-downloads">{t('safeguard.download')}</a>
</section>

	<section class="tile" id="stick-replace" use:revealTarget aria-labelledby="replace-title">
		<h2 class="tile-title" id="replace-title">{t('stick.replace')}</h2>
		<p class="muted">{t('stick.replaceIntro')}</p>
		<ol>
			<li>{t('stick.step1')}{#if version} <span class="mono">({version})</span>{/if}</li>
			<li>{t('stick.step2')}</li>
			<li>{t('stick.step3')}</li>
			<li>{t('stick.step4')}</li>
		</ol>
		<p class="muted">{t('stick.replaceNote')}</p>
	</section>
<style>
 section { display: flex; flex-direction: column; align-items: flex-start; gap: 14px; padding: var(--tile-pad); }
 ol { margin: 0; padding-left: 1.4rem; display: grid; gap: 10px; }
 p { margin: 0; }
 h2 { font-size: 1.125rem; }
</style>
