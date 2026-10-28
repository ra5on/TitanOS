<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import PageHead from '#lib/components/PageHead.svelte';
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

<PageHead
	title={t('coverage.title')}
	parent={{ href: '/settings/backups', label: t('safeguard.title') }}
	>{#snippet actions()}<button class="btn" disabled={busy} onclick={load}>{t('ops.refresh')}</button
		><a class="btn" href="/settings/backups/applications">{t('coverage.restoreApps')}</a
		>{/snippet}</PageHead
>
<p class="muted">{t('coverage.hint')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#each unavailable as source (source)}<p class="notice-warn">
		{t('coverage.unavailable', { source })}
	</p>{/each}
{#each items as item (`${item.kind}:${item.name}`)}
	<section class="tile panel item">
		<div class="head">
			<h2>{item.name}</h2>
			<span>{t(`coverage.status.${item.status}` as MessageKey)}</span>
		</div>
		<p class="muted">
			{t(`coverage.kind.${item.kind}` as MessageKey)} · {t('ops.lastOK')}: {date(item.lastOK)}
		</p>
		{#if !item.available}<p class="notice-warn">{t('coverage.sourceOffline')}</p>{/if}
		{#each item.jobs as job (job.id)}<div class="job">
				<a href={'/settings/backups/data#backup-' + encodeURIComponent(job.id)}>{job.id}</a> · {t(
					job.automatic ? 'coverage.automatic' : 'coverage.manual',
				)}
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
		{#if !item.jobs.length}<div class="row-actions">
				<a class="btn" href="/settings/backups/data">{t('coverage.configure')}</a>
			</div>{/if}
	</section>
{:else}{#if !busy && !error}<div class="tile panel">
			<p class="muted">{t('ops.none')}</p>
		</div>{/if}{/each}

<style>
	.head,
	.dates {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
		align-items: center;
		justify-content: space-between;
	}
	.head h2 {
		font-size: 1rem;
	}
	.head span {
		color: var(--muted);
		font-size: 0.875rem;
	}
	.job {
		padding-top: 14px;
		border-top: 1px solid var(--border);
		font-size: 0.875rem;
	}
	.dates {
		justify-content: start;
		color: var(--muted);
		margin-top: 8px;
		font-size: 0.8rem;
	}
	.job a {
		color: var(--accent);
	}
	p {
		overflow-wrap: anywhere;
	}
</style>
