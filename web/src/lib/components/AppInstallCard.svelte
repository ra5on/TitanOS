<script lang="ts">
	// PLAN 70: Mein Konto › "TitanOS als App" – install where the browser can,
	// a short way for iOS, "installed" when it runs as the app. One row
	// (PLAN 78-5).
	import { startApps, savedStart } from '#lib/pwa-start.ts';
	import { session } from '#lib/session.svelte.ts';
	import PushDevices from './PushDevices.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { install, isIOS, promptInstall } from '#lib/pwa-install.svelte.ts';
	import Icon from './Icon.svelte';
	import SettingRow from './settings/SettingRow.svelte';
	const ios = isIOS();
	const user = $derived(session.data?.user);
	const secure = typeof window !== 'undefined' && window.isSecureContext;
	let start = $state('/');
	let saveError = $state(false);
	$effect(() => {
		if (user) start = savedStart(user, session.services);
	});
	function setStart(path: string) {
		if (!user) return;
		try {
			localStorage.setItem('titanos.start.' + user.name, path);
			start = path;
			saveError = false;
		} catch {
			saveError = true;
		}
	}
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
		{#if user}<div class="srow-block">
				<p>{t(secure ? 'push.secureOn' : 'push.secureOff')}</p>
				<label
					>{t('push.startPage')}
					<select class="field" value={start} onchange={(e) => setStart(e.currentTarget.value)}>
						<option value="/">{t('push.appGrid')}</option>
						{#each startApps(user, session.services) as app (app.id)}
							<option value={app.href}>{t(app.label)}</option>
						{/each}
					</select>
				</label>
				{#if saveError}<p class="notice-error" role="alert">{t('push.startSaveError')}</p>{/if}
				<PushDevices bare personal={user.role === 'user'} />
			</div>{/if}
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
