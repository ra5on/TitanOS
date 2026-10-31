<script lang="ts">
	// The tab "Snapshots" of a VM: the list, the schedule and the dialogs
	// to make, restore and delete snapshots.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError, type VM } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import KeepVersions from '#lib/components/KeepVersions.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import type { SnapshotSchedule, SnapshotStatus, Snapshot } from './snapshots.ts';

	let { vm }: { vm: VM } = $props();

	type Panel = '' | 'create' | 'plan' | 'restore' | 'delete' | 'recover';
	let status = $state<SnapshotStatus | null>(null);
	let panel = $state<Panel>('');
	let target = $state<Snapshot | null>(null);
	let name = $state('');
	let memory = $state(true);
	let allowPause = $state(false);
	let discardMemory = $state(false);
	let schedule = $state<SnapshotSchedule>({
		enabled: true,
		time: '03:00',
		days: [0, 1, 2, 3, 4, 5, 6],
		keep: 7,
		memory: true,
		allowPause: false,
	});
	let loadError = $state('');
	let error = $state('');
	let busy = $state(false);

	const base = $derived(`/vms/${encodeURIComponent(vm.name)}`);
	const working = $derived(busy || status?.job?.state === 'running');
	const blocked = $derived(working || !vm.available || !!status?.reason || !!status?.pending);

	async function load(url = base) {
		try {
			const result = await api.get<SnapshotStatus>(url + '/snapshots');
			if (url === base) {
				status = result;
				loadError = '';
			}
		} catch (err) {
			if (url === base) loadError = err instanceof ApiError ? errorText(err.code) : String(err);
		}
	}
	$effect(() => {
		const url = base;
		status = null;
		panel = '';
		loadError = '';
		load(url);
	});
	// Quick while a snapshot job runs, slow while nothing happens.
	const polling = $derived(busy || status?.job?.state === 'running' || !!status?.pending);
	$effect(() => {
		const url = base;
		return poll(() => load(url), { interval: polling ? 3000 : 15000, immediate: false });
	});

	function open(next: Panel, snapshot: Snapshot | null = null) {
		error = '';
		target = snapshot;
		memory = snapshot?.memory ?? true;
		allowPause = false;
		discardMemory = false;
		name = new Date().toLocaleString();
		if (next === 'plan' && status?.schedule?.time)
			schedule = { ...status.schedule, days: [...(status.schedule.days ?? [])] };
		panel = next;
	}

	async function submit() {
		if (busy) return;
		busy = true;
		error = '';
		const url = base;
		// The API still wants a name as confirmation; it is sent once the
		// button is clicked (snapshot name for deleting, VM name otherwise).
		const confirm = panel === 'delete' ? (target?.name ?? '') : vm.name;
		try {
			if (panel === 'create') await api.post(url + '/snapshots', { name, memory, allowPause });
			if (panel === 'plan') await api.put(url + '/snapshot-schedule', schedule);
			if (panel === 'restore' && target)
				await api.post(url + `/snapshots/${target.id}/restore`, { confirm, memory });
			if (panel === 'delete' && target)
				await api.delete(url + `/snapshots/${target.id}`, { confirm });
			if (panel === 'recover')
				await api.post(url + '/snapshot-recovery', { confirm, discardMemory });
			panel = '';
			await load(url);
		} catch (err) {
			error =
				err instanceof ApiError
					? errorText(err.code) + (err.code === 'vm_failed' ? `\n${err.message}` : '')
					: String(err);
		} finally {
			busy = false;
		}
	}

	const days = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'] as const;
	// Destructive panels run only from a click on their button, never from Enter.
	const needsConfirm = $derived(panel === 'restore' || panel === 'delete' || panel === 'recover');
	const submitDisabled = $derived(
		busy ||
			(panel === 'create' && (blocked || (vm.state === 'running' && !memory))) ||
			(panel === 'plan' && schedule.enabled && !schedule.days.length),
	);
	const submitClass = $derived(
		panel === 'restore' || panel === 'delete'
			? 'btn-danger'
			: panel === 'recover'
				? 'btn-warn'
				: 'btn-primary',
	);
