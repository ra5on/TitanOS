<script lang="ts">
	// The daily SMART values of one disk (backend/internal/smarthistory),
	// read when the disk is opened on the SMART page.
	import { api } from '#lib/api.ts';
	import { formatDate, formatNumber } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		historyRows,
		isSmartHistory,
		rose,
		temperaturePoints,
		type HistoryField,
		type SmartHistory,
	} from '#lib/smart-history.ts';

	let { disk }: { disk: string } = $props();

	let history = $state<SmartHistory | null>(null),
		error = $state(''),
		showDays = $state(false);

	$effect(() => {
		const name = disk;
		history = null;
		error = '';
		showDays = false;
		api.get<unknown>(`/storage/smart/${encodeURIComponent(name)}/history`).then(
			(answer) => {
				if (name !== disk) return;
				if (!isSmartHistory(answer)) error = t('smartHistory.failed');
				else history = answer;
			},
			(err) => {
				if (name === disk) error = apiErrorText(err);
			},
		);
	});

	const rows = $derived(history ? historyRows(history.samples) : []);
	const W = 320,
		H = 80;
	const line = $derived(history ? temperaturePoints(history.samples, W, H) : null);
	const path = $derived(
		line?.points.map((p, i) => `${i ? 'L' : 'M'}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ') ??
			'',
	);
	const recent = $derived(history ? history.samples.slice(-14).reverse() : []);
	const day = (value: string) => formatDate(new Date(`${value}T12:00:00`));
	const unit = (field: HistoryField, value: number | undefined) =>
		typeof value !== 'number'
			? '–'
			: field === 'temperatureC'
				? `${value} °C`
				: field === 'percentageUsed'
					? `${value} %`
					: formatNumber(value);
</script>

<section class="history" aria-labelledby="smart-history-{disk}">
	<h3 id="smart-history-{disk}">{t('smartHistory.title')}</h3>
	{#if error}<p class="notice-error" role="alert">{error}</p>
	{:else if !history}<p class="muted" role="status">{t('common.loading')}</p>
	{:else if !history.volume}<p class="muted">{t('smartHistory.noVolume')}</p>
	{:else if history.samples.length === 0}<p class="muted">
			{t('smartHistory.empty', { volume: history.volume })}
		</p>
	{:else}
		<p class="muted small">
			{t('smartHistory.intro', {
				count: history.samples.length,
				since: day(history.samples[0].day),
				volume: history.volume,
			})}
		</p>
		{#if line && line.points.length > 1}
			<figure>
				<figcaption>{t('smartHistory.temperature')}</figcaption>
				<div class="chart">
					<span class="axis top mono">{line.max} °C</span>
					<svg
						viewBox="-4 -4 {W + 8} {H + 8}"
						preserveAspectRatio="none"
						role="img"
						aria-label={t('smartHistory.temperatureLabel', {
							min: Math.min(...line.points.map((p) => p.value)),
							max: Math.max(...line.points.map((p) => p.value)),
						})}
					>
						<line x1="0" x2={W} y1={H} y2={H} class="base" />
						<path d={path} vector-effect="non-scaling-stroke" />
					</svg>
					<span class="axis bottom mono">{line.min} °C</span>
				</div>
				<div class="days muted mono">
					<span>{day(line.points[0].day)}</span><span>{day(line.points.at(-1)!.day)}</span>
				</div>
			</figure>
		{/if}
		<div class="table">
			<table>
				<thead>
					<tr>
						<th>{t('smartHistory.value')}</th>
						<th class="num">{t('smartHistory.first')}</th>
						<th class="num">{t('smartHistory.last')}</th>
						<th>{t('smartHistory.change')}</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as row (row.field)}
						<tr class:bad={rose(row)}>
							<td>{t(`smartHistory.field.${row.field}`)}</td>
							<td class="num mono">{unit(row.field, row.first)}</td>
							<td class="num mono">{unit(row.field, row.last)}</td>
							<td>
								{#if row.field === 'temperatureC'}{t('smartHistory.range', {
										min: unit(row.field, row.min),
										max: unit(row.field, row.max),
									})}
								{:else if rose(row)}<strong
										>{t('smartHistory.rose', { day: day(row.roseOn ?? row.lastDay) })}</strong
									>
								{:else}{t('smartHistory.same')}{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		<button
			type="button"
			class="btn small"
			aria-expanded={showDays}
			onclick={() => (showDays = !showDays)}
			>{t(showDays ? 'smartHistory.hideDays' : 'smartHistory.showDays')}</button
		>
		{#if showDays}
			<div class="table">
				<table>
					<thead>
						<tr>
							<th>{t('smartHistory.day')}</th>
							{#each rows as row (row.field)}<th class="num"
									>{t(`smartHistory.short.${row.field}`)}</th
								>{/each}
						</tr>
					</thead>
					<tbody>
						{#each recent as s (s.day)}
							<tr>
								<td class="mono">{day(s.day)}</td>
								{#each rows as row (row.field)}<td class="num mono"
										>{unit(row.field, s[row.field])}</td
									>{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{/if}
</section>

<style>
	.history {
		display: flex;
		flex-direction: column;
		gap: 10px;
		min-width: 0;
	}
	h3,
	p,
	figure {
		margin: 0;
	}
	.small {
		font-size: 0.8125rem;
	}
	figure {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	figcaption {
		font-size: 0.8125rem;
		font-weight: 500;
	}
	.chart {
		position: relative;
		padding-left: 52px;
	}
	svg {
		display: block;
		width: 100%;
		height: 80px;
		overflow: visible;
	}
	svg path {
		fill: none;
		stroke: var(--accent);
		stroke-width: 2;
	}
	svg .base {
		stroke: var(--panel-border);
		stroke-width: 1;
		vector-effect: non-scaling-stroke;
	}
	.axis {
		position: absolute;
		left: 0;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.axis.top {
		top: 0;
	}
	.axis.bottom {
		bottom: 0;
	}
	.days {
		display: flex;
		justify-content: space-between;
		padding-left: 52px;
		font-size: 0.75rem;
	}
	.table {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.8125rem;
	}
	th,
	td {
		padding: 6px 8px;
		border-bottom: 1px solid var(--panel-border);
		text-align: left;
		white-space: nowrap;
	}
	th {
		color: var(--muted);
		font-weight: 500;
	}
	.num {
		text-align: right;
	}
	tr.bad td {
		color: var(--error-text);
	}
	.btn {
		align-self: flex-start;
	}
</style>
