<script lang="ts">
	// Updates: a new version goes into the other slot of the stick and
	// starts on trial; the one before stays as the fallback.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import PowerDialog from '#lib/components/PowerDialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	/** checkEvery: minutes between automatic checks; 0 or missing is once a day, -1 manual only. */
	type Settings = {
		channel: 'stable' | 'beta';
		autoInstall: boolean;
		installTime: string;
		checkEvery?: number;
		/** Kernel choice: '' (or missing) Debian's kernel, 'new' the newer one of trixie-backports. */
		kernel?: '' | 'stable' | 'new';
	};
	type Variant = { kernel: string; files: { size: number }[] };
	type Manifest = {
		version: string;
		channel: string;
		kernel: string;
		released: string;
		minVersion?: string;
		changelog: string;
		files: { size: number }[];
		app?: { base: string; size: number };
		variants?: { new?: Variant };
	};
	type Slot = {
		name: string;
		version: string;
		running: boolean;
		complete: boolean;
		next: boolean;
		triesLeft: number | null;
		/** Kernel release in the slot and its kernel variant, if known. */
		kernel?: string;
		variant?: '' | 'stable' | 'new';
	};
	type Status = {
		version: string;
		release: boolean;
		slots: Slot[];
		settings: Settings;
		canVerify: boolean;
		available: Manifest | null;
		lastCheck: string | null;
		/** When the next automatic check is due; null with manual checks or none scheduled. */
		nextCheck: string | null;
		checkError: string;
		progress: {
			phase: '' | 'download' | 'switch' | 'restart' | 'done' | 'failed';
			file: string;
			percent: number;
			speedBytesPerSecond: number;
			error: string;
			/** No room on the stick next to the old version: the fallback slot was emptied. */
			fallbackRemoved?: boolean;
		};
		pending: { kind: 'update' | 'rollback'; version: string; slot: string } | null;
		canRollback: boolean;
		unconfirmed: boolean;
		/** Version of the system image; version is newer while an app update runs on it. */
		base: string;
		/** How the available release would be installed: titand alone, or a whole system. */
		kind: '' | 'app' | 'system';
		app: { current: string; previous: string; trial: boolean; canRollback: boolean };
		/** Kernel variant of the running system. */
		variant: 'stable' | 'new';
		/** What started this system: systemd-boot, or GRUB for legacy BIOS / 32-bit UEFI. */
		bootLoader: string;
		/** The available release is the running version with the other kernel. */
		kernelSwitch?: boolean;
		/** A newer release without a system with the chosen kernel. */
		kernelMissing?: string;
	};

	let status = $state<Status | null>(null);
	let draft = $state<Settings | null>(null);
	let saved = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state<'' | 'check' | 'save' | 'action'>('');
	let asking = $state<'' | 'install' | 'rollback' | 'app-rollback'>('');
	let restarting = $state(false);
	// Encrypted volumes, looked up when going back is asked for: a system
	// before 0.31.0 cannot open them (they stay unmounted there).
	let encrypted = $state<string[]>([]);
	const beforeEncryption = (target: string) => {
		const [major, minor] = target.replace(/^v/, '').split('.').map(Number);
		return Number.isInteger(major) && Number.isInteger(minor) && major === 0 && minor < 31;
	};
	$effect(() => {
		if (asking !== 'rollback') return;
		encrypted = [];
		api
			.get<{ volumes: { name: string; encryption?: unknown }[] }>('/storage')
			.then((s) => (encrypted = s.volumes.filter((v) => v.encryption).map((v) => v.name)))
			.catch(() => {});
	});
	let checked = $state(false);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail:
				['update_failed', 'update_too_old', 'update_no_space'].includes(code) &&
				err instanceof ApiError
					? err.message
					: '',
		};
	}

	// How often to look, as the server keeps it (minutes): presets, or a
	// custom spacing from an hour to 30 days. Manual leaves it to the button.
	const MANUAL = -1;
	const presets = [360, 720, 0, 10080];
	const MIN_EVERY = 60;
	const MAX_EVERY = 30 * 24 * 60;
	let mode = $state('0');
	let amount = $state(1);
	let unit = $state<'hours' | 'days'>('hours');

	// The settings with a missing interval read as the default, so that the
	// draft compares equal to what the server sent.
	function normal(settings: Settings): Settings {
		const every = settings.checkEvery ?? 0;
		const kernel = settings.kernel === 'new' ? 'new' : '';
		return { ...settings, checkEvery: every === 24 * 60 ? 0 : every, kernel };
	}

	function showEvery(every: number) {
		if (every === MANUAL) mode = 'manual';
		else if (presets.includes(every)) mode = String(every);
		else {
			mode = 'custom';
			unit = every % (24 * 60) === 0 ? 'days' : 'hours';
			amount = every / (unit === 'days' ? 24 * 60 : 60);
		}
	}

	const chosenEvery = $derived(
		mode === 'manual'
			? MANUAL
			: mode === 'custom'
				? Math.round(amount * (unit === 'days' ? 24 * 60 : 60))
				: Number(mode),
	);
	const customInvalid = $derived(
		mode === 'custom' &&
			(!Number.isFinite(amount) || chosenEvery < MIN_EVERY || chosenEvery > MAX_EVERY),
	);
	const manual = $derived(mode === 'manual');

	// Applies the choice to the draft. Installing by itself needs automatic
	// checks, so manual turns it off; the checkbox stays disabled meanwhile.
	function chooseEvery() {
		if (!draft) return;
		// One day is the default and saved as such.
		if (!customInvalid) draft.checkEvery = chosenEvery === 24 * 60 ? 0 : chosenEvery;
		if (manual) draft.autoInstall = false;
	}

	// Custom starts from the interval chosen so far, so "once a week" turns
	// into 7 days rather than an hour.
	function pickMode() {
		if (mode === 'custom' && draft) {
			const every = draft.checkEvery || 24 * 60;
			const days = every > 0 && every % (24 * 60) === 0;
			unit = days || every < 0 ? 'days' : 'hours';
			amount = every < 0 ? 1 : every / (days ? 24 * 60 : 60);
		}
		chooseEvery();
	}

	function take(res: Status) {
		status = { ...res, settings: normal(res.settings) };
		if (!draft) {
			draft = { ...status.settings };
			showEvery(draft.checkEvery ?? 0);
		}
	}

	// titand restarts for an app update: the page keeps asking until the new
	// one answers, then says which version runs now.
	let restartFrom = $state('');
	let restartedTo = $state('');
	async function load() {
		try {
			const next = await api.get<Status>('/update');
			if (restartFrom && next.progress.phase !== 'restart') {
				restartedTo = next.version;
				restartFrom = '';
			}
			take(next);
		} catch (err) {
			if (!status) problem(err);
		}
	}

	const working = $derived(
		status?.progress.phase === 'download' ||
			status?.progress.phase === 'switch' ||
			status?.progress.phase === 'restart' ||
			!!restartFrom,
	);
	$effect(() => {
		if (status?.progress.phase === 'restart' && !restartFrom) restartFrom = status.version;
	});
	$effect(() => {
		const fast = working;
		return poll(load, { interval: fast ? 1500 : 15_000 });
	});

	async function act(action: 'check' | 'install' | 'rollback' | 'cancel' | 'app-rollback') {
		busy = action === 'check' ? 'check' : 'action';
		error = null;
		asking = '';
		checked = false;
		restartedTo = '';
		try {
			take(await api.post<Status>(`/update/${action}`));
			checked = action === 'check';
		} catch (err) {
			problem(err);
			load();
		}
		busy = '';
	}

	const changed = $derived(
		!!status && !!draft && JSON.stringify(draft) !== JSON.stringify(status.settings),
	);
	// The next check belongs to the saved interval; it is hidden while
	// another one is chosen but not saved yet.
	const nextCheck = $derived(
		status?.nextCheck && draft && (draft.checkEvery ?? 0) === (status.settings.checkEvery ?? 0)
			? new Date(status.nextCheck)
			: null,
	);
	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = 'save';
		saved = false;
		error = null;
		try {
			// Stable is the default and left out, as before the choice existed.
			take(
				await api.put<Status>('/update/settings', { ...draft, kernel: draft.kernel || undefined }),
			);
			draft = { ...status!.settings };
			saved = true;
			showEvery(draft.checkEvery ?? 0);
		} catch (err) {
			problem(err);
		}
		busy = '';
	}

	const appUpdate = $derived(status?.kind === 'app' && !!status.available?.app);
	// A system update installs the files of the chosen kernel.
	const chosen = $derived(
		status?.settings.kernel === 'new' && status.available?.variants?.new
			? status.available.variants.new
			: status?.available,
	);
	const size = $derived(
		appUpdate
			? (status?.available?.app?.size ?? 0)
			: (chosen?.files.reduce((sum, file) => sum + file.size, 0) ?? 0),
	);
	const running = $derived(status?.slots.find((slot) => slot.running));
	const loaderKey = $derived(
		status?.bootLoader === 'grub-i386-pc'
			? 'update.boot.bios'
			: status?.bootLoader === 'grub-i386-efi'
				? 'update.boot.ia32'
				: '',
	);
	// Where going back from an app update leads: the app before, or the titand of the system image.
	const appTarget = $derived(
		status?.app.previous || (status ? t('update.app.base', { version: status.base }) : ''),
	);
	const other = $derived(status?.slots.find((slot) => !slot.running));

	function slotState(slot: Slot): string {
		if (slot.running) return t(slot.next ? 'update.slot.running' : 'update.slot.runningNotNext');
		if (!slot.complete) return t('update.slot.empty');
		if (slot.triesLeft === 0) return t('update.slot.bad');
		return t(slot.next ? 'update.slot.next' : 'update.slot.fallback');
	}
