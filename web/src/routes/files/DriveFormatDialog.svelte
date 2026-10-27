<script lang="ts">
	// Formatting a whole unmanaged drive: one partition with a new filesystem.
	// Every drive is listed, also new and empty ones; protected or busy ones
	// say why they cannot be formatted. The work runs on the NAS and goes on
	// when the dialog is closed; a notification tells how it ended.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		driveFilesystems,
		formatRunning,
		hasForeignData,
		labelProblem,
		resolvedScheme,
		sizeProblem,
		suggestedFilesystem,
		type Drive,
		type DriveFilesystem,
		type FormatJob,
		type Scheme,
	} from './drive-format.ts';

	type Props = { onclose: () => void; onformatted: () => void };
	let { onclose, onformatted }: Props = $props();

	let disks = $state<Drive[]>([]),
		job = $state<FormatJob | null>(null),
		loaded = $state(false),
		loadError = $state(''),
		selected = $state(''),
		filesystem = $state<DriveFilesystem>('exfat'),
		label = $state(''),
		scheme = $state<Scheme>('auto'),
		mount = $state(true),
		confirming = $state(false),
		busy = $state(false),
		error = $state(''),
		// The job this dialog started; its end reloads the drives of the page.
		started = $state('');

	const drive = $derived(disks.find((d) => d.id === selected));
	const labelError = $derived(labelProblem(filesystem, label));
	const sizeError = $derived(drive ? sizeProblem(filesystem, drive.size, scheme) : '');
	const table = $derived(drive ? resolvedScheme(filesystem, drive.size, scheme) : 'gpt');
	const running = $derived(formatRunning(job));

	async function load() {
		try {
			const result = await api.get<{ disks: Drive[]; job: FormatJob | null }>('/usb/disks');
			disks = result.disks;
			const ended = started !== '' && job && formatRunning(job) && !formatRunning(result.job);
			job = result.job;
			loadError = '';
			if (ended) onformatted();
			// A drive that changed or went away is no longer the choice.
			if (selected && !disks.some((d) => d.id === selected)) {
				selected = '';
				confirming = false;
			}
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			loaded = true;
		}
	}

	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && load(), 2000);
		return () => clearInterval(timer);
	});

	function choose(d: Drive) {
		if (!d.formattable || running) return;
		selected = d.id;
		filesystem = suggestedFilesystem(d);
		confirming = false;
		error = '';
	}

	function driveName(d: Drive) {
		return d.model || d.device.replace('/dev/', '');
	}

	function partitionText(d: Drive) {
		if (d.partitions.length === 0)
			return t(d.table ? 'files.formatEmptyTable' : 'files.formatEmpty');
		return d.partitions
			.map(
				(p) =>
					`${p.fsType === 'vfat' ? 'FAT' : p.fsType || t('files.formatNoFilesystem')}${p.label ? ` „${p.label}“` : ''} · ${formatDiskBytes(p.size)}`,
			)
			.join(', ');
	}

	async function format() {
		if (!drive || busy) return;
		busy = true;
		error = '';
		try {
			const result = await api.post<{ disks: Drive[]; job: FormatJob | null }>(
				`/usb/disks/${encodeURIComponent(drive.id)}/format`,
				{ filesystem, label: label.trim(), scheme, mount },
			);
			disks = result.disks;
			job = result.job;
			started = result.job?.disk ?? drive.id;
			confirming = false;
			selected = '';
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			// The list, reloaded below, shows why a drive cannot be formatted.
			error = errorText(code);
			confirming = false;
			load();
		} finally {
			busy = false;
		}
	}
</script>

