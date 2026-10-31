<script lang="ts">
	// Testing disks before they go into a volume.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError, type Disk } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { resumeBody, resumeKind, restartHint } from '#lib/disktest.ts';
	import { formatDateTime, formatDiskBytes, formatDuration, formatTime } from '#lib/format.ts';
	import { errorText, t, tOr } from '#lib/i18n/index.svelte.ts';

	type Mode = 'read' | 'write';
	type Test = {
		disk: string;
		model: string;
		serial: string;
		sizeBytes: number;
		mode: Mode;
		phase: '' | 'write' | 'verify' | 'read' | 'smart' | 'done';
		percent: number;
		speedBytesPerSecond: number;
		remainingSeconds: number;
		errors: number;
		started: string;
		finished: string | null;
		result: '' | 'passed' | 'failed' | 'cancelled' | 'interrupted';
		notes: { kind: string; detail: string }[];
		autoResume: boolean;
		persistent: boolean;
		notPersistent?: string;
		resumed?: string;
		resumes?: number;
	};
	// A checkpoint that was dropped after a restart, and why.
	type Notice = {
		kind: string;
		serial?: string;
		model?: string;
		mode?: string;
		started: string;
		time: string;
	};
	type TestList = { tests: Test[]; notices?: Notice[] };

	let disks = $state<Disk[]>([]);
	let tests = $state<Test[]>([]);
	let notices = $state<Notice[]>([]);
	let autoResume = $state(true);
	let resuming = $state<Test | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let chosen = $state<Disk | null>(null);
	let mode = $state<Mode>('read');
	let busy = $state(false);
	// The last state that could be read, when a later refresh failed.
	let loaded = $state<Date | null>(null);
	let stale = $state(false);
	let stopping = $state<Test | null>(null);

	function show(list: TestList) {
		tests = list.tests;
		notices = list.notices ?? [];
	}

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail:
				(code === 'storage_failed' || code === 'disk_unavailable') && err instanceof ApiError
					? err.message
					: '',
		};
	}

	async function load() {
		try {
			const [storage, running] = await Promise.all([
				api.get<{ disks: Disk[] }>('/storage'),
				api.get<TestList>('/storage/tests'),
			]);
			disks = storage.disks;
			show(running);
			loaded = new Date();
			stale = false;
			if (error && !chosen && !stopping && !resuming) error = null;
		} catch (err) {
			// Once something is shown it stays, marked as possibly out of date.
			if (loaded) stale = true;
			else problem(err);
		}
	}

	const active = $derived(tests.some((test) => !test.result));
	$effect(() => {
		const fast = active;
		return poll(load, { interval: fast ? 2000 : 10_000 });
	});

	const candidates = $derived(
		disks.filter(
			(disk) =>
				(disk.role === 'free' || disk.role === 'used') &&
				!disk.mounted &&
				!disk.testing &&
				!tests.some((test) => test.disk === disk.name),
		),
	);

	function open(disk: Disk) {
		chosen = disk;
		mode = 'read';
		autoResume = true;
		error = null;
	}

	// One pass over the disk at a typical speed; the self-test counts as one more.
	function hours(disk: Disk, wanted: Mode): number {
		const speed = disk.kind === 'hdd' || disk.kind === 'usb' ? 150e6 : 400e6;
		const passes = wanted === 'write' ? 3 : 2;
		return Math.max(1, Math.round((disk.sizeBytes * passes) / speed / 3600));
	}

	// Started only by clicking the button: Enter in the dialog never erases a disk.
	async function start() {
		if (!chosen || busy) return;
		busy = true;
		error = null;
		try {
			show(
				await api.post<TestList>('/storage/tests', {
					disk: chosen.name,
					mode,
					// The API still wants the disk name as confirmation for an erasing test.
					confirm: mode === 'write' ? chosen.name : '',
					// Writing never continues by itself after a restart.
					autoResume: mode === 'read' && autoResume,
				}),
			);
			chosen = null;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	// Cancelling a running test asks first; dismissing a finished one does not.
	async function stop(test: Test) {
		busy = true;
		error = null;
		try {
			show(await api.delete<TestList>(`/storage/tests/${test.disk}`));
			stopping = null;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	// Continuing a test that a restart stopped. An overwriting one asks first;
	// the API wants the disk name as confirmation, which the page sends itself.
	async function resume(test: Test) {
		busy = true;
		error = null;
		try {
			show(await api.post<TestList>(`/storage/tests/${test.disk}/resume`, resumeBody(test)));
			resuming = null;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	async function dismissNotices() {
		busy = true;
		try {
			show(await api.delete<TestList>('/storage/test-notices'));
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	const TONE = { passed: 'ok', failed: 'error', cancelled: '', interrupted: 'warn' } as const;
	function progress(test: Test): string {
		const percent = test.percent.toFixed(test.percent < 10 ? 1 : 0);
		if (test.phase === '') return t('disktest.starting');
		if (test.phase === 'smart')
			return test.percent > 0 ? t('disktest.progressSmart', { percent }) : '';
		if (test.remainingSeconds < 0) return t('disktest.progressSmart', { percent });
		return t('disktest.progress', {
			percent,
			speed: Math.round(test.speedBytesPerSecond / 1e6),
			remaining: formatDuration(test.remainingSeconds),
		});
	}
</script>

<section class="tile" aria-labelledby="tools-disks">
	<h2 class="tile-title" id="tools-disks">{t('disktest.title')}</h2>
	<p class="muted">{t('disktest.intro')}</p>
	{#if error && !chosen && !stopping && !resuming}<div class="notice-error" role="alert">
			{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			<button type="button" class="btn small" onclick={load}>{t('ui.retry')}</button>
		</div>{/if}
	{#each notices as notice, index (index)}
		<p class="notice-warn" role="status">
			{tOr(`disktest.notice.${notice.kind}`, notice.kind)}
			<span class="muted"
				>{[
					notice.model,
					notice.serial,
					notice.mode ? t(`disktest.mode.${notice.mode as Mode}`) : '',
					t('disktest.startedAt', { time: formatDateTime(new Date(notice.started)) }),
				]
					.filter(Boolean)
					.join(' · ')}</span
			>
			{#if index === notices.length - 1}<button
					type="button"
					class="btn small"
					disabled={busy}
					onclick={dismissNotices}>{t('disktest.dismiss')}</button
				>{/if}
		</p>
	{/each}
	{#if stale && loaded}<p class="notice-warn" role="status">
			{t('disktest.stale', { time: formatTime(loaded) })}
			<button type="button" class="btn small" onclick={load}>{t('ui.retry')}</button>
		</p>{/if}

	{#if tests.length > 0}
		<h3>{t('disktest.running')}</h3>
		<ul class="tests">
			{#each tests as test (test.disk)}
				<li>
					<div class="head">
						<span
							><span class="mono name">{test.disk}</span>
							<span class="muted"
								>{[test.model, test.serial, formatDiskBytes(test.sizeBytes)]
									.filter(Boolean)
									.join(' · ')}</span
							></span
						>
						{#if test.result}
							<span class="chip {TONE[test.result]}">{t(`disktest.result.${test.result}`)}</span>
						{:else}
							<span class="chip info">{t(`disktest.mode.${test.mode}`)}</span>
						{/if}
					</div>
					<p class="muted">
						{t('disktest.startedAt', {
							time: formatDateTime(new Date(test.started)),
						})}{#if test.resumed}{' · '}{t('disktest.resumedAt', {
								time: formatDateTime(new Date(test.resumed)),
								count: test.resumes ?? 1,
							})}{/if}
					</p>
					{#if test.result === 'interrupted'}
						<p>
							{t(test.mode === 'write' ? 'disktest.interruptedWrite' : 'disktest.interruptedRead', {
								phase: test.phase ? t(`disktest.phase.${test.phase}`) : t('disktest.starting'),
								percent: test.percent.toFixed(0),
							})}
						</p>
						<div class="line">
							<button type="button" class="btn small" disabled={busy} onclick={() => stop(test)}
								>{t('disktest.discard')}</button
							>
							<button
								type="button"
								class="btn small btn-primary"
								disabled={busy}
								onclick={() => {
									error = null;
									if (resumeKind(test) === 'confirm') resuming = test;
									else resume(test);
								}}>{t('disktest.resume')}</button
							>
						</div>
					{:else if !test.result}
						<div
							class="bar"
							role="progressbar"
							aria-valuemin="0"
							aria-valuemax="100"
							aria-valuenow={Math.round(test.percent)}
							aria-label={test.phase ? t(`disktest.phase.${test.phase}`) : t('disktest.starting')}
						>
							<span style:width="{test.percent}%"></span>
						</div>
						<div class="line">
							<span
								>{test.phase
									? t(`disktest.phase.${test.phase}`)
									: t('disktest.starting')}{#if progress(test) && test.phase}{' · '}<span
										class="mono">{progress(test)}</span
									>{/if}</span
							>
							<button
								type="button"
								class="btn small"
								onclick={() => {
									error = null;
									stopping = test;
								}}>{t('disktest.cancel')}</button
							>
						</div>
						{#if restartHint(test)}<p class="muted">
								{t(`disktest.restart.${restartHint(test)}` as 'disktest.restart.kept')}
							</p>{/if}
					{:else}
						<div class="line">
							<span class:bad={test.result === 'failed'}>
								{#if test.result === 'passed'}{t(
										'disktest.passedText',
									)}{:else if test.result === 'failed'}{t('disktest.failedText')}{/if}
							</span>
							<button type="button" class="btn small" onclick={() => stop(test)}
								>{t('disktest.dismiss')}</button
							>
						</div>
					{/if}
					{#if test.errors > 0}<p class="bad">
							{t('disktest.errors', { count: test.errors })}
						</p>{/if}
					{#each test.notes as note, index (index)}
						<p class="muted">
							{tOr(`disktest.note.${note.kind}`, note.kind).replace('{detail}', note.detail)}
						</p>
					{/each}
				</li>
			{/each}
		</ul>
	{/if}

	<h3>{t('disktest.candidates')}</h3>
	{#if candidates.length === 0}
		<p class="muted">{t('disktest.none')}</p>
	{:else}
		<ul class="disks">
			{#each candidates as disk (disk.name)}
				<li>
					<span class="mono name">{disk.name}</span>
					<span class="muted grow"
						>{[disk.model, disk.serial, formatDiskBytes(disk.sizeBytes)]
							.filter(Boolean)
							.join(' · ')} · {disk.contents?.length
							? t('disktest.contents', { contents: disk.contents.join(', ') })
							: t('disktest.empty')}</span
					>
					<button type="button" class="btn small" onclick={() => open(disk)}
						>{t('disktest.start')}</button
					>
				</li>
			{/each}
		</ul>
	{/if}
</section>

{#if chosen}
	{@const disk = chosen}
	<Dialog
		title={t('disktest.dialog', { disk: disk.name })}
		locked={busy}
		onclose={() => (chosen = null)}
	>
		<form onsubmit={(event) => event.preventDefault()}>
			<p class="muted">
				{[disk.model, disk.serial, formatDiskBytes(disk.sizeBytes)].filter(Boolean).join(' · ')}
			</p>
			<fieldset>
				<legend>{t('disktest.mode')}</legend>
				<label class="choice"
					><input type="radio" bind:group={mode} value="read" /><span
						><span class="name">{t('disktest.mode.read')}</span><small
							>{t('disktest.mode.readHint')}</small
						></span
					></label
				>
				<label class="choice"
					><input type="radio" bind:group={mode} value="write" /><span
						><span class="name">{t('disktest.mode.write')}</span><small
							>{t('disktest.mode.writeHint')}</small
						></span
					></label
				>
			</fieldset>
			<p class="muted">
				{t('disktest.estimate', {
					size: formatDiskBytes(disk.sizeBytes),
					hours: hours(disk, mode),
				})}
			</p>
			{#if mode === 'write'}
				<div class="notice-warn">
					{t('disktest.eraseWarning', {
						disk: disk.name,
						model: [disk.model, disk.serial].filter(Boolean).join(' · '),
					})}
					{#if disk.contents?.length}<br />{t('disktest.eraseContents', {
							contents: disk.contents.join(', '),
						})}{/if}
				</div>
			{/if}
			{#if mode === 'read'}
				<label class="choice"
					><input type="checkbox" bind:checked={autoResume} /><span
						><span class="name">{t('disktest.autoResume')}</span><small
							>{t('disktest.autoResumeHint')}</small
						></span
					></label
				>
			{:else}
				<p class="muted">{t('disktest.writeResumeNote')}</p>
			{/if}
			<p class="muted">{t('disktest.restartNote')}</p>
			{#if error}<div class="notice-error" role="alert">
					{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
				</div>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (chosen = null)} disabled={busy}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class="btn {mode === 'write' ? 'btn-danger' : 'btn-primary'}"
					disabled={busy}
					onclick={start}>{t('disktest.begin')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if resuming}
	{@const test = resuming}
	<Dialog
		title={t('disktest.resumeTitle', { disk: test.disk })}
		locked={busy}
		onclose={() => (resuming = null)}
	>
		<div class="notice-warn">
			{t('disktest.resumeWrite', {
				disk: test.disk,
				model: [test.model, test.serial].filter(Boolean).join(' · '),
				percent: test.percent.toFixed(0),
			})}
		</div>
		{#if error}<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (resuming = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={() => resume(test)}
				>{t('disktest.resumeConfirm')}</button
			>
		</div>
	</Dialog>
{/if}

{#if stopping}
	{@const test = stopping}
	<Dialog
		title={t('disktest.cancelTitle', { disk: test.disk })}
		locked={busy}
		onclose={() => (stopping = null)}
	>
		<p>
			{t(test.mode === 'write' ? 'disktest.cancelWrite' : 'disktest.cancelRead', {
				disk: test.disk,
			})}
		</p>
		{#if error}<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (stopping = null)}
				>{t('disktest.keepRunning')}</button
			>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={() => stop(test)}
				>{t('disktest.cancelConfirm')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: var(--text-md);
	}
	p {
		margin: 0;
	}
	h3 {
		margin: 4px 0 0;
		font-size: var(--text-md);
		font-weight: 600;
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.disks li,
	.tests li {
		padding: 12px 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.disks li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 14px;
	}
	.tests li {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.grow {
		flex: 1 1 240px;
	}
	.name {
		font-weight: 600;
	}
	.head,
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 14px;
	}
	.small {
		height: 2rem;
		padding: 0 12px;
		font-size: var(--text-base);
	}
	.bad {
		color: var(--error-text);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
		font-size: var(--text-md);
	}
	fieldset {
		display: flex;
		flex-direction: column;
		gap: 10px;
		margin: 0;
		padding: 0;
		border: 0;
	}
	legend {
		padding: 0;
		margin-bottom: 8px;
		font-size: var(--text-base);
		color: var(--muted);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.choice {
		flex-direction: row;
		align-items: flex-start;
		gap: 10px;
	}
	.choice input {
		width: 18px;
		height: 18px;
		margin: 2px 0 0;
		flex: none;
	}
	.choice > span {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	small {
		font-size: var(--text-base);
		color: var(--muted);
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
