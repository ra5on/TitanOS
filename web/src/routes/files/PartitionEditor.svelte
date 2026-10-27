<script lang="ts">
	// The partitions of one unmanaged drive: a bar of partitions and free
	// space, a table, and an action per partition or gap. The NAS does the
	// work in the background and checks the drive again right before it
	// writes; the drive's ID changes with every change, so the editor finds
	// the same drive again by device, serial, model and size.
	import { untrack } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		driveFilesystems,
		formatRunning,
		labelProblem,
		type Drive,
		type DriveFilesystem,
		type FormatJob,
	} from './drive-format.ts';
	import {
		createProblem,
		growLimit,
		growProblem,
		MiB,
		mibToBytes,
		sameDrive,
		segments,
		typeName,
		type PartitionGap,
		type PartitionLayout,
		type PartitionSlot,
	} from './partitions.ts';

	type Props = { drive: Drive; onback: () => void; onchanged: () => void };
	let { drive, onback, onchanged }: Props = $props();

	type Action =
		| { kind: 'table' }
		| { kind: 'create'; gap: PartitionGap }
		| { kind: 'delete' | 'format' | 'label' | 'grow'; part: PartitionSlot };

	let id = $state(untrack(() => drive.id)),
		current = $state(untrack(() => drive)),
		layout = $state<PartitionLayout | null>(null),
		loadError = $state(''),
		selected = $state(''),
		action = $state<Action | null>(null),
		scheme = $state<'gpt' | 'dos'>('gpt'),
		filesystem = $state<DriveFilesystem | ''>('exfat'),
		label = $state(''),
		size = $state(''),
		rest = $state(true),
		confirming = $state(false),
		busy = $state(false),
		actionError = $state('');

	const job = $derived(layout?.job?.device === current.device ? layout.job : null);
	const running = $derived(formatRunning(layout?.job));
	const rows = $derived(layout ? segments(layout) : []);
	const diskName = $derived(current.model || current.device.replace('/dev/', ''));
	const locked = $derived(!layout || !!layout.reason || running);

	async function refind(): Promise<boolean> {
		const result = await api.get<{ disks: Drive[] }>('/usb/disks');
		const match = result.disks.find((d) => sameDrive(d, current));
		if (!match || match.id === id) return false;
		id = match.id;
		return true;
	}

	async function load(again = true) {
		try {
			const before = formatRunning(layout?.job);
			const result = await api.get<PartitionLayout>(
				`/usb/disks/${encodeURIComponent(id)}/partitions`,
			);
			layout = result;
			current = result.disk;
			loadError = '';
			if (before && !formatRunning(result.job)) onchanged();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			if (code === 'usb_not_found' && again) {
				try {
					if (await refind()) return load(false);
				} catch {
					// Shown below.
				}
				loadError = t('files.partitions.gone');
				return;
			}
			loadError = errorText(code);
		}
	}

	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && !busy && load(), 2000);
		return () => clearInterval(timer);
	});

	function partName(p: PartitionSlot) {
		return `${p.device.replace('/dev/', '')} (${t('files.partitions.colNumber')} ${p.number})`;
	}

	function fsName(fs: string) {
		if (!fs) return t('files.formatNoFilesystem');
		return fs === 'vfat' ? 'FAT' : fs;
	}

	function reasonText(reason: string) {
		return t(`files.formatReason.${reason}` as MessageKey, { name: current.claimed ?? '' });
	}

	function open(next: Action) {
		action = next;
		confirming = false;
		actionError = '';
		label = '';
		rest = true;
		scheme = current.size > 2 * 2 ** 40 ? 'gpt' : layout?.table === 'dos' ? 'dos' : 'gpt';
		filesystem = 'exfat';
		if (next.kind === 'create') size = String(Math.floor(next.gap.size / MiB));
		if (next.kind === 'format' || next.kind === 'label') {
			label = next.part.label;
			const fs = next.part.fsType as DriveFilesystem;
			if (fs in driveFilesystems) filesystem = fs;
		}
		if (next.kind === 'grow') size = String(Math.floor(growLimit(next.part) / MiB));
	}

	/** The label rules of the filesystem a label is for. */
	const labelFs = $derived.by((): DriveFilesystem | '' => {
		if (action?.kind === 'label') {
			const fs = action.part.fsType;
			if (fs === 'ext2' || fs === 'ext3') return 'ext4';
			return fs in driveFilesystems ? (fs as DriveFilesystem) : '';
		}
		return filesystem;
	});
	const labelError = $derived(labelFs ? labelProblem(labelFs, label) : '');
	const createSize = $derived(
		action?.kind === 'create'
			? rest
				? Math.floor(action.gap.size / MiB) * MiB
				: mibToBytes(size)
			: 0,
	);
	const createError = $derived(
		action?.kind === 'create' && layout
			? createProblem(action.gap, createSize, filesystem, layout.partitions.length, layout.table)
			: '',
	);
	const growSize = $derived(
		action?.kind === 'grow' ? (rest ? growLimit(action.part) : mibToBytes(size)) : 0,
	);
	const growError = $derived(action?.kind === 'grow' ? growProblem(action.part, growSize) : '');
	const formError = $derived.by(() => {
		if (!action) return '';
		if (action.kind === 'create') return createError || (filesystem ? labelError : '');
		if (action.kind === 'grow') return growError;
		if (action.kind === 'label')
			return label.trim() === '' ? 'files.partitions.labelEmpty' : labelError;
		if (action.kind === 'format') return labelError;
		return '';
	});
	const destructive = $derived(
		action?.kind === 'table' || action?.kind === 'delete' || action?.kind === 'format',
	);

	async function submit() {
		if (!action || busy || formError) return;
		busy = true;
		actionError = '';
		const base = `/usb/disks/${encodeURIComponent(id)}`;
		try {
			let result: { job: FormatJob | null };
			switch (action.kind) {
				case 'table':
					result = await api.post(`${base}/table`, { scheme });
					break;
				case 'create':
					result = await api.post(`${base}/partitions`, {
						start: action.gap.start,
						size: rest ? 0 : createSize,
						filesystem,
						label: filesystem ? label.trim() : '',
					});
					break;
				case 'delete':
					result = await api.delete(`${base}/partitions/${action.part.number}`);
					break;
				case 'format':
					result = await api.post(`${base}/partitions/${action.part.number}/format`, {
						filesystem,
						label: label.trim(),
					});
					break;
				case 'label':
					result = await api.put(`${base}/partitions/${action.part.number}/label`, {
						label: label.trim(),
					});
					break;
				case 'grow':
					result = await api.post(`${base}/partitions/${action.part.number}/grow`, {
						size: rest ? 0 : growSize,
					});
					break;
			}
			if (layout) layout.job = result.job;
			action = null;
		} catch (err) {
			actionError = errorText(err instanceof ApiError ? err.code : 'internal');
			confirming = false;
		} finally {
			busy = false;
		}
		load();
	}

	const actionTitle: Record<Action['kind'], MessageKey> = {
		table: 'files.partitions.tableTitle',
		create: 'files.partitions.createTitle',
		delete: 'files.partitions.deleteTitle',
		format: 'files.partitions.formatTitle',
		label: 'files.partitions.labelTitle',
		grow: 'files.partitions.growTitle',
	};
