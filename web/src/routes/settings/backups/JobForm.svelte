<script lang="ts">
	// The form of a backup job, new or changed: what is saved, where to,
	// when, how fast and how many versions stay.
	import { untrack } from 'svelte';
	import { api } from '#lib/api.ts';
	import {
		bandwidthForm,
		bandwidthPayload,
		maxBandwidth,
		type BandwidthForm,
	} from '#lib/backup-bandwidth.ts';
	import FolderPicker from '#lib/components/FolderPicker.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import KeepVersions, { keepPayload } from '#lib/components/KeepVersions.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import ApplicationSelection from '#lib/operations/ApplicationSelection.svelte';
	import {
		chosenOf,
		decodeCode,
		destinationPayload,
		EXTRAS,
		fullTarget,
		plainCopy,
		type Destination,
		type Job,
		type Overview,
		type UsbDrive,
	} from './data-backups.ts';
	import JobDestination from './JobDestination.svelte';
	import VmDisksChoice from './VmDisksChoice.svelte';
	import ScheduleFields from './ScheduleFields.svelte';
	import { selectionOfIn, shareOfIn, type Chosen } from './selection.ts';

	let {
		draft = $bindable(),
		error = $bindable(),
		busy = $bindable(),
		opened,
		isNew,
		secretSet,
		overview,
		volumes,
		usbDrives,
		usbError,
		loadUSB,
		publicKey,
		showKey,
		problem,
		onsaved,
		oncancel,
	}: {
		/** The job being created (isNew) or changed. */
		draft: Job;
		error: { text: string; detail: string } | null;
		busy: boolean;
		/** Counts up each time the page opens a job in the form. */
		opened: number;
		isNew: boolean;
		secretSet: Record<string, boolean>;
		overview: Overview;
		volumes: string[];
		usbDrives: UsbDrive[];
		usbError: string;
		loadUSB: () => void;
		publicKey: string;
		showKey: () => void;
		problem: (err: unknown) => void;
		onsaved: (next: Overview, created: Job | null) => void;
		oncancel: () => void;
	} = $props();

	// The destination as it was stored: a stored secret only stays for the
	// same server and account (the NAS does not send it anywhere else).
	const stored = (): Destination | null => (isNew ? null : { ...draft.destination });
	let original = $state<Destination | null>(stored());
	const secretKept = $derived.by(() => {
		const d = draft?.destination;
		if (!d || !original || d.type !== original.type) return { secretKey: false, password: false };
		return {
			secretKey:
				!!secretSet.secretKey &&
				d.endpoint?.trim() === original.endpoint &&
				d.accessKey?.trim() === original.accessKey,
			password:
				!!secretSet.password &&
				d.url?.trim() === original.url &&
				(d.username?.trim() ?? '') === (original.username ?? ''),
		};
	});

	let patterns = $state(untrack(() => (draft.patterns ?? []).join('\n')));
	let bandwidth = $state<BandwidthForm>(untrack(() => bandwidthForm(draft.bandwidth)));
	/** What is saved, as one list of folders (decided 31.10.2026). */
	let chosen = $state<Chosen[]>(untrack(() => chosenOf(draft, overview.shares)));
	/** The row whose folder is being picked; -1 adds a new one. */
	let picking = $state<number | null>(null);
	let pairCode = $state('');

	// The page opens another job while the form is shown: it starts over with that one.
	let shown = untrack(() => opened);
	$effect.pre(() => {
		if (opened === shown) return;
		shown = opened;
		untrack(() => {
			original = stored();
			chosen = chosenOf(draft, overview.shares);
			patterns = (draft.patterns ?? []).join('\n');
			bandwidth = bandwidthForm(draft.bandwidth);
		});
	});

	const shareOf = (c: Chosen) => shareOfIn(overview.shares, c);
	const selectionOf = (list: Chosen[]) => selectionOfIn(overview.shares, list);
	// What the chosen places mean for the job, for the list of VM disks.
	const selection = $derived(selectionOf(chosen));

	function pick(c: Chosen) {
		if (picking === null) return;
		if (picking < 0) chosen = [...chosen.filter((x) => x.volume), c];
		else chosen[picking] = c;
		picking = null;
	}

	function toggleExtra(extra: string) {
		if (!draft) return;
		const list = draft.extras ?? [];
		draft.extras = list.includes(extra) ? list.filter((e) => e !== extra) : [...list, extra];
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = true;
		error = null;
		if (draft.destination.type === 'titanos' && !decodeCode(pairCode)) {
			error = { text: t('backup.pairCodeInvalid'), detail: '' };
			busy = false;
			return;
		}
		// Left-out folders of shares that are no longer chosen go too.
		const skip = (draft.skip ?? []).filter((entry) =>
			selection.sources.includes(entry.split('/')[0]),
		);
		const times = [...new Set((draft.times ?? []).filter(Boolean))].sort();
		const weekdays = [...(draft.weekdays ?? [])].sort();
		const job = {
			...draft,
			sources: selection.sources,
			folders: selection.folders,
			skip,
			patterns: patterns
				.split('\n')
				.map((line) => line.trim())
				.filter(Boolean),
			extras: draft.extras ?? [],
			id: draft.id.trim(),
			interval: Number(draft.interval) || 0,
			monthDay: Number(draft.monthDay) || 0,
			times,
			time: times[0] ?? draft.time,
			weekdays,
			weekday: weekdays[0] ?? 0,
			windowFrom: draft.windowFrom || '',
			windowTo: draft.windowFrom ? draft.windowTo || '' : '',
			destination: destinationPayload(draft.destination, usbDrives, pairCode),
			bandwidth: bandwidthPayload(bandwidth),
			keep:
				plainCopy(draft.destination.type) && !draft.copy?.dated
					? { daily: 0, weekly: 0, monthly: 0 }
					: keepPayload(draft.keep),
			copy: plainCopy(draft.destination.type)
				? {
						dated: !!draft.copy?.dated,
						mirror: !draft.copy?.dated && !!draft.copy?.mirror,
						mirrorDays: draft.copy?.mirror ? Number(draft.copy.mirrorDays) || 30 : 0,
					}
				: undefined,
			volumes: selection.volumes,
			places: selection.places,
			// Only readable copies have the choice for disks of running VMs.
			vmDisks: plainCopy(draft.destination.type) ? draft.vmDisks : undefined,
		};
		try {
			const next = isNew
				? await api.post<Overview>('/backups', job)
				: await api.put<Overview>(`/backups/${job.id}`, job);
			onsaved(next, isNew ? (job as Job) : null);
		} catch (err) {
			problem(err);
		}
		busy = false;
	}
