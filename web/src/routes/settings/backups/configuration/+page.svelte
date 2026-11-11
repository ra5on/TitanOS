<script lang="ts">
	import { apiPath } from '#lib/cluster-node.ts';
	import { api, ApiError, type StorageStatus } from '#lib/api.ts';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { tick } from 'svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import KeepChoice, { keepChoicePayload } from '#lib/components/KeepChoice.svelte';
	import type { Keep } from '#lib/components/KeepVersions.svelte';
	import { revealTarget } from '#lib/reveal-target.ts';

	type Settings = {
		enabled: boolean;
		volume: string;
		keep: number;
		/** By day, week and month (0.41.0); null keeps the newest keep. */
		versions?: Keep | null;
		schedule: string;
		time: string;
		weekday: number;
		encrypt?: boolean;
		/** "change": after every change; "daily": at most once a day (0.77). */
		pace?: string;
	};
	type Backup = {
		name: string;
		volume: string;
		time: string;
		sizeBytes: number;
		hostname: string;
		version: string;
		encrypted?: boolean;
	};
	type State = {
		settings: Settings;
		target: string;
		lastCheck: string | null;
		lastError: string;
		backups: Backup[];
		next: string | null;
	};
	type Answer = { backup: State; volumes: string[] | null; passwordSet?: boolean };

	let status = $state<State | null>(null);
	let volumes = $state<string[]>([]);
	let draft = $state<Settings | null>(null);
	let saved = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state<'save' | 'run' | null>(null);
	let ran = $state(false);
	// The backup that waits for the password to be entered again.
	let wanted = $state<Backup | null>(null);
	// Encryption of the backups (decided 31.10.2026).
	let passwordSet = $state(false);
	let password = $state('');
	let repeat = $state('');
	const passwordProblem = $derived(
		password === '' && repeat === ''
			? ''
			: password.length < 8
				? t('stick.passwordShort')
				: password !== repeat
					? t('stick.passwordMismatch')
					: '',
	);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail: code === 'backup_failed' && err instanceof ApiError ? err.message : '',
		};
	}

	function take(res: Answer, keepDraft = false) {
		status = res.backup;
		volumes = res.volumes ?? [];
		passwordSet = !!res.passwordSet;
		if (!draft || !keepDraft) draft = { ...res.backup.settings };
	}

	async function load() {
		error = null;
		try {
			take(await api.get<Answer>('/config-backup'));
		} catch (err) {
			problem(err);
		}
	}
	$effect(() => {
		load();
	});

	// Which volumes are encrypted (plan 6e): the hints below appear only when
	// this is known, never on a guess.
	let encrypted = $state<Map<string, boolean> | null>(null);
	async function loadVolumes() {
		try {
			const storage = await api.get<StorageStatus>('/storage');
			encrypted = new Map(storage.volumes.map((volume) => [volume.name, !!volume.encryption]));
		} catch {
			encrypted = null;
		}
	}
	$effect(() => {
		loadVolumes();
	});
	/** The backups go without password to a volume that is not encrypted. */
	const plainTarget = $derived(
		!!status &&
			!status.settings.encrypt &&
			!!status.target &&
			encrypted?.get(status.target) === false,
	);
	function volumeLabel(volume: string) {
		if (!encrypted?.has(volume)) return volume;
		return `${volume} · ${t(encrypted.get(volume) ? 'stick.volumeEncrypted' : 'stick.volumePlain')}`;
	}
	const plainBackups = $derived(!!status?.backups.some((backup) => !backup.encrypted));
	$effect(() => {
		if (saved) {
			const timer = setTimeout(() => (saved = false), 5000);
			return () => clearTimeout(timer);
		}
	});

	const changed = $derived(
		!!status &&
			!!draft &&
			(JSON.stringify({
				...draft,
				...keepChoicePayload(draft.keep, draft.versions, 1, 100),
				encrypt: !!draft.encrypt,
			}) !==
				JSON.stringify({
					...status.settings,
					...keepChoicePayload(status.settings.keep, status.settings.versions, 1, 100),
					encrypt: !!status.settings.encrypt,
				}) ||
				password !== ''),
	);
	const newest = $derived(status?.backups[0]);

	// PLAN 78-1: the time of the schedule and the password are parts of their
	// rows, and a closed row has no fields the browser could check. Before
	// storing, those rows open, so that a required field is named as before.
	let form = $state<HTMLFormElement>();
	let open = $state({ schedule: false, password: false });
	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		if (draft.schedule) open.schedule = true;
		if (draft.encrypt) open.password = true;
		await tick();
		if (form && !form.reportValidity()) return;
		busy = 'save';
		saved = false;
		error = null;
		try {
			take(
				await api.put<Answer>('/config-backup', {
					...draft,
					...keepChoicePayload(draft.keep, draft.versions, 1, 100),
					password: password || undefined,
				}),
			);
			password = '';
			repeat = '';
			saved = true;
		} catch (err) {
			problem(err);
		}
		busy = null;
	}

	async function run() {
		busy = 'run';
		error = null;
		ran = false;
		try {
			take(await api.post<Answer>('/config-backup/run'), true);
			ran = true;
		} catch (err) {
			problem(err);
		}
		busy = null;
	}

	function fetchBackup(backup: Backup) {
		// The browser saves the file itself; nothing of it passes through the page.
		const link = document.createElement('a');
		link.href = apiPath(
			`/config-backup/download?volume=${encodeURIComponent(backup.volume)}&name=${encodeURIComponent(backup.name)}`,
		);
		link.download = '';
		link.click();
	}

	async function download(backup: Backup) {
		error = null;
		try {
			if ((await api.get<{ confirmed: boolean }>('/session/confirmed')).confirmed)
				fetchBackup(backup);
			else wanted = backup;
		} catch (err) {
			problem(err);
		}
	}

	function confirmed() {
		if (wanted) fetchBackup(wanted);
		wanted = null;
	}

	// PLAN 67-1: Sicherung › Ablage › Stick jetzt sichern in the menu bar of the desktop.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'backup.stickNow': { run: run, enabled: () => busy === null && !!status?.target },
	});