</script>

<p class="muted small">{t('files.partitions.intro')}</p>

<div class="head">
	<span class="muted small mono"
		>{[
			diskName,
			formatDiskBytes(current.size),
			current.device,
			layout?.table ? layout.table.toUpperCase().replace('DOS', 'MBR') : '',
			layout && layout.table ? t('files.partitions.sector', { n: layout.sectorSize }) : '',
		]
			.filter(Boolean)
			.join(' · ')}</span
	>
	<button
		type="button"
		class="btn"
		disabled={!layout || running || !current.formattable}
		title={current.formattable ? '' : reasonText(current.reason ?? 'inuse')}
		onclick={() => open({ kind: 'table' })}>{t('files.partitions.newTable')}</button
	>
</div>

{#if job}
	<div
		class={job.phase === 'failed' ? 'notice-error' : running ? 'notice-info' : 'notice-ok'}
		role="status"
		aria-live="polite"
	>
		<strong>{t(`files.partitions.action.${job.action ?? 'format'}`)}</strong>
		{#if job.partition}<span class="mono"> {job.partition}</span>{/if}
		— {job.phase === 'done'
			? t('files.partitions.done')
			: job.phase === 'failed'
				? t('files.partitions.failed')
				: t(`files.formatPhase.${job.phase}`)}{#if job.error}<br /><span class="mono small"
				>{job.error}</span
			>{/if}
	</div>
{/if}

{#if loadError}<p class="notice-error" role="alert">{loadError}</p>
{:else if !layout}<p class="muted">{t('common.loading')}</p>
{:else}
	{#if layout.reason === 'extended'}<p class="notice-warn small">
			{t('files.partitions.extendedHint')}
		</p>
	{:else if layout.reason === 'table'}<p class="notice-warn small">
			{t('files.partitions.tableUnknown')}
		</p>
	{:else if layout.reason}<p class="notice-warn small">{reasonText(layout.reason)}</p>{/if}

	{#if !layout.table}
		<p class="muted">{t('files.partitions.tableNone')}</p>
	{:else}
		<div class="bar" role="group" aria-label={t('files.partitions.bar')}>
			{#each rows as row (row.kind + row.start)}
				<button
					type="button"
					class="segment {row.kind}"
					class:busy={row.kind === 'part' && !!row.part.reason}
					class:selected={selected === row.kind + row.start}
					style:flex-grow={row.percent}
					title={row.kind === 'part'
						? `${partName(row.part)} · ${formatDiskBytes(row.size)} · ${fsName(row.part.fsType)}`
						: `${t('files.partitions.free')} · ${formatDiskBytes(row.size)}`}
					aria-label={row.kind === 'part'
						? `${partName(row.part)}, ${formatDiskBytes(row.size)}`
						: `${t('files.partitions.free')}, ${formatDiskBytes(row.size)}`}
					onclick={() => (selected = row.kind + row.start)}
					><span
						>{row.kind === 'part'
							? row.part.label || row.part.device.replace('/dev/', '')
							: ''}</span
					></button
				>
			{/each}
		</div>

		<div class="table-wrap">
			<table>
				<thead>
					<tr>
						<th>{t('files.partitions.colNumber')}</th>
						<th>{t('files.partitions.colStart')}</th>
						<th>{t('files.partitions.colSize')}</th>
						<th>{t('files.partitions.colType')}</th>
						<th>{t('files.partitions.colFs')}</th>
						<th>{t('files.partitions.colLabel')}</th>
						<th>{t('files.partitions.colActions')}</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as row (row.kind + row.start)}
						<tr
							class:selected={selected === row.kind + row.start}
							onclick={() => (selected = row.kind + row.start)}
						>
							{#if row.kind === 'part'}
								{@const p = row.part}
								<td class="mono">{p.number}<br /><span class="muted small">{p.device}</span></td>
								<td class="mono">{formatDiskBytes(p.start)}</td>
								<td class="mono">{formatDiskBytes(p.size)}</td>
								<td class="type">{typeName(layout.table, p.type)}</td>
								<td class="mono">{fsName(p.fsType)}</td>
								<td class="label">{p.label}</td>
								<td
									><div class="actions">
										{#if p.reason}<span class="chip">{reasonText(p.reason)}</span>{/if}
										<button
											type="button"
											class="btn compact"
											disabled={!!p.reason || running}
											onclick={() => open({ kind: 'format', part: p })}
											>{t('files.partitions.format')}</button
										>
										{#if p.renamable}<button
												type="button"
												class="btn compact"
												disabled={!!p.reason || running}
												onclick={() => open({ kind: 'label', part: p })}
												>{t('files.partitions.label')}</button
											>{/if}
										{#if p.growBy > 0}<button
												type="button"
												class="btn compact"
												disabled={!!p.reason || locked}
												onclick={() => open({ kind: 'grow', part: p })}
												>{t('files.partitions.grow')}</button
											>{/if}
										<button
											type="button"
											class="btn compact btn-danger"
											disabled={!!p.reason || locked}
											onclick={() => open({ kind: 'delete', part: p })}
											>{t('files.partitions.delete')}</button
										>
									</div></td
								>
							{:else}
								<td class="muted">–</td>
								<td class="mono">{formatDiskBytes(row.start)}</td>
								<td class="mono">{formatDiskBytes(row.size)}</td>
								<td class="muted" colspan="3">{t('files.partitions.free')}</td>
								<td
									><div class="actions">
										<button
											type="button"
											class="btn compact"
											disabled={locked}
											onclick={() => row.kind === 'gap' && open({ kind: 'create', gap: row.gap })}
											>{t('files.partitions.create')}</button
										>
									</div></td
								>
							{/if}
						</tr>
					{:else}
						<tr><td colspan="7" class="muted">{t('files.partitions.empty')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
{/if}

<div class="dialog-actions">
	<button type="button" class="btn" onclick={onback}>{t('files.partitions.back')}</button>
</div>

{#if action && layout}
	{@const a = action}
	<Dialog title={t(actionTitle[a.kind])} onclose={() => (action = null)} locked={busy}>
		<form
			onsubmit={(event) => {
				event.preventDefault();
				if (formError) return;
				if (destructive) confirming = true;
				else submit();
			}}
		>
			{#if a.kind === 'table'}
				<label
					>{t('files.formatScheme')}
					<select class="field" bind:value={scheme} disabled={busy}>
						<option value="gpt">GPT</option>
						<option value="dos" disabled={current.size > 2 * 2 ** 40}>MBR</option>
					</select></label
				>
				<p class="muted small">{t('files.partitions.tableHint')}</p>
			{/if}

			{#if a.kind === 'create'}
				<p class="small">
					{t('files.partitions.createIn', {
						start: formatDiskBytes(a.gap.start),
						size: formatDiskBytes(a.gap.size),
					})}
				</p>
			{/if}

			{#if a.kind === 'create' || a.kind === 'grow'}
				<label class="confirm"
					><input type="checkbox" bind:checked={rest} disabled={busy} />{a.kind === 'create'
						? t('files.partitions.rest')
						: t('files.partitions.growMax', { size: formatDiskBytes(growLimit(a.part)) })}</label
				>
				{#if !rest}
					<label
						>{a.kind === 'create'
							? t('files.partitions.size')
							: t('files.partitions.growTo', { max: Math.floor(growLimit(a.part) / MiB) })}<input
							class="field mono"
							inputmode="numeric"
							bind:value={size}
							disabled={busy}
						/></label
					>
					<p class="muted small">{t('files.partitions.sizeHint')}</p>
				{/if}
			{/if}

			{#if a.kind === 'grow'}<p class="muted small">{t('files.partitions.growHint')}</p>{/if}

			{#if a.kind === 'create' || a.kind === 'format'}
				<label
					>{t('files.formatFilesystem')}
					<select class="field" bind:value={filesystem} disabled={busy}>
						{#if a.kind === 'create'}<option value="">{t('files.partitions.fsNone')}</option>{/if}
						{#each Object.keys(driveFilesystems) as DriveFilesystem[] as fs (fs)}
							<option value={fs}>{t(`files.formatFs.${fs}` as MessageKey)}</option>
						{/each}
					</select></label
				>
				{#if filesystem}<p class="muted small">{t(`files.formatFsHint.${filesystem}`)}</p>{/if}
			{/if}

			{#if (a.kind === 'create' && filesystem) || a.kind === 'format' || a.kind === 'label'}
				<label
					>{t('files.formatLabel')}<input
						class="field"
						bind:value={label}
						autocomplete="off"
						spellcheck="false"
						placeholder={a.kind === 'label' ? '' : t('files.urlOptional')}
						disabled={busy}
						aria-invalid={labelError !== ''}
					/></label
				>
				{#if a.kind === 'label'}<p class="muted small">{t('files.partitions.labelHint')}</p>{/if}
			{/if}

			{#if formError}<p class="notice-warn small" role="status">
					{t(formError as MessageKey, {
						n: labelFs ? driveFilesystems[labelFs].limit : 0,
					})}
				</p>{/if}

			{#if a.kind === 'grow' && !growError}
				<p class="small">
					{t('files.partitions.growConfirm', {
						part: partName(a.part),
						disk: diskName,
						device: current.device,
						from: formatDiskBytes(a.part.size),
						to: formatDiskBytes(growSize),
					})}
				</p>
			{/if}

			{#if confirming}
				<div class="notice-error" role="alert">
					{#if a.kind === 'table'}
						{t('files.partitions.tableConfirm', {
							disk: diskName,
							size: formatDiskBytes(current.size),
							device: current.device,
							scheme: scheme === 'dos' ? 'MBR' : 'GPT',
						})}
					{:else if a.kind === 'delete'}
						{t('files.partitions.deleteConfirm', {
							part: partName(a.part),
							size: formatDiskBytes(a.part.size),
							fs: fsName(a.part.fsType),
							disk: diskName,
							device: current.device,
						})}
					{:else if a.kind === 'format'}
						{t('files.partitions.formatConfirm', {
							part: partName(a.part),
							size: formatDiskBytes(a.part.size),
							fs: fsName(filesystem),
							disk: diskName,
							device: current.device,
						})}
					{/if}
				</div>
			{:else if a.kind === 'delete'}
				<p class="small">
					{partName(a.part)} · {formatDiskBytes(a.part.size)} · {fsName(a.part.fsType)}{a.part.label
						? ` „${a.part.label}“`
						: ''}
				</p>
			{/if}

			{#if actionError}<p class="notice-error" role="alert">{actionError}</p>{/if}
			<div class="dialog-actions">
				<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() => (confirming ? (confirming = false) : (action = null))}
					>{t('common.cancel')}</button
				>
				{#if confirming}
					<button type="button" class="btn btn-danger" disabled={busy} onclick={submit}
						>{t(
							a.kind === 'table'
								? 'files.partitions.tableNow'
								: a.kind === 'delete'
									? 'files.partitions.deleteNow'
									: 'files.formatNow',
						)}</button
					>
				{:else}
					<button class="btn btn-primary" disabled={busy || running || !!formError}
						>{t(
							destructive
								? 'files.partitions.continue'
								: a.kind === 'create'
									? 'files.partitions.createNow'
									: a.kind === 'grow'
										? 'files.partitions.growNow'
										: 'files.partitions.labelNow',
						)}</button
					>
				{/if}
			</div>
		</form>
	</Dialog>
{/if}

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
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		flex-wrap: wrap;
	}
	.bar {
		display: flex;
		gap: 2px;
		width: 100%;
		height: 40px;
	}
	.segment {
		flex-basis: 0;
		font-family: inherit;
		min-width: 6px;
		padding: 0 4px;
		overflow: hidden;
		font-size: 0.75rem;
		white-space: nowrap;
		text-overflow: ellipsis;
		border-radius: var(--radius-small);
		border: 1px solid var(--border);
		cursor: pointer;
	}
	.segment span {
		display: block;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.segment.part {
		background: var(--accent);
		color: var(--on-accent);
		border-color: var(--accent);
	}
	.segment.part.busy {
		background: var(--muted);
		border-color: var(--muted);
		color: var(--surface);
	}
	.segment.gap {
		background: none;
		border: 1px dashed var(--muted);
		color: var(--muted);
	}
	.segment.selected {
		outline: 2px solid var(--text);
		outline-offset: 1px;
	}
	.table-wrap {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.875rem;
	}
	th,
	td {
		padding: 6px 8px;
		text-align: left;
		vertical-align: top;
		border-bottom: 1px solid var(--border);
	}
	th {
		font-weight: 600;
		color: var(--muted);
	}
	td.type {
		min-width: 8rem;
		font-size: 0.8125rem;
	}
	th,
	td.mono,
	td.label {
		white-space: nowrap;
	}
	tr.selected td {
		background: var(--chip);
	}
	.compact {
		padding: 3px 8px;
		font-size: 0.8125rem;
	}
	.actions {
		min-width: 14rem;
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		align-items: center;
	}
	.notice-info {
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm, 8px);
		background: var(--surface-2, var(--surface));
	}
</style>