</script>

<PageHead title={t('update.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if error}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>{/if}

{#if status && draft}
	<section class="tile panel" aria-labelledby="upd-now">
		<div class="head">
			<h2 class="tile-title" id="upd-now">{t('update.installed')}</h2>
			{#if !status.release}<span class="chip warn">{t('update.devBuild')}</span>{/if}
		</div>
		<p class="version">
			<span class="mono">TitanOS {status.version}</span
			>{#if status.base && status.base !== status.version}<span class="muted base"
					>{t('update.app.onBase', { version: status.base })}</span
				>{/if}
		</p>
		<p class="muted">
			{t(status.variant === 'new' ? 'update.kernel.runningNew' : 'update.kernel.runningStable', {
				kernel: running?.kernel || '–',
			})}{#if loaderKey}{' · '}{t(loaderKey as 'update.boot.bios')}{/if}
		</p>
		{#if restartedTo}<p class="notice-ok" role="status">
				{t('update.app.restarted', { version: restartedTo })}
			</p>{/if}
		{#if status.app.trial}<p class="notice-warn" role="status">{t('update.app.trial')}</p>{/if}

		{#if status.pending}
			<div class="notice-ok" role="status">
				{t(status.pending.kind === 'rollback' ? 'update.pendingRollback' : 'update.pendingUpdate', {
					version: status.pending.version,
				})}
				<div class="row">
					<button type="button" class="btn btn-primary" onclick={() => (restarting = true)}
						>{t('update.restartNow')}</button
					>
					<button type="button" class="btn" disabled={busy !== ''} onclick={() => act('cancel')}
						>{t('update.cancel')}</button
					>
				</div>
			</div>
		{:else if status.unconfirmed}
			<p class="notice-warn" role="status">{t('update.unconfirmed')}</p>
		{/if}

		{#if !status.canVerify}
			<p class="notice-warn">{t('update.noKey')}</p>
		{:else if working}
			<div class="progress">
				<div
					class="bar"
					role="progressbar"
					aria-valuemin="0"
					aria-valuemax="100"
					aria-valuenow={Math.round(status.progress.percent)}
					aria-label={t('update.downloading')}
				>
					<span style:width="{status.progress.percent}%"></span>
				</div>
				<p class="muted">
					{#if status.progress.phase === 'download'}
						{t('update.downloading')}{' · '}<span class="mono"
							>{status.progress.percent.toFixed(0)} % · {(
								status.progress.speedBytesPerSecond / 1e6
							).toFixed(1)} MB/s</span
						>
					{:else if status.progress.phase === 'restart' || restartFrom}{t('update.app.restarting')}
					{:else}{t('update.switching')}{/if}
				</p>
				{#if status.progress.fallbackRemoved}<p class="notice-warn" role="status">
						{t('update.fallbackRemoved')}
					</p>{/if}
			</div>
		{:else if status.progress.phase === 'failed'}
			<div class="notice-error" role="alert">
				{t('update.failed')}
				<pre>{status.progress.error}</pre>
				{#if status.progress.fallbackRemoved}<p>
						<strong>{t('update.fallbackRemovedFailed')}</strong>
					</p>{/if}
			</div>
		{:else if status.progress.phase === 'done' && status.progress.fallbackRemoved}
			<p class="notice-warn" role="status">{t('update.fallbackRemoved')}</p>
		{/if}

		{#if status.available && !status.pending && !working}
			<div class="available">
				<div class="head">
					<span class="name"
						>TitanOS {status.available.version}{#if status.kernelSwitch}{' · '}{t(
								status.settings.kernel === 'new'
									? 'update.kernel.withNew'
									: 'update.kernel.withStable',
							)}{/if}</span
					>
					<span class="chips">
						{#if appUpdate}<span class="chip ok">{t('update.app.noRestart')}</span>{/if}
						<span class="chip info"
							>{t(
								status.available.channel === 'beta'
									? 'update.channel.beta'
									: 'update.channel.stable',
							)}</span
						>
					</span>
				</div>
				<p class="muted">
					{t('update.availableMeta', {
						date: formatDateTime(new Date(status.available.released)),
						kernel: chosen?.kernel ?? status.available.kernel,
						size: formatBytes(size),
					})}
				</p>
				{#if status.available.changelog}<pre class="changelog">{status.available
							.changelog}</pre>{/if}
				<div>
					<button
						type="button"
						class="btn btn-primary"
						disabled={busy !== '' || status.unconfirmed || status.app.trial}
						onclick={() => (asking = 'install')}
						>{t(status.kernelSwitch ? 'update.kernel.switch' : 'update.install')}</button
					>
				</div>
			</div>
		{:else if status.canVerify && !status.pending && !working}
			{#if status.kernelMissing}<p class="notice-warn" role="status">
					{t('update.kernel.missing', { version: status.kernelMissing })}
				</p>{/if}
			<div class="row">
				<button type="button" class="btn" disabled={busy !== ''} onclick={() => act('check')}
					>{t(busy === 'check' ? 'update.checking' : 'update.check')}</button
				>
				<span class="muted" role="status">
					{#if status.checkError}{t('update.checkFailed')}
					{:else if checked}{t('update.upToDate')}
					{:else if status.lastCheck}{t('update.lastCheck', {
							time: formatDateTime(new Date(status.lastCheck)),
						})}{/if}
				</span>
			</div>
			{#if status.checkError}<pre class="muted">{status.checkError}</pre>{/if}
		{/if}
	</section>

	{#if status.app.current || status.app.previous}
		<section class="tile panel" aria-labelledby="upd-app">
			<h2 class="tile-title" id="upd-app">{t('update.app.title')}</h2>
			<p class="muted">{t('update.app.text', { version: status.base })}</p>
			<ul class="slots">
				<li>
					<span class="name">{t('update.app.running')}</span><span class="mono"
						>{status.app.current || t('update.app.base', { version: status.base })}</span
					>
				</li>
				{#if status.app.previous}<li>
						<span class="name">{t('update.app.previous')}</span><span class="mono"
							>{status.app.previous}</span
						>
					</li>{/if}
			</ul>
			{#if status.app.canRollback && !status.pending}
				<div>
					<button
						type="button"
						class="btn"
						disabled={busy !== '' || working}
						onclick={() => (asking = 'app-rollback')}
						>{t('update.app.rollback', { target: appTarget })}</button
					>
				</div>
			{/if}
		</section>
	{/if}

	{#if status.slots.length > 0}
		<section class="tile panel" aria-labelledby="upd-slots">
			<h2 class="tile-title" id="upd-slots">{t('update.slots')}</h2>
			<p class="muted">{t('update.slotsText')}</p>
			<ul class="slots">
				{#each status.slots as slot (slot.name)}
					<li>
						<span class="mono name">{t('update.slot', { name: slot.name.toUpperCase() })}</span>
						<span class="mono">{slot.complete || slot.running ? slot.version || '–' : '–'}</span>
						{#if slot.kernel}<span class="mono muted kernel"
								>{t(slot.variant === 'new' ? 'update.kernel.slotNew' : 'update.kernel.slotStable', {
									kernel: slot.kernel,
								})}</span
							>{/if}
						<span class="grow muted"
							>{slotState(slot)}{#if slot.triesLeft !== null && slot.triesLeft > 0}{' · '}{t(
									'update.slot.tries',
									{ count: slot.triesLeft },
								)}{/if}</span
						>
					</li>
				{/each}
			</ul>
			{#if status.canRollback && !status.pending && other}
				<div>
					<button
						type="button"
						class="btn"
						disabled={busy !== '' || working}
						onclick={() => (asking = 'rollback')}
						>{t('update.rollback', { version: other.version || '?' })}</button
					>
				</div>
			{/if}
		</section>
	{/if}

	<form class="tile panel" onsubmit={save}>
		<h2 class="tile-title">{t('update.settings')}</h2>
		<p class="muted">{t('update.singleChannel')}</p>
		<div class="interval">
			<label class="narrow"
				><span>{t('update.interval')}</span>
				<select class="field" bind:value={mode} onchange={pickMode}>
					{#each presets as every (every)}<option value={String(every)}
							>{t(`update.interval.${every}` as 'update.interval.0')}</option
						>{/each}
					<option value="custom">{t('update.interval.custom')}</option>
					<option value="manual">{t('update.interval.manual')}</option>
				</select>
			</label>
			{#if mode === 'custom'}
				<label class="amount"
					><span>{t('update.interval.every')}</span><input
						class="field mono"
						type="number"
						min="1"
						max={unit === 'days' ? 30 : 720}
						step="1"
						bind:value={amount}
						oninput={chooseEvery}
						aria-invalid={customInvalid}
					/></label
				>
				<select
					class="field unit"
					bind:value={unit}
					onchange={chooseEvery}
					aria-label={t('update.interval.unit')}
				>
					<option value="hours">{t('update.interval.hours')}</option>
					<option value="days">{t('update.interval.days')}</option>
				</select>
			{/if}
		</div>
		{#if customInvalid}<p class="notice-error" role="alert">{t('update.interval.range')}</p>{/if}
		<p class="muted">{t(manual ? 'update.interval.manualHint' : 'update.interval.hint')}</p>
		{#if nextCheck && !manual}<p class="muted" role="status">
				{nextCheck.getTime() <= Date.now()
					? t('update.nextCheckSoon')
					: t('update.nextCheck', { time: formatDateTime(nextCheck) })}
			</p>{/if}
		<fieldset class="kernel-choice">
			<legend>{t('update.kernel.title')}</legend>
			<label class="check"
				><input type="radio" name="kernel" value="" bind:group={draft.kernel} /><span
					><strong>{t('update.kernel.stable')}</strong><span class="muted"
						>{t('update.kernel.stableText')}</span
					></span
				></label
			>
			<label class="check"
				><input type="radio" name="kernel" value="new" bind:group={draft.kernel} /><span
					><strong>{t('update.kernel.new')}</strong><span class="muted"
						>{t('update.kernel.newText')}</span
					></span
				></label
			>
			<p class="muted">{t('update.kernel.hint')}</p>
		</fieldset>
		<label class="check"
			><input type="checkbox" bind:checked={draft.autoInstall} disabled={manual} /><span
				>{t('update.auto')}</span
			></label
		>
		{#if manual}
			<p class="muted">{t('update.autoNeedsChecks')}</p>
		{:else if draft.autoInstall}
			<label class="narrow"
				><span>{t('update.autoTime')}</span><input
					class="field mono"
					type="time"
					bind:value={draft.installTime}
					required
				/></label
			>
			<p class="muted">{t('update.autoHint')}</p>
		{:else}
			<p class="muted">{t('update.manualHint')}</p>
		{/if}
		<div class="row-actions">
			<button
				type="submit"
				class="btn btn-primary"
				disabled={!changed || customInvalid || busy !== ''}
				>{t(busy === 'save' ? 'ui.saving' : 'notify.save')}</button
			>{#if saved && !changed}<span class="muted" role="status">{t('ui.saved')}</span>{/if}
		</div>
	</form>
{/if}

{#if asking === 'install' && status?.available}
	<Dialog
		title={status.kernelSwitch
			? t('update.kernel.switchTitle')
			: t('update.confirmTitle', { version: status.available.version })}
		onclose={() => (asking = '')}
	>
		<p>
			{t(appUpdate ? 'update.app.confirmText' : 'update.confirmText', { size: formatBytes(size) })}
		</p>
		{#if status.kernelSwitch}<p>
				{t('update.kernel.confirm', { kernel: chosen?.kernel ?? '' })}
			</p>{/if}
		<p class="muted">{t(appUpdate ? 'update.app.confirmSafe' : 'update.confirmSafe')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (asking = '')}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={() => act('install')}
				>{t(status.kernelSwitch ? 'update.kernel.switch' : 'update.install')}</button
			>
		</div>
	</Dialog>
{/if}

{#if asking === 'rollback' && other}
	<Dialog
		title={t('update.rollbackTitle', { version: other.version || '?' })}
		onclose={() => (asking = '')}
	>
		<p>{t('update.rollbackText')}</p>
		<p class="notice-warn">{t('update.rollbackWarn')}</p>
		{#if encrypted.length && beforeEncryption(other.version || '')}
			<p class="notice-error" role="alert" data-testid="rollback-encrypted">
				{t('update.rollbackEncrypted', { version: other.version, volumes: encrypted.join(', ') })}
			</p>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (asking = '')}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={() => act('rollback')}
				>{t('update.rollbackDo')}</button
			>
		</div>
	</Dialog>
{/if}

{#if asking === 'app-rollback' && status}
	<Dialog
		title={t('update.app.rollbackTitle', { target: appTarget })}
		onclose={() => (asking = '')}
	>
		<p>{t('update.app.rollbackText')}</p>
		<p class="notice-warn">{t('update.rollbackWarn')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (asking = '')}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={() => act('app-rollback')}
				>{t('update.rollbackDo')}</button
			>
		</div>
	</Dialog>
{/if}

{#if restarting}
	<PowerDialog action="restart" onclose={() => (restarting = false)} />
{/if}

<style>
	section,
	form {
		font-size: 0.875rem;
	}
	p {
		max-width: 60rem;
	}
	.head,
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 14px;
	}
	.head {
		justify-content: space-between;
	}
	.notice-ok .row {
		margin-top: 10px;
	}
	.version {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 4px 12px;
		font-size: 1.125rem;
	}
	.base {
		font-size: 0.875rem;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.available {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		border-radius: var(--radius-ctl);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.name {
		font-weight: 600;
	}
	.available .name {
		font-size: 1rem;
	}
	.changelog {
		max-height: 18rem;
		overflow-y: auto;
		padding: 10px 12px;
		background: var(--track);
		border-radius: var(--radius-ctl);
	}
	pre {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.notice-error pre {
		margin-top: 6px;
	}
	.progress {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.slots {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.slots li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 18px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.slots .name {
		min-width: 4.5rem;
	}
	.grow {
		flex: 1 1 200px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.narrow {
		max-width: 320px;
	}
	.interval {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 10px 12px;
	}
	.interval .narrow {
		flex: 1 1 220px;
	}
	.amount {
		width: 6.5rem;
	}
	.unit {
		width: 8rem;
	}
	.check input:disabled + span {
		color: var(--muted);
	}
	label.check {
		flex-direction: row;
	}
	.check > span {
		color: var(--text);
	}
	.kernel-choice {
		margin: 0;
		padding: 0;
		border: 0;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.kernel-choice legend {
		padding: 0;
		margin-bottom: 4px;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.kernel-choice label.check {
		align-items: flex-start;
	}
	.kernel-choice label.check > span {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.slots .kernel {
		font-size: 0.75rem;
	}
</style>
