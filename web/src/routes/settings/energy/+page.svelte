<script lang="ts">
	import { api, type DiskSleepStatus } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import PowerSchedule from '#lib/components/PowerSchedule.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { apiErrorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	// Disk sleep stays set in its one dialog (Plan 28b): this page only links
	// to it, so there are never two places with their own values.
	import DiskSleepDialog from '../../storage/DiskSleepDialog.svelte';
	import { sleepDuration, sleepListed } from '../../storage/sleep-view.ts';
	type Settings = { governor: string; preference: string; platform: string };
	type Control = { key: keyof Settings; choices: string[]; current: string; mixed: boolean };
	type Status = {
		settings: Settings;
		controls: Control[];
		drivers: string[];
		readOnly: boolean;
		error?: string;
	};
	let status = $state<Status | null>(null);
	let draft = $state<Settings>({ governor: '', preference: '', platform: '' });
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	const label = (value: string) => tOr(`energy.profile.${value}`, value);
	function take(result: Status) {
		status = result;
		draft = { ...result.settings };
	}
	function fail(err: unknown) {
		error = apiErrorText(err, { detail: true });
	}
	$effect(() => {
		api.get<Status>('/system/energy').then(take, fail);
	});
	let sleep = $state<DiskSleepStatus | null>(null);
	let sleepError = $state('');
	let sleepOpen = $state(false);
	$effect(() => {
		api.get<DiskSleepStatus>('/storage/disk-sleep').then(
			(result) => (sleep = result),
			(err) => (sleepError = apiErrorText(err)),
		);
	});
	// The rows that lead to their own pages say the state there in passing;
	// if it cannot be read, the row simply says nothing.
	let button = $state<'ignore' | 'reboot' | 'poweroff' | ''>('');
	let upsSummary = $state('');
	$effect(() => {
		api
			.get<{ settings: { action: 'ignore' | 'reboot' | 'poweroff' } }>('/system/power-button')
			.then(
				(result) => (button = result.settings?.action ?? ''),
				() => {},
			);
		api
			.get<{
				status: { mode: string; connected: boolean; onBattery: boolean; model: string };
			}>('/ups')
			.then(
				({ status: ups }) =>
					(upsSummary = !ups
						? ''
						: ups.mode === 'off'
							? t('ups.mode.off')
							: [
									ups.model,
									t(
										!ups.connected
											? 'ups.notConnected'
											: ups.onBattery
												? 'ups.onBattery'
												: 'ups.online',
									),
								]
									.filter(Boolean)
									.join(' · ')),
				() => {},
			);
	});
	// A changed profile is stored right away.
	async function store() {
		busy = true;
		error = '';
		saved = false;
		try {
			take(await api.put<Status>('/system/energy', draft));
			saved = true;
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}
	const sleepSummary = $derived.by(() => {
		if (!sleep) return '';
		const current = t('energy.diskSleepCurrent', { value: sleepDuration(sleep.idleMinutes) });
		const disks = sleepListed(sleep) ? (sleep.diskStatus ?? []) : [];
		if (!disks.length) return current;
		const asleep = disks.filter((disk) => disk.state === 'asleep').length;
		return `${current} · ${t('storage.sleepCount', { asleep, total: disks.length })}`;
	});
</script>

<PageHead title={t('energy.title')}>
	<p class="muted">{t('energy.intro')}</p>
</PageHead>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if status?.error}<p class="notice-warn" role="status">
		{t('energy.startupError')}
		{status.error}
	</p>{/if}
{#if status?.readOnly}<p class="notice-warn">{t('energy.readOnly')}</p>{/if}

<!-- PLAN 78-1, draft 78: groups are plain headings, every setting a row. -->
<SettingGroup title={t('energy.group.power')}>
	<PowerSchedule />
	<SettingRow
		href="/settings/power-button"
		icon="power"
		title={t('energy.powerButton')}
		desc={t('energy.powerButtonDesc')}
		summary={button ? t(`powerButton.${button}`) : undefined}
	/>
</SettingGroup>

<SettingGroup title={t('energy.group.saving')}>
	<SettingRow
		id="disk-sleep"
		icon="disk"
		title={t('energy.diskSleep')}
		desc={sleepError || t('energy.diskSleepHint')}
		summary={sleepSummary}
	>
		{#snippet control()}
			<button type="button" class="btn" disabled={!sleep} onclick={() => (sleepOpen = true)}
				>{t('energy.diskSleepOpen')}</button
			>
		{/snippet}
	</SettingRow>
	{#if status}
		{#if status.controls.length === 0}
			<SettingRow icon="chip" title={t('energy.cpu')} desc={t('energy.unsupported')} />
		{:else}
			<SettingRow
				id="cpu"
				icon="chip"
				title={t('energy.cpu')}
				desc={status.drivers.length
					? `${t('energy.driver')} ${status.drivers.join(', ')}`
					: undefined}
				summary={status.controls
					.map((c) => (c.mixed ? t('energy.mixed') : label(c.current)))
					.join(' · ')}
			>
				{#each status.controls as item (item.key)}
					<SettingSub
						title={t(`energy.${item.key}`)}
						for="energy-{item.key}"
						desc="{t(`energy.${item.key}Hint`)} · {t('energy.current')}: {item.mixed
							? t('energy.mixed')
							: label(item.current)}"
					>
						{#snippet control()}
							<select
								id="energy-{item.key}"
								class="field"
								bind:value={draft[item.key]}
								disabled={busy || status?.readOnly}
								onchange={store}
							>
								<option value="">{t('energy.unmanaged')}</option>
								{#if draft[item.key] && !item.choices.includes(draft[item.key])}<option
										value={draft[item.key]}
										disabled>{draft[item.key]} — {t('energy.noLongerAvailable')}</option
									>{/if}
								{#each item.choices as choice (choice)}<option value={choice}
										>{label(choice)}{label(choice) !== choice ? ` (${choice})` : ''}</option
									>{/each}
							</select>
						{/snippet}
					</SettingSub>
				{/each}
				{#if saved}<div class="srow-block">
						<p class="muted" role="status">{t('energy.saved')}</p>
					</div>{/if}
			</SettingRow>
		{/if}
	{:else if !error}<p class="muted">{t('common.loading')}</p>{/if}
</SettingGroup>

<SettingGroup title={t('energy.group.ups')}>
	<SettingRow
		href="/settings/ups"
		icon="battery"
		title={t('ups.title')}
		desc={t('energy.upsDesc')}
		summary={upsSummary}
	/>
</SettingGroup>
{#if sleepOpen && sleep}<DiskSleepDialog
		status={sleep}
		onclose={() => (sleepOpen = false)}
		onsaved={(updated: DiskSleepStatus) => (sleep = updated)}
	/>{/if}

<style>
	p {
		margin: 0;
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
