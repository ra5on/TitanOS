<script lang="ts">
	// Remote access in one place: the Tailscale and NetBird settings of
	// their plugins when installed, links to the rest.
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import TailscaleSettings from '#lib/components/TailscaleSettings.svelte';
	import NetbirdSettings from '#lib/components/NetbirdSettings.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

	type Plugin = { name: string; installed: boolean };
	let plugins = $state<Plugin[] | null>(null),
		failed = $state(false);
	$effect(() => {
		api
			.get<{ plugins: Plugin[] }>('/plugins')
			.then((r) => (plugins = r.plugins ?? []))
			.catch(() => (failed = true));
	});
	const installed = (name: string) => !!plugins?.some((p) => p.name === name && p.installed);
</script>

<!-- PLAN 78-1: each service is a row that opens to the settings of its
     plugin (those components are shared with Plugins and keep their look);
     the rest are rows that lead to their pages. -->
<SettingGroup title={t('netx.tab.remote')} id="net-remote">
	{#each [{ name: 'tailscale', title: 'Tailscale' }, { name: 'netbird', title: 'NetBird' }] as remote (remote.name)}
		{#if installed(remote.name)}
			<SettingRow icon="tailnet" title={remote.title} id="remote-{remote.name}" open>
				<div class="srow-block">
					{#if remote.name === 'tailscale'}<TailscaleSettings />{:else}<NetbirdSettings />{/if}
				</div>
			</SettingRow>
		{:else}
			<SettingRow
				icon="tailnet"
				title={remote.title}
				id="remote-{remote.name}"
				desc={plugins === null && !failed
					? t('netx.loading')
					: failed
						? t('netx.remoteUnknown')
						: t('netx.remoteMissing')}
			>
				{#snippet control()}{#if plugins !== null || failed}<a class="btn" href="/plugins"
							>{t('netx.remoteInstall')}</a
						>{/if}{/snippet}
			</SettingRow>
		{/if}
	{/each}
</SettingGroup>
<SettingGroup title={t('netx.proxyTitle')} id="remote-proxy" desc={t('netx.proxyHint')}>
	<SettingRow icon="lock" title={t('proxy.settingsTitle')} href="/settings/proxy" />
	<SettingRow icon="external" title={t('netx.appLinks')} href="/docker" />
</SettingGroup>
<SettingGroup title={t('netx.dnsServices')} id="remote-dns" desc={t('netx.dnsHint')}>
	<SettingRow icon="globe" title={t('ddns.title')} href="/settings/ddns" />
	<SettingRow icon="globe" title={t('ipv64x.title')} href="/settings/ddns?tab=ipv64" />
</SettingGroup>
