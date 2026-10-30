<script lang="ts">
	// Settings › Temperatures › Fans (backend package fans): every fan stays
	// with the BIOS until it gets a fixed speed or a curve here.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Point = { celsius: number; percent: number };
	type Control = {
		id: string;
		mode: 'bios' | 'fixed' | 'curve';
		percent?: number;
		sensor?: string;
		points?: Point[];
		min: number;
	};
	type Fan = {
		id: string;
		chip: string;
		label: string;
		rpm: number | null;
		percent: number;
		control: Control;
		sensorC?: number;
		error?: string;
	};

	let fans = $state<Fan[] | null>(null);
	let drafts = $state<Record<string, Control>>({});
	let readOnly = $state(false);
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);

	const defaultPoints: Point[] = [
		{ celsius: 35, percent: 30 },
		{ celsius: 50, percent: 45 },
		{ celsius: 65, percent: 75 },
		{ celsius: 80, percent: 100 },
	];

	function take(answer: { fans: Fan[]; readOnly: boolean }) {
		fans = answer.fans;
		readOnly = answer.readOnly;
		const next: Record<string, Control> = {};
		for (const fan of answer.fans)
			next[fan.id] = drafts[fan.id] && !saved ? drafts[fan.id] : structuredClone(fan.control);
		drafts = next;
	}

	async function load() {
		try {
			take(await api.get<{ fans: Fan[]; readOnly: boolean }>('/system/fans'));
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	onMount(() => poll(load, { interval: 5000, skip: () => busy }));

	function setMode(id: string, mode: Control['mode']) {
		const d = drafts[id];
		d.mode = mode;
		if (mode === 'fixed' && !d.percent) d.percent = 60;
		if (mode === 'curve') {
			d.points ??= structuredClone(defaultPoints);
			d.sensor ??= 'disks';
		}
		saved = false;
	}

	async function save() {
		busy = true;
		error = '';
		saved = false;
		try {
			const body = { fans: Object.values(drafts).filter((d) => d.mode !== 'bios') };
			take(await api.put<{ fans: Fan[]; readOnly: boolean }>('/system/fans', body));
			saved = true;
		} catch (err) {
			error =
				err instanceof ApiError && err.code === 'invalid_fans'
					? `${errorText(err.code)} ${err.message.replace(/^fans: invalid settings: /, '')}`
					: apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	const sensors = $derived((system.info?.temperatures ?? []).filter((s) => s.celsius !== null));
</script>

<section class="tile panel" id="fans" aria-labelledby="fans-title">
	<h2 class="tile-title" id="fans-title">{t('fans.title')}</h2>
	<p class="muted">{t('fans.intro')}</p>
	{#if readOnly}<p class="notice-warn">{t('fans.readOnly')}</p>{/if}
	{#if fans && fans.length === 0}<p class="muted">{t('fans.none')}</p>{/if}
	{#each fans ?? [] as fan (fan.id)}
		{@const d = drafts[fan.id]}
		{#if d}
			<div class="fan">
				<div class="head">
					<strong>{fan.label}</strong>
					<span class="muted mono">{fan.chip}</span>
					<span class="mono"
						>{fan.rpm === null ? '–' : t('fans.rpm', { rpm: String(fan.rpm) })} · {fan.percent} %</span
					>
					{#if fan.sensorC !== undefined}<span class="mono muted">{fan.sensorC.toFixed(0)} °C</span
						>{/if}
				</div>
				{#if fan.error}<p class="notice-warn">{fan.error}</p>{/if}
				<div class="modes" role="radiogroup" aria-label={t('fans.mode')}>
					{#each ['bios', 'fixed', 'curve'] as mode (mode)}
						<label class="check"
							><input
								type="radio"
								name="mode-{fan.id}"
								checked={d.mode === mode}
								disabled={busy || readOnly}
								onchange={() => setMode(fan.id, mode as Control['mode'])}
							/><span>{t(`fans.mode.${mode}` as never)}</span></label
						>
					{/each}
				</div>
				{#if d.mode === 'fixed'}
					<label class="stack"
						><span>{t('fans.fixed', { percent: String(d.percent ?? 60) })}</span><input
							type="range"
							min={d.min}
							max="100"
							step="5"
							bind:value={d.percent}
							disabled={busy || readOnly}
						/></label
					>
				{:else if d.mode === 'curve'}
					<label class="stack"
						><span>{t('fans.sensor')}</span><select
							class="field"
							bind:value={d.sensor}
							disabled={busy || readOnly}
							><option value="disks">{t('fans.disks')}</option>{#each sensors as s (s.id)}<option
									value={s.id}>{s.chip} · {s.label} ({s.celsius?.toFixed(0)} °C)</option
								>{/each}</select
						></label
					>
					<div class="points">
						<span class="muted small">{t('fans.points')}</span>
						{#each d.points ?? [] as point, i (i)}
							<span class="point"
								><input
									class="field mono"
									type="number"
									min="0"
									max="110"
									bind:value={point.celsius}
									aria-label={t('fans.celsius')}
									disabled={busy || readOnly}
								/>
								°C →
								<input
									class="field mono"
									type="number"
									min="0"
									max="100"
									step="5"
									bind:value={point.percent}
									aria-label={t('fans.percent')}
									disabled={busy || readOnly}
								/> %</span
							>
						{/each}
					</div>
				{/if}
				{#if d.mode !== 'bios'}
					<label class="stack narrow"
						><span>{t('fans.min')}</span><input
							class="field mono"
							type="number"
							min="20"
							max="100"
							step="5"
							bind:value={d.min}
							disabled={busy || readOnly}
						/></label
					>
				{/if}
			</div>
		{/if}
	{/each}
	<p class="muted small">{t('fans.safety')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if fans?.length}
		<div class="row-actions">
			<button type="button" class="btn btn-primary" disabled={busy || readOnly} onclick={save}
				>{t('common.save')}</button
			>
			{#if saved}<span class="muted" role="status">{t('ui.saved')}</span>{/if}
		</div>
	{/if}
</section>

<style>
	.fan {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 12px 0;
		border-bottom: 1px solid var(--panel-border);
	}
	.head {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
		align-items: baseline;
	}
	.modes {
		display: flex;
		gap: 16px;
		flex-wrap: wrap;
	}
	.points {
		display: flex;
		flex-wrap: wrap;
		gap: 8px 16px;
		align-items: center;
	}
	.point {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		font-size: 0.8125rem;
	}
	.point input {
		width: 4.5rem;
	}
	.narrow input {
		width: 6rem;
	}
</style>
