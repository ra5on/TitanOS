<script lang="ts">
	import { api, type DiskSleepStatus } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import PowerSchedule from '#lib/components/PowerSchedule.svelte';
	import Expander from '#lib/components/explorer/Expander.svelte';
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
	let cpuOpen = $state(false);
	// E3: a changed profile is stored right away.
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
{#if status}
	{#if status.error}<p class="notice-warn" role="status">
			{t('energy.startupError')}
			{status.error}
		</p>{/if}
	{#if status.readOnly}<p class="notice-warn">{t('energy.readOnly')}</p>{/if}
	{#if status.controls.length === 0}
		<Expander icon="chip" title={t('energy.cpu')} summary={t('energy.unsupported')} />
	{:else}
		<!-- PLAN 75-19, draft E3: each profile a row, saved when it changes. -->
		<Expander
			icon="chip"
			title={t('energy.cpu')}
			summary={[
				...status.controls.map(
					(c) => `${t(`energy.${c.key}`)}: ${c.mixed ? t('energy.mixed') : label(c.current)}`,
				),
				status.drivers.length ? `${t('energy.driver')} ${status.drivers.join(', ')}` : '',
			]
				.filter(Boolean)
				.join(' · ')}
			bind:open={cpuOpen}
		>
			{#each status.controls as control (control.key)}
				<div class="part">
					<span class="t"
						><label for="energy-{control.key}">{t(`energy.${control.key}`)}</label><small
							>{t(`energy.${control.key}Hint`)} · {t('energy.current')}: {control.mixed
								? t('energy.mixed')
								: label(control.current)}</small
						></span
					>
					<select
						id="energy-{control.key}"
						class="field"
						bind:value={draft[control.key]}
						disabled={busy || status.readOnly}
						onchange={store}
					>
						<option value="">{t('energy.unmanaged')}</option>
						{#if draft[control.key] && !control.choices.includes(draft[control.key])}<option
								value={draft[control.key]}
								disabled>{draft[control.key]} — {t('energy.noLongerAvailable')}</option
							>{/if}
						{#each control.choices as choice (choice)}<option value={choice}
								>{label(choice)}{label(choice) !== choice ? ` (${choice})` : ''}</option
							>{/each}
					</select>
				</div>
			{/each}
			{#if saved}<p class="part muted" role="status">{t('energy.saved')}</p>{/if}
		</Expander>
	{/if}
{:else if !error}<p>{t('common.loading')}</p>{/if}
<Expander
	icon="disk"
	title={t('energy.diskSleep')}
	summary={sleepError || sleepSummary || t('energy.diskSleepHint')}
>
	{#snippet control()}
		<button type="button" class="btn" disabled={!sleep} onclick={() => (sleepOpen = true)}
			>{t('energy.diskSleepOpen')}</button
		>
	{/snippet}
</Expander>
{#if sleepOpen && sleep}<DiskSleepDialog
		status={sleep}
		onclose={() => (sleepOpen = false)}
		onsaved={(updated: DiskSleepStatus) => (sleep = updated)}
	/>{/if}
<PowerSchedule />

<style>
	.part select {
		max-width: 20rem;
	}
	.part label {
		font-weight: 500;
	}
	p {
		margin: 0;
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