</script>

<section class="snapshots" aria-label={t('snapshots.title')}>
	<div class="head">
		<p class="muted scope">{t('snapshots.scope')}</p>
		<div class="buttons">
			<button type="button" class="btn" onclick={() => open('plan')} disabled={working}
				>{t('snapshots.plan')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				onclick={() => open('create')}
				disabled={blocked}>{t('snapshots.create')}</button
			>
		</div>
	</div>

	{#if loadError}<p class="notice-error" role="alert">{loadError}</p>{/if}
	{#if status?.reason}<p class="notice-warn">{t(`snapshots.reason.${status.reason}`)}</p>{/if}
	{#if status?.job}
		<div class={status.job.state === 'failed' ? 'notice-error' : 'notice-info'} role="status">
			{t(`snapshots.job.${status.job.state}`)}{#if status.job.text}<pre>{status.job.text}</pre>{/if}
		</div>
	{/if}
	{#if status?.pending && !working}
		<div class="notice-warn">
			{t('snapshots.interrupted')}
			<button type="button" class="btn" onclick={() => open('recover')}
				>{t('snapshots.recover')}</button
			>
		</div>
	{/if}
	{#if status?.schedule.enabled}<p class="schedule">
			{t('snapshots.scheduled', { time: status.schedule.time, keep: status.schedule.keep })}
		</p>{/if}

	{#if status}
		<div class="tile list">
			{#each status.snapshots as snapshot (snapshot.id)}
				<div class="row">
					<div class="info">
						<strong>{snapshot.incomplete ? t('snapshots.incomplete') : snapshot.name}</strong>
						{#if snapshot.incomplete}
							<span class="mono">{snapshot.id}</span>
						{:else}
							<span class="muted"
								>{new Date(snapshot.created).toLocaleString()} · {formatBytes(snapshot.bytes)} · {t(
									snapshot.memory ? 'snapshots.withMemory' : 'snapshots.disksOnly',
								)}{snapshot.automatic ? ` · ${t('snapshots.automatic')}` : ''}</span
							>
						{/if}
					</div>
					<button
						type="button"
						class="btn"
						disabled={working || status.pending || vm.state === 'running' || snapshot.incomplete}
						onclick={() => open('restore', snapshot)}>{t('snapshots.restore')}</button
					>
					<button
						type="button"
						class="btn"
						disabled={working || status.pending}
						onclick={() => open('delete', snapshot)}>{t('snapshots.delete')}</button
					>
				</div>
			{:else}
				<EmptyState inline title={t('snapshots.empty')} />
			{/each}
		</div>
		{#if vm.state === 'running' && status.snapshots.length}<p class="muted">
				{t('snapshots.stopFirst')}
			</p>{/if}
	{/if}
</section>

{#if panel}
	<Dialog
		title={t(`snapshots.${panel}`)}
		locked={busy}
		wide={panel === 'plan'}
		onclose={() => (panel = '')}
	>
		<form
			onsubmit={(event) => {
				event.preventDefault();
				if (!needsConfirm) submit();
			}}
		>
			{#if panel === 'create'}
				<label
					>{t('snapshots.name')}<input
						class="field"
						bind:value={name}
						maxlength="80"
						required
					/></label
				>
				<p class="muted">{t('snapshots.scope')}</p>
				{#if vm.state === 'running'}
					<label class="check"
						><input type="checkbox" bind:checked={memory} />{t('snapshots.includeMemory')}</label
					>
					<p class="notice-warn">{t('snapshots.pauseInfo')}</p>
					<label class="check"
						><input type="checkbox" bind:checked={allowPause} required />{t(
							'snapshots.allowPause',
						)}</label
					>
				{:else}
					<p>{t('snapshots.stoppedInfo')}</p>
				{/if}
			{:else if panel === 'plan'}
				<label class="check"
					><input type="checkbox" bind:checked={schedule.enabled} />{t('snapshots.enable')}</label
				>
				<div class="fields">
					<label
						>{t('snapshots.time')}<input
							class="field"
							type="time"
							bind:value={schedule.time}
							required
							disabled={!schedule.enabled}
						/></label
					>
				</div>
				<fieldset disabled={!schedule.enabled}>
					<legend>{t('snapshots.keep')}</legend>
					<KeepVersions
						bind:keep={
							() => ({ last: schedule.keep, daily: 0, weekly: 0, monthly: 0 }),
							(k) => (schedule.keep = k.last ?? schedule.keep)
						}
						unit="snapshots"
						rolling={false}
						max={100}
					/>
				</fieldset>
				<fieldset disabled={!schedule.enabled}>
					<legend>{t('snapshots.days')}</legend>
					<div class="week">
						{#each days as day, index (day)}
							<label class="check"
								><input
									type="checkbox"
									checked={schedule.days.includes(index)}
									onchange={(event) => {
										schedule.days = event.currentTarget.checked
											? [...schedule.days, index]
											: schedule.days.filter((d) => d !== index);
									}}
								/>{t(`snapshots.day.${day}`)}</label
							>
						{/each}
					</div>
				</fieldset>
				<label class="check"
					><input type="checkbox" bind:checked={schedule.memory} disabled={!schedule.enabled} />{t(
						'snapshots.includeMemory',
					)}</label
				>
				<p class="notice-warn">{t('snapshots.pauseInfo')}</p>
				<label class="check"
					><input
						type="checkbox"
						bind:checked={schedule.allowPause}
						disabled={!schedule.enabled}
						required={schedule.enabled && schedule.memory}
					/>{t('snapshots.allowPause')}</label
				>
				<p class="muted">{t('snapshots.retention')}</p>
			{:else if panel === 'restore'}
				<p class="notice-warn">
					{t('snapshots.restoreWarning', { name: target?.name ?? '', vm: vmLabel(vm) })}
				</p>
				{#if target?.memory}
					<label class="check"
						><input type="checkbox" bind:checked={memory} />{t('snapshots.restoreMemory')}</label
					>
					<p class="muted">{t('snapshots.runtime')}</p>
				{/if}
			{:else if panel === 'delete'}
				<p class="notice-warn">{t('snapshots.deleteWarning', { name: target?.name ?? '' })}</p>
			{:else if panel === 'recover'}
				<p class="notice-warn">{t('snapshots.recoverInfo')}</p>
				<label class="check"
					><input type="checkbox" bind:checked={discardMemory} />{t(
						'snapshots.discardMemory',
					)}</label
				>
			{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (panel = '')} disabled={busy}
					>{t('common.cancel')}</button
				>
				<button
					type={needsConfirm ? 'button' : 'submit'}
					class="btn {submitClass}"
					disabled={submitDisabled}
					onclick={needsConfirm ? submit : undefined}
					>{t(panel === 'plan' ? 'snapshots.save' : `snapshots.${panel}`)}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

<style>
	.snapshots {
		display: flex;
		flex-direction: column;
		gap: 18px;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.scope {
		flex: 1 1 320px;
		margin: 0;
		font-size: var(--text-sm);
		max-width: 640px;
		line-height: 1.65;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.schedule {
		margin: 0;
		font-size: var(--text-md);
		padding: 12px 16px;
		background: var(--glass-surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.list {
		padding: 0 16px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 14px;
		padding: 20px 0;
		border-bottom: 1px solid var(--border);
	}
	.row:last-child {
		border-bottom: 0;
	}
	.info {
		display: flex;
		flex: 1 1 260px;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
		overflow-wrap: anywhere;
		font-size: var(--text-md);
	}
	.info .muted {
		font-size: var(--text-base);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 8px;
	}
	.check input {
		flex: none;
		width: 18px;
		height: 18px;
	}
	.fields,
	.week {
		display: flex;
		flex-wrap: wrap;
		gap: 16px;
	}
	.fields > label {
		flex: 1;
	}
	fieldset {
		padding: 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	pre {
		margin-top: 8px;
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