</script>

<form class="srow panel job-form" onsubmit={save}>
	<h2 class="sgroup-title">
		{isNew ? t('backup.add') : t('backup.editTitle', { name: draft.id })}
	</h2>
	{#if isNew}
		<label class="narrow"
			><span>{t('backup.name')}</span>
			<input
				class="field mono"
				type="text"
				bind:value={draft.id}
				pattern="[A-Za-z][A-Za-z0-9\-]*"
				maxlength="31"
				autocapitalize="none"
				spellcheck="false"
				required
			/>
			<small>{t('backup.nameHint')}</small>
		</label>
	{/if}
	<fieldset>
		<legend>{t('backup.sources')}</legend>
		<p class="muted small">{t('backup.pick.hint')}</p>
		{#each chosen as c, i (i)}
			<div class="picked">
				<button
					type="button"
					class="field pickfield"
					onclick={() => (picking = i)}
					aria-label={t('backup.pick.change')}
					>{#if c.volume}<Icon name="folder" />
						<span class="mono">{c.volume}{c.path ? ' › ' + c.path.replaceAll('/', ' › ') : ''}</span
						>{#if !c.path}<span class="muted small">{t('backup.pick.whole')}</span
							>{:else if shareOf(c) && !shareOf(c)?.rest}<span class="chip"
								>{t('backup.pick.share')}</span
							>{/if}{:else}<span class="muted">{t('backup.pick.choose')}</span>{/if}</button
				>
				{#if chosen.length > 1 || c.volume}<button
						type="button"
						class="btn small"
						aria-label={t('backup.pick.remove')}
						onclick={() =>
							(chosen =
								chosen.length > 1 ? chosen.filter((_, n) => n !== i) : [{ volume: '', path: '' }])}
						><Icon name="close" /></button
					>{/if}
			</div>
		{/each}
		<div>
			<button type="button" class="btn small" onclick={() => (picking = -1)}
				><Icon name="plus" /> {t('backup.pick.add')}</button
			>
		</div>
		<p class="muted small">{t('backup.neverSaved')}</p>
		<details class="more">
			<summary>{t('backup.moreOptions')}</summary>
			{#if !plainCopy(draft.destination.type)}<p class="legend-sub">{t('backup.extras')}</p>
				<div class="row">
					{#each EXTRAS as extra (extra)}
						<label class="check"
							><input
								type="checkbox"
								checked={draft.extras?.includes(extra)}
								onchange={() => toggleExtra(extra)}
							/><span>{t(`backup.extra.${extra}` as 'backup.extra.appdata')}</span></label
						>
					{/each}
				</div>
				{#if draft.extras?.includes('config')}<p class="notice-warn">
						{t('backup.extra.configHint')}
					</p>{/if}
				<ApplicationSelection
					bind:applications={draft.applications}
					bind:allowStop={draft.allowStop}
				/>
			{/if}
			{#if draft.skip?.length}
				<p class="legend-sub">{t('backup.skipList')}</p>
				<div class="row">
					{#each draft.skip as entry (entry)}<span class="chip"
							>{entry}
							<button
								type="button"
								class="link"
								aria-label={t('backup.pick.remove')}
								onclick={() => (draft!.skip = draft!.skip!.filter((e) => e !== entry))}
								><Icon name="close" size={12} /></button
							></span
						>{/each}
				</div>
			{/if}
			<label class="block"
				><span>{t('backup.patterns')}</span>
				<textarea class="field mono" rows="2" bind:value={patterns} placeholder="*.tmp"></textarea>
			</label>
			<p class="muted">
				{t(plainCopy(draft.destination.type) ? 'backup.copyPatterns' : 'backup.patternsHint')}
			</p>
		</details>
	</fieldset>
	{#if picking !== null}<FolderPicker
			volumes={overview.volumes ?? []}
			shares={overview.shares}
			start={picking >= 0 && chosen[picking]?.volume ? chosen[picking] : undefined}
			onpick={pick}
			onclose={() => (picking = null)}
		/>{/if}
	<JobDestination
		bind:draft
		bind:pairCode
		{isNew}
		{overview}
		{volumes}
		{usbDrives}
		{usbError}
		{loadUSB}
		{publicKey}
		{showKey}
		{secretSet}
		{secretKept}
	/>
	<ScheduleFields bind:schedule={draft} />
	<fieldset class="divided">
		<legend>{t('backup.bandwidth')}</legend>
		<label class="check"
			><input type="checkbox" bind:checked={bandwidth.on} /><span>{t('backup.bandwidthLimit')}</span
			></label
		>
		{#if bandwidth.on}
			<div class="row">
				<label class="tiny"
					><span>{t('backup.bandwidthMax')}</span><input
						class="field mono"
						type="number"
						min="0"
						max={maxBandwidth}
						step="1"
						bind:value={bandwidth.limit}
					/></label
				>
			</div>
			<label class="check"
				><input type="checkbox" bind:checked={bandwidth.window} /><span
					>{t('backup.bandwidthWindow')}</span
				></label
			>
			{#if bandwidth.window}
				<div class="row">
					<label class="tiny"
						><span>{t('backup.windowFrom')}</span><input
							class="field mono"
							type="time"
							bind:value={bandwidth.windowFrom}
							required
						/></label
					>
					<label class="tiny"
						><span>{t('backup.windowTo')}</span><input
							class="field mono"
							type="time"
							bind:value={bandwidth.windowTo}
							required
						/></label
					>
					<label class="tiny"
						><span>{t('backup.bandwidthWindowLimit')}</span><input
							class="field mono"
							type="number"
							min="0"
							max={maxBandwidth}
							step="1"
							bind:value={bandwidth.windowLimit}
						/></label
					>
				</div>
			{/if}
			<p class="muted">
				{t(
					plainCopy(draft.destination.type)
						? 'backup.bandwidthHintCopy'
						: 'backup.bandwidthHintRestic',
				)}
			</p>
		{/if}
	</fieldset>
	{#if plainCopy(draft.destination.type)}
		<VmDisksChoice
			bind:vmDisks={draft.vmDisks}
			sources={selection.sources}
			volumes={selection.volumes}
			places={selection.places}
		/>
	{/if}
	{#if plainCopy(draft.destination.type)}<fieldset class="divided">
			<legend>{t('backup.copy.deleted')}</legend>
			{#if draft.copy?.dated}
				<p class="muted">{t('backup.copy.datedDeleted')}</p>
			{:else}
				<label class="check"
					><input
						type="radio"
						name="deleted"
						checked={!draft.copy?.mirror}
						onchange={() => (draft!.copy = { ...draft!.copy, mirror: false })}
					/><span>{t('backup.copy.keepDeleted')}</span></label
				>
				<p class="muted small indent">{t('backup.copy.keepDeletedHint')}</p>
				<label class="check"
					><input
						type="radio"
						name="deleted"
						checked={!!draft.copy?.mirror}
						onchange={() =>
							(draft!.copy = {
								...draft!.copy,
								mirror: true,
								mirrorDays: draft!.copy?.mirrorDays || 30,
							})}
					/><span>{t('backup.copy.mirror')}</span></label
				>
				<p class="muted small indent">
					{t('backup.copy.mirrorHint')}
					{#if draft.copy?.mirror}<span class="inline"
							><input
								class="field mono days-input"
								type="number"
								min="1"
								max="3650"
								aria-label={t('backup.copy.mirrorDays')}
								bind:value={draft.copy.mirrorDays}
							/>
							{t('backup.copy.mirrorDays')}</span
						>{/if}
				</p>
			{/if}
		</fieldset>{/if}
	<fieldset class="divided">
		<legend>{t('backup.keeps')}</legend>
		{#if plainCopy(draft.destination.type) && !draft.copy?.dated}
			<p class="muted">{t('backup.copy.noVersions')}</p>
		{:else}
			<KeepVersions bind:keep={draft.keep} target={fullTarget(draft.destination.type)} />
		{/if}
	</fieldset>
	{#if error}<div class="notice-error" role="alert">
			{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
		</div>{/if}
	<div class="row-actions">
		<button type="button" class="btn" onclick={oncancel} disabled={busy}
			>{t('common.cancel')}</button
		>
		<button
			type="submit"
			class="btn btn-primary"
			disabled={busy ||
				(!chosen.some((c) => c.volume) && !draft.extras?.length && !draft.applications?.length)}
			>{isNew ? t('backup.create') : t('notify.save')}</button
		>
	</div>
</form>

<style>
	/* The quiet surface of a row, with the layout of a panel. */
	form.job-form {
		display: flex;
		flex-direction: column;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3) 20px;
	}
	fieldset {
		gap: var(--space-3);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	/* Directly in the form (a column) the basis would be a height. */
	form.job-form > .narrow {
		flex: none;
	}
	.narrow {
		flex: 0 1 260px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 120px;
		max-width: 140px;
	}
	label.check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2);
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.picked {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		margin-bottom: var(--space-1-5);
	}
	.pickfield {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		flex: 1;
		min-width: 0;
		text-align: left;
		cursor: pointer;
	}
	.pickfield .mono {
		overflow-wrap: anywhere;
	}
	.more {
		margin-top: var(--space-2);
	}
	.more summary {
		cursor: pointer;
		color: var(--accent);
	}
	.link {
		background: none;
		border: 0;
		color: inherit;
		cursor: pointer;
		padding: 0 0 0 var(--space-1);
	}
	.small {
		font-size: 0.8125rem;
	}
	.indent {
		margin: 0 0 0 var(--space-5);
	}
	.days-input {
		width: 4.5rem;
		text-align: center;
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.inline {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
	.legend-sub {
		margin: 0.8rem 0 0.3rem;
		font-weight: 600;
	}
	.block {
		display: grid;
		gap: 0.3rem;
		margin-top: 0.6rem;
	}
</style>
