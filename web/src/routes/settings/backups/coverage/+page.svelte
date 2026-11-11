<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { operationError, type BackupView } from '#lib/operations.ts';
	type Coverage = {
		kind: string;
		name: string;
		available: boolean;
		status: string;
		lastOK: string | null;
		jobs: {
			id: string;
			automatic: boolean;
			stateKnown?: boolean;
			partial: boolean;
			method: string;
			destination: string;
			destinationAvailable: boolean | null;
			state: BackupView['state'];
		}[];
	};
	let items = $state<Coverage[]>([]),
		unavailable = $state<string[]>([]),
		error = $state(''),
		busy = $state(false);
	const date = (value: string | null | undefined) =>
		value ? formatDateTime(new Date(value)) : '—';
	async function load() {
		busy = true;
		error = '';
		try {
			const result = await api.get<{ items: Coverage[]; unavailable: string[] }>(
				'/backups/coverage',
			);
			items = result.items;
			unavailable = result.unavailable;
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	onMount(load);
</script>

<PageHead title={t('coverage.title')}
	>{#snippet actions()}<button class="btn" disabled={busy} onclick={load}>{t('ops.refresh')}</button
		><a class="btn" href="/settings/backups/applications">{t('coverage.restoreApps')}</a
		>{/snippet}</PageHead
>
<p class="muted">{t('coverage.hint')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#each unavailable as source (source)}<p class="notice-warn">
		{t('coverage.unavailable', { source })}
	</p>{/each}
<!-- PLAN 78-1: every share and application is a row that says its state; its
     backup jobs are its parts (open at first: this page is a report). -->
<SettingGroup>
	{#each items as item (`${item.kind}:${item.name}`)}
		{#snippet parts()}
			{#if !item.available}<div class="srow-block">
					<p class="notice-warn">{t('coverage.sourceOffline')}</p>
				</div>{/if}
			{#each item.jobs as job (job.id)}<div class="srow-block job">
					<p>
						<a href={'/settings/backups/data#backup-' + encodeURIComponent(job.id)}>{job.id}</a> · {t(
							job.automatic ? 'coverage.automatic' : 'coverage.manual',
						)}
					</p>
					{#if job.stateKnown === false}<p class="notice-warn">
							{t('coverage.stateUnknown')}
						</p>{/if}{#if job.partial}<p class="notice-warn">{t('coverage.partial')}</p>{/if}
					{#if job.destinationAvailable === false}<p class="notice-warn">
							{t('coverage.destinationOffline')}
						</p>{/if}
					{#if job.destinationAvailable === null}<p class="muted">
							{t('coverage.destinationUnknown')}
						</p>{/if}
					<div class="dates">
						<span>{t('ops.lastOK')}: {date(job.state.lastOK)}</span><span
							>{t('coverage.lastCheck')}: {date(job.state.lastCheck)}</span
						><span>{t('coverage.lastProbe')}: {date(job.state.probe?.at)}</span>
					</div>
					{#if job.state.lastError || job.state.warning || job.state.checkError || job.state.probe?.error}<p
							class="notice-warn"
						>
							{job.state.lastError ||
								job.state.warning ||
								job.state.checkError ||
								job.state.probe?.error}
						</p>{/if}
				</div>{/each}
			{#if !item.jobs.length}<div class="srow-block actions">
					<a class="btn" href="/settings/backups/data">{t('coverage.configure')}</a>
				</div>{/if}
		{/snippet}
		<SettingRow
			id="coverage-{item.kind}-{encodeURIComponent(item.name)}"
			icon={item.kind === 'share' ? 'folder' : item.kind === 'vm' ? 'monitor' : 'box'}
			title={item.name}
			desc="{t(`coverage.kind.${item.kind}` as MessageKey)} · {t('ops.lastOK')}: {date(
				item.lastOK,
			)}"
			summary={t(`coverage.status.${item.status}` as MessageKey)}
			open
			children={parts}
		/>
	{:else}{#if !busy && !error}<p class="muted">{t('ops.none')}</p>{/if}{/each}
</SettingGroup>

<style>
	.dates {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-3);
		align-items: center;
		color: var(--muted);
		font-size: 0.8rem;
	}
	.job {
		gap: var(--space-2);
		font-size: 0.875rem;
	}
	.job a {
		color: var(--accent);
	}
	p {
		overflow-wrap: anywhere;
	}
</style>
