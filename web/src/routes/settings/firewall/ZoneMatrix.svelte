<script lang="ts">
	// The zone matrix: rows are where a connection comes from, columns
	// where it goes (the NAS itself, published ports of Docker apps, other
	// zones). Cells for traffic that does not pass the NAS are grey.
	import type { FirewallConfig, FirewallService } from '#lib/api.ts';
	import { APPS, cellOf, cellState, exceptions, NAS, routable } from '#lib/firewall.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		cfg,
		services,
		selected,
		zoneLabel,
		zoneText,
		onselect,
	}: {
		cfg: FirewallConfig;
		services: FirewallService[];
		selected: { from: string; to: string } | null;
		zoneLabel: (id: string) => string;
		zoneText: (id: string) => string;
		onselect: (from: string, to: string) => void;
	} = $props();

	const columns = $derived([NAS, APPS, ...cfg.zones.map((z) => z.id)]);
	const columnLabel = (to: string) =>
		to === NAS ? t('firewall.toNas') : to === APPS ? t('firewall.toApps') : zoneLabel(to);
</script>

<div class="table-wrap">
	<table class="data-table matrix">
		<thead>
			<tr>
				<th scope="col" class="corner">{t('firewall.from')} \ {t('firewall.to')}</th>
				{#each columns as to (to)}<th scope="col" class:dest={to !== NAS && to !== APPS}
						>{columnLabel(to)}</th
					>{/each}
			</tr>
		</thead>
		<tbody>
			{#each cfg.zones as zone (zone.id)}
				<tr>
					<th scope="row"
						><strong>{zoneLabel(zone.id)}</strong>{#if zoneText(zone.id)}<br /><span
								class="muted small">{zoneText(zone.id)}</span
							>{/if}</th
					>
					{#each columns as to (to)}
						{#if routable(cfg, zone.id, to)}
							{@const cell = cellOf(cfg, services, zone.id, to)}
							{@const state = cellState(cell)}
							{@const count = exceptions(cell)}
							<td
								><button
									type="button"
									class="cell {state}"
									aria-pressed={selected?.from === zone.id && selected?.to === to}
									aria-label={t('firewall.cellTitle', {
										from: zoneLabel(zone.id),
										to: columnLabel(to),
									})}
									data-cell="{zone.id}>{to}"
									onclick={() => onselect(zone.id, to)}
									><span class="state">{t(`firewall.cell.${state}`)}</span>{#if count}<span
											class="small"
											>{t(
												cell.default === 'allow' ? 'firewall.cellBlocked' : 'firewall.cellAllowed',
												{ count },
											)}</span
										>{/if}</button
								></td
							>
						{:else}
							<td
								><span class="cell none" title={t('firewall.notThroughNas')}
									><span aria-hidden="true">—</span><span class="sr-only"
										>{t('firewall.notThroughNas')}</span
									></span
								></td
							>
						{/if}
					{/each}
				</tr>
			{/each}
		</tbody>
	</table>
</div>

<style>
	.matrix {
		min-width: 34rem;
	}
	.matrix th[scope='row'] {
		color: var(--text);
		font-size: 0.875rem;
		font-weight: 400;
		min-width: 8rem;
	}
	.matrix th.dest {
		font-style: italic;
	}
	.matrix td {
		padding: 4px;
	}
	.cell {
		/* Keeps the screen-reader text inside the scrolling table. */
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		justify-content: center;
		gap: 2px;
		width: 100%;
		min-width: 6.5rem;
		min-height: 2.75rem;
		padding: 4px 8px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		text-align: left;
	}
	button.cell {
		cursor: pointer;
	}
	.cell .state {
		font-weight: 500;
	}
	.cell .small {
		color: var(--muted);
		font-size: 0.75rem;
	}
	.cell.allow {
		border-color: var(--ok-border);
		background: var(--ok-bg);
		color: var(--ok-text);
	}
	.cell.deny {
		border-color: var(--error-border);
		background: var(--error-bg);
		color: var(--error-text);
	}
	.cell.partial {
		border-color: var(--warn-border);
		background: var(--warn-bg);
		color: var(--warn-text);
	}
	.cell.allow .small,
	.cell.deny .small,
	.cell.partial .small {
		color: inherit;
		opacity: 0.85;
	}
	.cell.none {
		align-items: center;
		color: var(--muted);
		border-style: dashed;
	}
	button.cell[aria-pressed='true'] {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
</style>
