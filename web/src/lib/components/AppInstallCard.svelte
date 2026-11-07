<script lang="ts">
	// PLAN 70: Mein Konto › "TitanOS als App" – install where the browser can,
	// a short way for iOS, "installed" when it runs as the app.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { install, isIOS, promptInstall } from '#lib/pwa-install.svelte.ts';
	import Icon from './Icon.svelte';
	const ios = isIOS();
</script>

<section class="tile ui-card" aria-labelledby="acc-app" data-testid="app-install">
	<div class="card-head"><h2 id="acc-app">{t('share.appTitle')}</h2></div>
	<div class="set-row">
		<span class="set-label"
			>{t('share.appTitle')}<span class="set-hint">{t('share.appText')}</span></span
		>
		<span class="set-control end">
			{#if install.installed}
				<span class="chip ok"><Icon name="check" size={14} />{t('share.installed')}</span>
			{:else if install.prompt}
				<button type="button" class="btn btn-primary" onclick={() => void promptInstall()}
					>{t('share.install')}</button
				>
			{/if}
		</span>
	</div>
	{#if !install.installed && !install.prompt}
		<p class="hint muted">{ios ? t('share.iosHint') : t('share.otherHint')}</p>
	{/if}
</section>

<style>
	.hint {
		margin: 0;
		padding: 0 16px 14px;
		font-size: var(--text-sm);
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
</style>
