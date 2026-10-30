<script lang="ts">
	// Remote access in one place: the Tailscale and NetBird settings of
	// their plugins when installed, links to the rest.
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import TailscaleSettings from '#lib/components/TailscaleSettings.svelte';
	import NetbirdSettings from '#lib/components/NetbirdSettings.svelte';

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

{#each [{ name: 'tailscale', title: 'Tailscale' }, { name: 'netbird', title: 'NetBird' }] as remote (remote.name)}
	<section class="tile panel" aria-labelledby="remote-{remote.name}">
		<h2 class="tile-title" id="remote-{remote.name}">{remote.title}</h2>
		{#if plugins === null && !failed}
			<p class="muted">{t('netx.loading')}</p>
		{:else if installed(remote.name)}
			{#if remote.name === 'tailscale'}<TailscaleSettings />{:else}<NetbirdSettings />{/if}
		{:else}
			<p class="muted">{failed ? t('netx.remoteUnknown') : t('netx.remoteMissing')}</p>
			<div class="row-actions"><a class="btn" href="/plugins">{t('netx.remoteInstall')}</a></div>
		{/if}
	</section>
{/each}
<section class="tile panel" aria-labelledby="remote-proxy">
	<h2 class="tile-title" id="remote-proxy">{t('netx.proxyTitle')}</h2>
	<p class="muted">{t('netx.proxyHint')}</p>
	<div class="row-actions">
		<a class="btn" href="/settings/proxy">{t('proxy.settingsTitle')}</a><a
			class="btn"
			href="/docker">{t('netx.appLinks')}</a
		>
	</div>
</section>
<section class="tile panel" aria-labelledby="remote-dns">
	<h2 class="tile-title" id="remote-dns">{t('netx.dnsServices')}</h2>
	<p class="muted">{t('netx.dnsHint')}</p>
	<div class="row-actions">
		<a class="btn" href="/settings/ddns">{t('ddns.title')}</a><a
			class="btn"
			href="/settings/ddns?tab=ipv64">{t('ipv64x.title')}</a
		>
	</div>
</section>
