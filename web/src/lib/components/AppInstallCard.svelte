<script lang="ts">
	// PLAN 70: Mein Konto › "TitanOS als App" – install where the browser can,
	// a short way for iOS, "installed" when it runs as the app. One row
	// (PLAN 78-5).
	import { t } from '#lib/i18n/index.svelte.ts';
	import { install, isIOS, promptInstall } from '#lib/pwa-install.svelte.ts';
	import Icon from './Icon.svelte';
	import SettingRow from './settings/SettingRow.svelte';
	const ios = isIOS();
</script>

<div data-testid="app-install">
	<SettingRow id="acc-app" icon="download" title={t('share.appTitle')}>
		{#snippet description()}
			{t('share.appText')}
			{#if !install.installed && !install.prompt}
				<span class="line">{ios ? t('share.iosHint') : t('share.otherHint')}</span>
			{/if}
		{/snippet}
		{#snippet control()}
			{#if install.installed}
				<span class="chip ok"><Icon name="check" size={14} />{t('share.installed')}</span>
			{:else if install.prompt}
				<button type="button" class="btn btn-primary" onclick={() => void promptInstall()}
					>{t('share.install')}</button
				>
			{/if}
		{/snippet}
	</SettingRow>
</div>

<style>
	.line {
		display: block;
		margin-top: var(--space-1);
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
</style>
