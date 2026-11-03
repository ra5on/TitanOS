<script lang="ts">
	// Ruhemodus in the volume details: the value of the volume, the state of
	// each disk, and a small editor for the volume and its disks. It sends the
	// same request as the sleep dialog.
	import { api, type DiskSleepStatus } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { overridesFrom } from './disk-sleep.ts';
	import { sleepDuration, sleepOrigin, sleepSince, volumeSleep } from './sleep-view.ts';

	type Props = {
		volume: string;
		status: DiskSleepStatus | undefined;
		canAdminister: boolean;
		onsaved: (status: DiskSleepStatus) => void;
		onopen: () => void;
	};
	let { volume, status, canAdminister, onsaved, onopen }: Props = $props();

	const INHERIT = 'inherit';
	const minutes = [0, 5, 10, 15, 30, 60, 120, 180];
	const info = $derived(volumeSleep(status, volume));
	const unavailable = $derived(
		(status?.unavailable ?? []).filter((disk) => disk.volume === volume),
	);

	let editing = $state(false);
	let busy = $state(false);
	let error = $state('');
	let draftVolume = $state(INHERIT);
	let draftDisks = $state<Record<string, string>>({});

	function open() {
		draftVolume =
			status?.volumes?.[volume] === undefined ? INHERIT : String(status.volumes[volume]);
		draftDisks = Object.fromEntries(
			(info?.disks ?? [])
				.filter((disk) => disk.id)
				.map((disk) => [disk.id, disk.override == null ? INHERIT : String(disk.override)]),
		);
		error = '';
		editing = true;
	}

	async function save() {
		if (!status || busy) return;
		busy = true;
		error = '';
		const volumes = { ...(status.volumes ?? {}) };
		if (draftVolume === INHERIT) delete volumes[volume];
		else volumes[volume] = Number(draftVolume);
		const disks = { ...(status.disks ?? {}) };
		for (const [id, value] of Object.entries(draftDisks)) {
			if (value === INHERIT) delete disks[id];
			else disks[id] = Number(value);
		}
		try {
			const updated = await api.put<DiskSleepStatus>('/storage/disk-sleep', {
				idleMinutes: status.idleMinutes,
				volumes: overridesFrom(
					volumes,
					(status.volumeStatus ?? []).map((entry) => entry.name),
				),
				disks: overridesFrom(
					disks,
					(status.diskStatus ?? []).map((entry) => entry.id).filter(Boolean),
				),
			});
			editing = false;
			onsaved(updated);
		} catch (err) {
			error = apiErrorText(err);
		}
		busy = false;
	}
</script>

{#if info}
	<div class="block sleep">
		<div class="head">
			<span class="label">{t('storage.sleepCard')}</span>
			{#if info.total > 0}
				<span>{sleepDuration(info.minutes)}</span>
				<span class="muted small">({sleepOrigin(info.origin)})</span>
				<span class="chip" class:ok={info.asleep > 0}
					>{t('storage.sleepCount', { asleep: info.asleep, total: info.total })}</span
				>
				{#if canAdminister && !editing}
					<button type="button" class="btn small" onclick={open}>{t('storage.sleepChange')}</button>
				{/if}
			{:else}
				<span class="muted">{t('storage.sleepNoSleeper')}</span>
				{#if canAdminister}
					<button type="button" class="btn small" onclick={onopen}
						>{t('storage.sleepOpenHint')}</button
					>
				{/if}
			{/if}
		</div>
		{#if info.total === 0 && unavailable.length}
			<ul class="reasons muted small">
				{#each unavailable as disk (disk.name)}
					<li><span class="mono">{disk.name}</span>: {t(`storage.sleepReason.${disk.reason}`)}</li>
				{/each}
			</ul>
		{/if}
		{#if editing}
			<div class="editor">
				<label for="sleep-edit-volume-{volume}">
					{t('storage.sleepVolumeWhole')}
					<span class="muted small">{t('storage.sleepVolumeWholeHint')}</span>
				</label>
				<select
					id="sleep-edit-volume-{volume}"
					class="field"
					bind:value={draftVolume}
					disabled={busy}
				>
					<option value={INHERIT}>{t('storage.sleepInherit')}</option>
					{#each minutes as value}<option value={String(value)}>{sleepDuration(value)}</option
						>{/each}
				</select>
				{#each info.disks as disk (disk.name)}
					<label for="sleep-disk-{disk.name}-{volume}">
						<span class="mono">{disk.name}</span>
						<span class="muted small"
							>{disk.model} · {disk.state === 'asleep'
								? t('storage.sleepAsleep')
								: t('storage.sleepAwake')}</span
						>
					</label>
					<select
						id="sleep-disk-{disk.name}-{volume}"
						class="field"
						bind:value={draftDisks[disk.id]}
						disabled={busy || !disk.id}
					>
						<option value={INHERIT}>{t('storage.sleepVolumeOption')}</option>
						{#each minutes as value}<option value={String(value)}>{sleepDuration(value)}</option
							>{/each}
					</select>
				{/each}
			</div>
			<p class="muted small">{t('storage.sleepRaidNote')}</p>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="actions">
				<button type="button" class="btn btn-primary" onclick={save} disabled={busy}
					>{t('storage.sleepSave')}</button
				>
				<button type="button" class="btn" onclick={() => (editing = false)} disabled={busy}
					>{t('storage.sleepCancel')}</button
				>
			</div>
		{:else if info.total > 0}
			<ul class="members-sleep small">
				{#each info.disks as disk (disk.name)}
					<li>
						<span class="mono">{disk.name}</span>
						<span class="chip" class:ok={disk.state === 'asleep'}
							>{disk.state === 'asleep' ? t('storage.sleepAsleep') : t('storage.sleepAwake')}</span
						>
						{#if sleepSince(disk)}<span class="muted">{sleepSince(disk)}</span>{/if}
						<span class="muted">{sleepDuration(disk.effective)} · {sleepOrigin(disk.origin)}</span>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
{/if}

<style>
	.sleep {
		display: grid;
		gap: 10px;
		border-top: 1px solid var(--border);
		padding-top: var(--space-3);
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.label {
		font-weight: 600;
	}
	.small {
		font-size: var(--text-sm);
	}
	.members-sleep,
	.reasons {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 6px;
	}
	.members-sleep li {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.editor {
		display: grid;
		grid-template-columns: 1fr auto;
		gap: var(--space-2) var(--space-3);
		align-items: center;
	}
	.editor label {
		display: grid;
		gap: 2px;
	}
	.editor select {
		width: auto;
		min-width: 150px;
	}
	.actions {
		display: flex;
		gap: var(--space-2);
	}
	p {
		margin: 0;
	}
</style>
