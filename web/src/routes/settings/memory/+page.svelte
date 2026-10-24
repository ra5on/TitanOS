<script lang="ts">
	import { api, ApiError, type SystemInfo } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t, errorText } from '#lib/i18n/index.svelte.ts';
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
	function failure(err: unknown) {
		return (
			errorText(err instanceof ApiError ? err.code : 'internal') +
			(err instanceof ApiError ? `\n${err.message}` : '')
		);
	}
	onMount(() => {
		let stopped = false,
			reading = false,
			initialized = false;
		async function refresh() {
			if (stopped || reading || document.hidden || busy) return;
			reading = true;
			const started = revision;
			try {
				const result = await api.get<Status>('/system/memory', AbortSignal.timeout(10000));
				if (!stopped && started === revision) {
					status = result;
					if (!initialized) {
						percent = result.settings.cachePercent;
						initialized = true;
					}
					pollError = '';
				}
			} catch (err) {
				if (!stopped && started === revision) pollError = failure(err);
			} finally {
				reading = false;
			}
		}
		void refresh();
		const timer = setInterval(refresh, 5000);
		return () => {
			stopped = true;
			clearInterval(timer);
		};
	});
	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		revision++;
		error = '';
		saved = false;
		try {
			status = await api.put<Status>('/system/memory', { cachePercent: percent });
			saved = true;
		} catch (err) {
			error = failure(err);
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('memoryPolicy.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
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
	<form class="tile" onsubmit={save}>
		<label for="cache-budget">{t('memoryPolicy.percent')}</label>
		<select id="cache-budget" class="field" bind:value={percent} disabled={busy || status.readOnly}>
			{#each [10, 20, 30, 40, 50, 60, 70, 80, 90] as value (value)}<option {value}>{value} %</option
				>{/each}
		</select>
		<p class="muted">{t('memoryPolicy.hint')}</p>
		<dl>
			<div>
				<dt>{t('memoryPolicy.budget')}</dt>
				<dd>{formatBytes(status.budgetBytes)}</dd>
			</div>
			<div>
				<dt>{t('memoryPolicy.reserve')}</dt>
				<dd>{formatBytes(status.reserveBytes)}</dd>
			</div>
			<div>
				<dt>{t('memoryPolicy.available')}</dt>
				<dd>{formatBytes(status.memory.availableBytes)}</dd>
			</div>
			<div>
				<dt>{t('memoryPolicy.cache')}</dt>
				<dd>{formatBytes(status.memory.cacheBytes ?? 0)}</dd>
			</div>
			<div>
				<dt>{t('memoryPolicy.dirty')}</dt>
				<dd>
					{formatBytes((status.memory.dirtyBytes ?? 0) + (status.memory.writebackBytes ?? 0))}
				</dd>
			</div>
			<div>
				<dt>{t('memoryPolicy.pressure')}</dt>
				<dd>
					{status.memory.pressure
						? `${status.memory.pressure.someAvg10.toFixed(1)} % / ${status.memory.pressure.fullAvg10.toFixed(1)} %`
						: t('common.unavailable')}
				</dd>
			</div>
		</dl>
		<p class="muted">{t('memoryPolicy.pressureHint')}</p>
		<p class="muted">{t('memoryPolicy.imageHint')}</p>
		{#if status.paused}<p class="notice-warn" role="status">
				{t('error.upload_memory_pressure')}
			</p>{/if}
		<button class="btn btn-primary" disabled={busy || status.readOnly}>{t('common.save')}</button>
		{#if saved}<p role="status">{t('memoryPolicy.saved')}</p>{/if}
	</form>
{:else if !error}<p>{t('common.loading')}</p>{/if}

<style>
	.tile {
		max-width: 680px;
		padding: var(--tile-pad);
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	label {
		font-weight: 600;
	}
	p,
	dl {
		margin: 0;
	}
	dl {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	dl div {
		display: flex;
		justify-content: space-between;
		gap: 16px;
		flex-wrap: wrap;
	}
	dd {
		margin: 0;
		font-family: var(--font-mono);
	}
	dt {
		color: var(--muted);
	}
	button {
		align-self: flex-start;
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
