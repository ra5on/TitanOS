<script lang="ts">
	// Speicher › Ruhemodus für Festplatten: the default, then optional values
	// per volume and per disk (disk > volume > default). Saving sends only
	// existing volumes and disks, so overrides of removed ones go on save.
	import { untrack } from 'svelte';
	import { api, ApiError, type DiskSleepOrigin, type DiskSleepStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { effectiveSleep, overridesFrom, shortSerial } from './disk-sleep.ts';

	type Props = {
		status: DiskSleepStatus | undefined;
		onclose: () => void;
		onsaved: (status: DiskSleepStatus) => void;
	};
	let { status, onclose, onsaved }: Props = $props();

	const idleMinutes = [0, 5, 10, 15, 30, 60, 120, 180];
	const INHERIT = 'inherit';

	// The draft starts from the settings when the dialog opens; status
	// updates while it is open only refresh the lists and the disk states.
	let draftDefault = $state(untrack(() => status?.idleMinutes ?? 0));
	let draftVolumes = $state<Record<string, number>>(
		untrack(() => ({ ...(status?.volumes ?? {}) })),
	);
	let draftDisks = $state<Record<string, number>>(untrack(() => ({ ...(status?.disks ?? {}) })));
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);

	const listed = $derived(status?.volumeStatus !== undefined && status?.diskStatus !== undefined);
	const volumes = $derived(status?.volumeStatus ?? []);
	const disks = $derived(status?.diskStatus ?? []);
	const missing = $derived(
		(status?.missingVolumes?.length ?? 0) + (status?.missingDisks?.length ?? 0),
	);
	const body = $derived({
		idleMinutes: draftDefault,
		...(listed
			? {
					volumes: overridesFrom(
						draftVolumes,
						volumes.map((volume) => volume.name),
					),
					disks: overridesFrom(draftDisks, disks.map((disk) => disk.id).filter(Boolean)),
				}
			: {}),
	});
	const changed = $derived(
		JSON.stringify(body) !==
			JSON.stringify({
				idleMinutes: status?.idleMinutes ?? 0,
				...(listed
					? {
							volumes: overridesFrom(
								status?.volumes ?? {},
								volumes.map((volume) => volume.name),
							),
							disks: overridesFrom(
								status?.disks ?? {},
								disks.map((disk) => disk.id).filter(Boolean),
							),
						}
					: {}),
			}) ||
			(listed && missing > 0),
	);

	function duration(minutes: number): string {
		return minutes === 0
			? t('storage.sleepOff')
			: minutes < 60
				? t('storage.sleepMinutes', { count: minutes })
				: minutes === 60
					? t('storage.sleepHour')
					: t('storage.sleepHours', { count: minutes / 60 });
	}
	function origin(from: DiskSleepOrigin): string {
		return t(
			from === 'disk'
				? 'storage.sleepFromDisk'
				: from === 'volume'
					? 'storage.sleepFromVolume'
					: 'storage.sleepFromDefault',
		);
	}
	function choose(target: Record<string, number>, key: string, value: string) {
		if (value === INHERIT) delete target[key];
		else target[key] = Number(value);
		saved = false;
	}

	async function save() {
		if (busy || !changed) return;
		busy = true;
		error = '';
		try {
			const updated = await api.put<DiskSleepStatus>('/storage/disk-sleep', body);
			draftDefault = updated.idleMinutes;
			draftVolumes = { ...(updated.volumes ?? {}) };
			draftDisks = { ...(updated.disks ?? {}) };
			saved = true;
			onsaved(updated);
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		busy = false;
	}
</script>

<Dialog title={t('storage.sleepTitle')} wide {onclose}>
	<div class="disk-sleep">
		<p class="muted small">{t('storage.sleepHint')}</p>
		<div class="sleep-default">
			<label for="disk-sleep-time">{t('storage.sleepDefault')}</label>
			<select
				class="field"
				id="disk-sleep-time"
				value={draftDefault}
				onchange={(event) => {
					draftDefault = Number(event.currentTarget.value);
					saved = false;
				}}
				disabled={busy || !status?.available}
			>
				{#each idleMinutes as minutes}<option value={minutes}>{duration(minutes)}</option>{/each}
			</select>
		</div>

		{#if listed && volumes.length}
			<section aria-labelledby="sleep-volumes">
				<h3 id="sleep-volumes" class="sleep-heading">{t('storage.sleepVolumes')}</h3>
				<ul class="sleep-list">
					{#each volumes as volume (volume.name)}
						{@const own = draftVolumes[volume.name]}
						{@const effective = own ?? draftDefault}
						<li>
							<div class="sleep-name">
								<strong>{volume.name}</strong>
								<span class="muted small"
									>{t('storage.sleepEffective', {
										value: duration(effective),
										origin: origin(own === undefined ? 'default' : 'volume'),
									})}</span
								>
							</div>
							<select
								class="field"
								aria-label={t('storage.sleepVolumeLabel', { volume: volume.name })}
								value={own === undefined ? INHERIT : String(own)}
								onchange={(event) => choose(draftVolumes, volume.name, event.currentTarget.value)}
								disabled={busy || !status?.available}
							>
								<option value={INHERIT}>{t('storage.sleepInherit')}</option>
								{#each idleMinutes as minutes}<option value={String(minutes)}
										>{duration(minutes)}</option
									>{/each}
							</select>
						</li>
					{/each}
				</ul>
				<p class="muted small">{t('storage.sleepRaidNote')}</p>
			</section>
		{/if}

		{#if listed}
			<section aria-labelledby="sleep-disks">
				<h3 id="sleep-disks" class="sleep-heading">{t('storage.sleepDisks')}</h3>
				{#if disks.length === 0}
					<p class="muted small">{t('storage.sleepNoDisks')}</p>
				{:else}
					<ul class="sleep-list">
						{#each disks as disk (disk.name)}
							{@const own = disk.id ? draftDisks[disk.id] : undefined}
							{@const result = effectiveSleep(draftDefault, draftVolumes, disk.volume, own)}
							<li>
								<div class="sleep-name">
									<strong>{disk.model || disk.name}</strong>
									<span class="muted small">
										<span class="mono">{disk.name}</span>
										{#if disk.serial}· <span class="mono" title={disk.serial}
												>{shortSerial(disk.serial)}</span
											>{/if}
										· {formatDiskBytes(disk.sizeBytes)}
										· {disk.volume
											? t('storage.sleepInVolume', { volume: disk.volume })
											: t('storage.sleepNoVolume')}
									</span>
									<span class="muted small"
										>{t('storage.sleepEffective', {
											value: duration(result.minutes),
											origin: origin(result.origin),
										})}</span
									>
									<span class="sleep-state">
										<span class="chip" class:ok={disk.state === 'asleep'}
											>{t(
												disk.state === 'asleep' ? 'storage.sleepAsleep' : 'storage.sleepAwake',
											)}</span
										>
										{#if disk.error}<span class="chip error" title={disk.error}
												>{t('storage.sleepStandbyFailed')}</span
											>{/if}
									</span>
									{#if !disk.id}<span class="muted small">{t('storage.sleepNoIdentity')}</span>{/if}
								</div>
								<select
									class="field"
									aria-label={t('storage.sleepDiskLabel', { disk: disk.model || disk.name })}
									value={own === undefined ? INHERIT : String(own)}
									onchange={(event) => choose(draftDisks, disk.id, event.currentTarget.value)}
									disabled={busy || !status?.available || !disk.id}
								>
									<option value={INHERIT}>{t('storage.sleepInherit')}</option>
									{#each idleMinutes as minutes}<option value={String(minutes)}
											>{duration(minutes)}</option
										>{/each}
								</select>
							</li>
						{/each}
					</ul>
				{/if}
				{#if missing > 0}<p class="notice-warn small">
						{t('storage.sleepMissing', { count: missing })}
					</p>{/if}
			</section>
		{/if}

		<p class="muted small">{t('storage.sleepDetails')}</p>
		{#if status && !status.available}<p class="notice-error" role="alert">
				{t('storage.sleepUnavailable')}
			</p>{/if}
		{#if status?.errors.length}
			<div class="notice-error" role="alert">
				<p>{t('storage.sleepFailed')}</p>
				{#each status.errors as problem}<p>
						<strong class="mono">{problem.disk}</strong>: {problem.message}
					</p>{/each}
			</div>
		{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	</div>
	<div class="dialog-actions">
		{#if saved && !changed}<span class="muted small" role="status">{t('storage.sleepSaved')}</span
			>{/if}
		<button class="btn" onclick={onclose}>{t('common.close')}</button>
		<button
			type="button"
			class="btn btn-primary"
			onclick={save}
			disabled={busy || !status?.available || !changed}>{t('common.save')}</button
		>
	</div>
</Dialog>

<style>
	.disk-sleep {
		display: grid;
		gap: 16px;
	}
	.sleep-default {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.sleep-default select,
	.sleep-list select {
		width: auto;
		max-width: 100%;
	}
	.sleep-heading {
		margin: 0 0 8px;
		font-size: 0.9rem;
	}
	.sleep-list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
	}
	.sleep-list li {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
		padding: 8px 0;
		border-top: 1px solid var(--border);
	}
	.sleep-list li:first-child {
		border-top: 0;
	}
	.sleep-name {
		display: grid;
		gap: 2px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.sleep-state {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}
	.small {
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
</style>