<Dialog title={t('files.formatTitle')} {onclose} wide locked={busy}>
	<p class="muted small">{t('files.formatIntro')}</p>

	{#if job && (running || job.disk === started || job.phase === 'failed')}
		<div
			class={job.phase === 'failed' ? 'notice-error' : running ? 'notice-info' : 'notice-ok'}
			role="status"
			aria-live="polite"
		>
			<strong>{job.name}</strong> · <span class="mono">{job.filesystem}</span>
			— {t(`files.formatPhase.${job.phase}`)}{#if job.error}<br /><span class="mono small"
					>{job.error}</span
				>{/if}
		</div>
	{/if}

	{#if !loaded}<p class="muted">{t('common.loading')}</p>
	{:else if loadError}<p class="notice-error" role="alert">{loadError}</p>
	{:else if disks.length === 0}<p class="muted">{t('files.formatNone')}</p>
	{:else}
		<div class="drives" role="radiogroup" aria-label={t('files.formatChoose')}>
			{#each disks as d (d.id)}
				<button
					type="button"
					class="drive"
					role="radio"
					aria-checked={selected === d.id}
					class:selected={selected === d.id}
					disabled={!d.formattable || running}
					onclick={() => choose(d)}
				>
					<Icon name="disk" size={18} />
					<span class="drive-text">
						<span class="drive-name">{driveName(d)}</span>
						<span class="muted small mono"
							>{[
								t(`files.driveKind.${d.kind}`),
								d.transport.toUpperCase(),
								formatDiskBytes(d.size),
								d.device,
								d.serial,
							]
								.filter(Boolean)
								.join(' · ')}</span
						>
						<span class="muted small">{partitionText(d)}</span>
					</span>
					{#if d.reason}<span class="chip"
							>{t(`files.formatReason.${d.reason}`, { name: d.claimed ?? '' })}</span
						>{/if}
				</button>
			{/each}
		</div>
	{/if}

	{#if drive}
		<form
			onsubmit={(event) => {
				event.preventDefault();
				if (!labelError && !sizeError) confirming = true;
			}}
		>
			<label
				>{t('files.formatFilesystem')}
				<select class="field" bind:value={filesystem} disabled={busy}>
					{#each Object.keys(driveFilesystems) as DriveFilesystem[] as fs (fs)}
						<option value={fs}>{t(`files.formatFs.${fs}` as MessageKey)}</option>
					{/each}
				</select></label
			>
			<p class="muted small">{t(`files.formatFsHint.${filesystem}`)}</p>
			{#if filesystem === 'vfat' && drive.size > 32 * 2 ** 30}<p class="notice-warn small">
					{t('files.formatFatLarge')}
				</p>{/if}
			<label
				>{t('files.formatLabel')}<input
					class="field"
					bind:value={label}
					maxlength={driveFilesystems[filesystem].limit}
					autocomplete="off"
					spellcheck="false"
					placeholder={t('files.urlOptional')}
					disabled={busy}
					aria-invalid={labelError !== ''}
				/></label
			>
			{#if labelError}<p class="notice-warn small" role="status">
					{t(labelError as MessageKey, { n: driveFilesystems[filesystem].limit })}
				</p>{/if}
			<details>
				<summary>{t('files.formatAdvanced')}</summary>
				<label
					>{t('files.formatScheme')}
					<select class="field" bind:value={scheme} disabled={busy}>
						<option value="auto"
							>{t('files.formatSchemeAuto', { scheme: table.toUpperCase() })}</option
						>
						<option value="mbr">MBR</option>
						<option value="gpt">GPT</option>
					</select></label
				>
				<p class="muted small">{t('files.formatSchemeHint')}</p>
			</details>
			<label class="confirm"
				><input type="checkbox" bind:checked={mount} disabled={busy} />{t(
					'files.formatMount',
				)}</label
			>
			{#if sizeError}<p class="notice-warn small" role="status">
					{t(sizeError as MessageKey)}
				</p>{/if}
			{#if hasForeignData(drive)}<p class="notice-warn small">{t('files.formatForeign')}</p>{/if}

			{#if confirming}
				<div class="notice-error" role="alert">
					<p>
						{t('files.formatConfirm', {
							name: driveName(drive),
							size: formatDiskBytes(drive.size),
							device: drive.device,
						})}
					</p>
					<p class="small">{partitionText(drive)}</p>
				</div>
			{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() => (confirming ? (confirming = false) : onclose())}
					>{t('common.cancel')}</button
				>
				{#if confirming}
					<button type="button" class="btn btn-danger" disabled={busy} onclick={format}
						>{t('files.formatNow')}</button
					>
				{:else}
					<button class="btn btn-primary" disabled={busy || running || !!labelError || !!sizeError}
						>{t('files.formatStart')}</button
					>
				{/if}
			</div>
		</form>
	{:else}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.close')}</button>
		</div>
	{/if}
</Dialog>

<style>
	form {
		display: grid;
		gap: 0.7rem;
	}
	label:not(.confirm) {
		display: grid;
		gap: 0.3rem;
	}
	.small {
		font-size: 0.8125rem;
		overflow-wrap: anywhere;
	}
	.drives {
		display: grid;
		gap: 6px;
	}
	.drive {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 8px 10px;
		text-align: left;
		background: var(--surface);
		color: var(--text);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 8px);
		cursor: pointer;
	}
	.drive:disabled {
		cursor: default;
		opacity: 0.7;
	}
	.drive.selected {
		border-color: var(--accent);
		box-shadow: 0 0 0 1px var(--accent);
	}
	.drive-text {
		display: grid;
		gap: 2px;
		flex: 1;
		min-width: 0;
	}
	.drive-name {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.notice-error p {
		margin: 0 0 0.4rem;
	}
	.notice-info {
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 8px);
		background: var(--surface-2, var(--surface));
	}
	details summary {
		cursor: pointer;
	}
</style>
