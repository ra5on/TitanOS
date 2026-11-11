<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import SmartHistory from './SmartHistory.svelte';
	import { formatDateTime, formatNumber } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		isSmartList,
		sectorProblems,
		smartCounts,
		smartTone,
		sortReports,
		validDiskName,
		type SmartReport,
		type SmartState,
	} from '#lib/smart.ts';

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let reports = $state<SmartReport[] | null>(null),
		loading = $state(false),
		error = $state(''),
		detail = $state<SmartReport | null>(null),
		detailError = $state(''),
		detailLoading = $state(false);
	const selected = $derived(page.url.searchParams.get('disk') ?? '');
	const sorted = $derived(sortReports(reports ?? []));
	const counts = $derived(smartCounts(reports ?? []));

	async function load(refresh = false) {
		loading = true;
		error = '';
		try {
			const answer = refresh
				? await api.post<unknown>('/storage/smart/refresh')
				: await api.get<unknown>('/storage/smart');
			if (!isSmartList(answer)) throw new ApiError(0, 'internal', '');
			reports = answer.disks;
		} catch (err) {
			error =
				err instanceof ApiError && err.code !== 'internal'
					? errorText(err.code)
					: t('smart.loadFailed');
		}
		loading = false;
		if (selected) void loadDetail(selected);
	}

	// The details carry the attribute table; read when a disk is opened.
	async function loadDetail(disk: string) {
		if (!validDiskName(disk)) {
			detail = null;
			detailError = errorText('disk_not_found');
			return;
		}
		detailLoading = true;
		detailError = '';
		try {
			const answer = await api.get<SmartReport>(`/storage/smart/${encodeURIComponent(disk)}`);
			if (selected !== disk) return;
			detail = answer;
		} catch (err) {
			if (selected !== disk) return;
			detail = null;
			detailError = apiErrorText(err);
		}
		detailLoading = false;
	}
	$effect(() => {
		const disk = selected;
		untrack(() => {
			if (!admin || !reports) return;
			if (disk) void loadDetail(disk);
			else detail = null;
		});
	});
	onMount(() => {
		if (admin) void load();
	});

	function choose(disk: string) {
		const url = new URL(page.url.href);
		if (disk && disk !== selected) url.searchParams.set('disk', disk);
		else url.searchParams.delete('disk');
		void goto(`${url.pathname}${url.search}`, { replace: true, reset: false });
	}

	const stateText = (state: SmartState) => t(`smart.state.${state}`);
	const time = (value?: string) => (value ? formatDateTime(new Date(value)) : '–');
	const count = (value: number | null | undefined) =>
		typeof value === 'number' ? formatNumber(value) : '–';
</script>

