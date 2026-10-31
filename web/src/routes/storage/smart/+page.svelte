<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, ApiError } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
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
	<PageHead title={t('smart.title')} parent={{ href: '/storage', label: t('nav.storage') }}>
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
			<aside class="tile detail" aria-labelledby="smart-detail-title">
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
						<section aria-labelledby="smart-findings">
							<h3 id="smart-findings">{t('smart.warningsTitle')}</h3>
							<ul class="findings">
								{#each d.warnings as warning (warning)}<li>{t(`smart.warn.${warning}`)}</li>{/each}
							</ul>
						</section>
					{/if}
					<dl>
						<div>
							<dt>{t('smart.device')}</dt>
							<dd>{d.model || '–'}</dd>
						</div>
						<div>
							<dt>{t('disk.serial')}</dt>
							<dd class="mono">{d.serial || '–'}</dd>
						</div>
						{#if v?.modelFamily}<div>
								<dt>{t('smart.family')}</dt>
								<dd>{v.modelFamily}</dd>
							</div>{/if}
						{#if v?.firmware}<div>
								<dt>{t('smart.firmware')}</dt>
								<dd class="mono">{v.firmware}</dd>
							</div>{/if}
						{#if v}<div>
								<dt>{t('smart.protocol')}</dt>
								<dd>{v.protocol || '–'}</dd>
							</div>
							<div>
								<dt>{t('smart.passed')}</dt>
								<dd class:bad={!v.passed}>
									{v.passed ? t('smart.passedYes') : t('smart.passedNo')}
								</dd>
							</div>
							<div>
								<dt>{t('smart.temperature')}</dt>
								<dd class="mono">
									{typeof v.temperatureC === 'number' ? `${v.temperatureC} °C` : '–'}
								</dd>
							</div>
							<div>
								<dt>{t('smart.hours')}</dt>
								<dd class="mono">{count(v.powerOnHours)}</dd>
							</div>
							<div>
								<dt>{t('smart.powerCycles')}</dt>
								<dd class="mono">{count(v.powerCycles)}</dd>
							</div>
							{#if !v.nvme}
								<div>
									<dt>{t('smart.reallocated')}</dt>
									<dd class="mono">{count(v.reallocatedSectors)}</dd>
								</div>
								<div>
									<dt>{t('smart.pending')}</dt>
									<dd class="mono">{count(v.pendingSectors)}</dd>
								</div>
								<div>
									<dt>{t('smart.uncorrectable')}</dt>
									<dd class="mono">{count(v.uncorrectableSectors)}</dd>
								</div>
								<div>
									<dt>{t('smart.crc')}</dt>
									<dd class="mono">{count(v.crcErrors)}</dd>
								</div>
							{:else}
								<div>
									<dt>{t('smart.wear')}</dt>
									<dd class="mono">{v.nvme.percentageUsed} %</dd>
								</div>
								<div>
									<dt>{t('smart.spare')}</dt>
									<dd class="mono">
										{v.nvme.availableSpare} % / {v.nvme.availableSpareThreshold} %
									</dd>
								</div>
								<div>
									<dt>{t('smart.mediaErrors')}</dt>
									<dd class="mono">{count(v.nvme.mediaErrors)}</dd>
								</div>
								<div>
									<dt>{t('smart.errorLog')}</dt>
									<dd class="mono">{count(v.nvme.errorLogEntries)}</dd>
								</div>
								<div>
									<dt>{t('smart.unsafeShutdowns')}</dt>
									<dd class="mono">{count(v.nvme.unsafeShutdowns)}</dd>
								</div>
							{/if}
						{/if}
						<div>
							<dt>{t('smart.readAt')}</dt>
							<dd>{time(d.readAt)}</dd>
						</div>
						<div>
							<dt>{t('smart.checkedAt')}</dt>
							<dd>{time(d.checkedAt)}</dd>
						</div>
					</dl>
					<p class="links">
						<a class="btn" href="/storage?tab=disks#disk-{encodeURIComponent(d.disk)}"
							>{t('smart.openStorage')}</a
						>
						{#if d.volume}<a class="btn" href="/storage#volume-{encodeURIComponent(d.volume)}"
								>{t('smart.volume')}: {d.volume}</a
							>{/if}
					</p>
					{#if v}
						<section aria-labelledby="smart-attributes">
							<h3 id="smart-attributes">{v.nvme ? t('smart.healthLog') : t('smart.attributes')}</h3>
							{#if !v.attributes?.length}<p class="muted">{t('smart.noAttributes')}</p>
							{:else if v.nvme}
								<div class="attr-table">
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
								<div class="attr-table">
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
						</section>
					{/if}
				{/if}
			</aside>
		{/if}
	</div>
	<details class="tile thresholds">
		<summary>{t('smart.thresholdsTitle')}</summary>
		<p>{t('smart.thresholds')}</p>
	</details>
{:else}<p class="notice-error">{t('error.forbidden')}</p>{/if}

<style>
	.intro {
		margin: 0 0 16px;
		font-size: var(--text-md);
	}
	.state-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin: 16px;
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
		padding: 12px 14px;
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
		margin-top: 4px;
	}
	.num {
		text-align: right;
		white-space: nowrap;
	}
	.badges {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin: 0;
	}
	.bad,
	.sub.bad {
		color: var(--error-text);
	}
	.detail {
		padding: 20px;
		min-width: 0;
		display: grid;
		gap: 14px;
	}
	.detail-heading {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
	}
	.detail-heading h2 {
		font-size: var(--text-lg);
	}
	.detail-heading .btn {
		padding: 6px;
		min-height: 30px;
	}
	.detail h3 {
		margin: 0 0 8px;
		font-size: var(--text-md);
	}
	.detail dl {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
		gap: 12px;
		margin: 0;
	}
	.detail dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.detail dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	.findings {
		margin: 0;
		padding-left: 18px;
		font-size: var(--text-md);
	}
	.links {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin: 0;
	}
	.attr-table td {
		padding: 6px 8px;
		font-size: var(--text-base);
	}
	.warn-row {
		background: var(--warn-bg);
	}
	.thresholds {
		margin-top: 20px;
		padding: 14px 18px;
		font-size: var(--text-md);
	}
	.thresholds summary {
		cursor: pointer;
		font-weight: 500;
	}
	.thresholds p {
		margin: 10px 0 0;
	}
	@media (max-width: 1100px) {
		.smart-layout.with-detail {
			grid-template-columns: 1fr;
		}
	}
</style>
