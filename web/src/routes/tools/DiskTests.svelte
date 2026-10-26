<script lang="ts">
	// Testing disks before they go into a volume.
	import { api, ApiError, type Disk } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes, formatDuration } from '#lib/format.ts';
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
		result: '' | 'passed' | 'failed' | 'cancelled';
		notes: { kind: string; detail: string }[];
	};

	let disks = $state<Disk[]>([]);
	let tests = $state<Test[]>([]);
	let error = $state<{ text: string; detail: string } | null>(null);
	let chosen = $state<Disk | null>(null);
	let mode = $state<Mode>('read');
	let busy = $state(false);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: (code === 'storage_failed' || code === 'disk_unavailable') && err instanceof ApiError ? err.message : '' };
	}

	async function load() {
		try {
			const [storage, running] = await Promise.all([api.get<{ disks: Disk[] }>('/storage'), api.get<{ tests: Test[] }>('/storage/tests')]);
			disks = storage.disks;
			tests = running.tests;
		} catch (err) {
			if (disks.length === 0) problem(err);
		}
	}

	const active = $derived(tests.some((test) => !test.result));
	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && load(), active ? 2000 : 10_000);
		return () => clearInterval(timer);
	});

	const candidates = $derived(disks.filter((disk) => (disk.role === 'free' || disk.role === 'used') && !disk.mounted && !disk.testing && !tests.some((test) => test.disk === disk.name)));

	function open(disk: Disk) {
		chosen = disk;
		mode = 'read';
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
			tests = (await api.post<{ tests: Test[] }>('/storage/tests', {
				disk: chosen.name,
				mode,
				// The API still wants the disk name as confirmation for an erasing test.
				confirm: mode === 'write' ? chosen.name : ''
			})).tests;
			chosen = null;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	async function stop(test: Test) {
		error = null;
		try {
			tests = (await api.delete<{ tests: Test[] }>(`/storage/tests/${test.disk}`)).tests;
		} catch (err) {
			problem(err);
		}
	}

	const TONE = { passed: 'ok', failed: 'error', cancelled: '' } as const;
	function progress(test: Test): string {
		const percent = test.percent.toFixed(test.percent < 10 ? 1 : 0);
		if (test.phase === '') return t('disktest.starting');
		if (test.phase === 'smart') return test.percent > 0 ? t('disktest.progressSmart', { percent }) : '';
		if (test.remainingSeconds < 0) return t('disktest.progressSmart', { percent });
		return t('disktest.progress', { percent, speed: Math.round(test.speedBytesPerSecond / 1e6), remaining: formatDuration(test.remainingSeconds) });
	}
</script>

<section class="tile" aria-labelledby="tools-disks">
	<h2 class="tile-title" id="tools-disks">{t('disktest.title')}</h2>
	<p class="muted">{t('disktest.intro')}</p>
	{#if error && !chosen}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

	{#if tests.length > 0}
		<h3>{t('disktest.running')}</h3>
		<ul class="tests">
			{#each tests as test (test.disk)}
				<li>
					<div class="head">
						<span><span class="mono name">{test.disk}</span> <span class="muted">{[test.model, test.serial, formatDiskBytes(test.sizeBytes)].filter(Boolean).join(' · ')}</span></span>
						{#if test.result}
							<span class="chip {TONE[test.result]}">{t(`disktest.result.${test.result}`)}</span>
						{:else}
							<span class="chip info">{t(`disktest.mode.${test.mode}`)}</span>
						{/if}
					</div>
					{#if !test.result}
						<div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(test.percent)} aria-label={test.phase ? t(`disktest.phase.${test.phase}`) : t('disktest.starting')}><span style:width="{test.percent}%"></span></div>
						<div class="line">
							<span>{test.phase ? t(`disktest.phase.${test.phase}`) : t('disktest.starting')}{#if progress(test) && test.phase}{' · '}<span class="mono">{progress(test)}</span>{/if}</span>
							<button type="button" class="btn small" onclick={() => stop(test)}>{t('disktest.cancel')}</button>
						</div>
					{:else}
						<div class="line">
							<span class:bad={test.result === 'failed'}>
								{#if test.result === 'passed'}{t('disktest.passedText')}{:else if test.result === 'failed'}{t('disktest.failedText')}{/if}
							</span>
							<button type="button" class="btn small" onclick={() => stop(test)}>{t('disktest.dismiss')}</button>
						</div>
					{/if}
					{#if test.errors > 0}<p class="bad">{t('disktest.errors', { count: test.errors })}</p>{/if}
					{#each test.notes as note, index (index)}
						<p class="muted">{tOr(`disktest.note.${note.kind}`, note.kind).replace('{detail}', note.detail)}</p>
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
					<span class="muted grow">{[disk.model, disk.serial, formatDiskBytes(disk.sizeBytes)].filter(Boolean).join(' · ')} · {disk.contents?.length ? t('disktest.contents', { contents: disk.contents.join(', ') }) : t('disktest.empty')}</span>
					<button type="button" class="btn small" onclick={() => open(disk)}>{t('disktest.start')}</button>
				</li>
			{/each}
		</ul>
	{/if}
</section>

{#if chosen}
	{@const disk = chosen}
	<Dialog title={t('disktest.dialog', { disk: disk.name })} locked={busy} onclose={() => (chosen = null)}>
		<form onsubmit={(event) => event.preventDefault()}>
			<p class="muted">{[disk.model, disk.serial, formatDiskBytes(disk.sizeBytes)].filter(Boolean).join(' · ')}</p>
			<fieldset>
				<legend>{t('disktest.mode')}</legend>
				<label class="choice"><input type="radio" bind:group={mode} value="read" /><span><span class="name">{t('disktest.mode.read')}</span><small>{t('disktest.mode.readHint')}</small></span></label>
				<label class="choice"><input type="radio" bind:group={mode} value="write" /><span><span class="name">{t('disktest.mode.write')}</span><small>{t('disktest.mode.writeHint')}</small></span></label>
			</fieldset>
			<p class="muted">{t('disktest.estimate', { size: formatDiskBytes(disk.sizeBytes), hours: hours(disk, mode) })}</p>
			{#if mode === 'write'}
				<div class="notice-warn">
					{t('disktest.eraseWarning', { disk: disk.name, model: [disk.model, disk.serial].filter(Boolean).join(' · ') })}
					{#if disk.contents?.length}<br />{t('disktest.eraseContents', { contents: disk.contents.join(', ') })}{/if}
				</div>
			{/if}
			<p class="muted">{t('disktest.restartNote')}</p>
			{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (chosen = null)} disabled={busy}>{t('common.cancel')}</button>
				<button type="button" class="btn {mode === 'write' ? 'btn-danger' : 'btn-primary'}" disabled={busy} onclick={start}>{t('disktest.begin')}</button>
			</div>
		</form>
	</Dialog>
{/if}

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
	h3 {
		margin: 4px 0 0;
		font-size: 0.875rem;
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
		font-size: 0.8125rem;
	}
	.bad {
		color: var(--error-text);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
		font-size: 0.875rem;
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
		font-size: 0.8125rem;
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
		font-size: 0.8125rem;
		color: var(--muted);
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
