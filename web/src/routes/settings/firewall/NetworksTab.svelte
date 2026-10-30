<script lang="ts">
	// Every network the NAS knows, with the zone it is in; reassigning one
	// changes the draft (applied on trial like the matrix).
	import type { FirewallConfig, FirewallNetwork } from '#lib/api.ts';
	import { assignNetwork, ownZones, zoneOfNetwork } from '#lib/firewall.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let {
		draft = $bindable(),
		networks,
		zoneLabel,
		locked,
	}: {
		draft: FirewallConfig;
		networks: FirewallNetwork[];
		zoneLabel: (id: string) => string;
		locked: boolean;
	} = $props();

	const own = $derived(ownZones(draft));
	/** The zone a network has without an own one. */
	const fallback = (n: FirewallNetwork) => (n.kind === 'docker' ? 'docker' : '');
	const fallbackLabel = (n: FirewallNetwork) =>
		n.kind === 'docker' ? t('firewall.dockerDefault') : t('firewall.byAddress');
</script>

<section class="tile panel" aria-labelledby="firewall-networks">
	<h2 class="tile-title" id="firewall-networks">{t('firewall.tab.networks')}</h2>
	<p class="muted">{t('firewall.networksText')}</p>
	{#if networks.length}<div class="table-wrap">
			<table class="data-table networks">
				<thead
					><tr
						><th>{t('firewall.netName')}</th><th>{t('firewall.netKind')}</th><th
							>{t('firewall.netDevice')}</th
						><th>{t('firewall.netSubnets')}</th><th>{t('firewall.netZone')}</th></tr
					></thead
				>
				<tbody>
					{#each networks as n (n.id)}
						{@const zone = zoneOfNetwork(draft, n)}
						<tr data-network={n.id}>
							<td
								><strong>{n.name}</strong>{#if n.missing}
									<span class="chip warn">{t('firewall.netMissing')}</span>{/if}</td
							>
							<td>{t(`firewall.kind.${n.kind}` as MessageKey)}</td>
							<td class="mono">{n.device ?? (n.parent ? `↳ ${n.parent}` : '–')}</td>
							<td class="mono small">{n.subnets.join(', ') || '–'}</td>
							<td>
								{#if n.kind === 'dockerlan'}<span class="muted small"
										>{t('firewall.netBypass')}</span
									>
								{:else}<select
										class="field"
										value={zone === fallback(n) ? '' : zone}
										disabled={locked}
										aria-label={t('firewall.netZone')}
										onchange={(event) => assignNetwork(draft, n.id, event.currentTarget.value)}
									>
										<option value="">{fallbackLabel(n)}</option>
										{#each own as z (z.id)}<option value={z.id}>{zoneLabel(z.id)}</option>{/each}
									</select>{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>{:else}<p class="muted">{t('firewall.noNetworks')}</p>{/if}
	{#if !own.length}<p class="muted small">{t('firewall.networksNoZones')}</p>{/if}
	<p class="muted small">{t('firewall.bypassText')}</p>
</section>

<style>
	.networks {
		min-width: 40rem;
	}
	.small {
		font-size: 0.8125rem;
	}
</style>
