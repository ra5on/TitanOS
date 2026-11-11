<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import { onMount } from 'svelte';
	import { api, type StorageStatus } from '#lib/api.ts';
	import { MB, formatDateTime } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { problemText, requestProblem, type Problem } from '../problem.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import KeepChoice, { keepChoicePayload } from '#lib/components/KeepChoice.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import {
		newMaintenanceJob,
		type MaintenanceJob,
		type MaintenanceSettings,
		type MaintenanceStatus,
		type BackupView,
	} from '#lib/operations.ts';
	let status = $state<MaintenanceStatus | null>(null),
		settings = $state<MaintenanceSettings | null>(null),
		storage = $state<StorageStatus | null>(null),
		backups = $state<BackupView[]>([]);
	let draftVolume = $state(''),
		savedSettings = $state('');
	let error = $state<Problem | null>(null),
		busy = $state(false),
		saved = $state(false),
		draft = $state<MaintenanceJob | null>(null),
		initial = $state(''),
		discard = $state(false),
		review = $state('');
	// The NAS's own time zone comes first; the list is the one /welcome offers.
	let nasZone = $state('');
	const zones = $derived(
		[
			...new Set([nasZone, settings?.timezone, 'UTC', ...Intl.supportedValuesOf('timeZone')]),
		].filter((zone): zone is string => !!zone),
	);
	const kinds = [
		'raid-check',
		'smart-short',
		'smart-long',
		'backup-check',
		'restore-probe',
		'snapshot',
	] as const;
	const targets = $derived<{ id: string; name: string; disabled?: boolean }[]>(
		draft?.kind === 'smart-long' || draft?.kind === 'smart-short'
			? (storage?.disks ?? [])
					.filter((d) => d.serial && d.role !== 'stick')
					.map((d) => ({ id: d.serial, name: `${d.model} · ${d.serial}` }))
			: ['backup-check', 'restore-probe'].includes(draft?.kind ?? '')
				? backups
						.filter((b) => !['usb-copy', 'smb-copy'].includes(b.job.destination.type))
						.map((b) => ({ id: b.job.id, name: b.job.id }))
				: (storage?.volumes ?? []).map((v) =>
						// ext4/XFS have no snapshots (plan 7h): listed, but not to be chosen.
						draft?.kind === 'snapshot' && v.filesystem !== 'btrfs'
							? {
									id: v.name,
									name: `${v.name} · ${t('ops.noSnapshots', { filesystem: v.filesystem === 'xfs' ? 'XFS' : 'ext4' })}`,
									disabled: true,
								}
							: { id: v.name, name: v.name },
					),
	);
	async function load() {
		try {
			status = await api.get<MaintenanceStatus>('/maintenance');
			error = null;
			if (!settings) {
				settings = structuredClone($state.snapshot(status!.settings));
				// Before 0.47.2 the API left backupsFirst out when false; the checkbox would add it.
				settings.backupsFirst = !!settings.backupsFirst;
				savedSettings = JSON.stringify(settings);
			}
		} catch (e) {
			error = requestProblem(e, t);
		}
	}
	onMount(() => {
		api
			.get<{ timezone: string }>('/network')
			.then((res) => (nasZone = res.timezone))
			.catch(() => {});
		load();
		Promise.all([api.get<StorageStatus>('/storage'), api.get<{ jobs: BackupView[] }>('/backups')])
			.then(([s, b]) => {
				storage = s;
				backups = b.jobs;
			})
			.catch((e) => (error = requestProblem(e, t)));
		return poll(load, { interval: 10000, immediate: false, skip: () => !!draft || busy });
	});
	async function save(next: MaintenanceSettings): Promise<boolean> {
		busy = true;
		error = null;
		saved = false;
		try {
			status = await api.put<MaintenanceStatus>('/maintenance', next);
			settings = structuredClone($state.snapshot(status!.settings));
			// Before 0.47.2 the API left backupsFirst out when false; the checkbox would add it.
			settings.backupsFirst = !!settings.backupsFirst;
			draft = null;
			savedSettings = JSON.stringify(settings);
			saved = true;
			return true;
		} catch (e) {
			error = requestProblem(e, t);
			return false;
		} finally {
			busy = false;
		}
	}
	function edit(job?: MaintenanceJob) {
		draft = job ? structuredClone($state.snapshot(job)) : newMaintenanceJob();
		if (draft) {
			draft.dependsOn ??= '';
			draft.weekdays ??= [0];
			draft.monthDay ||= 1;
			draft.everyHours ||= 6;
		}
		// The dialog starts from what is stored, not from unsaved changes above it.
		draftVolume = stored()?.volume ?? '';
		initial = JSON.stringify({ job: draft, volume: draftVolume });
		error = null;
	}
	const jobDirty = $derived(
		!!draft && JSON.stringify({ job: draft, volume: draftVolume }) !== initial,
	);
	const settingsDirty = $derived(!!settings && JSON.stringify(settings) !== savedSettings);
	function close() {
		if (jobDirty) discard = true;
		else draft = null;
	}
	/** The settings as stored on the NAS, without unsaved changes of the form. */
	function stored(): MaintenanceSettings | null {
		return savedSettings ? (JSON.parse(savedSettings) as MaintenanceSettings) : null;
	}
	/**
	 * Saves a job on top of the stored settings. Unsaved changes of the global
	 * form stay a draft of their own and are kept after saving.
	 */
	async function saveJobs(change: (jobs: MaintenanceJob[]) => MaintenanceJob[], volume?: string) {
		const base = stored();
		if (!base || !settings) return;
		const pending = settingsDirty ? $state.snapshot(settings) : null;
		const ok = await save({
			...base,
			volume: volume ?? base.volume,
			jobs: change(base.jobs),
		});
		if (ok && pending && settings) {
			const now = $state.snapshot(settings);
			settings = {
				...pending,
				jobs: now.jobs,
				volume: pending.volume === base.volume ? now.volume : pending.volume,
			};
		}
	}
	async function saveJob() {
		const snapshot = draft ? $state.snapshot(draft) : null;
		if (!snapshot) return;
		const job =
			snapshot.kind === 'snapshot'
				? { ...snapshot, ...keepChoicePayload(snapshot.keep, snapshot.versions, 1, 100) }
				: { ...snapshot, versions: undefined };
		await saveJobs((jobs) => [...jobs.filter((j) => j.id !== job.id), job], draftVolume);
	}
	async function action(id: string, kind: string) {
		busy = true;
		error = null;
		try {
			status = await api.post<MaintenanceStatus>(
				`/maintenance/${encodeURIComponent(id)}/${kind}`,
				// The API still wants the job ID as confirmation of the review.
				kind === 'review' ? { confirm: id } : {},
			);
			review = '';
		} catch (e) {
			error = requestProblem(e, t);
		} finally {
			busy = false;
		}
	}
	// Times of the schedule are shown in the NAS's maintenance time zone.
	const time = (value: string | null | undefined) =>
		value ? formatDateTime(new Date(value), status?.settings.timezone || undefined) : '—';

	// PLAN 67-1: Ablage › Neue Aufgabe / Jetzt ausführen in the menu bar of the desktop.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'planner.newTask': { run: () => edit(), enabled: () => !!settings && !busy },
		'planner.run': {
			items: () =>
				busy
					? []
					: (status?.jobs ?? [])
							.filter((v) => !v.state.running && !v.state.blocked)
							.map((v) => ({ id: v.job.id, text: v.job.name })),
			run: (id) => {
				if (id) void action(id, 'run');
			},
		},
		'planner.refresh': { run: () => void load(), enabled: () => !busy },
	});
