<script lang="ts">
	// Packets the rules dropped lately (RAM only).
	import { api, type FirewallStatus } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';

	let {
		status,
		zoneLabel,
		onreload,
		ontake,
	}: {
		status: FirewallStatus;
		zoneLabel: (id: string) => string;
		onreload: () => void;
		ontake: (next: FirewallStatus) => void;
	} = $props();

	let busy = $state(false);
	let error = $state('');
	async function clear() {
		busy = true;
		error = '';
		try {
			ontake(await api.delete<FirewallStatus>('/firewall/recent'));
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<section class="tile panel" aria-labelledby="firewall-recent">
	<h2 class="tile-title" id="firewall-recent" use:revealTarget>{t('firewall.recent')}</h2>
	<p class="muted">{t('firewall.recentText')}</p>
	{#if status.recent.length}<div class="table-wrap">
			<table class="data-table recent">
				<thead
					><tr
						><th>{t('firewall.time')}</th><th>{t('firewall.address')}</th><th
							>{t('firewall.zoneColumn')}</th
						><th>{t('firewall.ruleProtocol')}</th><th>{t('firewall.port')}</th><th
							>{t('firewall.interface')}</th
						></tr
					></thead
				>
				<tbody>
					{#each status.recent as drop, i (i)}
						<tr>
							<td>{formatDateTime(new Date(drop.at))}</td>
							<td class="mono">{drop.source}</td>
							<td
								>{drop.zone === 'blocked'
									? t('firewall.zoneBlocked')
									: zoneLabel(drop.zone)}{#if drop.app}
									· {t('firewall.toApp')}{/if}{#if drop.forward}
									· {t('firewall.forwardTo', { address: drop.destination ?? '–' })}{/if}</td
							>
							<td>{drop.protocol.toUpperCase()}</td>
							<td class="mono">{drop.port ?? '–'}</td>
							<td class="mono">{drop.interface ?? ''}{drop.out ? ` → ${drop.out}` : ''}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>{:else}<p class="muted">{t('firewall.noRecent')}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="row-actions">
		<button class="btn" type="button" onclick={onreload}>{t('firewall.refresh')}</button>
		<button class="btn" type="button" disabled={busy || !status.recent.length} onclick={clear}
			>{t('firewall.clearRecent')}</button
		>
	</div>
</section>

<style>
	.recent {
		min-width: 36rem;
	}
</style>
