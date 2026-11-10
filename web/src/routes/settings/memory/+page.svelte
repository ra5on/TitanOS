<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { api, type SystemInfo } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';
	type Status = {
		settings: { cachePercent: number };
		readOnly: boolean;
		budgetBytes: number;
		reserveBytes: number;
		inFlightBytes: number;
		memory: SystemInfo['memory'];
		paused: boolean;
		error?: string;
	};
	let status = $state<Status | null>(null);
	let percent = $state(20),
		busy = $state(false),
		error = $state(''),
		pollError = $state(''),
		saved = $state(false);
	let revision = 0;
	onMount(() => {
		let initialized = false;
		async function refresh(signal: AbortSignal) {
			const started = revision;
			try {
				const result = await api.get<Status>(
					'/system/memory',
					AbortSignal.any([signal, AbortSignal.timeout(10000)]),
				);
				if (!signal.aborted && started === revision) {
					status = result;
					if (!initialized) {
						percent = result.settings.cachePercent;
						initialized = true;
					}
					pollError = '';
				}
			} catch (err) {
				if (!signal.aborted && started === revision)
					pollError = apiErrorText(err, { detail: true });
			}
		}
		return poll(refresh, { interval: 5000, skip: () => busy });
	});
	// PLAN 78-1: the share is stored when it is chosen (same request as the
	// Save button sent before).
	async function save() {
		busy = true;
		revision++;
		error = '';
		saved = false;
		try {
			status = await api.put<Status>('/system/memory', { cachePercent: percent });
			saved = true;
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('memoryPolicy.title')}>
	<p class="muted">{t('memoryPolicy.intro')}</p>
</PageHead>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if pollError}<p class="notice-error" role="alert">{pollError}</p>{/if}
{#if status}
	{#if status.error}<p class="notice-warn" role="status">{status.error}</p>{/if}
	{#if status.readOnly}<p class="notice-warn">{t('memoryPolicy.readOnly')}</p>{/if}
	{#if status.memory.lowAvailable}<p class="notice-warn" role="status">
			{t('memoryPolicy.lowAvailable')}
		</p>{/if}
	<SettingGroup title={t('memoryPolicy.group.budget')}>
		<SettingRow
			icon="memory"
			title={t('memoryPolicy.percent')}
			desc={t('memoryPolicy.hint')}
			for="cache-budget"
		>
			{#snippet control()}
				<select
					id="cache-budget"
					class="field"
					bind:value={percent}
					disabled={busy || status?.readOnly}
					onchange={save}
				>
					{#each [10, 20, 30, 40, 50, 60, 70, 80, 90] as value (value)}<option {value}
							>{value} %</option
						>{/each}
				</select>
			{/snippet}
		</SettingRow>
		{#if saved}<p class="muted" role="status">{t('memoryPolicy.saved')}</p>{/if}
	</SettingGroup>
	<SettingGroup title={t('memoryPolicy.group.now')}>
		<SettingRow title={t('memoryPolicy.budget')}>
			{#snippet control()}<span class="mono value">{formatBytes(status?.budgetBytes ?? 0)}</span
				>{/snippet}
		</SettingRow>
		<SettingRow title={t('memoryPolicy.reserve')}>
			{#snippet control()}<span class="mono value">{formatBytes(status?.reserveBytes ?? 0)}</span
				>{/snippet}
		</SettingRow>
		<SettingRow title={t('memoryPolicy.available')}>
			{#snippet control()}<span class="mono value"
					>{formatBytes(status?.memory.availableBytes ?? 0)}</span
				>{/snippet}
		</SettingRow>
		<SettingRow title={t('memoryPolicy.dirty')}>
			{#snippet control()}<span class="mono value"
					>{formatBytes(
						(status?.memory.dirtyBytes ?? 0) + (status?.memory.writebackBytes ?? 0),
					)}</span
				>{/snippet}
		</SettingRow>
		<SettingRow title={t('memoryPolicy.pressure')} desc={t('memoryPolicy.pressureHint')}>
			{#snippet control()}<span class="mono value"
					>{status?.memory.pressure
						? `${status.memory.pressure.someAvg10.toFixed(1)} % / ${status.memory.pressure.fullAvg10.toFixed(1)} %`
						: t('common.unavailable')}</span
				>{/snippet}
		</SettingRow>
		{#if status.paused}<p class="notice-warn" role="status">
				{t('error.upload_memory_pressure')}
			</p>{/if}
	</SettingGroup>
{:else if !error}<p>{t('common.loading')}</p>{/if}

<style>
	p {
		margin: 0;
	}
	.value {
		color: var(--text);
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