{#if admin}
	<PageHead title={t('smart.title')}>
		{#if reports}<p class="muted">
				{t('smart.summary', {
					ok: counts.ok,
					warning: counts.warning,
					failed: counts.failed,
					sleeping: counts.sleeping,
				})}
			</p>{/if}
		{#snippet actions()}<button class="btn" disabled={loading} onclick={() => load(true)}
				>{t('smart.refresh')}</button
			>{/snippet}
	</PageHead>
	<p class="muted intro">{t('smart.intro')}</p>
	{#if error}<div class="notice-error state-line" role="alert">
			<span>{error}</span><button class="btn" disabled={loading} onclick={() => load()}
				>{t('common.retry')}</button
			>
		</div>{/if}
	<div class="smart-layout" class:with-detail={!!selected}>
		<div class="tile table">
			{#if !reports}
				{#if !error}<p class="muted state-line" role="status">{t('common.loading')}</p>{/if}
			{:else}
				<table>
					<thead>
						<tr>
							<th>{t('smart.device')}</th>
							<th>{t('smart.status')}</th>
							<th class="num">{t('smart.temperature')}</th>
							<th class="num">{t('smart.hours')}</th>
							<th class="num">{t('smart.sectors')}</th>
							<th class="num">{t('smart.crc')}</th>
							<th class="num">{t('smart.wear')}</th>
						</tr>
					</thead>
					<tbody>
						{#each sorted as report (report.disk)}{@const v = report.values}
							<tr class:selected={selected === report.disk}>
								<td
									><button
										class="row-name mono"
										aria-pressed={selected === report.disk}
										onclick={() => choose(report.disk)}>{report.disk}</button
									><span class="sub">{report.model || t(`disk.${report.kind}`)}</span></td
								>
								<td>
									<span class="badges">
										<span class="chip {smartTone(report.state)}">{stateText(report.state)}</span>
										{#if report.sleeping}<span class="chip">{t('smart.sleeping')}</span>{/if}
									</span>
								</td>
								<td class="num mono"
									>{typeof v?.temperatureC === 'number' ? `${v.temperatureC} °C` : '–'}</td
								>
								<td class="num mono">{count(v?.powerOnHours)}</td>
								<td class="num mono" class:bad={(sectorProblems(v) ?? 0) > 0}
									>{count(sectorProblems(v))}</td
								>
								<td class="num mono" class:bad={(v?.crcErrors ?? 0) > 0}>{count(v?.crcErrors)}</td>
								<td class="num mono">{v?.nvme ? `${v.nvme.percentageUsed} %` : '–'}</td>
							</tr>
						{:else}<tr><td colspan="7"><EmptyState inline title={t('smart.noDisks')} /></td></tr
							>{/each}
					</tbody>
				</table>
			{/if}
		</div>
		{#if selected}
			<!-- PLAN 78-5: the detail is no tile: plain headings, facts as flat rows. -->
			<aside class="detail" aria-labelledby="smart-detail-title">
				<div class="detail-heading">
					<h2 class="mono" id="smart-detail-title">{selected}</h2>
					<button class="btn" aria-label={t('common.close')} onclick={() => choose('')}
						><Icon name="close" size={16} /></button
					>
				</div>
				{#if detailError}<p class="notice-error" role="alert">{detailError}</p>
				{:else if !detail || detail.disk !== selected}<p class="muted" role="status">
						{detailLoading ? t('common.loading') : ''}
					</p>
				{:else}{@const d = detail}{@const v = d.values}
					<p class="badges">
						<span class="chip {smartTone(d.state)}">{stateText(d.state)}</span>
						{#if d.sleeping}<span class="chip">{t('smart.sleeping')}</span>{/if}
					</p>
					{#if d.sleeping}<p class="notice-warn">
							{d.readAt
								? t('smart.sleepingHint', { time: time(d.readAt) })
								: t('smart.sleepingNever')}
						</p>
					{:else if d.state === 'unavailable'}<p class="muted">{t('smart.unavailableHint')}</p>
					{:else if d.state === 'unknown'}<p class="muted">{t('smart.unknownHint')}</p>{/if}
					{#if d.warnings.length}
						<SettingGroup title={t('smart.warningsTitle')} id="smart-findings">
							<ul class="findings">
								{#each d.warnings as warning (warning)}<li>{t(`smart.warn.${warning}`)}</li>{/each}
							</ul>
						</SettingGroup>
					{/if}
					<div class="sfacts">
						<SettingGroup title={t('smart.facts')} id="smart-facts">
							<SettingRow title={t('smart.device')}
								>{#snippet control()}<span>{d.model || '–'}</span>{/snippet}</SettingRow
							>
							<SettingRow title={t('disk.serial')}
								>{#snippet control()}<span class="mono">{d.serial || '–'}</span
									>{/snippet}</SettingRow
							>
							{#if v?.modelFamily}<SettingRow title={t('smart.family')}
									>{#snippet control()}<span>{v.modelFamily}</span>{/snippet}</SettingRow
								>{/if}
							{#if v?.firmware}<SettingRow title={t('smart.firmware')}
									>{#snippet control()}<span class="mono">{v.firmware}</span>{/snippet}</SettingRow
								>{/if}
							{#if v}<SettingRow title={t('smart.protocol')}
									>{#snippet control()}<span>{v.protocol || '–'}</span>{/snippet}</SettingRow
								>
								<SettingRow title={t('smart.passed')}
									>{#snippet control()}<span class:bad={!v.passed}
											>{v.passed ? t('smart.passedYes') : t('smart.passedNo')}</span
										>{/snippet}</SettingRow
								>
								<SettingRow title={t('smart.temperature')}
									>{#snippet control()}<span class="mono"
											>{typeof v.temperatureC === 'number' ? `${v.temperatureC} °C` : '–'}</span
										>{/snippet}</SettingRow
								>
								<SettingRow title={t('smart.hours')}
									>{#snippet control()}<span class="mono">{count(v.powerOnHours)}</span
										>{/snippet}</SettingRow
								>
								<SettingRow title={t('smart.powerCycles')}
									>{#snippet control()}<span class="mono">{count(v.powerCycles)}</span
										>{/snippet}</SettingRow
								>
								{#if !v.nvme}
									<SettingRow title={t('smart.reallocated')}
										>{#snippet control()}<span class="mono">{count(v.reallocatedSectors)}</span
											>{/snippet}</SettingRow
									>
									<SettingRow title={t('smart.pending')}
										>{#snippet control()}<span class="mono">{count(v.pendingSectors)}</span
											>{/snippet}</SettingRow
									>
									<SettingRow title={t('smart.uncorrectable')}
										>{#snippet control()}<span class="mono">{count(v.uncorrectableSectors)}</span
											>{/snippet}</SettingRow
									>
									<SettingRow title={t('smart.crc')}
										>{#snippet control()}<span class="mono">{count(v.crcErrors)}</span
											>{/snippet}</SettingRow
									>
								{:else}{@const nvme = v.nvme}
									<SettingRow title={t('smart.wear')}
										>{#snippet control()}<span class="mono">{nvme.percentageUsed} %</span
											>{/snippet}</SettingRow
									>
									<SettingRow title={t('smart.spare')}
										>{#snippet control()}<span class="mono"
												>{nvme.availableSpare} % / {nvme.availableSpareThreshold} %</span
											>{/snippet}</SettingRow
									>
									<SettingRow title={t('smart.mediaErrors')}
										>{#snippet control()}<span class="mono">{count(nvme.mediaErrors)}</span
											>{/snippet}</SettingRow
									>
									<SettingRow title={t('smart.errorLog')}
										>{#snippet control()}<span class="mono">{count(nvme.errorLogEntries)}</span
											>{/snippet}</SettingRow
									>
									<SettingRow title={t('smart.unsafeShutdowns')}
										>{#snippet control()}<span class="mono">{count(nvme.unsafeShutdowns)}</span
											>{/snippet}</SettingRow
									>
								{/if}
							{/if}
							<SettingRow title={t('smart.readAt')}
								>{#snippet control()}<span>{time(d.readAt)}</span>{/snippet}</SettingRow
							>
							<SettingRow title={t('smart.checkedAt')}
								>{#snippet control()}<span>{time(d.checkedAt)}</span>{/snippet}</SettingRow
							>
						</SettingGroup>
					</div>
					<p class="links">
						<a class="btn" href="/storage?tab=disks#disk-{encodeURIComponent(d.disk)}"
							>{t('smart.openStorage')}</a
						>
						{#if d.volume}<a class="btn" href="/storage#volume-{encodeURIComponent(d.volume)}"
								>{t('smart.volume')}: {d.volume}</a
							>{/if}
					</p>
					<SmartHistory disk={d.disk} />
					{#if v}
						<SettingGroup
							title={v.nvme ? t('smart.healthLog') : t('smart.attributes')}
							id="smart-attributes"
						>
							{#if !v.attributes?.length}<p class="muted">{t('smart.noAttributes')}</p>
							{:else if v.nvme}
								<div class="tile attr-table">
									<table>
										<thead
											><tr
												><th>{t('smart.attrName')}</th><th class="num">{t('smart.attrRaw')}</th></tr
											></thead
										>
										<tbody>
											{#each v.attributes as a (a.name)}<tr
													><td class="mono">{a.name}</td><td class="num mono">{a.raw}</td></tr
												>{/each}
										</tbody>
									</table>
								</div>
							{:else}
								<div class="tile attr-table">
									<table>
										<thead>
											<tr>
												<th class="num">{t('smart.attrId')}</th>
												<th>{t('smart.attrName')}</th>
												<th class="num">{t('smart.attrValue')}</th>
												<th class="num">{t('smart.attrWorst')}</th>
												<th class="num">{t('smart.attrThreshold')}</th>
												<th class="num">{t('smart.attrRaw')}</th>
												<th>{t('smart.attrType')}</th>
											</tr>
										</thead>
										<tbody>
											{#each v.attributes as a (a.id)}
												<tr class:warn-row={a.warn}>
													<td class="num mono">{a.id}</td>
													<td class="mono"
														>{a.name}{#if a.whenFailed}<span class="sub bad"
																>{t('smart.attrFailed', { when: a.whenFailed })}</span
															>{/if}</td
													>
													<td class="num mono">{a.value}</td>
													<td class="num mono">{a.worst}</td>
													<td class="num mono">{a.threshold}</td>
													<td class="num mono" class:bad={a.warn}>{a.raw}</td>
													<td>{a.prefailure ? t('smart.attrPrefail') : t('smart.attrOldAge')}</td>
												</tr>
											{/each}
										</tbody>
									</table>
								</div>
							{/if}
						</SettingGroup>
					{/if}
				{/if}
			</aside>
		{/if}
	</div>
	<div class="thresholds">
		<SettingGroup>
			<SettingRow icon="info" id="thresholds" title={t('smart.thresholdsTitle')}>
				<div class="srow-block"><p>{t('smart.thresholds')}</p></div>
			</SettingRow>
		</SettingGroup>
	</div>
	{#if reports}<StatusBar label={t('smart.statusBar')}>
			<span>{t('smart.statDisks', { count: reports.length })}</span>
			<span>{t('smart.statOk')} <b>{counts.ok}</b></span>
			<span class:warn={counts.warning > 0}>{t('smart.statWarning')} <b>{counts.warning}</b></span>
			<span class:warn={counts.failed > 0}>{t('smart.statFailed')} <b>{counts.failed}</b></span>
			{#if counts.sleeping}<span>{t('smart.statSleeping')} <b>{counts.sleeping}</b></span>{/if}
		</StatusBar>{/if}
{:else}<p class="notice-error">{t('error.forbidden')}</p>{/if}

<style>
	.intro {
		margin: 0 0 var(--space-4);
		font-size: var(--text-md);
	}
	.state-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin: var(--space-4);
	}
	.smart-layout {
		display: grid;
		gap: 20px;
		align-items: start;
	}
	.smart-layout.with-detail {
		grid-template-columns: minmax(0, 1.3fr) minmax(320px, 1fr);
	}
	.table,
	.attr-table {
		min-width: 0;
		overflow-x: auto;
	}
	table {
		width: 100%;
	}
	td {
		padding: var(--space-3) 14px;
	}
	tr.selected {
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.row-name {
		border: 0;
		background: transparent;
		padding: 0;
		cursor: pointer;
		color: var(--text);
		font-weight: 500;
		text-align: left;
	}
	.row-name:hover {
		color: var(--accent);
	}
	.sub {
		display: block;
		font-size: var(--text-sm);
		color: var(--muted);
		margin-top: var(--space-1);
	}
	.num {
		text-align: right;
		white-space: nowrap;
	}
	.badges {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		margin: 0;
	}
	.bad,
	.sub.bad {
		color: var(--error-text);
	}
	.detail {
		min-width: 0;
		display: grid;
		gap: 14px;
	}
	.detail-heading {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2-5);
	}
	.detail-heading h2 {
		font-size: var(--text-lg);
	}
	.detail-heading .btn {
		padding: var(--space-1-5);
		min-height: 30px;
	}
	.findings {
		margin: 0;
		padding-left: 18px;
		font-size: var(--text-md);
	}
	.links {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin: 0;
	}
	.attr-table td {
		padding: var(--space-1-5) var(--space-2);
		font-size: var(--text-base);
	}
	.warn-row {
		background: var(--warn-bg);
	}
	.thresholds {
		margin-top: 20px;
	}
	@media (max-width: 1100px) {
		.smart-layout.with-detail {
			grid-template-columns: 1fr;
		}
	}
</style>
