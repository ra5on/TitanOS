<script lang="ts">
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import DataBackups from '../DataBackups.svelte';
	const version = $derived(system.info?.version ?? '');
</script>

<PageHead title={t('safeguard.restore')} />
<DataBackups restoreOnly />
<!-- PLAN 78-1: the two other ways back as rows; the steps of a new stick
     open inside their row (open at first: they are what this row is for). -->
<SettingGroup>
	<div id="restore-config" use:revealTarget>
		<SettingRow icon="undo" title={t('safeguard.configRestore')} desc={t('safeguard.restoreHint')}>
			{#snippet control()}<a class="btn" href="/settings/backups/configuration#config-downloads"
					>{t('safeguard.download')}</a
				>{/snippet}
		</SettingRow>
	</div>
	<div id="stick-replace" use:revealTarget>
		<SettingRow
			id="stick-replace-steps"
			anchors={['stick-replace']}
			icon="key"
			title={t('stick.replace')}
			desc={t('stick.replaceIntro')}
			open
		>
			<div class="srow-block">
				<ol>
					<li>
						{t('stick.step1')}{#if version}
							<span class="mono">({version})</span>{/if}
					</li>
					<li>{t('stick.step2')}</li>
					<li>{t('stick.step3')}</li>
					<li>{t('stick.step4')}</li>
				</ol>
				<p class="muted">{t('stick.replaceNote')}</p>
			</div>
		</SettingRow>
	</div>
</SettingGroup>

<style>
	ol {
		margin: 0;
		padding-left: 1.4rem;
		display: grid;
		gap: var(--space-2-5);
	}
</style>