</script>

<PageHead title={t('safeguard.config')}>
	<p class="muted">{t('stick.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}{#if !status}<button
				class="btn"
				onclick={load}>{t('ui.retry')}</button
			>{/if}
	</div>{/if}
{#if !status && !error}<p class="loading" role="status">{t('common.loading')}</p>{/if}

{#if status && draft}
	{@const d = draft}
	<!-- PLAN 78-1: the state as rows of values; the notices stand above them. -->
	<div class="part" id="config-history" use:revealTarget>
		<SettingGroup id="stick-state" title={t('stick.state')}>
			{#snippet tools()}
				{#if ran}<span class="muted" role="status">{t('stick.ran')}</span>{/if}
				<button type="button" class="btn" disabled={busy !== null || !status?.target} onclick={run}
					>{t(busy === 'run' ? 'stick.running' : 'stick.run')}</button
				>
			{/snippet}
			{#if !status.settings.enabled}
				<p class="notice-warn">{t('stick.off')}</p>
			{:else if !status.target}
				<p class="notice-warn">
					{t(status.settings.volume ? 'stick.volumeMissing' : 'stick.noVolume', {
						volume: status.settings.volume,
					})}
				</p>
			{:else if status.lastError}
				<div class="notice-error" role="alert">
					{t('stick.failed')}
					<pre>{status.lastError}</pre>
				</div>
			{:else if newest}
				<p class="notice-ok" role="status">
					{t('stick.ok', { time: formatDateTime(new Date(newest.time)), volume: newest.volume })}
				</p>
			{:else}
				<p class="muted">{t('stick.none')}</p>
			{/if}
			{#if plainTarget}<p class="notice-warn" role="status">
					{t('stick.plainVolume', { volume: status.target })}
				</p>{/if}
			<SettingRow icon="folder" title={t('stick.target')}>
				{#snippet control()}<span class="mono value"
						>{status?.target ? `/mnt/${status.target}/.titanos/backups` : '–'}</span
					>{/snippet}
			</SettingRow>
			<SettingRow icon="check" title={t('stick.lastCheck')}>
				{#snippet control()}<span class="value"
						>{status?.lastCheck ? formatDateTime(new Date(status.lastCheck)) : '–'}</span
					>{/snippet}
			</SettingRow>
			{#if status.next}
				<SettingRow icon="clock" title={t('stick.next')}>
					{#snippet control()}<span class="value"
							>{status?.next ? formatDateTime(new Date(status.next)) : ''}</span
						>{/snippet}
				</SettingRow>
			{/if}
			<SettingRow icon="archive" title={t('stick.count')}>
				{#snippet control()}<span class="value">{status?.backups.length}</span>{/snippet}
			</SettingRow>
		</SettingGroup>
	</div>

	<!-- The settings are rows of one form with one Save, as before. -->
	<form id="config-schedule" use:revealTarget onsubmit={save} bind:this={form}>
		{#snippet scheduleParts()}
			<SettingSub title={t('stick.schedule.time')} for="stick-time">
				{#snippet control()}
					<input id="stick-time" class="field" type="time" bind:value={d.time} required />
				{/snippet}
			</SettingSub>
			{#if d.schedule === 'weekly'}
				<SettingSub title={t('stick.schedule.day')} for="stick-weekday">
					{#snippet control()}
						<select id="stick-weekday" class="field" bind:value={d.weekday}
							>{#each [1, 2, 3, 4, 5, 6, 0] as const as day}<option value={day}
									>{t(`backup.weekday.${day}`)}</option
								>{/each}</select
						>
					{/snippet}
				</SettingSub>
			{/if}
		{/snippet}
		<!-- A stored password never comes back: that one is stored stands below
		     the field's name, not as a placeholder. -->
		{#snippet passwordParts()}
			<SettingSub
				title={t(passwordSet ? 'stick.passwordNew' : 'stick.password')}
				desc={passwordSet ? t('notify.secretStored') : undefined}
				for="stick-password"
			>
				{#snippet control()}
					<input
						id="stick-password"
						class="field"
						type="password"
						autocomplete="new-password"
						bind:value={password}
						required={!passwordSet}
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('stick.passwordRepeat')} for="stick-password-repeat">
				{#snippet control()}
					<input
						id="stick-password-repeat"
						class="field"
						type="password"
						autocomplete="new-password"
						bind:value={repeat}
						required={!passwordSet || password !== ''}
					/>
				{/snippet}
			</SettingSub>
			{#if passwordProblem}<div class="srow-block">
					<p class="notice-warn" role="alert">{passwordProblem}</p>
				</div>{/if}
		{/snippet}
		<SettingGroup id="stick-settings" title={t('stick.settings')}>
			<SettingRow
				icon="power"
				title={t('stick.enabled')}
				desc={t('stick.enabledHint')}
				for="stick-enabled"
			>
				{#snippet control()}
					<input
						id="stick-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.enabled}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="clock"
				title={t('stick.schedule')}
				desc={t('stick.schedule.hint')}
				for="stick-schedule"
				bind:open={open.schedule}
				children={d.schedule ? scheduleParts : undefined}
			>
				{#snippet control()}
					<select
						id="stick-schedule"
						class="field"
						bind:value={d.schedule}
						onchange={() => {
							d.time ||= '03:00';
							if (d.schedule) open.schedule = true;
						}}
					>
						<option value="">{t('stick.schedule.off')}</option><option value="daily"
							>{t('stick.schedule.daily')}</option
						><option value="weekly">{t('stick.schedule.weekly')}</option></select
					>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="refresh"
				title={t('stick.pace')}
				desc={t('stick.pace.hint')}
				for="stick-pace"
			>
				{#snippet control()}
					<select id="stick-pace" class="field" bind:value={d.pace}>
						<option value="daily">{t('sysdata.config.pace.daily')}</option>
						<option value="change">{t('sysdata.config.pace.change')}</option>
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow icon="disk" title={t('stick.volume')} for="stick-volume">
				{#snippet control()}
					<select id="stick-volume" class="field" bind:value={d.volume}>
						<option value="">{t('stick.volumeAuto')}</option>
						{#each volumes as volume (volume)}<option value={volume}>{volumeLabel(volume)}</option
							>{/each}
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow icon="archive" title={t('stick.keep')}>
				{#snippet control()}
					<div class="keep">
						<KeepChoice
							bind:count={d.keep}
							bind:versions={d.versions}
							min={1}
							max={100}
							unit="backups"
						/>
					</div>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="lock"
				title={t('stick.encrypt')}
				desc={t('stick.encryptHint')}
				for="stick-encrypt"
				bind:open={open.password}
				children={d.encrypt ? passwordParts : undefined}
			>
				{#snippet control()}
					<input
						id="stick-encrypt"
						class="switch"
						type="checkbox"
						role="switch"
						checked={!!d.encrypt}
						onchange={(e) => {
							d.encrypt = e.currentTarget.checked;
							if (d.encrypt) open.password = true;
						}}
					/>
				{/snippet}
			</SettingRow>
			<div class="row-actions">
				<button
					type="submit"
					class="btn btn-primary"
					disabled={!changed ||
						busy !== null ||
						!!passwordProblem ||
						(!!d.encrypt && !passwordSet && !password)}
					>{t(busy === 'save' ? 'ui.saving' : 'notify.save')}</button
				>{#if saved && !changed}<span class="muted" role="status">{t('ui.saved')}</span>{/if}
			</div>
		</SettingGroup>
	</form>

	<!-- The copies are a table: it stays one, under a plain heading. -->
	<div class="part" id="config-downloads" use:revealTarget>
		<SettingGroup id="stick-list" title={t('stick.backups')}>
			{#if status.backups.length === 0}
				<p class="muted">{t('stick.none')}</p>
			{:else}
				<p class="muted">{t('stick.downloadHint')}</p>
				{#if plainBackups}<p class="notice-warn" role="status">{t('stick.downloadPlain')}</p>
				{:else}<p class="muted">{t('stick.downloadEncrypted')}</p>{/if}
				<div class="scroll">
					<table>
						<thead
							><tr
								><th>{t('stick.when')}</th><th>{t('stick.volume')}</th><th>{t('stick.version')}</th
								><th class="num">{t('stick.size')}</th><th></th></tr
							></thead
						>
						<tbody>
							{#each status.backups as backup (backup.volume + backup.name)}
								<tr>
									<td>{formatDateTime(new Date(backup.time))}</td>
									<td class="mono">{backup.volume}</td>
									<td class="mono"
										>{backup.version}{#if backup.encrypted}
											<span class="chip ok" title={t('stick.encryptedTitle')}>🔒</span>{/if}</td
									>
									<td class="num mono">{formatBytes(backup.sizeBytes)}</td>
									<td class="num"
										><button type="button" class="btn small" onclick={() => download(backup)}
											>{t('stick.download')}</button
										></td
									>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</SettingGroup>
	</div>
{/if}

{#if wanted}
	<ConfirmDialog
		reason={t('stick.confirmReason')}
		onconfirmed={confirmed}
		onclose={() => (wanted = null)}
	/>
{/if}

<style>
	.part,
	form {
		min-width: 0;
	}
	.part,
	.scroll {
		font-size: 0.875rem;
	}
	/* A notice above the rows keeps its distance from them. */
	.part :global(.sgroup > :is(p, .notice-error)) {
		margin-bottom: var(--space-2);
	}
	.value {
		color: var(--text);
		overflow-wrap: anywhere;
	}
	/* "Die neuesten N Sicherungen" and its note, at the right of the row. */
	.keep {
		text-align: left;
	}
	form .row-actions {
		margin-top: var(--space-2);
	}
	.scroll {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
	}
	th,
	td {
		padding: var(--space-2) var(--space-3) var(--space-2) 0;
		text-align: left;
		border-bottom: 1px solid var(--panel-border);
		white-space: nowrap;
	}
	th {
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--muted);
	}
	.num {
		text-align: right;
	}
	.small {
		height: 2rem;
		padding: 0 var(--space-3);
		font-size: 0.8125rem;
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