</script>

<NavigationGuard dirty={settingsDirty || jobDirty} />
<PageHead title={t('ops.maintenance')}
	>{#snippet actions()}<button class="btn" disabled={busy} onclick={load}>{t('ops.refresh')}</button
		><button class="btn btn-primary" disabled={!settings || busy} onclick={() => edit()}
			>{t('ops.add')}</button
		>{/snippet}</PageHead
>
{#snippet problemLine(problem: Problem)}{problem.text}{#if problem.detail}<code class="detail"
			>{problem.detail}</code
		>{/if}{/snippet}
{#if error && !draft && !review}<p class="notice-error" role="alert">
		{@render problemLine(error)}
		<button class="btn small" disabled={busy} onclick={load}>{t('ui.retry')}</button>
	</p>{/if}
<p class="muted">{t('ops.maintenanceHint')}</p>
<!-- PLAN 78-5: the settings of the planner as flat rows, stored with "Speichern". -->
{#if settings}<form
		class="configuration"
		onsubmit={(e) => {
			e.preventDefault();
			if (settings) save(settings);
		}}
	>
		<SettingGroup title={t('ops.maintenanceSettings')} id="maintenance-settings">
			<SettingRow icon="tool" title={t('ops.enabled')} for="maintenance-enabled">
				{#snippet control()}<input
						id="maintenance-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={settings!.enabled}
					/>{/snippet}
			</SettingRow>
			<SettingRow icon="disk" title={t('ops.volume')} for="maintenance-volume">
				{#snippet control()}<select
						id="maintenance-volume"
						class="field"
						bind:value={settings!.volume}
						required={settings!.enabled || settings!.jobs.length > 0}
						><option value="">{t('ops.choose')}</option
						>{#if settings!.volume && !storage?.volumes.some((v) => v.name === settings!.volume)}<option
								value={settings!.volume}>{settings!.volume} · {t('tasks.wait.volume')}</option
							>{/if}{#each storage?.volumes ?? [] as v (v.name)}<option value={v.name}
								>{v.name}</option
							>{/each}</select
					>{/snippet}
			</SettingRow>
			<SettingRow icon="archive" title={t('tasks.backupsFirst')} for="maintenance-backups-first">
				{#snippet control()}<input
						id="maintenance-backups-first"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={settings!.backupsFirst}
					/>{/snippet}
			</SettingRow>
			<SettingRow
				icon="globe"
				title={t('ops.timezone')}
				for="maintenance-timezone"
				desc={nasZone && settings.timezone !== nasZone
					? t('ops.timezoneDiffers', { zone: nasZone })
					: undefined}
			>
				{#snippet control()}<select
						id="maintenance-timezone"
						class="field"
						bind:value={settings!.timezone}
						required
						>{#each zones as zone (zone)}<option value={zone}
								>{zone === nasZone ? t('ops.timezoneNas', { zone }) : zone}</option
							>{/each}</select
					>{/snippet}
			</SettingRow>
			<SettingRow icon="clock" title={t('ops.from')} for="maintenance-from">
				{#snippet control()}<input
						id="maintenance-from"
						class="field"
						type="time"
						bind:value={settings!.windowFrom}
						required
					/>{/snippet}
			</SettingRow>
			<SettingRow icon="clock" title={t('ops.to')} for="maintenance-to">
				{#snippet control()}<input
						id="maintenance-to"
						class="field"
						type="time"
						bind:value={settings!.windowTo}
						required
					/>{/snippet}
			</SettingRow>
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy}>{t('ops.save')}</button
				>{#if saved && !settingsDirty && !jobDirty}<span role="status">{t('notify.saved')}</span
					>{/if}
			</div>
		</SettingGroup>
	</form>{/if}
{#if status?.error}<p class="notice-warn">
		{@render problemLine(problemText(status.error, t))}
	</p>{/if}
<!-- The jobs stay a table on its own surface, under a plain heading. -->
<div class="schedules">
	<SettingGroup title={t('ops.schedules')} id="maintenance-jobs">
		<div class="tile table-wrap">
			<table>
				<thead
					><tr
						><th>{t('ops.name')}</th><th>{t('ops.target')}</th><th>{t('ops.next')}</th><th
							>{t('ops.lastOK')}</th
						><th>{t('ops.state')}</th><th></th></tr
					></thead
				><tbody>
					{#each status?.jobs ?? [] as view (view.job.id)}<tr
							><td
								><strong>{view.job.name}</strong>
								<div class="muted">{t(`ops.kind.${view.job.kind}`)}</div></td
							><td class="mono">{view.job.target}</td><td
								>{view.job.enabled && status?.settings.enabled ? time(view.next) : '—'}</td
							><td>{time(view.state.lastOK)}</td><td
								>{t(
									view.state.running
										? 'ops.running'
										: view.state.blocked
											? 'ops.blocked'
											: 'ops.ready',
								)}{#if view.wait && !view.state.running}<p class="muted">
										{t(`tasks.wait.${view.wait}` as MessageKey)}
									</p>{/if}{#if view.state.error}<p class="notice-warn">
										{@render problemLine(problemText(view.state.error, t))}
									</p>{/if}</td
							><td
								><div class="actions">
									<button
										class="btn"
										disabled={busy || view.state.running}
										onclick={() => edit(view.job)}>{t('ops.edit')}</button
									>{#if view.state.running}<button
											class="btn"
											disabled={busy}
											onclick={() => action(view.job.id, 'stop')}>{t('ops.stop')}</button
										>{:else if view.state.blocked}<button
											class="btn"
											onclick={() => {
												review = view.job.id;
											}}>{t('ops.review')}</button
										>{:else}<button
											class="btn"
											disabled={busy}
											onclick={() => action(view.job.id, 'run')}>{t('ops.run')}</button
										>{/if}
								</div></td
							></tr
						>{:else}<tr><td colspan="6"><EmptyState inline title={t('ops.none')} /></td></tr>{/each}
				</tbody>
			</table>
		</div>
	</SettingGroup>
</div>
<div class="history">
	<SettingGroup title={t('ops.history')} id="maintenance-history">
		{#each status?.history ?? [] as result, i (`${result.id}-${result.started}-${i}`)}<SettingRow
				title={result.name}
			>
				{#snippet description()}<span class="result"
						>{#if result.error}{@render problemLine(problemText(result.error, t))}{:else}{t(
								'ops.finished',
							)}{/if}</span
					>{/snippet}
				{#snippet control()}<time>{time(result.finished)}</time>{/snippet}
			</SettingRow>{:else}<p class="muted">{t('ops.none')}</p>{/each}
	</SettingGroup>
</div>
{#if draft}<Dialog
		title={t(draft.id ? 'ops.edit' : 'ops.add')}
		locked={busy}
		onclose={close}
		onrequestclose={close}
	>
		<form
			id="maintenance-form"
			class="editor"
			onsubmit={(e) => {
				e.preventDefault();
				saveJob();
			}}
		>
			<label
				>{t('ops.volume')}<select class="field" bind:value={draftVolume} required
					><option value="">{t('ops.choose')}</option
					>{#each storage?.volumes ?? [] as v (v.name)}<option value={v.name}>{v.name}</option
						>{/each}</select
				></label
			>
			<label
				>{t('ops.name')}<input
					class="field"
					bind:value={draft.name}
					maxlength="100"
					required
				/></label
			>
			<label
				>{t('ops.kind')}<select
					class="field"
					bind:value={draft.kind}
					onchange={() => {
						if (draft) draft.target = '';
					}}
					>{#each kinds as kind (kind)}<option value={kind}>{t(`ops.kind.${kind}`)}</option
						>{/each}</select
				></label
			>
			<label
				>{t('ops.target')}<select class="field" bind:value={draft.target} required
					><option value="">{t('ops.choose')}</option>{#each targets as target (target.id)}<option
							value={target.id}
							disabled={target.disabled}>{target.name}</option
						>{/each}</select
				></label
			>
			{#if draft.kind === 'snapshot'}<p class="muted">{t('ops.snapshotBtrfsOnly')}</p>{/if}
			<label
				>{t('tasks.dependsOn')}<select class="field" bind:value={draft.dependsOn}
					><option value="">{t('tasks.noDependency')}</option
					>{#each settings?.jobs.filter((j) => j.id !== draft?.id) ?? [] as parent (parent.id)}<option
							value={parent.id}>{parent.name}</option
						>{/each}</select
				></label
			>
			<p class="muted">{t('tasks.sharedVolume')}</p>
			<label
				>{t('ops.every')}<select class="field" bind:value={draft.every}
					>{#each ['hours', 'daily', 'weekly', 'monthly'] as every (every)}<option value={every}
							>{t(`ops.${every}` as MessageKey)}</option
						>{/each}</select
				></label
			>
			{#if draft.every === 'hours'}<label
					>{t('ops.everyHours')}<input
						class="field"
						type="number"
						bind:value={draft.everyHours}
						min="1"
						max="168"
						required
					/></label
				>
				<p class="muted small">
					{t(
						draft.kind === 'raid-check' || draft.kind === 'smart-long'
							? 'ops.everyHoursLong'
							: 'ops.everyHoursHint',
					)}
				</p>{/if}
			{#if draft.every === 'weekly'}<fieldset>
					<legend>{t('ops.weekdays')}</legend>
					<div class="actions">
						{#each [0, 1, 2, 3, 4, 5, 6] as day (day)}<label class="check"
								><input
									type="checkbox"
									checked={draft.weekdays.includes(day)}
									onchange={(e) => {
										if (draft)
											draft.weekdays = e.currentTarget.checked
												? [...draft.weekdays, day]
												: draft.weekdays.filter((d) => d !== day);
									}}
								/>{new Intl.DateTimeFormat(undefined, { weekday: 'short' }).format(
									new Date(2026, 9, 18 + day),
								)}</label
							>{/each}
					</div>
				</fieldset>{/if}
			{#if draft.every === 'monthly'}<label
					>{t('ops.monthDay')}<input
						class="field"
						type="number"
						bind:value={draft.monthDay}
						min="1"
						max="28"
						required
					/></label
				>{/if}
			<label
				>{t('ops.time')}<input class="field" type="time" bind:value={draft.time} required /></label
			><label
				>{t('ops.missed')}<select class="field" bind:value={draft.missed}
					><option value="window">{t('ops.window')}</option><option value="skip"
						>{t('ops.skip')}</option
					></select
				></label
			>
			{#if draft.kind === 'backup-check'}<label
					>{t('ops.readData')}<input
						class="field"
						type="number"
						min="0"
						max="100"
						bind:value={draft.readData}
					/></label
				>{/if}
			{#if draft.kind === 'restore-probe'}<label
					>{t('ops.maxMiB')}<input
						class="field"
						type="number"
						min="1"
						max="1073"
						value={Math.round(draft.maxBytes / MB)}
						onchange={(e) => {
							if (draft) draft.maxBytes = Number(e.currentTarget.value) * MB;
						}}
					/></label
				><label
					>{t('ops.maxFiles')}<input
						class="field"
						type="number"
						min="1"
						max="200"
						bind:value={draft.maxFiles}
					/></label
				>{/if}
			{#if draft.kind === 'snapshot'}<div class="keep-block">
					<span>{t('ops.keep')}</span>
					<KeepChoice
						bind:count={draft.keep}
						bind:versions={draft.versions}
						min={1}
						max={100}
						unit="snapshots"
					/>
				</div>
				<label
					>{t('ops.lockDays')}<input
						class="field"
						type="number"
						min="0"
						max="365"
						bind:value={draft.lockDays}
					/><small class="muted">{t('ops.lockDaysHint')}</small></label
				><label class="check"
					><input type="checkbox" bind:checked={draft.allowPrune} required />{t(
						'ops.pruneConsent',
					)}</label
				>{/if}
			<label class="check"
				><input type="checkbox" bind:checked={draft.enabled} />{t('ops.enabled')}</label
			>
		</form>
		{#if error}<p class="notice-error" role="alert">{@render problemLine(error)}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={close}>{t('common.cancel')}</button
			>{#if draft.id}<button
					class="btn btn-warn"
					disabled={busy}
					onclick={() => {
						const id = draft?.id;
						if (id) saveJobs((jobs) => jobs.filter((j) => j.id !== id));
					}}>{t('ops.remove')}</button
				>{/if}<button form="maintenance-form" class="btn btn-primary" disabled={busy}
				>{t('ops.save')}</button
			>
		</div>
	</Dialog>{/if}
{#if discard}<Dialog title={t('ops.unsaved')} onclose={() => (discard = false)}
		><p>{t('ops.unsavedHint')}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (discard = false)}>{t('common.cancel')}</button><button
				class="btn btn-warn"
				onclick={() => {
					draft = null;
					discard = false;
				}}>{t('ops.discard')}</button
			>
		</div></Dialog
	>{/if}
{#if review}<Dialog title={t('ops.review')} locked={busy} onclose={() => (review = '')}
		><p>{t('ops.reviewHint')}</p>
		<p class="mono">{review}</p>
		{#if error}<p class="notice-error" role="alert">{@render problemLine(error)}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (review = '')}>{t('common.cancel')}</button
			><button class="btn btn-warn" disabled={busy} onclick={() => action(review, 'review')}
				>{t('ops.review')}</button
			>
		</div></Dialog
	>{/if}

<style>
	.row-actions {
		margin-top: var(--space-2);
	}
	.configuration {
		margin: var(--gap) 0;
	}
	.editor {
		display: flex;
		flex-wrap: wrap;
		align-items: end;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-base);
		flex: 1 1 170px;
		min-width: 0;
	}
	.check {
		flex-direction: row;
		align-items: center;
	}
	.editor label {
		flex-basis: 45%;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.table-wrap {
		overflow-x: auto;
	}
	/* Narrow windows: the table scrolls sideways instead of breaking words. */
	table {
		min-width: 48rem;
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-base);
	}
	th,
	td {
		padding: var(--space-3);
		text-align: left;
		border-bottom: 1px solid var(--border);
		vertical-align: top;
	}
	td p {
		max-width: 350px;
		overflow-wrap: anywhere;
	}
	.history {
		margin-top: var(--gap);
	}
	.detail {
		display: block;
		margin-top: var(--space-1);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.schedules {
		margin-top: var(--gap);
	}
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		width: 100%;
	}
	@media (max-width: 760px) {
		.editor label {
			flex-basis: 100%;
		}
	}
</style>
