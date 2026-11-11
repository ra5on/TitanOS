<script lang="ts">
	// Settings › Temperatures › Fans (backend package fans): every fan stays
	// with the BIOS until it gets a fixed speed or a curve here.
	import { poll } from '#lib/poll.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
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

<!-- PLAN 78-1: every fan is a row that opens (its state at the right);
     one Save for all of them, as the request carries every fan. -->
<SettingGroup title={t('fans.title')} id="fans" desc={t('fans.intro')}>
	{#if readOnly}<p class="notice-warn">{t('fans.readOnly')}</p>{/if}
	{#if fans && fans.length === 0}<p class="muted">{t('fans.none')}</p>{/if}
	{#each fans ?? [] as fan, n (fan.id)}
		{@const d = drafts[fan.id]}
		{#if d}
			<SettingRow
				id="fan-{n}"
				icon="activity"
				title={fan.label}
				summary={[
					fan.rpm === null ? '–' : t('fans.rpm', { rpm: String(fan.rpm) }),
					`${fan.percent} %`,
					...(fan.sensorC !== undefined ? [`${fan.sensorC.toFixed(0)} °C`] : []),
					t(`fans.mode.${d.mode}` as never),
				].join(' · ')}
			>
				{#snippet description()}<span class="mono">{fan.chip}</span>{/snippet}
				{#if fan.error}<div class="srow-block"><p class="notice-warn">{fan.error}</p></div>{/if}
				<SettingSub title={t('fans.mode')} for="fan-{n}-mode">
					{#snippet control()}
						<select
							id="fan-{n}-mode"
							class="field"
							value={d.mode}
							disabled={busy || readOnly}
							onchange={(e) => setMode(fan.id, e.currentTarget.value as Control['mode'])}
						>
							{#each ['bios', 'fixed', 'curve'] as mode (mode)}<option value={mode}
									>{t(`fans.mode.${mode}` as never)}</option
								>{/each}
						</select>
					{/snippet}
				</SettingSub>
				{#if d.mode === 'fixed'}
					<SettingSub
						title={t('fans.fixed', { percent: String(d.percent ?? 60) })}
						for="fan-{n}-percent"
					>
						{#snippet control()}
							<input
								id="fan-{n}-percent"
								type="range"
								min={d.min}
								max="100"
								step="5"
								bind:value={d.percent}
								disabled={busy || readOnly}
							/>
						{/snippet}
					</SettingSub>
				{:else if d.mode === 'curve'}
					<SettingSub title={t('fans.sensor')} for="fan-{n}-sensor">
						{#snippet control()}
							<select
								id="fan-{n}-sensor"
								class="field"
								bind:value={d.sensor}
								disabled={busy || readOnly}
								><option value="disks">{t('fans.disks')}</option>{#each sensors as s (s.id)}<option
										value={s.id}>{s.chip} · {s.label} ({s.celsius?.toFixed(0)} °C)</option
									>{/each}</select
							>
						{/snippet}
					</SettingSub>
					<div class="srow-block">
						<div class="points" role="group" aria-label={t('fans.points')}>
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
					</div>
				{/if}
				{#if d.mode !== 'bios'}
					<SettingSub title={t('fans.min')} for="fan-{n}-min">
						{#snippet control()}
							<input
								id="fan-{n}-min"
								class="field mono"
								type="number"
								min="20"
								max="100"
								step="5"
								bind:value={d.min}
								disabled={busy || readOnly}
							/>
						{/snippet}
					</SettingSub>
				{/if}
			</SettingRow>
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
</SettingGroup>

<style>
	p {
		margin: 0;
	}
	.small {
		margin-top: var(--space-1-5);
	}
	.row-actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5);
		margin-top: var(--space-2);
	}
	.points {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-4);
		align-items: center;
	}
	.point {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		font-size: var(--text-base);
	}
	.point input {
		width: 4.5rem;
	}
	input[type='range'] {
		width: 14rem;
		max-width: 100%;
	}
</style>
