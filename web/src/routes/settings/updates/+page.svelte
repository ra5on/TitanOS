<script lang="ts">
	// Updates: a new version goes into the other slot of the stick and
	// starts on trial; the one before stays as the fallback.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import ClusterUpdates from '#lib/components/cluster/ClusterUpdates.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { activeNode } from '#lib/cluster-node.ts';
	import PowerDialog from '#lib/components/PowerDialog.svelte';
	import { formatBytes, formatDate, formatDateTime, osVersion } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import Changelog from '#lib/update/Changelog.svelte';
	import {
		counts,
		leadOf,
		parseChangelog,
		type Changelog as Notes,
	} from '#lib/update/changelog.ts';

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
		/** The release that failed its trial starts on this NAS. */
		failedVersion?: string;
	};

	/** A release that can be chosen (GET /update/versions). */
	type Choice = {
		version: string;
		channel: string;
		released: string;
		kernel: string;
		size: number;
		changelog: string;
		running: boolean;
		newest: boolean;
	};

	let status = $state<Status | null>(null);
	let draft = $state<Settings | null>(null);
	let saved = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state<'' | 'check' | 'save' | 'action'>('');
	let asking = $state<'' | 'install' | 'rollback' | 'app-rollback' | 'choose'>('');
	// Versions to choose from, from the oldest one with the current keys on.
	let versions = $state<Choice[] | null>(null);
	let versionsFailed = $state(false);
	let floor = $state('');
	let picked = $state('');
	async function loadVersions() {
		versionsFailed = false;
		try {
			const res = await api.get<{ versions: Choice[]; floor: string }>('/update/versions');
			versions = res.versions ?? [];
			floor = res.floor ?? '';
			if (!versions.some((v) => v.version === picked)) {
				picked = (versions.find((v) => !v.running) ?? versions[0])?.version ?? '';
			}
		} catch {
			versionsFailed = true;
		}
	}
	$effect(() => {
		loadVersions();
	});
	const pick = $derived(versions?.find((v) => v.version === picked));
	function versionNumbers(v: string): number[] {
		return v.split('-')[0].split('.').map(Number);
	}
	function olderThan(a: string, b: string): boolean {
		const [x, y] = [versionNumbers(a), versionNumbers(b)];
		for (let i = 0; i < 3; i++) if (x[i] !== y[i]) return x[i] < y[i];
		return a.includes('-') && !b.includes('-');
	}
	const pickOlder = $derived(
		!!pick && !!status && (olderThan(pick.version, status.version) || !pick.newest),
	);
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

	async function installVersion() {
		flow = 'system';
		target = picked;
		busy = 'action';
		error = null;
		asking = '';
		restartedTo = '';
		try {
			take(await api.post<Status>('/update/install-version', { version: picked }));
			// The settings may have changed: automatic installation off.
			draft = { ...status!.settings };
		} catch (err) {
			problem(err);
			load();
		}
		busy = '';
	}

	async function act(action: 'check' | 'install' | 'rollback' | 'cancel' | 'app-rollback') {
		if (action !== 'check') {
			// What the steps show: an app update, a system update, or (going
			// back, cancelling) no installation at all.
			flow = action === 'install' ? (appUpdate ? 'app' : 'system') : '';
			if (action === 'install') target = status?.available?.version ?? '';
		}
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
	// PLAN 74: also while an update waits for the restart: a newer one replaces it.
	const canCheck = $derived(!!status?.canVerify && !working);

	function slotState(slot: Slot): string {
		if (slot.running) return t(slot.next ? 'update.slot.running' : 'update.slot.runningNotNext');
		if (!slot.complete) return t('update.slot.empty');
		if (slot.triesLeft === 0) return t('update.slot.bad');
		return t(slot.next ? 'update.slot.next' : 'update.slot.fallback');
	}

	// PLAN 78-3: the release notes as data (lib/update/changelog.ts), shown as
	// text by Changelog.svelte. Parsed again only when the text changes, not
	// with every poll.
	const newsText = $derived(status?.available?.changelog ?? '');
	const news = $derived(parseChangelog(newsText));
	// "Was war neu?": the notes of the running version, if the list of
	// releases has it (GET /update/versions). Without them the row of the
	// installed version simply has nothing to open.
	const ranText = $derived(versions?.find((v) => v.running)?.changelog ?? '');
	const ran = $derived(parseChangelog(ranText));
	const hasNotes = (log: Notes) => log.sections.length > 0 || !!log.lead;
	// "4 Neuerungen · 3 zur Sicherheit · 12 Korrekturen": only what the notes have.
	function countText(log: Notes): string {
		const sum = counts(log);
		const parts: string[] = [];
		if (sum.new)
			parts.push(
				sum.new === 1 ? t('update.news.new.one') : t('update.news.new.many', { count: sum.new }),
			);
		if (sum.security) parts.push(t('update.news.security', { count: sum.security }));
		if (sum.fixed)
			parts.push(
				sum.fixed === 1
					? t('update.news.fixed.one')
					: t('update.news.fixed.many', { count: sum.fixed }),
			);
		return parts.join(' · ');
	}

	// The steps of an installation follow the phases titand reports
	// (backend/internal/update/update_install.go, app.go):
	//   system: download → switch → done, then `pending` waits for the restart,
	//           after it the new system runs `unconfirmed` (trial starts);
	//   app:    download → restart, then `app.trial`.
	// "Prüfen" has no phase of its own: titand checks size and SHA-256 of
	// every file as it has loaded it, inside `download`. The step therefore
	// never shows as the current one; it counts as done once `download` is over.
	// Which kind runs: the page knows it from its own click, else from the
	// download (an app update loads the one file "titand"). A restart of the
	// web interface whose reason the page did not see (going back to the app
	// before, a reload in the middle) shows no steps rather than guessed ones.
	let flow = $state<'' | 'app' | 'system'>('');
	// The version being installed, for the title of the row.
	let target = $state('');
	$effect(() => {
		const progress = status?.progress;
		if (!progress) return;
		if (progress.phase === 'download') flow = progress.file === 'titand' ? 'app' : 'system';
		else if (progress.phase === 'switch') flow = 'system';
		// install-version makes the chosen release the available one too.
		if ((progress.phase === 'download' || progress.phase === 'switch') && status?.available)
			target = status.available.version;
	});
	// Nothing runs and nothing was just asked for: the next installation says
	// its kind anew.
	$effect(() => {
		if (!working && busy === '') flow = '';
	});
	const stage = $derived.by((): { kind: '' | 'app' | 'system'; at: number } | null => {
		if (!status) return null;
		if (working) {
			if (!flow) return { kind: '', at: 0 };
			return { kind: flow, at: status.progress.phase === 'download' ? 1 : 3 };
		}
		if (status.pending?.kind === 'update') return { kind: 'system', at: 4 };
		if (status.app.trial) return { kind: 'app', at: 4 };
		if (status.unconfirmed && !status.pending) return { kind: 'system', at: 5 };
		return null;
	});
	const steps = $derived.by(() => {
		if (!stage || !stage.kind) return [];
		const names =
			stage.kind === 'app'
				? (['load', 'check', 'service', 'appTrial'] as const)
				: (['load', 'check', 'switch', 'restart', 'trialStart'] as const);
		return names.map((name, i) => ({
			label: t(`update.steps.${name}`),
			state:
				i + 1 < stage.at
					? ('done' as const)
					: i + 1 === stage.at
						? ('now' as const)
						: ('next' as const),
		}));
	});
	const stepsTitle = $derived.by(() => {
		if (!status || !stage) return '';
		if (working) {
			if (!stage.kind) return t('update.steps.restarting');
			return target
				? t('update.steps.title', { version: osVersion(target) })
				: t('update.steps.titlePlain');
		}
		if (status.pending)
			return t('update.steps.waiting', { version: osVersion(status.pending.version) });
		return t('update.steps.trial', { version: osVersion(status.version) });
	});
	// What comes back if the new system does not start: the running one while
	// the update waits, the other slot once the new one runs on trial.
	const fallbackVersion = $derived(
		!status || stage?.kind !== 'system' || status.progress.fallbackRemoved
			? ''
			: stage.at === 5
				? other?.complete
					? other.version
					: ''
				: status.version,
	);
</script>

{#snippet percent()}
	{#if status}<span class="mono"
			>{status.progress.percent.toFixed(0)} % · {(
				status.progress.speedBytesPerSecond / 1e6
			).toFixed(1)} MB/s</span
		>{/if}
{/snippet}
{#snippet devChip()}
	<span class="chip warn">{t('update.devBuild')}</span>
{/snippet}
{#snippet ranNotes()}
	<div class="srow-block"><Changelog log={ran} version={status?.version ?? ''} /></div>
{/snippet}
<PageHead title={t('update.title')} />

{#if error}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>{/if}

{#if status && draft}
	<!-- What happened or is in the way, above everything else. -->
	{#if restartedTo}<p class="notice-ok" role="status">
			{t('update.app.restarted', { version: restartedTo })}
			<!-- PLAN 67: the whole UI loads the new version (desktop and every
			     window), so no part keeps running the old one. Never by itself:
			     a window may hold unsaved work. -->
			<button
				type="button"
				class="btn small"
				onclick={() => (window.top ?? window).location.reload()}>{t('update.app.reloadUi')}</button
			>
		</p>{/if}
	{#if !status.canVerify}<p class="notice-warn">{t('update.noKey')}</p>{/if}
	{#if status.pending?.kind === 'rollback'}
		<div class="notice-ok" role="status">
			{t('update.pendingRollback', { version: status.pending.version })}
			<div class="row">
				<button type="button" class="btn btn-primary" onclick={() => (restarting = true)}
					>{t('update.restartNow')}</button
				>
				<button type="button" class="btn" disabled={busy !== ''} onclick={() => act('cancel')}
					>{t('update.cancel')}</button
				>
			</div>
		</div>
	{/if}
	{#if status.failedVersion && status.available && status.available.version.replace(/^v/, '') === status.failedVersion}
		<p class="notice-warn" role="status">
			{t('update.failedBefore', { version: status.available.version })}
		</p>
	{/if}
	{#if status.canVerify}
		{#if working}
			{#if status.progress.fallbackRemoved}<p class="notice-warn" role="status">
					{t('update.fallbackRemoved')}
				</p>{/if}
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
		{#if !working && !status.available && status.kernelMissing}<p class="notice-warn" role="status">
				{t('update.kernel.missing', { version: status.kernelMissing })}
			</p>{/if}
		{#if !working && status.checkError}
			<div class="notice-warn" role="status">
				{t('update.checkFailed')}
				<pre>{status.checkError}</pre>
			</div>
		{/if}
	{/if}

	<!-- PLAN 78-3, draft 78-updates: the update itself first – the card of
	     the new version, what is new in it, and where an installation stands. -->
	{#if (status.available && !working) || (status.available && hasNotes(news)) || stage}
		<SettingGroup>
			{#if status.available && !working}
				{@const kernelName = chosen?.kernel ?? status.available.kernel}
				<div class="hero" data-testid="update-hero">
					<span class="hero-ico" aria-hidden="true"><Icon name="download" size={26} /></span>
					<div class="hero-text">
						<h2 class="hero-title">
							{#if status.kernelSwitch}TitanOS {osVersion(status.available.version)}{' · '}{t(
									status.settings.kernel === 'new'
										? 'update.kernel.withNew'
										: 'update.kernel.withStable',
								)}{:else}{t('update.hero.title', {
									version: osVersion(status.available.version),
								})}{/if}
						</h2>
						<!-- True at this point: the check verified the signature of the
						     release's manifest (update_check.go, latest). Size and
						     SHA-256 of the files are checked while installing. -->
						<p class="muted">
							{t('update.hero.released', {
								date: formatDate(new Date(status.available.released)),
							})}
						</p>
						<div class="facts">
							{#if !status.kernelSwitch}
								<span class="jump">
									<span class="sr-only"
										>{t('update.hero.jump', {
											from: osVersion(status.version),
											to: osVersion(status.available.version),
										})}</span
									>
									<b aria-hidden="true">{osVersion(status.version)}</b>
									<span class="arrow" aria-hidden="true"><Icon name="arrowRight" size={15} /></span>
									<b class="to" aria-hidden="true">{osVersion(status.available.version)}</b>
								</span>
							{/if}
							<span class="chip {appUpdate ? 'ok' : 'info'}" data-testid="update-kind"
								>{t(
									status.kernelSwitch
										? 'update.kind.kernel'
										: appUpdate
											? 'update.kind.app'
											: 'update.kind.system',
								)}</span
							>
							<span class="chip" data-testid="update-size">{formatBytes(size)}</span>
							{#if kernelName}<span class="chip"
									>{t('update.hero.kernel', { kernel: kernelName })}</span
								>{/if}
							{#if status.available.channel === 'beta'}<span class="chip warn"
									>{t('update.channel.beta')}</span
								>{/if}
						</div>
					</div>
					<div class="hero-act">
						<button
							type="button"
							class="btn btn-primary"
							disabled={busy !== '' || status.unconfirmed || status.app.trial}
							onclick={() => (asking = 'install')}
							>{status.pending
								? t('update.replacePending', {
										version: status.available.version,
										waiting: status.pending.version,
									})
								: t(status.kernelSwitch ? 'update.kernel.switch' : 'update.install')}</button
						>
					</div>
				</div>
			{/if}
			{#if status.available && hasNotes(news)}
				<SettingRow
					id="news"
					icon="sparkle"
					title={t('update.news.title', { version: osVersion(status.available.version) })}
					desc={countText(news) || undefined}
				>
					<div class="srow-block">
						<Changelog log={news} version={status.available.version} />
					</div>
				</SettingRow>
			{/if}
			{#if stage}
				<SettingRow
					icon="clock"
					title={stepsTitle}
					desc={!working || !stage.kind
						? undefined
						: t(stage.kind === 'app' ? 'update.steps.appRuns' : 'update.steps.reachable')}
					control={status.progress.phase === 'download' ? percent : undefined}
					open
				>
					<div class="srow-block" data-testid="update-steps">
						{#if steps.length}
							<ol class="steps" aria-label={t('update.steps.label')}>
								{#each steps as step, i (i)}
									<li class={step.state} aria-current={step.state === 'now' ? 'step' : undefined}>
										<span class="mark" aria-hidden="true"
											>{#if step.state === 'done'}<Icon name="check" size={16} />{:else}{i +
													1}{/if}</span
										>
										<span class="step-name">{step.label}</span>
										<span class="sr-only">({t(`update.steps.${step.state}`)})</span>
									</li>
								{/each}
							</ol>
						{/if}
						{#if working}
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
								{#if status.progress.phase === 'download'}{t('update.downloading')}
								{:else if status.progress.phase === 'restart' || restartFrom}{t(
										'update.app.restarting',
									)}
								{:else}{t('update.switching')}{/if}
							</p>
						{:else if status.pending}
							<div class="notice-ok" role="status">
								{t('update.pendingUpdate', { version: status.pending.version })}
								<div class="row">
									<button type="button" class="btn btn-primary" onclick={() => (restarting = true)}
										>{t('update.restartNow')}</button
									>
									<button
										type="button"
										class="btn"
										disabled={busy !== ''}
										onclick={() => act('cancel')}>{t('update.cancel')}</button
									>
								</div>
							</div>
						{:else if status.unconfirmed}
							<p class="notice-warn" role="status">{t('update.unconfirmed')}</p>
						{/if}
						{#if status.app.trial}<p class="notice-warn" role="status">
								{t('update.app.trial')}
							</p>{/if}
						{#if stage.kind === 'system' && fallbackVersion}
							<p class="muted">
								{t('update.steps.fallback', { version: osVersion(fallbackVersion) })}
							</p>
						{:else if stage.kind === 'app' && working}
							<p class="muted">{t('update.steps.appFallback')}</p>
						{/if}
					</div>
				</SettingRow>
			{/if}
		</SettingGroup>
	{/if}

	<SettingGroup title={t('update.nas')} id="nas">
		{#if status.canVerify && !working && !status.available && !status.kernelMissing && !status.checkError && (checked || status.lastCheck)}
			<SettingRow
				icon="check"
				title={t('update.upToDate')}
				desc={status.lastCheck
					? t('update.lastCheck', { time: formatDateTime(new Date(status.lastCheck)) })
					: undefined}
			/>
		{/if}
		<SettingRow
			id="installed"
			icon="box"
			title="TitanOS {osVersion(status.version)}"
			badge={status.release ? undefined : devChip}
			summary={hasNotes(ran) ? t('update.news.was') : undefined}
			children={hasNotes(ran) ? ranNotes : undefined}
		>
			{#snippet description()}
				<span class="line"
					>{t(
						status?.variant === 'new' ? 'update.kernel.runningNew' : 'update.kernel.runningStable',
						{ kernel: running?.kernel || '–' },
					)}{#if loaderKey}{' · '}{t(loaderKey as 'update.boot.bios')}{/if}</span
				>
				{#if status?.base && status.base !== status.version}<span class="line"
						>{t('update.app.onBase', { version: osVersion(status.base) })}</span
					>{/if}
			{/snippet}
		</SettingRow>
		{#if status.canRollback && !status.pending && other}
			<SettingRow
				icon="undo"
				title={t('update.back.title', { version: other.version || '?' })}
				desc={t('update.back.desc')}
			>
				{#snippet control()}
					<button
						type="button"
						class="btn"
						disabled={busy !== '' || working}
						aria-label="{t('update.back.do')} – TitanOS {other.version || '?'}"
						onclick={() => (asking = 'rollback')}>{t('update.back.do')}</button
					>
				{/snippet}
			</SettingRow>
		{/if}
		{#if status.app.current || status.app.previous}
			<SettingRow
				id="app"
				icon="sparkle"
				title={t('update.app.title')}
				desc={t('update.app.text', { version: osVersion(status.base) })}
				summary={status.app.current || t('update.app.base', { version: osVersion(status.base) })}
			>
				<SettingSub title={t('update.app.running')}>
					{#snippet control()}<span class="mono value"
							>{status?.app.current ||
								t('update.app.base', { version: osVersion(status?.base) })}</span
						>{/snippet}
				</SettingSub>
				{#if status.app.previous}
					<SettingSub title={t('update.app.previous')}>
						{#snippet control()}<span class="mono value">{status?.app.previous}</span>{/snippet}
					</SettingSub>
				{/if}
			</SettingRow>
			{#if status.app.canRollback && !status.pending}
				<SettingRow
					icon="undo"
					title={t('update.app.backTitle', { target: appTarget })}
					desc={t('update.app.backDesc')}
				>
					{#snippet control()}
						<button
							type="button"
							class="btn"
							disabled={busy !== '' || working}
							aria-label="{t('update.back.do')} – {appTarget}"
							onclick={() => (asking = 'app-rollback')}>{t('update.back.do')}</button
						>
					{/snippet}
				</SettingRow>
			{/if}
		{/if}
		{#if status.slots.length > 0}
			<SettingRow
				id="slots"
				icon="disk"
				title={t('update.slots')}
				desc={t('update.slotsText')}
				summary={status.slots
					.map(
						(slot) =>
							`${slot.name.toUpperCase()}: ${slot.complete || slot.running ? slot.version || '–' : '–'}`,
					)
					.join(' · ')}
			>
				{#each status.slots as slot (slot.name)}
					<SettingSub
						title={t('update.slot', { name: slot.name.toUpperCase() })}
						desc={slot.kernel
							? t(slot.variant === 'new' ? 'update.kernel.slotNew' : 'update.kernel.slotStable', {
									kernel: slot.kernel,
								})
							: undefined}
					>
						{#snippet control()}
							<span class="mono value"
								>{slot.complete || slot.running ? slot.version || '–' : '–'}</span
							>
							<span
								>{slotState(slot)}{#if slot.triesLeft !== null && slot.triesLeft > 0}{' · '}{t(
										'update.slot.tries',
										{ count: slot.triesLeft },
									)}{/if}</span
							>
						{/snippet}
					</SettingSub>
				{/each}
			</SettingRow>
		{/if}
		{#if status.canVerify}
			<SettingRow
				id="choose"
				icon="list"
				title={t('update.choose.title')}
				desc={t('update.choose.text', { floor: floor || '0.46.2' })}
			>
				{#if versionsFailed}
					<div class="srow-block">
						<p class="muted" role="status">{t('update.choose.failed')}</p>
					</div>
				{:else if !versions}
					<div class="srow-block">
						<p class="muted" role="status">{t('update.choose.loading')}</p>
					</div>
				{:else if versions.length === 0}
					<div class="srow-block">
						<p class="muted" role="status">{t('update.choose.none', { floor })}</p>
					</div>
				{:else}
					<SettingSub title={t('update.choose.version')} for="upd-pick">
						{#snippet control()}
							<select id="upd-pick" class="field" bind:value={picked}>
								{#each versions ?? [] as v (v.version)}<option value={v.version}
										>TitanOS {osVersion(v.version)} · {formatDate(
											new Date(v.released),
										)}{#if v.running}{' · '}{t(
												'update.choose.running',
											)}{:else if v.newest}{' · '}{t('update.choose.newest')}{/if}</option
									>{/each}
							</select>
						{/snippet}
					</SettingSub>
					{#if pick}
						<div class="srow-block">
							<p class="muted">
								{t('update.choose.meta', {
									date: formatDate(new Date(pick.released)),
									kernel: pick.kernel,
									size: formatBytes(pick.size),
								})}
							</p>
							{#if leadOf(pick.changelog)}<p>{leadOf(pick.changelog)}</p>{/if}
							<div>
								<button
									type="button"
									class="btn"
									disabled={pick.running ||
										busy !== '' ||
										working ||
										!!status.pending ||
										status.unconfirmed ||
										status.app.trial}
									onclick={() => (asking = 'choose')}
									>{t('update.choose.install', { version: pick.version })}</button
								>
							</div>
						</div>
					{/if}
				{/if}
			</SettingRow>
		{/if}
	</SettingGroup>

	<form onsubmit={save}>
		<SettingGroup title={t('update.auto.group')} id="auto" desc={t('update.singleChannel')}>
			<SettingRow icon="search" title={t('update.check')}>
				{#snippet description()}
					{#if status?.lastCheck}<span class="line" role="status"
							>{t('update.lastCheck', {
								time: formatDateTime(new Date(status.lastCheck)),
							})}</span
						>{/if}
					{#if nextCheck && !manual}<span class="line" role="status"
							>{nextCheck.getTime() <= Date.now()
								? t('update.nextCheckSoon')
								: t('update.nextCheck', { time: formatDateTime(nextCheck) })}</span
						>{/if}
					<span class="line"
						>{t(manual ? 'update.interval.manualHint' : 'update.interval.hint')}</span
					>
				{/snippet}
				{#snippet control()}
					<select
						class="field"
						bind:value={mode}
						onchange={pickMode}
						aria-label={t('update.interval')}
					>
						{#each presets as every (every)}<option value={String(every)}
								>{t(`update.interval.${every}` as 'update.interval.0')}</option
							>{/each}
						<option value="custom">{t('update.interval.custom')}</option>
						<option value="manual">{t('update.interval.manual')}</option>
					</select>
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
					<!-- PLAN 74: also while an update waits for the restart: a newer one replaces it. -->
					<button
						type="button"
						class="btn"
						disabled={!canCheck || busy !== ''}
						onclick={() => act('check')}
						>{t(busy === 'check' ? 'update.checking' : 'update.checkNow')}</button
					>
				{/snippet}
			</SettingRow>
			{#if customInvalid}<p class="notice-error" role="alert">{t('update.interval.range')}</p>{/if}
			<SettingRow
				icon="clock"
				title={t('update.auto.title')}
				for="upd-auto"
				desc={t(
					manual
						? 'update.autoNeedsChecks'
						: draft.autoInstall
							? 'update.autoHint'
							: 'update.manualHint',
				)}
			>
				{#snippet control()}
					{#if draft?.autoInstall && !manual}
						<input
							class="field mono"
							type="time"
							bind:value={draft.installTime}
							aria-label={t('update.autoTime')}
							required
						/>
					{/if}
					{#if draft}<input
							id="upd-auto"
							class="switch"
							type="checkbox"
							role="switch"
							bind:checked={draft.autoInstall}
							disabled={manual}
							aria-label={t('update.auto')}
						/>{/if}
				{/snippet}
			</SettingRow>
			<SettingRow icon="chip" title={t('update.kernel.title')} for="upd-kernel">
				{#snippet description()}
					<span class="line"
						><strong>{t('update.kernel.stable')}:</strong> {t('update.kernel.stableText')}</span
					>
					<span class="line"
						><strong>{t('update.kernel.new')}:</strong> {t('update.kernel.newText')}</span
					>
					<span class="line">{t('update.kernel.hint')}</span>
				{/snippet}
				{#snippet control()}
					{#if draft}<select id="upd-kernel" class="field" bind:value={draft.kernel}>
							<option value="">{t('update.kernel.stable')}</option>
							<option value="new">{t('update.kernel.new')}</option>
						</select>{/if}
				{/snippet}
			</SettingRow>
			<div class="save">
				<button
					type="submit"
					class="btn btn-primary"
					disabled={!changed || customInvalid || busy !== ''}
					>{t(busy === 'save' ? 'ui.saving' : 'notify.save')}</button
				>{#if saved && !changed}<span class="muted" role="status">{t('ui.saved')}</span>{/if}
			</div>
		</SettingGroup>
	</form>

	<!-- The nodes of the cluster keep their own section (cluster work). -->
	{#if !activeNode()}<ClusterUpdates />{/if}
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

{#if asking === 'choose' && pick && status}
	<Dialog
		title={t('update.choose.confirmTitle', { version: pick.version })}
		onclose={() => (asking = '')}
	>
		<p>{t('update.confirmText', { size: formatBytes(pick.size) })}</p>
		{#if olderThan(pick.version, status.version)}<p class="notice-warn">
				{t('update.rollbackWarn')}
			</p>{/if}
		{#if pickOlder && status.settings.autoInstall}<p class="notice-warn">
				{t('update.choose.autoOff')}
			</p>{/if}
		<p class="muted">{t('update.confirmSafe')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (asking = '')}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={installVersion}
				>{t('update.choose.do')}</button
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
	form {
		display: contents;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5) 14px;
	}
	.notice-ok .row {
		margin-top: var(--space-2-5);
	}
	pre {
		margin: 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	:is(.notice-error, .notice-warn) pre {
		margin-top: var(--space-1-5);
	}
	/* The card of the new version: a tint of the accent, the text in the
	   theme's own colours. */
	.hero {
		display: grid;
		grid-template-columns: 56px minmax(0, 1fr) auto;
		align-items: center;
		gap: var(--space-2) var(--space-4);
		padding: var(--space-4);
		min-width: 0;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		border-radius: calc(8px * var(--radius-scale, 1));
		background: color-mix(in srgb, var(--accent) 9%, transparent);
	}
	.hero-ico {
		display: grid;
		place-items: center;
		width: 56px;
		height: 56px;
		border-radius: calc(14px * var(--radius-scale, 1));
		background: color-mix(in srgb, var(--accent) 22%, transparent);
		color: var(--text);
	}
	.hero-text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.hero-title {
		margin: 0;
		font-size: 1.25rem;
		font-weight: 600;
		line-height: 1.3;
		letter-spacing: 0;
		text-transform: none;
		color: var(--text);
		overflow-wrap: anywhere;
	}
	.hero p {
		margin: 0;
	}
	.facts {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5) var(--space-2);
		margin-top: var(--space-2);
	}
	.jump {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		font-variant-numeric: tabular-nums;
	}
	.jump b {
		padding: 2px 9px;
		border-radius: var(--radius-small);
		background: var(--chip);
		border: 1px solid var(--border);
		font-weight: 600;
	}
	.jump b.to {
		background: var(--accent);
		border-color: transparent;
		color: var(--on-accent);
	}
	.arrow {
		display: grid;
		color: var(--muted);
	}
	/* The steps of the installation: the done ones with a check mark, the
	   current one framed and bold – never told apart by colour alone. */
	.steps {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(7.5rem, 1fr));
		gap: var(--space-1-5);
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.steps li {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
		padding: var(--space-2-5) var(--space-2);
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
		color: var(--muted);
		font-size: var(--text-sm);
		text-align: center;
		overflow-wrap: anywhere;
	}
	.steps .mark {
		display: grid;
		place-items: center;
		min-height: 1.5rem;
		font-size: 1rem;
		font-weight: 700;
	}
	.steps li.done {
		border-color: var(--ok-border);
		color: var(--text);
	}
	.steps li.done .mark {
		color: var(--ok-text);
	}
	.steps li.now {
		border: 2px solid var(--accent);
		padding: calc(var(--space-2-5) - 1px) calc(var(--space-2) - 1px);
		background: color-mix(in srgb, var(--accent) 10%, transparent);
		color: var(--text);
		font-weight: 600;
	}
	.line {
		display: block;
	}
	.value {
		color: var(--text);
	}
	.amount {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		color: var(--muted);
	}
	.save {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5);
		margin-top: var(--space-2);
	}
	@media (max-width: 639px) {
		.hero {
			grid-template-columns: 44px minmax(0, 1fr);
			padding: var(--space-3);
		}
		.hero-ico {
			width: 44px;
			height: 44px;
		}
		.hero-act {
			grid-column: 1 / -1;
		}
		.hero-act .btn {
			width: 100%;
		}
		/* Two columns of steps, as in the draft. */
		.steps {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
	}
</style>
